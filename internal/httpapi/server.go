package httpapi

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	"fragata/internal/retention"
	fragrtsp "fragata/internal/rtsp"
	"fragata/internal/store"
	"fragata/internal/stream"
	"fragata/internal/upload"
)

//go:embed static/*
var staticFiles embed.FS

type Server struct {
	cfg      config.Config
	auth     *auth.Manager
	cameras  *camera.Manager
	live     *live.Manager
	uploader *upload.Uploader
	store    *store.Store
	logger   *slog.Logger
	limiter  *loginLimiter
	http     *http.Server
}

func New(cfg config.Config, authManager *auth.Manager, cameras *camera.Manager, liveManager *live.Manager, uploader *upload.Uploader, state *store.Store, logger *slog.Logger) (*Server, error) {
	s := &Server{cfg: cfg, auth: authManager, cameras: cameras, live: liveManager, uploader: uploader, store: state, logger: logger, limiter: newLoginLimiter()}
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
	protected.HandleFunc("GET /events", s.eventsPage)
	protected.HandleFunc("GET /events/{id}", s.eventPage)
	protected.HandleFunc("GET /settings/sftp", s.sftpSettingsPage)
	protected.HandleFunc("GET /settings/storage", s.storageSettingsPage)
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
	protected.HandleFunc("GET /api/events", s.listDetectionEvents)
	protected.HandleFunc("GET /api/events/{id}", s.getDetectionEvent)
	protected.HandleFunc("GET /api/events/{id}/snapshot", s.detectionEventSnapshot)
	protected.HandleFunc("GET /api/events/{id}/video", s.detectionEventVideo)
	protected.HandleFunc("GET /api/events/{id}/recording", s.detectionEventRecording)
	protected.HandleFunc("GET /api/uploads", s.uploads)
	protected.HandleFunc("GET /api/sftp-profiles", s.listSFTPProfiles)
	protected.HandleFunc("POST /api/sftp-profiles", s.createSFTPProfile)
	protected.HandleFunc("PATCH /api/sftp-profiles/{id}", s.updateSFTPProfile)
	protected.HandleFunc("DELETE /api/sftp-profiles/{id}", s.deleteSFTPProfile)
	protected.HandleFunc("POST /api/sftp-profiles/{id}/test", s.testSFTPProfile)
	protected.HandleFunc("GET /api/retention", s.getRetention)
	protected.HandleFunc("PATCH /api/retention", s.updateRetention)
	protected.HandleFunc("POST /api/cameras/{id}/offer", s.offer)

	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, err
	}
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
	mux.Handle("GET /static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		staticHandler.ServeHTTP(w, r)
	}))
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

func (s *Server) eventsPage(w http.ResponseWriter, _ *http.Request) {
	s.serveAsset(w, "events.html", "text/html; charset=utf-8")
}

func (s *Server) eventPage(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if _, ok := s.store.DetectionEvent(id); !ok {
		http.NotFound(w, r)
		return
	}
	s.serveAsset(w, "event-viewer.html", "text/html; charset=utf-8")
}

func (s *Server) sftpSettingsPage(w http.ResponseWriter, _ *http.Request) {
	s.serveAsset(w, "settings-sftp.html", "text/html; charset=utf-8")
}

func (s *Server) storageSettingsPage(w http.ResponseWriter, _ *http.Request) {
	s.serveAsset(w, "settings-storage.html", "text/html; charset=utf-8")
}

