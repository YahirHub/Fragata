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
// bounded by the caller and never attempts credentials other than those supplied
// by the user. Hosts can be local IPs, public IPs or DNS names.
type SearchOptions struct {
	Ports           []int
	ConnectTimeout  time.Duration
	ProbeTimeout    time.Duration
	MaxCandidates   int
	DictionaryPath  string
	Parallelism     int
	FindH264Preview bool
}

type SearchResult struct {
	Primary ProbeResult  `json:"primary"`
	Preview *ProbeResult `json:"preview,omitempty"`
}

// SearchReport contains safe diagnostics without credentials.
type SearchReport struct {
	OpenPorts          []int       `json:"open_ports"`
	PortChecks         []PortCheck `json:"port_checks,omitempty"`
	CandidatesTried    int         `json:"candidates_tried"`
	AuthenticationFail int         `json:"authentication_failures"`
	NotFoundFail       int         `json:"not_found_failures"`
	TimeoutFail        int         `json:"timeout_failures"`
}

// PortCheck describes the TCP reachability of one camera port. It never
// contains credentials or stream paths and is safe to expose to an admin.
type PortCheck struct {
	Port      int    `json:"port"`
	Reachable bool   `json:"reachable"`
	State     string `json:"state"`
	ElapsedMS int64  `json:"elapsed_ms"`
	Error     string `json:"error,omitempty"`
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

// CheckPorts checks each configured TCP port once and preserves the reason for
// failure. This distinguishes a disabled service from a missing route, firewall
// drop or isolated camera network.
func CheckPorts(ctx context.Context, host string, ports []int, timeout time.Duration) []PortCheck {
	ports = normalizePorts(ports)
	if timeout <= 0 {
		timeout = 1200 * time.Millisecond
	}
	results := make(chan PortCheck, len(ports))
	var wg sync.WaitGroup
	for _, port := range ports {
		port := port
		wg.Add(1)
		go func() {
			defer wg.Done()
			started := time.Now()
			dialer := net.Dialer{Timeout: timeout}
			conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port)))
			elapsed := time.Since(started).Milliseconds()
			if err == nil {
				_ = conn.Close()
				results <- PortCheck{Port: port, Reachable: true, State: "open", ElapsedMS: elapsed}
				return
			}
			state, safeError := classifyDialError(err)
			results <- PortCheck{Port: port, State: state, ElapsedMS: elapsed, Error: safeError}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	byPort := make(map[int]PortCheck, len(ports))
	for result := range results {
		byPort[result.Port] = result
	}
	ordered := make([]PortCheck, 0, len(ports))
	for _, port := range ports {
		if result, ok := byPort[port]; ok {
			ordered = append(ordered, result)
		}
	}
	return ordered
}

// ReachablePorts retains the original compact API used by callers that only
// need the open port list.
func ReachablePorts(ctx context.Context, host string, ports []int, timeout time.Duration) []int {
	checks := CheckPorts(ctx, host, ports, timeout)
	open := make([]int, 0, len(checks))
	for _, check := range checks {
		if check.Reachable {
			open = append(open, check.Port)
		}
	}
	return open
}

func classifyDialError(err error) (string, string) {
	if err == nil {
		return "open", ""
	}
	lower := strings.ToLower(err.Error())
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded") {
		return "timeout", "tiempo de conexión agotado"
	}
	if strings.Contains(lower, "connection refused") || strings.Contains(lower, "conexión rehusada") || strings.Contains(lower, "actively refused") {
		return "refused", "conexión rechazada"
	}
	if strings.Contains(lower, "network is unreachable") || strings.Contains(lower, "no route to host") || strings.Contains(lower, "network unreachable") {
		return "no_route", "sin ruta hacia la red"
	}
	if strings.Contains(lower, "host is unreachable") || strings.Contains(lower, "host unreachable") {
		return "unreachable", "host inalcanzable"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled", "comprobación cancelada"
	}
	return "error", "error de conexión TCP"
}

