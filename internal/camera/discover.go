package camera

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"fragata/internal/config"
	"fragata/internal/model"
	"fragata/internal/onvif"
	fragrtsp "fragata/internal/rtsp"
)

type AddRequest struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	RTSPURL     string `json:"rtsp_url"`
	Enabled     *bool  `json:"enabled,omitempty"`
	Record      *bool  `json:"record,omitempty"`
	Upload      *bool  `json:"upload,omitempty"`
	InsecureTLS bool   `json:"insecure_tls,omitempty"`
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
		return DetectionResult{Camera: base, Method: "rtsp-manual"}, nil
	}

	onvifClient := onvif.NewClient(cfg.ProbeTimeout, base.Username, base.Password, request.InsecureTLS)
	inspection, onvifErr := onvifClient.Inspect(ctx, base.Host)
	if onvifErr == nil {
		profiles := append([]onvif.Profile(nil), inspection.Profiles...)
		sort.SliceStable(profiles, func(i, j int) bool {
			iH264 := strings.EqualFold(profiles[i].Encoding, "H264")
			jH264 := strings.EqualFold(profiles[j].Encoding, "H264")
			if iH264 != jH264 {
				return iH264
			}
			return profiles[i].Width*profiles[i].Height > profiles[j].Width*profiles[j].Height
		})
		var last error
		for _, profile := range profiles {
			streamURL := inspection.StreamURIs[profile.Token]
			if streamURL == "" {
				continue
			}
			streamURL = fragrtsp.NormalizeHost(streamURL, base.Host)
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
			base.RTSPURL = probe.URL
			base.Codec = probe.Codec
			base.Width = profile.Width
			base.Height = profile.Height
			base.ProfileToken = profile.Token
			base.Manufacturer = inspection.Information.Manufacturer
			base.Model = inspection.Information.Model
			base.SerialNumber = inspection.Information.SerialNumber
			base.FirmwareVersion = inspection.Information.FirmwareVersion
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
	probe, report, searchErr := fragrtsp.Search(ctx, base.Host, base.Username, base.Password, fragrtsp.SearchOptions{
		Ports:          ports,
		ConnectTimeout: cfg.RTSPConnectTimeout,
		ProbeTimeout:   cfg.RTSPCandidateTimeout,
		MaxCandidates:  cfg.RTSPMaxCandidates,
		DictionaryPath: cfg.RTSPDictionaryPath,
		Parallelism:    4,
	})
	if searchErr == nil {
		base.RTSPURL = probe.URL
		base.Codec = probe.Codec
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
	withAuth, err := fragrtsp.WithCredentials(normalized, base.Username, base.Password)
	if err != nil {
		return ProbeResponse{}, err
	}
	probe, err := fragrtsp.Probe(ctx, withAuth, cfg.ProbeTimeout)
	if err != nil {
		return ProbeResponse{}, fmt.Errorf("no se pudo abrir la URL RTSP: %w", err)
	}
	u, _ := url.Parse(probe.URL)
	port := 554
	if value, convErr := strconv.Atoi(u.Port()); convErr == nil && value > 0 {
		port = value
	}
	return ProbeResponse{Host: base.Host, RTSPURL: probe.URL, Codec: probe.Codec, Port: port}, nil
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
		Name:     strings.TrimSpace(request.Name),
		Host:     host,
		Username: username,
		Password: password,
		Enabled:  boolValue(request.Enabled, true),
		Record:   boolValue(request.Record, true),
		Upload:   boolValue(request.Upload, cfg.SFTP.Enabled),
	}
	if base.Name == "" {
		base.Name = "Cámara " + host
	}
	return base, rawURL, requestedPort, nil
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
