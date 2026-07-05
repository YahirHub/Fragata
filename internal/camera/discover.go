package camera

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

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

type DetectionResult struct {
	Camera model.Camera `json:"camera"`
	Method string       `json:"method"`
}

func Detect(ctx context.Context, cfg config.Config, request AddRequest) (DetectionResult, error) {
	host := strings.TrimSpace(request.Host)
	rawURL := strings.TrimSpace(request.RTSPURL)
	if host == "" && rawURL != "" {
		u, err := url.Parse(rawURL)
		if err != nil {
			return DetectionResult{}, errors.New("URL RTSP inválida")
		}
		host = u.Hostname()
	}
	if err := cfg.ValidateCameraIP(host); err != nil {
		return DetectionResult{}, err
	}
	base := model.Camera{
		Name: strings.TrimSpace(request.Name), Host: host, Username: strings.TrimSpace(request.Username), Password: request.Password,
		Enabled: boolValue(request.Enabled, true), Record: boolValue(request.Record, true), Upload: boolValue(request.Upload, cfg.SFTP.Enabled),
	}
	if base.Name == "" {
		base.Name = "Cámara " + host
	}

	if rawURL != "" {
		normalized := fragrtsp.NormalizeHost(rawURL, host)
		withAuth, err := fragrtsp.WithCredentials(normalized, base.Username, base.Password)
		if err != nil {
			return DetectionResult{}, err
		}
		probe, err := fragrtsp.Probe(ctx, withAuth, cfg.ProbeTimeout)
		if err != nil {
			return DetectionResult{}, fmt.Errorf("no se pudo abrir la URL RTSP: %w", err)
		}
		base.RTSPURL = probe.URL
		base.Codec = probe.Codec
		return DetectionResult{Camera: base, Method: "rtsp-manual"}, nil
	}

	onvifClient := onvif.NewClient(cfg.ProbeTimeout, base.Username, base.Password, request.InsecureTLS)
	inspection, onvifErr := onvifClient.Inspect(ctx, host)
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
			streamURL = fragrtsp.NormalizeHost(streamURL, host)
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

	candidateTimeout := cfg.ProbeTimeout
	if candidateTimeout > 3*time.Second {
		candidateTimeout = 3 * time.Second
	}
	var (
		last         error
		h265Fallback *fragrtsp.ProbeResult
	)
	for _, candidate := range fragrtsp.CommonCandidates(host) {
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.DeadlineExceeded) && h265Fallback != nil {
				base.RTSPURL = h265Fallback.URL
				base.Codec = h265Fallback.Codec
				return DetectionResult{Camera: base, Method: "rtsp-auto"}, nil
			}
			return DetectionResult{}, err
		}
		withAuth, err := fragrtsp.WithCredentials(candidate, base.Username, base.Password)
		if err != nil {
			continue
		}
		probe, err := fragrtsp.Probe(ctx, withAuth, candidateTimeout)
		if err != nil {
			last = err
			continue
		}
		if strings.EqualFold(probe.Codec, "H264") {
			base.RTSPURL = probe.URL
			base.Codec = probe.Codec
			return DetectionResult{Camera: base, Method: "rtsp-auto"}, nil
		}
		if h265Fallback == nil {
			copy := probe
			h265Fallback = &copy
		}
	}
	if h265Fallback != nil {
		base.RTSPURL = h265Fallback.URL
		base.Codec = h265Fallback.Codec
		return DetectionResult{Camera: base, Method: "rtsp-auto"}, nil
	}
	if last == nil {
		last = onvifErr
	}
	return DetectionResult{}, fmt.Errorf("no se encontró un stream ONVIF/RTSP en %s: %w", host, last)
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