func (s *Server) serveAsset(w http.ResponseWriter, name, contentType string) {
	data, err := staticFiles.ReadFile("static/" + name)
	if err != nil {
		http.Error(w, "archivo no disponible", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
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
		"environment_sftp_available":       s.cfg.SFTP.Enabled,
		"default_segment_duration_seconds": int64(s.cfg.SegmentDuration / time.Second),
		"log_path":                         s.cfg.LogPath,
		"log_max_bytes":                    s.cfg.LogMaxSize,
		"retention_interval_seconds":       int64(s.cfg.RetentionInterval / time.Second),
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
			strings.Contains(message, "obligatorio"), strings.Contains(message, "no puede superar"), strings.Contains(message, "snapshot"),
			strings.Contains(message, "sensibilidad"), strings.Contains(message, "detección"), strings.Contains(message, "intervalo"),
			strings.Contains(message, "confianza"), strings.Contains(message, "enfriamiento"), strings.Contains(message, "active movimiento"):
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
	if err := s.cfg.ValidateCameraHost(host); err != nil {
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
		return "", 0, errors.New("introduzca la IP, dominio o URL RTSP de la cámara")
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
	return config.NormalizeCameraHostInput(raw)
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
func (s *Server) listDetectionEvents(w http.ResponseWriter, r *http.Request) {
	cameraID := strings.TrimSpace(r.URL.Query().Get("camera_id"))
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}
	events := s.store.DetectionEvents(cameraID, limit)
	out := make([]publicDetectionEvent, 0, len(events))
	for _, event := range events {
		out = append(out, s.publicDetectionEvent(event))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) detectionEventSnapshot(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	event, ok := s.store.DetectionEvent(id)
	if !ok || event.SnapshotPath == "" {
		http.NotFound(w, r)
		return
	}
	eventsRoot, err := filepath.Abs(filepath.Join(s.cfg.DataDir, "events"))
	if err != nil {
		http.Error(w, "snapshot no disponible", http.StatusInternalServerError)
		return
	}
	absolute, err := filepath.Abs(filepath.Join(s.cfg.DataDir, filepath.FromSlash(event.SnapshotPath)))
	if err != nil || (absolute != eventsRoot && !strings.HasPrefix(absolute, eventsRoot+string(os.PathSeparator))) {
		http.Error(w, "ruta de snapshot inválida", http.StatusBadRequest)
		return
	}
	file, err := os.Open(absolute)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "snapshot no disponible", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		http.Error(w, "snapshot no disponible", http.StatusInternalServerError)
		return
	}
	header := make([]byte, 512)
	read, _ := file.Read(header)
	_, _ = file.Seek(0, 0)
	w.Header().Set("Content-Type", http.DetectContentType(header[:read]))
	w.Header().Set("Cache-Control", "private, max-age=60")
	http.ServeContent(w, r, filepath.Base(absolute), info.ModTime(), file)
}

func (s *Server) uploads(w http.ResponseWriter, _ *http.Request) {
	jobs := s.store.UploadJobs()
	publicJobs := make([]model.UploadJobPublic, 0, len(jobs))
	for _, job := range jobs {
		publicJobs = append(publicJobs, job.Public())
	}
	writeJSON(w, http.StatusOK, publicJobs)
}

type sftpProfileRequest struct {
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	User           string `json:"user"`
	Password       string `json:"password,omitempty"`
	PrivateKeyPath string `json:"private_key_path,omitempty"`
	KnownHostsPath string `json:"known_hosts_path"`
	RemoteBaseDir  string `json:"remote_base_dir"`
	DeleteLocal    bool   `json:"delete_local"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type sftpProfileView struct {
	model.SFTPProfilePublic
	ReadOnly bool `json:"read_only"`
}

func (s *Server) listSFTPProfiles(w http.ResponseWriter, _ *http.Request) {
	profiles := s.store.SFTPProfiles()
	out := make([]sftpProfileView, 0, len(profiles)+1)
	if s.cfg.SFTP.Enabled {
		out = append(out, sftpProfileView{SFTPProfilePublic: model.SFTPProfilePublic{
			ID: upload.EnvironmentProfileID, Name: "Servidor configurado en .env", Enabled: true,
			Host: s.cfg.SFTP.Host, Port: s.cfg.SFTP.Port, User: s.cfg.SFTP.User,
			HasPassword: s.cfg.SFTP.Password != "", PrivateKeyPath: s.cfg.SFTP.PrivateKeyPath,
			KnownHostsPath: s.cfg.SFTP.KnownHostsPath, RemoteBaseDir: s.cfg.SFTP.RemoteBaseDir,
			DeleteLocal: s.cfg.SFTP.DeleteLocal, TimeoutSeconds: int(s.cfg.SFTP.Timeout / time.Second),
		}, ReadOnly: true})
	}
	for _, profile := range profiles {
		out = append(out, sftpProfileView{SFTPProfilePublic: profile.Public()})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createSFTPProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireCSRF(w, r) {
		return
	}
	var request sftpProfileRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	profile, err := buildSFTPProfile(model.SFTPProfile{}, request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	profile.ID, err = secureID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "no se pudo generar el identificador")
		return
	}
	now := time.Now().UTC()
	profile.CreatedAt, profile.UpdatedAt = now, now
	if err := s.store.SaveSFTPProfile(profile); err != nil {
		writeError(w, http.StatusInternalServerError, "no se pudo guardar el servidor SFTP")
		return
	}
	writeJSON(w, http.StatusCreated, profile.Public())
}

func (s *Server) updateSFTPProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireCSRF(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == upload.EnvironmentProfileID {
		writeError(w, http.StatusBadRequest, "el servidor configurado en .env es de solo lectura")
		return
	}
	current, ok := s.store.SFTPProfile(id)
	if !ok {
		writeError(w, http.StatusNotFound, "servidor SFTP no encontrado")
		return
	}
	var request sftpProfileRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	profile, err := buildSFTPProfile(current, request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	profile.ID, profile.CreatedAt, profile.UpdatedAt = current.ID, current.CreatedAt, time.Now().UTC()
	if err := s.store.SaveSFTPProfile(profile); err != nil {
		writeError(w, http.StatusInternalServerError, "no se pudo actualizar el servidor SFTP")
		return
	}
	writeJSON(w, http.StatusOK, profile.Public())
}

func (s *Server) deleteSFTPProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireCSRF(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == upload.EnvironmentProfileID {
		writeError(w, http.StatusBadRequest, "el servidor configurado en .env no puede eliminarse desde el panel")
		return
	}
	if _, ok := s.store.SFTPProfile(id); !ok {
		writeError(w, http.StatusNotFound, "servidor SFTP no encontrado")
		return
	}
	for _, camera := range s.store.Cameras() {
		if camera.SFTPProfileID == id {
			writeError(w, http.StatusConflict, "el servidor SFTP está asignado a una o más cámaras")
			return
		}
	}
	for _, job := range s.store.UploadJobs() {
		if job.SFTPProfileID == id {
			writeError(w, http.StatusConflict, "el servidor SFTP tiene archivos pendientes de subir")
			return
		}
	}
	if err := s.store.DeleteSFTPProfile(id); err != nil {
		writeError(w, http.StatusInternalServerError, "no se pudo eliminar el servidor SFTP")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) testSFTPProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireCSRF(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var profile model.SFTPProfile
	if id == upload.EnvironmentProfileID && s.cfg.SFTP.Enabled {
		profile = model.SFTPProfile{Enabled: true, Host: s.cfg.SFTP.Host, Port: s.cfg.SFTP.Port, User: s.cfg.SFTP.User,
			Password: s.cfg.SFTP.Password, PrivateKeyPath: s.cfg.SFTP.PrivateKeyPath, KnownHostsPath: s.cfg.SFTP.KnownHostsPath,
			RemoteBaseDir: s.cfg.SFTP.RemoteBaseDir, DeleteLocal: s.cfg.SFTP.DeleteLocal, TimeoutSeconds: int(s.cfg.SFTP.Timeout / time.Second)}
	} else {
		var ok bool
		profile, ok = s.store.SFTPProfile(id)
		if !ok {
			writeError(w, http.StatusNotFound, "servidor SFTP no encontrado")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	if err := s.uploader.Test(ctx, profile); err != nil {
		writeError(w, http.StatusUnprocessableEntity, model.RedactSecrets(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func buildSFTPProfile(current model.SFTPProfile, request sftpProfileRequest) (model.SFTPProfile, error) {
	profile := current
	profile.Name = strings.TrimSpace(request.Name)
	profile.Enabled = request.Enabled
	profile.Host = strings.TrimSpace(request.Host)
	profile.Port = request.Port
	profile.User = strings.TrimSpace(request.User)
	if request.Password != "" {
		profile.Password = request.Password
	}
	profile.PrivateKeyPath = strings.TrimSpace(request.PrivateKeyPath)
	profile.KnownHostsPath = strings.TrimSpace(request.KnownHostsPath)
	profile.RemoteBaseDir = strings.TrimSpace(request.RemoteBaseDir)
	profile.DeleteLocal = request.DeleteLocal
	profile.TimeoutSeconds = request.TimeoutSeconds
	if profile.Name == "" || len([]rune(profile.Name)) > 80 {
		return model.SFTPProfile{}, errors.New("el nombre SFTP es obligatorio y admite hasta 80 caracteres")
	}
	if profile.Host == "" || profile.User == "" {
		return model.SFTPProfile{}, errors.New("el host y usuario SFTP son obligatorios")
	}
	if profile.Port < 1 || profile.Port > 65535 {
		return model.SFTPProfile{}, errors.New("el puerto SFTP es inválido")
	}
	if profile.Password == "" && profile.PrivateKeyPath == "" {
		return model.SFTPProfile{}, errors.New("configure una contraseña o ruta de llave privada")
	}
	if profile.KnownHostsPath == "" {
		return model.SFTPProfile{}, errors.New("configure el archivo known_hosts para validar el servidor")
	}
	if profile.RemoteBaseDir == "" || !strings.HasPrefix(profile.RemoteBaseDir, "/") {
		return model.SFTPProfile{}, errors.New("el directorio remoto debe ser una ruta absoluta")
	}
	if profile.TimeoutSeconds < 5 || profile.TimeoutSeconds > 300 {
		return model.SFTPProfile{}, errors.New("el timeout SFTP debe estar entre 5 y 300 segundos")
	}
	return profile, nil
}

func (s *Server) getRetention(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Retention())
}

func (s *Server) updateRetention(w http.ResponseWriter, r *http.Request) {
	if !s.requireCSRF(w, r) {
		return
	}
	var policy model.RetentionPolicy
	if err := decodeJSON(w, r, &policy); err != nil {
		return
	}
	policy.Unit = strings.TrimSpace(strings.ToLower(policy.Unit))
	limits := map[string]int{"days": 3650, "months": 120, "years": 10}
	limit, valid := limits[policy.Unit]
	if !valid || policy.Value < 1 || policy.Value > limit {
		writeError(w, http.StatusBadRequest, "configure una retención válida en días, meses o años")
		return
	}
	policy.UpdatedAt = time.Now().UTC()
	if err := s.store.SaveRetention(policy); err != nil {
		writeError(w, http.StatusInternalServerError, "no se pudo guardar la política de retención")
		return
	}
	cleaner := retention.Cleaner{BaseDir: s.cfg.RecordingsDir, EventsDir: filepath.Join(s.cfg.DataDir, "events"), Store: s.store, Logger: s.logger}
	result := cleaner.Cleanup(time.Now())
	writeJSON(w, http.StatusOK, map[string]any{"policy": policy, "cleanup": result})
}

func secureID() (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (s *Server) offer(w http.ResponseWriter, r *http.Request) {
	if !s.requireCSRF(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var request struct {
		SDP   string `json:"sdp"`
		Media string `json:"media,omitempty"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	mediaKind := strings.ToLower(strings.TrimSpace(request.Media))
	if mediaKind == "" {
		mediaKind = "video"
	}
	var (
		hub  *stream.Hub
		mode string
		err  error
	)
	switch mediaKind {
	case "video":
		hub, mode, err = s.cameras.LiveHub(ctx, id)
	case "audio":
		hub, mode, err = s.cameras.LiveAudioHub(ctx, id)
	default:
		writeError(w, http.StatusBadRequest, "tipo de medio WebRTC inválido")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, model.RedactSecrets(err.Error()))
		return
	}
	var answer string
	switch mediaKind {
	case "video":
		answer, err = s.live.OfferVideo(ctx, hub, request.SDP, mode)
	case "audio":
		answer, err = s.live.OfferAudio(ctx, hub, request.SDP)
	default:
		writeError(w, http.StatusBadRequest, "tipo de medio WebRTC inválido")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, model.RedactSecrets(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"sdp": answer, "mode": mode, "media": mediaKind})
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' https://cdn.jsdelivr.net; style-src 'self' https://cdn.jsdelivr.net; style-src-attr 'unsafe-inline'; font-src 'self' https://cdn.jsdelivr.net data:; img-src 'self' data:; media-src 'self' blob:; connect-src 'self' https://cdn.jsdelivr.net; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
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
