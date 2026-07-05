package httpapi

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"fragata/internal/auth"
	"fragata/internal/camera"
	"fragata/internal/config"
	"fragata/internal/live"
	"fragata/internal/model"
	"fragata/internal/networkdiag"
	"fragata/internal/onvif"
	fragrtsp "fragata/internal/rtsp"
	"fragata/internal/store"
)

//go:embed static/*
var staticFiles embed.FS

type Server struct {
	cfg     config.Config
	auth    *auth.Manager
	cameras *camera.Manager
	live    *live.Manager
	store   *store.Store
	logger  *slog.Logger
	limiter *loginLimiter
	http    *http.Server
}

func New(cfg config.Config, authManager *auth.Manager, cameras *camera.Manager, liveManager *live.Manager, state *store.Store, logger *slog.Logger) (*Server, error) {
	s := &Server{cfg: cfg, auth: authManager, cameras: cameras, live: liveManager, store: state, logger: logger, limiter: newLoginLimiter()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /api/login", s.login)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /", s.index)
	protected.HandleFunc("GET /cameras", s.camerasPage)
	protected.HandleFunc("GET /cameras/new", s.newCameraPage)
	protected.HandleFunc("GET /cameras/{id}/settings", s.cameraSettingsPage)
	protected.HandleFunc("GET /camera/{id}", s.cameraPage)
	protected.HandleFunc("GET /api/session", s.session)
	protected.HandleFunc("POST /api/logout", s.logout)
	protected.HandleFunc("GET /api/cameras", s.listCameras)
	protected.HandleFunc("GET /api/cameras/{id}", s.getCamera)
	protected.HandleFunc("POST /api/cameras", s.addCamera)
	protected.HandleFunc("PATCH /api/cameras/{id}", s.updateCamera)
	protected.HandleFunc("POST /api/cameras/{id}/redetect", s.redetectCamera)
	protected.HandleFunc("POST /api/cameras/{id}/probe-settings", s.probeCameraSettings)
	protected.HandleFunc("POST /api/rtsp/probe", s.probeRTSP)
	protected.HandleFunc("POST /api/network/diagnose", s.diagnoseNetwork)
	protected.HandleFunc("DELETE /api/cameras/{id}", s.deleteCamera)
	protected.HandleFunc("POST /api/discovery", s.discovery)
	protected.HandleFunc("GET /api/status", s.status)
	protected.HandleFunc("GET /api/uploads", s.uploads)
	protected.HandleFunc("POST /api/cameras/{id}/offer", s.offer)

	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, err
	}
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
	mux.Handle("GET /static/", staticHandler)
	mux.Handle("/", s.auth.Require(protected))

	s.http = &http.Server{
		Addr: cfg.ListenAddress, Handler: securityHeaders(mux),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 180 * time.Second, WriteTimeout: 180 * time.Second, IdleTimeout: 90 * time.Second,
	}
	return s, nil
}

func (s *Server) ListenAndServe() error {
	s.logger.Info("Fragata listening", "address", s.cfg.ListenAddress, "auth_enabled", s.auth.Enabled())
	err := s.http.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error { return s.http.Shutdown(ctx) }

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if !s.auth.Enabled() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if _, ok := s.auth.Session(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.serveAsset(w, "login.html", "text/html; charset=utf-8")
}

func (s *Server) index(w http.ResponseWriter, _ *http.Request) {
	s.serveAsset(w, "index.html", "text/html; charset=utf-8")
}

func (s *Server) camerasPage(w http.ResponseWriter, _ *http.Request) {
	s.serveAsset(w, "cameras.html", "text/html; charset=utf-8")
}

func (s *Server) newCameraPage(w http.ResponseWriter, _ *http.Request) {
	s.serveAsset(w, "camera-new.html", "text/html; charset=utf-8")
}

func (s *Server) cameraSettingsPage(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if _, ok := s.cameras.Camera(id); !ok {
		http.NotFound(w, r)
		return
	}
	s.serveAsset(w, "camera-settings.html", "text/html; charset=utf-8")
}

func (s *Server) cameraPage(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if _, ok := s.cameras.Camera(id); !ok {
		http.NotFound(w, r)
		return
	}
	s.serveAsset(w, "viewer.html", "text/html; charset=utf-8")
}

func (s *Server) serveAsset(w http.ResponseWriter, name, contentType string) {
	data, err := staticFiles.ReadFile("static/" + name)
	if err != nil {
		http.Error(w, "archivo no disponible", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(data)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := remoteIP(r)
	if !s.limiter.Allow(ip) {
		writeError(w, http.StatusTooManyRequests, "demasiados intentos; espere un minuto")
		return
	}
	var request struct{ Username, Password string }
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	sess, err := s.auth.Login(w, request.Username, request.Password)
	if err != nil {
		s.limiter.Fail(ip)
		time.Sleep(300 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "usuario o contraseña incorrectos")
		return
	}
	s.limiter.Success(ip)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "csrf_token": sess.CSRFToken})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.requireCSRF(w, r) {
		return
	}
	_ = s.auth.Logout(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	sess, _ := s.auth.Session(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"auth_enabled": s.auth.Enabled(), "csrf_token": sess.CSRFToken, "username": s.cfg.AdminUser,
		"ffmpeg_available":                 s.cfg.FFmpegPath != "",
		"default_segment_duration_seconds": int64(s.cfg.SegmentDuration / time.Second),
	})
}

func (s *Server) listCameras(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.cameras.Cameras())
}

