package config

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddress        string
	DataDir              string
	RecordingsDir        string
	SegmentDuration      time.Duration
	ShutdownTimeout      time.Duration
	SessionDuration      time.Duration
	AdminUser            string
	AdminPassword        string
	SecureCookies        bool
	AllowPublicCameras   bool
	DiscoveryTimeout     time.Duration
	ProbeTimeout         time.Duration
	RTSPConnectTimeout   time.Duration
	RTSPCandidateTimeout time.Duration
	RTSPPorts            []int
	RTSPMaxCandidates    int
	RTSPDictionaryPath   string
	STUNServers          []string
	MaxViewers           int
	LiveIdleTimeout      time.Duration
	FFmpegPath           string
	SecretKey            []byte
	SFTP                 SFTPConfig
}

type SFTPConfig struct {
	Enabled        bool
	Host           string
	Port           int
	User           string
	Password       string
	PrivateKeyPath string
	KnownHostsPath string
	RemoteBaseDir  string
	Workers        int
	Timeout        time.Duration
	DeleteLocal    bool
}

func Load(dotenvPath string) (Config, error) {
	if dotenvPath != "" {
		if err := loadDotEnv(dotenvPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("cargar .env: %w", err)
		}
	}

	dataDir := env("FRAGATA_DATA_DIR", "./data")
	cfg := Config{
		ListenAddress:        env("FRAGATA_LISTEN", ":8080"),
		DataDir:              dataDir,
		RecordingsDir:        env("FRAGATA_RECORDINGS_DIR", filepath.Join(dataDir, "recordings")),
		SegmentDuration:      envDuration("FRAGATA_SEGMENT_DURATION", 5*time.Minute),
		ShutdownTimeout:      envDuration("FRAGATA_SHUTDOWN_TIMEOUT", 20*time.Second),
		SessionDuration:      envDuration("FRAGATA_SESSION_DURATION", 30*24*time.Hour),
		AdminUser:            strings.TrimSpace(os.Getenv("FRAGATA_ADMIN_USER")),
		AdminPassword:        os.Getenv("FRAGATA_ADMIN_PASSWORD"),
		SecureCookies:        envBool("FRAGATA_SECURE_COOKIES", false),
		AllowPublicCameras:   envBool("FRAGATA_ALLOW_PUBLIC_CAMERAS", false),
		DiscoveryTimeout:     envDuration("FRAGATA_DISCOVERY_TIMEOUT", 4*time.Second),
		ProbeTimeout:         envDuration("FRAGATA_PROBE_TIMEOUT", 6*time.Second),
		RTSPConnectTimeout:   envDuration("FRAGATA_RTSP_CONNECT_TIMEOUT", 3*time.Second),
		RTSPCandidateTimeout: envDuration("FRAGATA_RTSP_CANDIDATE_TIMEOUT", 3*time.Second),
		RTSPPorts:            envIntList("FRAGATA_RTSP_PORTS", []int{554, 8554, 10554, 7070, 7447, 8555, 88, 80}),
		RTSPMaxCandidates:    envInt("FRAGATA_RTSP_MAX_CANDIDATES", 160),
		RTSPDictionaryPath:   strings.TrimSpace(os.Getenv("FRAGATA_RTSP_DICTIONARY")),
		STUNServers:          envList("FRAGATA_STUN_SERVERS", nil),
		MaxViewers:           envInt("FRAGATA_MAX_VIEWERS", 32),
		LiveIdleTimeout:      envDuration("FRAGATA_LIVE_IDLE_TIMEOUT", 30*time.Second),
		SFTP: SFTPConfig{
			Enabled:        envBool("FRAGATA_SFTP_ENABLED", false),
			Host:           strings.TrimSpace(os.Getenv("FRAGATA_SFTP_HOST")),
			Port:           envInt("FRAGATA_SFTP_PORT", 22),
			User:           strings.TrimSpace(os.Getenv("FRAGATA_SFTP_USER")),
			Password:       os.Getenv("FRAGATA_SFTP_PASSWORD"),
			PrivateKeyPath: strings.TrimSpace(os.Getenv("FRAGATA_SFTP_PRIVATE_KEY")),
			KnownHostsPath: strings.TrimSpace(os.Getenv("FRAGATA_SFTP_KNOWN_HOSTS")),
			RemoteBaseDir:  env("FRAGATA_SFTP_REMOTE_DIR", "/fragata"),
			Workers:        envInt("FRAGATA_SFTP_WORKERS", 1),
			Timeout:        envDuration("FRAGATA_SFTP_TIMEOUT", 30*time.Second),
			DeleteLocal:    envBool("FRAGATA_SFTP_DELETE_LOCAL", false),
		},
	}
	ffmpegPath, err := resolveExecutable(strings.TrimSpace(os.Getenv("FRAGATA_FFMPEG_PATH")), "ffmpeg")
	if err != nil {
		return Config{}, err
	}
	cfg.FFmpegPath = ffmpegPath

	if cfg.AdminUser == "" || cfg.AdminPassword == "" {
		cfg.AdminUser = ""
		cfg.AdminPassword = ""
	}
	if cfg.SegmentDuration < 10*time.Second {
		return Config{}, errors.New("FRAGATA_SEGMENT_DURATION debe ser al menos 10s")
	}
	if cfg.MaxViewers < 1 || cfg.MaxViewers > 256 {
		return Config{}, errors.New("FRAGATA_MAX_VIEWERS debe estar entre 1 y 256")
	}
	if cfg.LiveIdleTimeout < 10*time.Second || cfg.LiveIdleTimeout > 10*time.Minute {
		return Config{}, errors.New("FRAGATA_LIVE_IDLE_TIMEOUT debe estar entre 10s y 10m")
	}
	if cfg.RTSPConnectTimeout < 100*time.Millisecond || cfg.RTSPConnectTimeout > 10*time.Second {
		return Config{}, errors.New("FRAGATA_RTSP_CONNECT_TIMEOUT debe estar entre 100ms y 10s")
	}
	if cfg.RTSPCandidateTimeout < time.Second || cfg.RTSPCandidateTimeout > 30*time.Second {
		return Config{}, errors.New("FRAGATA_RTSP_CANDIDATE_TIMEOUT debe estar entre 1s y 30s")
	}
	if cfg.RTSPMaxCandidates < 1 || cfg.RTSPMaxCandidates > 512 {
		return Config{}, errors.New("FRAGATA_RTSP_MAX_CANDIDATES debe estar entre 1 y 512")
	}
	if cfg.SFTP.Workers < 1 || cfg.SFTP.Workers > 8 {
		return Config{}, errors.New("FRAGATA_SFTP_WORKERS debe estar entre 1 y 8")
	}
	if cfg.SFTP.Enabled {
		if cfg.SFTP.Host == "" || cfg.SFTP.User == "" {
			return Config{}, errors.New("SFTP habilitado sin host o usuario")
		}
		if cfg.SFTP.PrivateKeyPath == "" && cfg.SFTP.Password == "" {
			return Config{}, errors.New("SFTP habilitado sin llave privada ni contraseña")
		}
		if cfg.SFTP.KnownHostsPath == "" {
			return Config{}, errors.New("SFTP requiere FRAGATA_SFTP_KNOWN_HOSTS para validar el servidor")
		}
	}

	key, err := loadOrCreateKey(dataDir)
	if err != nil {
		return Config{}, err
	}
	cfg.SecretKey = key
	return cfg, nil
}

