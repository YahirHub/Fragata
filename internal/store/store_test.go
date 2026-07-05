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
