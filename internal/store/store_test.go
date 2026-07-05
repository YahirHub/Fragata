package store

import (
	"bytes"
	"os"
	"path/filepath"
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
		ID: "event-1", CameraID: "cam-1", CameraName: "Entrada", Type: "person", Confidence: .81,
		SnapshotPath: "events/entrada/event.jpg", SnapshotWidth: 2304, SnapshotHeight: 1296,
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
	if len(events) != 1 || events[0].ID != event.ID || events[0].SnapshotPath != event.SnapshotPath ||
		events[0].RecordingPath != event.RecordingPath || events[0].RecordingOffsetMillis != event.RecordingOffsetMillis ||
		events[0].SnapshotWidth != event.SnapshotWidth || events[0].SnapshotHeight != event.SnapshotHeight {
		t.Fatalf("evento no recuperado: %#v", events)
	}
}

func TestStorePersistsEncryptedSnapshotURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	key := bytes.Repeat([]byte{10}, 32)
	state, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	snapshotURL := "http://192.168.1.30/snapshot.jpg?token=snapshot-secret"
	if err := state.SaveCamera(model.Camera{ID: "cam-snapshot", Name: "Entrada", SnapshotURL: snapshotURL, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(snapshotURL)) || bytes.Contains(raw, []byte("snapshot-secret")) {
		t.Fatal("la URL de snapshot quedó en texto plano")
	}
	reopened, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	camera, ok := reopened.Camera("cam-snapshot")
	if !ok || camera.SnapshotURL != snapshotURL {
		t.Fatalf("snapshot no recuperado: %#v", camera)
	}
}