func resolveExecutable(configured, fallback string) (string, error) {
	if configured == "" {
		path, err := exec.LookPath(fallback)
		if err != nil {
			return "", nil
		}
		return path, nil
	}
	path, err := exec.LookPath(configured)
	if err != nil {
		return "", fmt.Errorf("FRAGATA_FFMPEG_PATH no apunta a un ejecutable válido: %w", err)
	}
	return path, nil
}

func (c Config) AuthEnabled() bool { return c.AdminUser != "" && c.AdminPassword != "" }

func (c Config) ValidateCameraIP(host string) error {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	ip := net.ParseIP(strings.Trim(name, "[]"))
	if ip == nil {
		return errors.New("la cámara debe indicarse mediante una dirección IP")
	}
	if !c.AllowPublicCameras && !isPrivateOrLocal(ip) {
		return errors.New("por seguridad solo se permiten IP privadas; habilite FRAGATA_ALLOW_PUBLIC_CAMERAS para cambiarlo")
	}
	return nil
}

func isPrivateOrLocal(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}

func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		if _, exists := os.LookupEnv(k); !exists {
			_ = os.Setenv(k, v)
		}
	}
	return s.Err()
}

func loadOrCreateKey(dataDir string) ([]byte, error) {
	if raw := strings.TrimSpace(os.Getenv("FRAGATA_SECRET_KEY")); raw != "" {
		if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
			return b, nil
		}
		if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
			return b, nil
		}
		return nil, errors.New("FRAGATA_SECRET_KEY debe ser base64 o hex de 32 bytes")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("crear data dir: %w", err)
	}
	path := filepath.Join(dataDir, "secret.key")
	if b, err := os.ReadFile(path); err == nil {
		decoded, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if derr != nil || len(decoded) != 32 {
			return nil, errors.New("data/secret.key no es válido")
		}
		return decoded, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generar clave aleatoria: %w", err)
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(b)+"\n"), 0o600); err != nil {
		return nil, err
	}
	return b, nil
}

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return def
}
func envBool(k string, def bool) bool {
	v, ok := os.LookupEnv(k)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
func envInt(k string, def int) int {
	v, ok := os.LookupEnv(k)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
func envDuration(k string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(k)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
func envList(k string, def []string) []string {
	v, ok := os.LookupEnv(k)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envIntList(k string, def []int) []int {
	v, ok := os.LookupEnv(k)
	if !ok || strings.TrimSpace(v) == "" {
		return append([]int(nil), def...)
	}
	seen := make(map[int]struct{})
	out := make([]int, 0)
	for _, raw := range strings.Split(v, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || n < 1 || n > 65535 {
			continue
		}
		if _, exists := seen[n]; exists {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	if len(out) == 0 {
		return append([]int(nil), def...)
	}
	return out
}
