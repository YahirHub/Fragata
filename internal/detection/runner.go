package detection

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fragata/internal/model"
	"fragata/internal/onvif"
	"fragata/internal/store"
)

type Runner struct {
	Camera            model.Camera
	DataDir           string
	Store             *store.Store
	Logger            *slog.Logger
	OnStatus          func(state string, score float64, eventType string, at time.Time)
	RecordingSnapshot func(at time.Time) (path string, startedAt time.Time, ok bool)
}

func (r Runner) Run(ctx context.Context) error {
	camera := normalizeDetectionCamera(r.Camera)
	if !camera.DetectionEnabled {
		return nil
	}
	if strings.TrimSpace(camera.SnapshotURL) == "" {
		return errors.New("la detección requiere una URL de snapshot ONVIF o manual")
	}
	if r.Store == nil {
		return errors.New("store de detección no configurado")
	}
	if r.Logger == nil {
		r.Logger = slog.Default()
	}
	interval := time.Duration(camera.DetectionIntervalSecs) * time.Second
	client := onvif.NewClient(10*time.Second, camera.Username, camera.Password, false)
	motion := &MotionDetector{}
	person := NewPersonDetector()
	lastMotionEvent := time.Time{}
	lastPersonEvent := time.Time{}
	lastErrorLog := time.Time{}

	r.updateStatus("initializing", 0, "", time.Time{})
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	first := true
	for {
		if !first {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
		first = false
		if err := r.analyzeOnce(ctx, client, motion, person, camera, &lastMotionEvent, &lastPersonEvent); err != nil {
			r.updateStatus("error", 0, "", time.Time{})
			now := time.Now()
			if lastErrorLog.IsZero() || now.Sub(lastErrorLog) >= time.Minute {
				r.Logger.Warn("detection snapshot failed", "camera_id", camera.ID, "error", model.RedactSecrets(err.Error()))
				lastErrorLog = now
			}
			continue
		}
		lastErrorLog = time.Time{}
	}
}

func (r Runner) analyzeOnce(ctx context.Context, client *onvif.Client, motion *MotionDetector, person *PersonDetector, camera model.Camera, lastMotionEvent, lastPersonEvent *time.Time) error {
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, _, err := client.FetchSnapshot(requestCtx, camera.SnapshotURL, 8<<20)
	if err != nil {
		return err
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("leer snapshot: %w", err)
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 10000 || config.Height > 10000 || int64(config.Width)*int64(config.Height) > 32_000_000 {
		return fmt.Errorf("snapshot con dimensiones no permitidas: %dx%d", config.Width, config.Height)
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("decodificar snapshot: %w", err)
	}
	zone := zoneRectangle(decoded.Bounds(), camera.DetectionZone)
	motionResult := motion.Analyze(decoded, zone, camera.MotionSensitivity)
	r.updateStatus("watching", motionResult.Score, "", time.Time{})
	if !motionResult.Detected {
		return nil
	}

	now := time.Now().UTC()
	cooldown := time.Duration(camera.DetectionCooldownSecs) * time.Second
	personFound := false
	bestConfidence := 0.0
	if camera.DetectPerson {
		for _, found := range person.Detect(decoded, zone, camera.PersonConfidence) {
			personFound = true
			if found.Confidence > bestConfidence {
				bestConfidence = found.Confidence
			}
		}
	}

	shouldPerson := personFound && (lastPersonEvent.IsZero() || now.Sub(*lastPersonEvent) >= cooldown)
	// A confirmed person supersedes the generic motion event to avoid duplicate
	// entries and duplicate state writes for the same snapshot.
	shouldMotion := camera.DetectMotion && !personFound && (lastMotionEvent.IsZero() || now.Sub(*lastMotionEvent) >= cooldown)
	if !shouldPerson && !shouldMotion {
		return nil
	}
	snapshotPath, err := r.saveSnapshot(camera, raw, format, now)
	if err != nil {
		return err
	}
	if shouldPerson {
		event, err := r.saveEvent(camera, "person", bestConfidence, motionResult.Score, snapshotPath, config.Width, config.Height, now)
		if err != nil {
			return err
		}
		*lastPersonEvent = now
		r.updateStatus("event", motionResult.Score, event.Type, now)
	}
	if shouldMotion {
		event, err := r.saveEvent(camera, "motion", 0, motionResult.Score, snapshotPath, config.Width, config.Height, now)
		if err != nil {
			return err
		}
		*lastMotionEvent = now
		if !shouldPerson {
			r.updateStatus("event", motionResult.Score, event.Type, now)
		}
	}
	return nil
}

func (r Runner) saveSnapshot(camera model.Camera, raw []byte, format string, now time.Time) (string, error) {
	id, err := eventID()
	if err != nil {
		return "", err
	}
	extension := ".jpg"
	if strings.EqualFold(format, "png") {
		extension = ".png"
	}
	relative := filepath.Join("events", camera.FolderName, now.Format("2006"), now.Format("01"), now.Format("02"), id+extension)
	absolute := filepath.Join(r.DataDir, relative)
	if err := os.MkdirAll(filepath.Dir(absolute), 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(absolute, raw, 0o640); err != nil {
		return "", err
	}
	return filepath.ToSlash(relative), nil
}

func (r Runner) saveEvent(camera model.Camera, eventType string, confidence, motionScore float64, snapshotPath string, snapshotWidth, snapshotHeight int, now time.Time) (model.DetectionEvent, error) {
	id, err := eventID()
	if err != nil {
		return model.DetectionEvent{}, err
	}
	event := model.DetectionEvent{
		ID: id, CameraID: camera.ID, CameraName: camera.Name, Type: eventType,
		Confidence: confidence, MotionScore: motionScore, SnapshotPath: snapshotPath,
		SnapshotWidth: snapshotWidth, SnapshotHeight: snapshotHeight, CreatedAt: now,
	}
	if r.RecordingSnapshot != nil {
		if path, startedAt, ok := r.RecordingSnapshot(now); ok {
			event.RecordingPath = strings.TrimSuffix(filepath.ToSlash(path), ".partial")
			event.RecordingStartedAt = startedAt.UTC()
			offset := now.Sub(startedAt)
			if offset < 0 {
				offset = 0
			}
			event.RecordingOffsetMillis = offset.Milliseconds()
		}
	}
	if err := r.Store.SaveDetectionEvent(event); err != nil {
		return model.DetectionEvent{}, err
	}
	r.Logger.Info("detection event", "camera_id", camera.ID, "type", eventType, "confidence", confidence, "motion_score", motionScore)
	return event, nil
}

func (r Runner) updateStatus(state string, score float64, eventType string, at time.Time) {
	if r.OnStatus != nil {
		r.OnStatus(state, score, eventType, at)
	}
}

func normalizeDetectionCamera(camera model.Camera) model.Camera {
	if camera.MotionSensitivity < 1 || camera.MotionSensitivity > 100 {
		camera.MotionSensitivity = 65
	}
	if camera.DetectionIntervalSecs < 1 || camera.DetectionIntervalSecs > 60 {
		camera.DetectionIntervalSecs = 1
	}
	if camera.PersonConfidence < 40 || camera.PersonConfidence > 95 {
		camera.PersonConfidence = 55
	}
	if camera.DetectionCooldownSecs < 1 || camera.DetectionCooldownSecs > 3600 {
		camera.DetectionCooldownSecs = 30
	}
	camera.DetectionZone = camera.DetectionZone.Normalized()
	return camera
}

func zoneRectangle(bounds image.Rectangle, zone model.DetectionZone) image.Rectangle {
	zone = zone.Normalized()
	return image.Rect(
		bounds.Min.X+bounds.Dx()*zone.X/100,
		bounds.Min.Y+bounds.Dy()*zone.Y/100,
		bounds.Min.X+bounds.Dx()*(zone.X+zone.Width)/100,
		bounds.Min.Y+bounds.Dy()*(zone.Y+zone.Height)/100,
	).Intersect(bounds)
}

func eventID() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
