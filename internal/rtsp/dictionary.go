package rtsp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CandidateTemplate describes a known RTSP path. Port 0 means that the path is
// tested on every reachable port configured in Fragata.
type CandidateTemplate struct {
	Name string
	Port int
	Path string
}

// Candidate is an expanded URL that can be probed.
type Candidate struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Port int    `json:"port"`
}

// SearchOptions bounds automatic RTSP discovery. The search is deliberately
// limited to private camera IPs by the caller and never attempts credentials
// other than those supplied by the user.
type SearchOptions struct {
	Ports          []int
	ConnectTimeout time.Duration
	ProbeTimeout   time.Duration
	MaxCandidates  int
	DictionaryPath string
	Parallelism    int
}

// SearchReport contains safe diagnostics without credentials.
type SearchReport struct {
	OpenPorts          []int `json:"open_ports"`
	CandidatesTried    int   `json:"candidates_tried"`
	AuthenticationFail int   `json:"authentication_failures"`
	NotFoundFail       int   `json:"not_found_failures"`
	TimeoutFail        int   `json:"timeout_failures"`
}

var builtinTemplates = []CandidateTemplate{
	// Dahua, Imou and Amcrest.
	{Name: "Dahua/Imou principal", Path: "/cam/realmonitor?channel=1&subtype=0"},
	{Name: "Dahua/Imou secundario", Path: "/cam/realmonitor?channel=1&subtype=1"},
	{Name: "Dahua/Imou terciario", Path: "/cam/realmonitor?channel=1&subtype=2"},
	{Name: "Dahua alternativo principal", Path: "/cam/realmonitor?channel=1&subtype=00"},
	{Name: "Dahua alternativo secundario", Path: "/cam/realmonitor?channel=1&subtype=01"},

	// Hikvision and compatible firmware.
	{Name: "Hikvision principal", Path: "/Streaming/Channels/101"},
	{Name: "Hikvision secundario", Path: "/Streaming/Channels/102"},
	{Name: "Hikvision terciario", Path: "/Streaming/Channels/103"},
	{Name: "Hikvision ISAPI principal", Path: "/ISAPI/Streaming/channels/101"},
	{Name: "Hikvision ISAPI secundario", Path: "/ISAPI/Streaming/channels/102"},
	{Name: "Hikvision antiguo principal", Path: "/h264/ch1/main/av_stream"},
	{Name: "Hikvision antiguo secundario", Path: "/h264/ch1/sub/av_stream"},
	{Name: "Hikvision canal principal", Path: "/ch1/main/av_stream"},
	{Name: "Hikvision canal secundario", Path: "/ch1/sub/av_stream"},

	// Reolink.
	{Name: "Reolink H264 principal", Path: "/h264Preview_01_main"},
	{Name: "Reolink H264 secundario", Path: "/h264Preview_01_sub"},
	{Name: "Reolink H265 principal", Path: "/h265Preview_01_main"},
	{Name: "Reolink H265 secundario", Path: "/h265Preview_01_sub"},
	{Name: "Reolink principal alternativo", Path: "/Preview_01_main"},
	{Name: "Reolink secundario alternativo", Path: "/Preview_01_sub"},

	// Uniview and common OEM firmware.
	{Name: "Uniview principal", Path: "/media/video1"},
	{Name: "Uniview secundario", Path: "/media/video2"},
	{Name: "Uniview unicast principal", Path: "/unicast/c1/s0/live"},
	{Name: "Uniview unicast secundario", Path: "/unicast/c1/s1/live"},

	// Axis and Vivotek.
	{Name: "Axis", Path: "/axis-media/media.amp"},
	{Name: "Axis cámara 1", Path: "/axis-media/media.amp?camera=1"},
	{Name: "Vivotek", Path: "/live.sdp"},
	{Name: "Vivotek stream 1", Path: "/live1.sdp"},
	{Name: "Vivotek stream 2", Path: "/live2.sdp"},

	// Hanwha/Samsung.
	{Name: "Hanwha perfil 1", Path: "/profile1/media.smp"},
	{Name: "Hanwha perfil 2", Path: "/profile2/media.smp"},

	// Foscam, Tapo, Ezviz and generic ONVIF devices.
	{Name: "Video principal", Path: "/videoMain"},
	{Name: "Video secundario", Path: "/videoSub"},
	{Name: "Stream 1", Path: "/stream1"},
	{Name: "Stream 2", Path: "/stream2"},
	{Name: "ONVIF 1", Path: "/onvif1"},
	{Name: "ONVIF 2", Path: "/onvif2"},
	{Name: "Live", Path: "/live"},
	{Name: "Live SDP", Path: "/live.sdp"},
	{Name: "Live canal principal", Path: "/live/ch00_0"},
	{Name: "Live canal secundario", Path: "/live/ch00_1"},
	{Name: "Canal 0 principal", Path: "/ch0_0.h264"},
	{Name: "Canal 0 secundario", Path: "/ch0_1.h264"},
	{Name: "Canal 1 principal", Path: "/11"},
	{Name: "Canal 1 secundario", Path: "/12"},
	{Name: "Media principal", Path: "/media.amp"},
	{Name: "H264 stream", Path: "/h264_stream"},
	{Name: "MPEG4 media", Path: "/mpeg4/media.amp"},
}

