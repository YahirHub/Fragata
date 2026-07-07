package store

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fragata/internal/model"
)

func TestStorePersistsEncryptedPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	key := bytes.Repeat([]byte{7}, 32)
	s, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := s.SaveCamera(model.Camera{ID: "cam1", Name: "Test", Password: "secreto", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	raw, err := osReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("secreto")) {
		t.Fatal("la contraseña quedó en texto plano")
	}
	s2, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	cam, ok := s2.Camera("cam1")
	if !ok || cam.Password != "secreto" {
		t.Fatalf("cámara no recuperada: %#v", cam)
	}
}

func osReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func TestStorePersistsEncryptedSFTPPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	key := bytes.Repeat([]byte{8}, 32)
	s, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	profile := model.SFTPProfile{
		ID: "backup-1", Name: "Principal", Enabled: true, Host: "backup.example", Port: 22,
		User: "fragata", Password: "sftp-secreto", KnownHostsPath: "/tmp/known_hosts",
		RemoteBaseDir: "/fragata", TimeoutSeconds: 30, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.SaveSFTPProfile(profile); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("sftp-secreto")) {
		t.Fatal("la contraseña SFTP quedó en texto plano")
	}
	s2, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	restored, ok := s2.SFTPProfile("backup-1")
	if !ok || restored.Password != "sftp-secreto" {
		t.Fatalf("perfil SFTP no recuperado: %#v", restored)
	}
}

func TestStorePersistsDetectionEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	key := bytes.Repeat([]byte{9}, 32)
	state, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	event := model.DetectionEvent{
		ID: "event-1", CameraID: "cam-1", CameraName: "Entrada", Type: "person",
		Source: "onvif", ONVIFTopic: "tns1:RuleEngine/PeopleDetector/Person",
		RecordingPath: "entrada/2026/07/05/12-00-00.000.mkv", RecordingStartedAt: now.Add(-15 * time.Second),
		RecordingOffsetMillis: 15000, CreatedAt: now,
	}
	if err := state.SaveDetectionEvent(event); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	events := reopened.DetectionEvents("cam-1", 10)
	if len(events) != 1 || events[0].ID != event.ID || events[0].Source != "onvif" ||
		events[0].ONVIFTopic != event.ONVIFTopic || events[0].RecordingPath != event.RecordingPath ||
		events[0].RecordingOffsetMillis != event.RecordingOffsetMillis {
		t.Fatalf("evento ONVIF no recuperado: %#v", events)
	}
}

func TestStoreMigratesLegacyDetectorFieldsWithoutDeletingHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	legacy := `{
  "version": 3,
  "cameras": {
    "cam-1": {
      "id": "cam-1",
      "name": "Entrada",
      "host": "192.168.1.20",
      "rtsp_url": "rtsp://192.168.1.20/live",
      "folder_name": "entrada",
      "enabled": true,
      "record": true,
      "segment_duration_seconds": 300,
      "detection_enabled": true,
      "snapshot_url": "http://192.168.1.20/snapshot.jpg",
      "detect_motion": true,
      "motion_sensitivity": 50
    }
  },
  "events": {
    "old-event": {
      "id": "old-event",
      "camera_id": "cam-1",
      "camera_name": "Entrada",
      "type": "motion",
      "snapshot_path": "events/entrada/old.jpg",
      "created_at": "2026-07-01T12:00:00Z"
    }
  }
}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := Open(path, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	camera, ok := state.Camera("cam-1")
	if !ok || !camera.DetectionEnabled {
		t.Fatalf("native ONVIF event flag was not preserved: %#v", camera)
	}
	events := state.DetectionEvents("cam-1", 10)
	if len(events) != 1 || events[0].SnapshotPath != "events/entrada/old.jpg" {
		t.Fatalf("historical event was not preserved: %#v", events)
	}
	rewritten, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(rewritten)
	if strings.Contains(text, "snapshot_url") || strings.Contains(text, "detect_motion") || strings.Contains(text, "motion_sensitivity") {
		t.Fatalf("obsolete detector fields survived migration: %s", text)
	}
	if !strings.Contains(text, `"version": 4`) {
		t.Fatalf("state version was not migrated: %s", text)
	}
}
