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
	"strings"
	"sync"
	"time"

	"fragata/internal/auth"
	"fragata/internal/camera"
	"fragata/internal/config"
	"fragata/internal/live"
	"fragata/internal/model"
	"fragata/internal/onvif"
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
	protected.HandleFunc("GET /api/session", s.session)
	protected.HandleFunc("POST /api/logout", s.logout)
	protected.HandleFunc("GET /api/cameras", s.listCameras)
	protected.HandleFunc("POST /api/cameras", s.addCamera)
	protected.HandleFunc("POST /api/rtsp/probe", s.probeRTSP)
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
	})
}

func (s *Server) listCameras(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.cameras.Cameras())
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
	hub, ok := s.cameras.Hub(id)
	if !ok {
		writeError(w, http.StatusNotFound, "cámara no encontrada o deshabilitada")
		return
	}
	var request struct {
		SDP string `json:"sdp"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	answer, err := s.live.Offer(ctx, hub, request.SDP)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, model.RedactSecrets(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"sdp": answer})
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; media-src 'self' blob:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
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