// BuiltinTemplates returns a copy so callers cannot mutate the shared list.
func BuiltinTemplates() []CandidateTemplate {
	out := make([]CandidateTemplate, len(builtinTemplates))
	copy(out, builtinTemplates)
	return out
}

// LoadCustomTemplates reads an optional local dictionary. It never downloads
// content. Accepted formats per non-comment line are:
//
//	/path
//	8554|/path
//	Friendly name|8554|/path
//
// Port 0 or * means every configured port.
func LoadCustomTemplates(path string) ([]CandidateTemplate, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("abrir diccionario RTSP %s: %w", path, err)
	}
	defer f.Close()

	var out []CandidateTemplate
	scanner := bufio.NewScanner(f)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tpl, err := parseTemplateLine(line)
		if err != nil {
			return nil, fmt.Errorf("diccionario RTSP línea %d: %w", lineNumber, err)
		}
		out = append(out, tpl)
		if len(out) > 512 {
			return nil, errors.New("el diccionario RTSP no puede superar 512 rutas")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseTemplateLine(line string) (CandidateTemplate, error) {
	parts := strings.Split(line, "|")
	var tpl CandidateTemplate
	switch len(parts) {
	case 1:
		tpl = CandidateTemplate{Name: "Ruta personalizada", Path: parts[0]}
	case 2:
		port, err := parseTemplatePort(parts[0])
		if err != nil {
			return CandidateTemplate{}, err
		}
		tpl = CandidateTemplate{Name: "Ruta personalizada", Port: port, Path: parts[1]}
	case 3:
		port, err := parseTemplatePort(parts[1])
		if err != nil {
			return CandidateTemplate{}, err
		}
		tpl = CandidateTemplate{Name: strings.TrimSpace(parts[0]), Port: port, Path: parts[2]}
	default:
		return CandidateTemplate{}, errors.New("formato esperado: ruta, puerto|ruta o nombre|puerto|ruta")
	}
	tpl.Path = strings.TrimSpace(tpl.Path)
	if tpl.Name == "" {
		tpl.Name = "Ruta personalizada"
	}
	if tpl.Path == "" || !strings.HasPrefix(tpl.Path, "/") {
		return CandidateTemplate{}, errors.New("la ruta debe comenzar con /")
	}
	if strings.ContainsAny(tpl.Path, "\r\n") {
		return CandidateTemplate{}, errors.New("ruta inválida")
	}
	return tpl, nil
}

func parseTemplatePort(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" || raw == "0" {
		return 0, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, errors.New("puerto inválido")
	}
	return port, nil
}

// ReachablePorts checks each configured TCP port once. This prevents repeating
// the same multi-second dial timeout for every path in the dictionary.
func ReachablePorts(ctx context.Context, host string, ports []int, timeout time.Duration) []int {
	ports = normalizePorts(ports)
	if timeout <= 0 {
		timeout = 1200 * time.Millisecond
	}
	type result struct {
		port int
		open bool
	}
	results := make(chan result, len(ports))
	var wg sync.WaitGroup
	for _, port := range ports {
		port := port
		wg.Add(1)
		go func() {
			defer wg.Done()
			dialer := net.Dialer{Timeout: timeout}
			conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port)))
			if err == nil {
				_ = conn.Close()
			}
			results <- result{port: port, open: err == nil}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	openSet := make(map[int]struct{})
	for result := range results {
		if result.open {
			openSet[result.port] = struct{}{}
		}
	}
	open := make([]int, 0, len(openSet))
	for _, port := range ports {
		if _, exists := openSet[port]; exists {
			open = append(open, port)
		}
	}
	return open
}

func ExpandCandidates(host string, ports []int, custom []CandidateTemplate, max int) []Candidate {
	if max <= 0 {
		max = 96
	}
	ports = normalizePorts(ports)
	templates := append(append([]CandidateTemplate(nil), custom...), BuiltinTemplates()...)
	seen := make(map[string]struct{})
	out := make([]Candidate, 0, min(max, len(ports)*len(templates)))
	for _, port := range ports {
		for _, tpl := range templates {
			if tpl.Port != 0 && tpl.Port != port {
				continue
			}
			u := url.URL{Scheme: "rtsp", Host: net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))}
			pathAndQuery := strings.TrimSpace(tpl.Path)
			if parsed, err := url.Parse(pathAndQuery); err == nil {
				u.Path = parsed.Path
				u.RawPath = parsed.RawPath
				u.RawQuery = parsed.RawQuery
			} else {
				continue
			}
			raw := u.String()
			if _, exists := seen[raw]; exists {
				continue
			}
			seen[raw] = struct{}{}
			out = append(out, Candidate{Name: tpl.Name, URL: raw, Port: port})
			if len(out) >= max {
				return out
			}
		}
	}
	return out
}

