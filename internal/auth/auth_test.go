package auth

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fragata/internal/config"
	"fragata/internal/store"
)

func TestPersistentSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	key := bytes.Repeat([]byte{9}, 32)
	state, err := store.Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{AdminUser: "admin", AdminPassword: "secret", SessionDuration: time.Hour}
	manager := New(cfg, state)
	recorder := httptest.NewRecorder()
	session, err := manager.Login(recorder, "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	cookie := recorder.Result().Cookies()[0]
	rawState, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rawState, []byte(cookie.Value)) {
		t.Fatal("el token de sesión quedó almacenado en texto plano")
	}

	reopened, err := store.Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(cookie)
	persisted, ok := New(cfg, reopened).Session(request)
	if !ok || persisted.ID != session.ID || persisted.CSRFToken == "" {
		t.Fatalf("sesión no persistió: %#v, ok=%v", persisted, ok)
	}
}

func TestDisabledAuthUsesAnonymousSession(t *testing.T) {
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	manager := New(config.Config{}, state)
	request := httptest.NewRequest("GET", "/", nil)
	session, ok := manager.Session(request)
	if !ok || session.ID != "anonymous" || !manager.CheckCSRF(request) {
		t.Fatalf("sesión anónima inválida: %#v", session)
	}
}