func (s *Server) getCamera(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	cam, ok := s.cameras.Camera(id)
	if !ok {
		writeError(w, http.StatusNotFound, "cámara no encontrada")
		return
	}
	writeJSON(w, http.StatusOK, cam.Public())
}

func (s *Server) addCamera(w http.ResponseWriter, r *http.Request) {
	if !s.requireCSRF(w, r) {
		return
	}
	var request camera.AddRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	detected, err := camera.Detect(ctx, s.cfg, request)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, model.RedactSecrets(err.Error()))
		return
	}
	cam, err := s.cameras.Add(detected.Camera)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "no se pudo guardar la cámara")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"camera": cam.Public(), "detection_method": detected.Method, "diagnostics": detected.Diagnostics})
}

func (s *Server) updateCamera(w http.ResponseWriter, r *http.Request) {
	if !s.requireCSRF(w, r) {
		return
	}
	var request camera.UpdateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	cam, _, err := s.cameras.Update(ctx, id, request)
	if err != nil {
		message := model.RedactSecrets(err.Error())
		switch {
		case strings.Contains(message, "no encontrada"):
			writeError(w, http.StatusNotFound, message)
		case strings.Contains(message, "detección"), strings.Contains(message, "RTSP"), strings.Contains(message, "ONVIF"), strings.Contains(message, "conexión"):
			writeError(w, http.StatusUnprocessableEntity, message)
		case strings.Contains(message, "nombre"), strings.Contains(message, "carpeta"), strings.Contains(message, "duración"),
			strings.Contains(message, "introduzca"), strings.Contains(message, "dirección IP"), strings.Contains(message, "puerto inválido"),
			strings.Contains(message, "obligatorio"), strings.Contains(message, "no puede superar"):
			writeError(w, http.StatusBadRequest, message)
		default:
			writeError(w, http.StatusInternalServerError, "no se pudo actualizar la cámara")
		}
		return
	}
	writeJSON(w, http.StatusOK, cam.Public())
}