// Search tries a bounded built-in/local dictionary only on ports that accepted
// a TCP connection. Candidates are tested in small ordered batches to avoid
// overloading low-cost cameras.
func Search(ctx context.Context, host, username, password string, options SearchOptions) (ProbeResult, SearchReport, error) {
	if options.ConnectTimeout <= 0 {
		options.ConnectTimeout = 1200 * time.Millisecond
	}
	if options.ProbeTimeout <= 0 {
		options.ProbeTimeout = 4 * time.Second
	}
	if options.MaxCandidates <= 0 {
		options.MaxCandidates = 96
	}
	if options.Parallelism <= 0 {
		options.Parallelism = 3
	}
	if options.Parallelism > 6 {
		options.Parallelism = 6
	}

	custom, err := LoadCustomTemplates(options.DictionaryPath)
	if err != nil {
		return ProbeResult{}, SearchReport{}, err
	}
	ports := append([]int(nil), options.Ports...)
	for _, template := range custom {
		if template.Port > 0 {
			ports = append(ports, template.Port)
		}
	}
	openPorts := ReachablePorts(ctx, host, ports, options.ConnectTimeout)
	report := SearchReport{OpenPorts: openPorts}
	if len(openPorts) == 0 {
		return ProbeResult{}, report, fmt.Errorf(
			"ningún puerto RTSP respondió en %s (probados: %s); confirme que RTSP esté habilitado y que Fragata tenga acceso de red a la cámara",
			host, joinPorts(ports),
		)
	}

	candidates := ExpandCandidates(host, openPorts, custom, options.MaxCandidates)
	var h265Fallback *ProbeResult
	for start := 0; start < len(candidates); start += options.Parallelism {
		end := min(start+options.Parallelism, len(candidates))
		batch := candidates[start:end]
		type outcome struct {
			index int
			probe ProbeResult
			err   error
		}
		outcomes := make(chan outcome, len(batch))
		for index, candidate := range batch {
			index, candidate := index, candidate
			go func() {
				withAuth, authErr := WithCredentials(candidate.URL, username, password)
				if authErr != nil {
					outcomes <- outcome{index: index, err: authErr}
					return
				}
				probeCtx, cancel := context.WithTimeout(ctx, options.ProbeTimeout)
				defer cancel()
				probe, probeErr := Probe(probeCtx, withAuth, options.ProbeTimeout)
				outcomes <- outcome{index: index, probe: probe, err: probeErr}
			}()
		}

		batchResults := make([]outcome, len(batch))
		for range batch {
			result := <-outcomes
			batchResults[result.index] = result
		}
		for _, result := range batchResults {
			report.CandidatesTried++
			if result.err == nil {
				if strings.EqualFold(result.probe.Codec, "H264") {
					return result.probe, report, nil
				}
				if h265Fallback == nil {
					copy := result.probe
					h265Fallback = &copy
				}
				continue
			}
			classifySearchFailure(&report, result.err)
		}
		if err := ctx.Err(); err != nil {
			if h265Fallback != nil {
				return *h265Fallback, report, nil
			}
			return ProbeResult{}, report, err
		}
	}
	if h265Fallback != nil {
		return *h265Fallback, report, nil
	}

	switch {
	case report.AuthenticationFail > 0 && report.AuthenticationFail >= report.NotFoundFail:
		return ProbeResult{}, report, fmt.Errorf("el servicio RTSP respondió en los puertos %s, pero rechazó las credenciales", joinPorts(openPorts))
	case report.NotFoundFail > 0:
		return ProbeResult{}, report, fmt.Errorf("el servicio RTSP respondió en los puertos %s, pero ninguna de %d rutas conocidas entregó video", joinPorts(openPorts), report.CandidatesTried)
	default:
		return ProbeResult{}, report, fmt.Errorf("se encontraron puertos abiertos %s, pero no se pudo confirmar un stream H.264/H.265 después de %d intentos", joinPorts(openPorts), report.CandidatesTried)
	}
}

func classifySearchFailure(report *SearchReport, err error) {
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "401"), strings.Contains(lower, "unauthorized"), strings.Contains(lower, "authentication"):
		report.AuthenticationFail++
	case strings.Contains(lower, "404"), strings.Contains(lower, "not found"):
		report.NotFoundFail++
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(lower, "timeout"), strings.Contains(lower, "deadline"):
		report.TimeoutFail++
	}
}

func normalizePorts(ports []int) []int {
	seen := make(map[int]struct{})
	out := make([]int, 0, len(ports))
	for _, port := range ports {
		if port < 1 || port > 65535 {
			continue
		}
		if _, exists := seen[port]; exists {
			continue
		}
		seen[port] = struct{}{}
		out = append(out, port)
	}
	if len(out) == 0 {
		out = []int{554}
	}
	return out
}

func joinPorts(ports []int) string {
	ports = normalizePorts(ports)
	values := make([]string, 0, len(ports))
	for _, port := range ports {
		values = append(values, strconv.Itoa(port))
	}
	return strings.Join(values, ", ")
}
