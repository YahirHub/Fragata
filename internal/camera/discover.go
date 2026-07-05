package camera

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"fragata/internal/config"
	"fragata/internal/model"
	"fragata/internal/onvif"
	fragrtsp "fragata/internal/rtsp"
)

type AddRequest struct {
	Name                   string `json:"name"`
	Host                   string `json:"host"`
	Username               string `json:"username"`
	Password               string `json:"password"`
	RTSPURL                string `json:"rtsp_url"`
	FolderName             string `json:"folder_name,omitempty"`
	Enabled                *bool  `json:"enabled,omitempty"`
	Record                 *bool  `json:"record,omitempty"`
	SegmentDurationSeconds int64  `json:"segment_duration_seconds,omitempty"`
	Upload                 *bool  `json:"upload,omitempty"`
	InsecureTLS            bool   `json:"insecure_tls,omitempty"`
}

type ProbeRequest struct {
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
	RTSPURL  string `json:"rtsp_url"`
}

type ProbeResponse struct {
	Host    string `json:"host"`
	RTSPURL string `json:"rtsp_url"`
	Codec   string `json:"codec"`
	Port    int    `json:"port"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
}

type DetectionResult struct {
	Camera      model.Camera           `json:"camera"`
	Method      string                 `json:"method"`
	Diagnostics *fragrtsp.SearchReport `json:"diagnostics,omitempty"`
}

func Detect(ctx context.Context, cfg config.Config, request AddRequest) (DetectionResult, error) {
	base, rawURL, requestedPort, err := prepareCamera(cfg, request)
	if err != nil {
		return DetectionResult{}, err
	}

	if rawURL != "" {
		probe, err := probeManual(ctx, cfg, base, rawURL)
		if err != nil {
			return DetectionResult{}, err
		}
		base.RTSPURL = probe.RTSPURL
		base.Codec = probe.Codec
		base.Width = probe.Width
		base.Height = probe.Height
		setDirectLiveStream(&base)
		return DetectionResult{Camera: base, Method: "rtsp-manual"}, nil
	}

	onvifClient := onvif.NewClient(cfg.ProbeTimeout, base.Username, base.Password, request.InsecureTLS)
	inspection, onvifErr := onvifClient.Inspect(ctx, base.Host)
	if onvifErr == nil {
		type detectedProfile struct {
			profile onvif.Profile
			probe   fragrtsp.ProbeResult
			width   int
			height  int
			order   int
		}
		var primary *detectedProfile
		var preview *detectedProfile
		var last error
		seenURLs := make(map[string]struct{})
		for order, profile := range inspection.Profiles {
			streamURL := inspection.StreamURIs[profile.Token]
			if streamURL == "" {
				continue
			}
			streamURL = fragrtsp.NormalizeHost(streamURL, base.Host)
			if _, exists := seenURLs[streamURL]; exists {
				continue
			}
			seenURLs[streamURL] = struct{}{}
			withAuth, err := fragrtsp.WithCredentials(streamURL, base.Username, base.Password)
			if err != nil {
				last = err
				continue
			}
			probe, err := fragrtsp.Probe(ctx, withAuth, cfg.ProbeTimeout)
			if err != nil {
				last = err
				continue
			}
			width, height := preferredDimensions(profile.Width, profile.Height, probe.Width, probe.Height)
			candidate := &detectedProfile{profile: profile, probe: probe, width: width, height: height, order: order}
			if primary == nil || betterVideo(candidate.width, candidate.height, candidate.probe.Codec, candidate.order, primary.width, primary.height, primary.probe.Codec, primary.order) {
				primary = candidate
			}
			if strings.EqualFold(probe.Codec, "H264") && (preview == nil || betterVideo(candidate.width, candidate.height, candidate.probe.Codec, candidate.order, preview.width, preview.height, preview.probe.Codec, preview.order)) {
				preview = candidate
			}
		}
		if primary != nil {
			base.RTSPURL = primary.probe.URL
			base.Codec = primary.probe.Codec
			base.Width = primary.width
			base.Height = primary.height
			base.ProfileToken = primary.profile.Token
			base.Manufacturer = inspection.Information.Manufacturer
			base.Model = inspection.Information.Model
			base.SerialNumber = inspection.Information.SerialNumber
			base.FirmwareVersion = inspection.Information.FirmwareVersion
			if strings.EqualFold(primary.probe.Codec, "H264") {
				setDirectLiveStream(&base)
			} else if preview != nil {
				base.LiveRTSPURL = preview.probe.URL
				base.LiveCodec = preview.probe.Codec
				base.LiveWidth = preview.width
				base.LiveHeight = preview.height
				base.LiveProfileToken = preview.profile.Token
			}
			return DetectionResult{Camera: base, Method: "onvif"}, nil
		}
		if last != nil {
			onvifErr = last
		} else {
			onvifErr = errors.New("ONVIF no entregó perfiles de video válidos")
		}
	}

	ports := append([]int(nil), cfg.RTSPPorts...)
	if requestedPort > 0 {
		ports = prependUniquePort(ports, requestedPort)
	}
	streams, report, searchErr := fragrtsp.SearchStreams(ctx, base.Host, base.Username, base.Password, fragrtsp.SearchOptions{
		Ports:           ports,
		ConnectTimeout:  cfg.RTSPConnectTimeout,
		ProbeTimeout:    cfg.RTSPCandidateTimeout,
		MaxCandidates:   cfg.RTSPMaxCandidates,
		DictionaryPath:  cfg.RTSPDictionaryPath,
		Parallelism:     4,
		FindH264Preview: true,
	})
	if searchErr == nil {
		base.RTSPURL = streams.Primary.URL
		base.Codec = streams.Primary.Codec
		base.Width = streams.Primary.Width
		base.Height = streams.Primary.Height
		if strings.EqualFold(streams.Primary.Codec, "H264") {
			setDirectLiveStream(&base)
		} else if streams.Preview != nil {
			base.LiveRTSPURL = streams.Preview.URL
			base.LiveCodec = streams.Preview.Codec
			base.LiveWidth = streams.Preview.Width
			base.LiveHeight = streams.Preview.Height
		}
		return DetectionResult{Camera: base, Method: "rtsp-dictionary", Diagnostics: &report}, nil
	}
	if onvifErr != nil {
		return DetectionResult{}, fmt.Errorf("detección automática fallida: %w; ONVIF: %v", searchErr, onvifErr)
	}
	return DetectionResult{}, searchErr
}

func ProbeManual(ctx context.Context, cfg config.Config, request ProbeRequest) (ProbeResponse, error) {
	base, rawURL, _, err := prepareCamera(cfg, AddRequest{
		Host: request.Host, Username: request.Username, Password: request.Password, RTSPURL: request.RTSPURL,
	})
	if err != nil {
		return ProbeResponse{}, err
	}
	if rawURL == "" {
		return ProbeResponse{}, errors.New("introduzca una URL RTSP para probar")
	}
	return probeManual(ctx, cfg, base, rawURL)
}

func probeManual(ctx context.Context, cfg config.Config, base model.Camera, rawURL string) (ProbeResponse, error) {
	normalized := fragrtsp.NormalizeHost(rawURL, base.Host)
	parsed, err := url.Parse(normalized)
	if err != nil {
		return ProbeResponse{}, errors.New("URL RTSP inválida")
	}
	port := 554
	if parsed.Scheme == "rtsps" {
		port = 322
	}
	if value, convErr := strconv.Atoi(parsed.Port()); convErr == nil && value > 0 {
		port = value
	}
	checks := fragrtsp.CheckPorts(ctx, base.Host, []int{port}, cfg.RTSPConnectTimeout)
	if len(checks) == 0 || !checks[0].Reachable {
		return ProbeResponse{}, fmt.Errorf("no se pudo abrir la URL RTSP: %s", fragrtsp.PortFailureSummary(base.Host, checks))
	}
	withAuth, err := fragrtsp.WithCredentials(normalized, base.Username, base.Password)
	if err != nil {
		return ProbeResponse{}, err
	}
	probe, err := fragrtsp.Probe(ctx, withAuth, cfg.ProbeTimeout)
	if err != nil {
		return ProbeResponse{}, fmt.Errorf("no se pudo abrir la URL RTSP: %w", err)
	}
	return ProbeResponse{Host: base.Host, RTSPURL: probe.URL, Codec: probe.Codec, Port: port, Width: probe.Width, Height: probe.Height}, nil
}

func prepareCamera(cfg config.Config, request AddRequest) (model.Camera, string, int, error) {
	hostInput := strings.TrimSpace(request.Host)
	rawURL := strings.TrimSpace(request.RTSPURL)
	requestedPort := 0

	if rawURL != "" {
		u, err := url.Parse(rawURL)
		if err != nil || (u.Scheme != "rtsp" && u.Scheme != "rtsps") || u.Hostname() == "" {
			return model.Camera{}, "", 0, errors.New("URL RTSP inválida")
		}
		if hostInput == "" {
			hostInput = u.Hostname()
		}
		if port, convErr := strconv.Atoi(u.Port()); convErr == nil {
			requestedPort = port
		}
	}

	host, hostPort, err := normalizeHostInput(hostInput)
	if err != nil {
		return model.Camera{}, "", 0, err
	}
	if requestedPort == 0 {
		requestedPort = hostPort
	}
	if err := cfg.ValidateCameraIP(host); err != nil {
		return model.Camera{}, "", 0, err
	}

	username := strings.TrimSpace(request.Username)
	password := request.Password
	if rawURL != "" {
		urlUser, urlPassword, hasCredentials := fragrtsp.ExtractCredentials(rawURL)
		if hasCredentials {
			if username == "" {
				username = urlUser
			}
			if password == "" && (strings.TrimSpace(request.Username) == "" || username == urlUser) {
				password = urlPassword
			}
		}
	}

	base := model.Camera{
		Name:                   strings.TrimSpace(request.Name),
		Host:                   host,
		Username:               username,
		Password:               password,
		Enabled:                boolValue(request.Enabled, true),
		Record:                 boolValue(request.Record, false),
		SegmentDurationSeconds: request.SegmentDurationSeconds,
		Upload:                 boolValue(request.Upload, cfg.SFTP.Enabled),
	}
	if base.Name == "" {
		base.Name = "Cámara " + host
	}
	folder, err := normalizeFolderName(request.FolderName, base.Name)
	if err != nil {
		return model.Camera{}, "", 0, err
	}
	base.FolderName = folder
	return base, rawURL, requestedPort, nil
}

func betterVideo(width, height int, codec string, order int, currentWidth, currentHeight int, currentCodec string, currentOrder int) bool {
	pixels := int64(width) * int64(height)
	currentPixels := int64(currentWidth) * int64(currentHeight)
	if pixels != currentPixels {
		return pixels > currentPixels
	}
	if strings.EqualFold(codec, "H265") != strings.EqualFold(currentCodec, "H265") {
		return strings.EqualFold(codec, "H265")
	}
	return order < currentOrder
}

func preferredDimensions(profileWidth, profileHeight, probedWidth, probedHeight int) (int, int) {
	if probedWidth > 0 && probedHeight > 0 {
		return probedWidth, probedHeight
	}
	return profileWidth, profileHeight
}

func setDirectLiveStream(camera *model.Camera) {
	if camera == nil || !strings.EqualFold(camera.Codec, "H264") {
		return
	}
	camera.LiveRTSPURL = camera.RTSPURL
	camera.LiveCodec = camera.Codec
	camera.LiveWidth = camera.Width
	camera.LiveHeight = camera.Height
	camera.LiveProfileToken = camera.ProfileToken
}

func normalizeHostInput(raw string) (string, int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0, errors.New("introduzca la IP de la cámara o una URL RTSP")
	}
	if ip := net.ParseIP(strings.Trim(raw, "[]")); ip != nil {
		return strings.Trim(raw, "[]"), 0, nil
	}
	host, portRaw, err := net.SplitHostPort(raw)
	if err != nil {
		return "", 0, errors.New("la cámara debe indicarse mediante una dirección IP; el puerto es opcional")
	}
	if net.ParseIP(strings.Trim(host, "[]")) == nil {
		return "", 0, errors.New("la cámara debe indicarse mediante una dirección IP")
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, errors.New("puerto de cámara inválido")
	}
	return strings.Trim(host, "[]"), port, nil
}

func prependUniquePort(ports []int, port int) []int {
	out := []int{port}
	for _, existing := range ports {
		if existing != port {
			out = append(out, existing)
		}
	}
	return out
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