func (s *Server) redetectCamera(w http.ResponseWriter, r *http.Request) {
	if !s.requireCSRF(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	detected, err := s.cameras.Redetect(ctx, id)
	if err != nil {
		if strings.Contains(err.Error(), "no encontrada") {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusUnprocessableEntity, model.RedactSecrets(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"camera": detected.Camera.Public(), "detection_method": detected.Method, "diagnostics": detected.Diagnostics,
	})
}

func (s *Server) probeCameraSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireCSRF(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	current, ok := s.cameras.Camera(id)
	if !ok {
		writeError(w, http.StatusNotFound, "cámara no encontrada")
		return
	}
	var request camera.UpdateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	host, username, password, rawURL := current.Host, current.Username, current.Password, current.RTSPURL
	rtspURLChanged := false
	if request.Host != nil {
		host = strings.TrimSpace(*request.Host)
	}
	if request.Username != nil {
		username = strings.TrimSpace(*request.Username)
	}
	if request.Password != nil && *request.Password != "" {
		password = *request.Password
	}
	if request.RTSPURL != nil {
		rawURL = strings.TrimSpace(*request.RTSPURL)
		if rawURL == model.RedactURL(current.RTSPURL) {
			rawURL = current.RTSPURL
		}
		rtspURLChanged = rawURL != current.RTSPURL
	}
	if !rtspURLChanged {
		rawURL = ""
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	detectRequest := camera.AddRequest{
		Name: current.Name, Host: host, Username: username, Password: password, RTSPURL: rawURL,
		FolderName: current.FolderName,
	}
	detected, err := camera.Detect(ctx, s.cfg, detectRequest)
	if err != nil && !rtspURLChanged && current.RTSPURL != "" {
		detectRequest.RTSPURL = fragrtsp.NormalizeHost(current.RTSPURL, host)
		detected, err = camera.Detect(ctx, s.cfg, detectRequest)
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, model.RedactSecrets(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"camera": detected.Camera.Public(), "detection_method": detected.Method, "diagnostics": detected.Diagnostics,
	})
}

func (s *Server) probeRTSP(w http.ResponseWriter, r *http.Request) {
	if !s.requireCSRF(w, r) {
		return
	}
	var request camera.ProbeRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.ProbeTimeout+2*time.Second)
	defer cancel()
	probe, err := camera.ProbeManual(ctx, s.cfg, request)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, model.RedactSecrets(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, probe)
}

func (s *Server) diagnoseNetwork(w http.ResponseWriter, r *http.Request) {
	if !s.requireCSRF(w, r) {
		return
	}
	var request struct {
		Host  string `json:"host"`
		Ports []int  `json:"ports,omitempty"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	host, requestedPort, err := normalizeDiagnosticHost(request.Host)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.cfg.ValidateCameraIP(host); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ports := append([]int(nil), request.Ports...)
	if len(ports) == 0 {
		ports = append(ports, s.cfg.RTSPPorts...)
		ports = append(ports, 443, 8899, 8000)
	}
	if requestedPort > 0 {
		ports = append([]int{requestedPort}, ports...)
	}
	if len(ports) > 32 {
		writeError(w, http.StatusBadRequest, "el diagnóstico permite como máximo 32 puertos")
		return
	}
	for _, port := range ports {
		if port < 1 || port > 65535 {
			writeError(w, http.StatusBadRequest, "puerto de diagnóstico inválido")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RTSPConnectTimeout+2*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, networkdiag.Diagnose(ctx, host, ports, s.cfg.RTSPConnectTimeout))
}

func normalizeDiagnosticHost(raw string) (string, int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0, errors.New("introduzca la IP o URL RTSP de la cámara")
	}
	if strings.HasPrefix(strings.ToLower(raw), "rtsp://") || strings.HasPrefix(strings.ToLower(raw), "rtsps://") {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Hostname() == "" {
			return "", 0, errors.New("URL RTSP inválida")
		}
		port := 0
		if parsed.Port() != "" {
			port, err = strconv.Atoi(parsed.Port())
			if err != nil || port < 1 || port > 65535 {
				return "", 0, errors.New("puerto RTSP inválido")
			}
		}
		return parsed.Hostname(), port, nil
	}
	if ip := net.ParseIP(strings.Trim(raw, "[]")); ip != nil {
		return strings.Trim(raw, "[]"), 0, nil
	}
	host, portRaw, err := net.SplitHostPort(raw)
	if err != nil || net.ParseIP(strings.Trim(host, "[]")) == nil {
		return "", 0, errors.New("la cámara debe indicarse mediante una dirección IP")
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, errors.New("puerto de cámara inválido")
	}
	return strings.Trim(host, "[]"), port, nil
}

func (s *Server) deleteCamera(w http.ResponseWriter, r *http.Request) {
	if !s.requireCSRF(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if _, ok := s.cameras.Camera(id); !ok {
		writeError(w, http.StatusNotFound, "cámara no encontrada")
		return
	}
	if err := s.cameras.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "no se pudo eliminar la cámara")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) discovery(w http.ResponseWriter, r *http.Request) {
	if !s.requireCSRF(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.DiscoveryTimeout+time.Second)
	defer cancel()
	devices, err := onvif.Discover(ctx, s.cfg.DiscoveryTimeout)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("descubrimiento ONVIF: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, devices)
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.cameras.Statuses())
}
func (s *Server) uploads(w http.ResponseWriter, _ *http.Request) {
	jobs := s.store.UploadJobs()
	publicJobs := make([]model.UploadJobPublic, 0, len(jobs))
	for _, job := range jobs {
		publicJobs = append(publicJobs, job.Public())
	}
	writeJSON(w, http.StatusOK, publicJobs)
}

func (s *Server) offer(w http.ResponseWriter, r *http.Request) {
	if !s.requireCSRF(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var request struct {
		SDP string `json:"sdp"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	hub, mode, err := s.cameras.LiveHub(ctx, id)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, model.RedactSecrets(err.Error()))
		return
	}
	answer, err := s.live.Offer(ctx, hub, request.SDP)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, model.RedactSecrets(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"sdp": answer, "mode": mode})
}

func (s *Server) requireCSRF(w http.ResponseWriter, r *http.Request) bool {
	if s.auth.CheckCSRF(r) {
		return true
	}
	writeError(w, http.StatusForbidden, "token CSRF inválido")
	return false
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' https://cdn.jsdelivr.net; style-src 'self' https://cdn.jsdelivr.net; font-src 'self' https://cdn.jsdelivr.net data:; img-src 'self' data:; media-src 'self' blob:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{attempts: make(map[string][]time.Time)} }

func (l *loginLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-time.Minute)
	items := l.attempts[ip][:0]
	for _, at := range l.attempts[ip] {
		if at.After(cutoff) {
			items = append(items, at)
		}
	}
	l.attempts[ip] = items
	return len(items) < 5
}

func (l *loginLimiter) Fail(ip string) {
	l.mu.Lock()
	l.attempts[ip] = append(l.attempts[ip], time.Now())
	l.mu.Unlock()
}

func (l *loginLimiter) Success(ip string) {
	l.mu.Lock()
	delete(l.attempts, ip)
	l.mu.Unlock()
}