// PortFailureSummary explains connectivity failures without implying that a
// dictionary, URL or password can fix a TCP routing problem.
func PortFailureSummary(host string, checks []PortCheck) string {
	if len(checks) == 0 {
		return fmt.Sprintf("no se pudieron comprobar puertos en %s", host)
	}
	states := make(map[string]int)
	ports := make([]int, 0, len(checks))
	for _, check := range checks {
		states[check.State]++
		ports = append(ports, check.Port)
	}
	switch {
	case states["no_route"] > 0:
		return fmt.Sprintf("Fragata no tiene una ruta de red hacia %s (puertos probados: %s); la URL y las credenciales todavía no se evaluaron", host, joinPorts(ports))
	case states["unreachable"] > 0:
		return fmt.Sprintf("el host %s es inalcanzable desde el entorno donde corre Fragata (puertos probados: %s); revise VLAN, aislamiento Wi-Fi, VPN o firewall", host, joinPorts(ports))
	case states["timeout"] == len(checks):
		return fmt.Sprintf("%s no respondió a conexiones TCP desde el entorno donde corre Fragata (puertos probados: %s); esto ocurre antes de validar ruta RTSP, usuario o contraseña", host, joinPorts(ports))
	case states["refused"] == len(checks):
		return fmt.Sprintf("%s es alcanzable, pero rechazó todos los puertos probados (%s); habilite RTSP/ONVIF o indique el puerto correcto", host, joinPorts(ports))
	default:
		return fmt.Sprintf("ningún puerto de cámara respondió correctamente en %s (probados: %s); revise el diagnóstico de red antes de cambiar la URL RTSP", host, joinPorts(ports))
	}
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
	result, report, err := SearchStreams(ctx, host, username, password, options)
	return result.Primary, report, err
}

// SearchStreams preserves vendor ordering but compares a bounded window after
// the first success. This catches nearby main/substream and H.264/H.265 variants
// without scanning the entire dictionary against a low-cost camera.
type dictionaryProbe struct {
	probe     ProbeResult
	candidate Candidate
	index     int
}

