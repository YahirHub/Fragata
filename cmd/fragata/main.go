package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"fragata/internal/auth"
	"fragata/internal/camera"
	"fragata/internal/config"
	"fragata/internal/httpapi"
	"fragata/internal/live"
	"fragata/internal/logging"
	"fragata/internal/recording"
	"fragata/internal/retention"
	"fragata/internal/store"
	"fragata/internal/upload"
)

var version = "dev"

func main() {
	envPath := flag.String("env", ".env", "ruta del archivo .env")
	showVersion := flag.Bool("version", false, "mostrar versión")
	healthcheckURL := flag.String("healthcheck", "", "comprobar un endpoint /healthz y terminar")
	flag.Parse()
	if *showVersion {
		fmt.Println("Fragata", version)
		return
	}
	if *healthcheckURL != "" {
		if err := runHealthcheck(*healthcheckURL); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	bootstrapLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load(*envPath)
	if err != nil {
		bootstrapLogger.Error("configuration error", "error", err)
		os.Exit(1)
	}
	logWriter, err := logging.Open(cfg.LogPath, cfg.LogMaxSize)
	if err != nil {
		bootstrapLogger.Error("log file error", "error", err)
		os.Exit(1)
	}
	defer logWriter.Close()
	logger := slog.New(slog.NewTextHandler(logging.MultiOutput(logWriter), &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := os.MkdirAll(cfg.RecordingsDir, 0o750); err != nil {
		logger.Error("recordings directory error", "error", err)
		os.Exit(1)
	}
	if cfg.FFmpegPath != "" {
		logger.Info("FFmpeg detected for browser-compatible live view", "path", cfg.FFmpegPath)
	} else {
		logger.Info("FFmpeg not detected; live view will use native WebRTC-compatible streams when available")
	}
	if recovered, err := recording.RecoverPartials(cfg.RecordingsDir); err != nil {
		logger.Warn("partial recording recovery failed", "error", err)
	} else if len(recovered) > 0 {
		logger.Info("partial recordings recovered", "count", len(recovered))
	}

	state, err := store.Open(filepath.Join(cfg.DataDir, "state.json"), cfg.SecretKey)
	if err != nil {
		logger.Error("state store error", "error", err)
		os.Exit(1)
	}
	_ = state.PruneSessions(time.Now())

	authManager := auth.New(cfg, state)
	uploader := upload.New(cfg.SFTP, state, func(err error) { logger.Warn("upload error", "error", err) })
	cameraManager := camera.NewManager(cfg, state, uploader, logger)
	liveManager := live.New(cfg.STUNServers, cfg.MaxViewers)
	api, err := httpapi.New(cfg, authManager, cameraManager, liveManager, uploader, state, logger)
	if err != nil {
		logger.Error("HTTP server initialization error", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cameraManager.Start()
	go uploader.Run(ctx)
	go (retention.Cleaner{BaseDir: cfg.RecordingsDir, EventsDir: filepath.Join(cfg.DataDir, "events"), Store: state, Logger: logger, Interval: cfg.RetentionInterval}).Run(ctx)
	go pruneSessions(ctx, state, logger)

	serverErr := make(chan error, 1)
	go func() { serverErr <- api.ListenAndServe() }()
	select {
	case <-ctx.Done():
		logger.Info("shutting down Fragata")
	case err := <-serverErr:
		if err != nil {
			logger.Error("HTTP server stopped", "error", err)
		}
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	_ = api.Shutdown(shutdownCtx)
	liveManager.Close()
	cameraManager.Close()
	logger.Info("Fragata stopped")
}

func pruneSessions(ctx context.Context, state *store.Store, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := state.PruneSessions(now); err != nil {
				logger.Warn("session cleanup failed", "error", err)
			}
		}
	}
}

func runHealthcheck(endpoint string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: HTTP %d", resp.StatusCode)
	}
	return nil
}
