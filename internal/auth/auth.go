package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"fragata/internal/config"
	"fragata/internal/model"
	"fragata/internal/store"
)

const CookieName = "fragata_session"

type Manager struct {
	cfg   config.Config
	store *store.Store
}

func New(cfg config.Config, st *store.Store) *Manager { return &Manager{cfg: cfg, store: st} }
func (m *Manager) Enabled() bool                      { return m.cfg.AuthEnabled() }

func (m *Manager) Login(w http.ResponseWriter, username, password string) (model.Session, error) {
	if !m.Enabled() {
		return model.Session{}, errors.New("autenticación deshabilitada")
	}
	if subtle.ConstantTimeCompare([]byte(username), []byte(m.cfg.AdminUser)) != 1 || subtle.ConstantTimeCompare([]byte(password), []byte(m.cfg.AdminPassword)) != 1 {
		return model.Session{}, errors.New("credenciales inválidas")
	}
	token, err := randomToken(32)
	if err != nil {
		return model.Session{}, err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return model.Session{}, err
	}
	now := time.Now().UTC()
	sess := model.Session{ID: sessionKey(token), CSRFToken: csrf, CreatedAt: now, ExpiresAt: now.Add(m.cfg.SessionDuration)}
	if err := m.store.SaveSession(sess); err != nil {
		return model.Session{}, err
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: token, Path: "/", Expires: sess.ExpiresAt, MaxAge: int(m.cfg.SessionDuration.Seconds()), HttpOnly: true, Secure: m.cfg.SecureCookies, SameSite: http.SameSiteStrictMode})
	return sess, nil
}

func (m *Manager) Logout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(CookieName); err == nil {
		_ = m.store.DeleteSession(sessionKey(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: m.cfg.SecureCookies, SameSite: http.SameSiteStrictMode})
	return nil
}

func (m *Manager) Session(r *http.Request) (model.Session, bool) {
	if !m.Enabled() {
		return model.Session{ID: "anonymous", CSRFToken: "anonymous"}, true
	}
	c, err := r.Cookie(CookieName)
	if err != nil || strings.TrimSpace(c.Value) == "" {
		return model.Session{}, false
	}
	return m.store.Session(sessionKey(c.Value))
}

func (m *Manager) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := m.Session(r); !ok {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, "no autorizado", http.StatusUnauthorized)
			} else {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Manager) CheckCSRF(r *http.Request) bool {
	if !m.Enabled() {
		return true
	}
	sess, ok := m.Session(r)
	if !ok {
		return false
	}
	token := r.Header.Get("X-Fragata-CSRF")
	return subtle.ConstantTimeCompare([]byte(token), []byte(sess.CSRFToken)) == 1
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func sessionKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