func SearchStreams(ctx context.Context, host, username, password string, options SearchOptions) (SearchResult, SearchReport, error) {
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
		return SearchResult{}, SearchReport{}, err
	}
	ports := append([]int(nil), options.Ports...)
	for _, template := range custom {
		if template.Port > 0 {
			ports = append(ports, template.Port)
		}
	}
	portChecks := CheckPorts(ctx, host, ports, options.ConnectTimeout)
	openPorts := make([]int, 0, len(portChecks))
	for _, check := range portChecks {
		if check.Reachable {
			openPorts = append(openPorts, check.Port)
		}
	}
	report := SearchReport{OpenPorts: openPorts, PortChecks: portChecks}
	if len(openPorts) == 0 {
		return SearchResult{}, report, errors.New(PortFailureSummary(host, portChecks))
	}

	candidates := ExpandCandidates(host, openPorts, custom, options.MaxCandidates)
	var primary *dictionaryProbe
	var preview *dictionaryProbe
	stopAfter := len(candidates)
	firstSuccess := -1

	for start := 0; start < len(candidates) && start < stopAfter; start += options.Parallelism {
		end := min(start+options.Parallelism, len(candidates), stopAfter)
		batch := candidates[start:end]
		type outcome struct {
			batchIndex     int
			candidateIndex int
			candidate      Candidate
			probe          ProbeResult
			err            error
		}
		outcomes := make(chan outcome, len(batch))
		for batchIndex, candidate := range batch {
			batchIndex, candidate := batchIndex, candidate
			candidateIndex := start + batchIndex
			go func() {
				withAuth, authErr := WithCredentials(candidate.URL, username, password)
				if authErr != nil {
					outcomes <- outcome{batchIndex: batchIndex, candidateIndex: candidateIndex, candidate: candidate, err: authErr}
					return
				}
				probeCtx, cancel := context.WithTimeout(ctx, options.ProbeTimeout)
				defer cancel()
				probe, probeErr := Probe(probeCtx, withAuth, options.ProbeTimeout)
				outcomes <- outcome{batchIndex: batchIndex, candidateIndex: candidateIndex, candidate: candidate, probe: probe, err: probeErr}
			}()
		}

		batchResults := make([]outcome, len(batch))
		for range batch {
			result := <-outcomes
			batchResults[result.batchIndex] = result
		}
		for _, result := range batchResults {
			report.CandidatesTried++
			if result.err == nil {
				found := &dictionaryProbe{probe: result.probe, candidate: result.candidate, index: result.candidateIndex}
				if primary == nil || betterDictionaryProbe(found, primary) {
					primary = found
				}
				if strings.EqualFold(found.probe.Codec, "H264") && (preview == nil || betterDictionaryProbe(found, preview)) {
					preview = found
				}
				if !options.FindH264Preview {
					return SearchResult{Primary: found.probe}, report, nil
				}
				if firstSuccess < 0 {
					firstSuccess = result.candidateIndex
					// Main, secondary and codec variants from one vendor are kept
					// adjacent. Probe a bounded window so H.264 is never chosen
					// merely because it appeared just before a higher-quality H.265.
					stopAfter = min(len(candidates), firstSuccess+16)
				}
				continue
			}
			classifySearchFailure(&report, result.err)
		}
		if err := ctx.Err(); err != nil {
			if primary != nil {
				return dictionarySearchResult(primary, preview), report, nil
			}
			return SearchResult{}, report, err
		}
	}
	if primary != nil {
		return dictionarySearchResult(primary, preview), report, nil
	}

	switch {
	case report.AuthenticationFail > 0 && report.AuthenticationFail >= report.NotFoundFail:
		return SearchResult{}, report, fmt.Errorf("el servicio RTSP respondió en los puertos %s, pero rechazó las credenciales", joinPorts(openPorts))
	case report.NotFoundFail > 0:
		return SearchResult{}, report, fmt.Errorf("el servicio RTSP respondió en los puertos %s, pero ninguna de %d rutas conocidas entregó video", joinPorts(openPorts), report.CandidatesTried)
	default:
		return SearchResult{}, report, fmt.Errorf("se encontraron puertos abiertos %s, pero no se pudo confirmar un stream H.264/H.265 después de %d intentos", joinPorts(openPorts), report.CandidatesTried)
	}
}

func dictionarySearchResult(primary, preview *dictionaryProbe) SearchResult {
	result := SearchResult{Primary: primary.probe}
	if !strings.EqualFold(primary.probe.Codec, "H264") && preview != nil && preview.probe.URL != primary.probe.URL {
		copy := preview.probe
		result.Preview = &copy
	}
	return result
}

func betterDictionaryProbe(candidate, current *dictionaryProbe) bool {
	candidatePixels := int64(candidate.probe.Width) * int64(candidate.probe.Height)
	currentPixels := int64(current.probe.Width) * int64(current.probe.Height)
	if candidatePixels != currentPixels {
		return candidatePixels > currentPixels
	}
	candidatePriority := dictionaryCandidatePriority(candidate.candidate)
	currentPriority := dictionaryCandidatePriority(current.candidate)
	if candidatePriority != currentPriority {
		return candidatePriority > currentPriority
	}
	candidateH265 := strings.EqualFold(candidate.probe.Codec, "H265")
	currentH265 := strings.EqualFold(current.probe.Codec, "H265")
	if candidateH265 != currentH265 {
		return candidateH265
	}
	return candidate.index < current.index
}

func dictionaryCandidatePriority(candidate Candidate) int {
	value := strings.ToLower(candidate.Name + " " + candidate.URL)
	switch {
	case strings.Contains(value, "terciario"), strings.Contains(value, "subtype=2"), strings.Contains(value, "channels/103"):
		return -200
	case strings.Contains(value, "secundario"), strings.Contains(value, " sub "), strings.Contains(value, "_sub"), strings.Contains(value, "subtype=1"), strings.Contains(value, "channels/102"):
		return -100
	case strings.Contains(value, "principal"), strings.Contains(value, " main"), strings.Contains(value, "_main"), strings.Contains(value, "subtype=0"), strings.Contains(value, "channels/101"):
		return 100
	default:
		return 0
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
