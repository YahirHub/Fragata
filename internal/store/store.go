package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"fragata/internal/model"
)

type Store struct {
	mu   sync.RWMutex
	path string
	key  []byte
	data model.State
}

type diskState struct {
	Version     int                        `json:"version"`
	Cameras     map[string]encryptedCamera `json:"cameras"`
	Sessions    map[string]model.Session   `json:"sessions"`
	UploadQueue map[string]model.UploadJob `json:"upload_queue"`
}

type encryptedCamera struct {
	model.Camera
	PasswordCipher string `json:"password_cipher,omitempty"`
}

func Open(path string, key []byte) (*Store, error) {
	if len(key) != 32 {
		return nil, errors.New("la clave del store debe tener 32 bytes")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: path, key: append([]byte(nil), key...)}
	s.data = model.State{Version: 1, Cameras: map[string]model.Camera{}, Sessions: map[string]model.Session{}, UploadQueue: map[string]model.UploadJob{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s.persistLocked()
	}
	if err != nil {
		return fmt.Errorf("leer estado: %w", err)
	}
	var d diskState
	if err := json.Unmarshal(b, &d); err != nil {
		return fmt.Errorf("decodificar estado: %w", err)
	}
	if d.Cameras == nil {
		d.Cameras = map[string]encryptedCamera{}
	}
	if d.Sessions == nil {
		d.Sessions = map[string]model.Session{}
	}
	if d.UploadQueue == nil {
		d.UploadQueue = map[string]model.UploadJob{}
	}
	state := model.State{Version: d.Version, Cameras: map[string]model.Camera{}, Sessions: d.Sessions, UploadQueue: d.UploadQueue}
	if state.Version == 0 {
		state.Version = 1
	}
	for id, ec := range d.Cameras {
		c := ec.Camera
		if ec.PasswordCipher != "" {
			plain, err := s.decrypt(ec.PasswordCipher)
			if err != nil {
				return fmt.Errorf("descifrar cámara %s: %w", id, err)
			}
			c.Password = plain
		}
		state.Cameras[id] = c
	}
	s.data = state
	return nil
}

func (s *Store) Cameras() []model.Camera {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Camera, 0, len(s.data.Cameras))
	for _, c := range s.data.Cameras {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *Store) Camera(id string) (model.Camera, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.data.Cameras[id]
	return c, ok
}

func (s *Store) SaveCamera(c model.Camera) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.data.Cameras[c.ID]
	s.data.Cameras[c.ID] = c
	if err := s.persistLocked(); err != nil {
		if existed {
			s.data.Cameras[c.ID] = previous
		} else {
			delete(s.data.Cameras, c.ID)
		}
		return err
	}
	return nil
}

func (s *Store) DeleteCamera(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.data.Cameras[id]
	delete(s.data.Cameras, id)
	if err := s.persistLocked(); err != nil {
		if existed {
			s.data.Cameras[id] = previous
		}
		return err
	}
	return nil
}

func (s *Store) SaveSession(sess model.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.data.Sessions[sess.ID]
	s.data.Sessions[sess.ID] = sess
	if err := s.persistLocked(); err != nil {
		if existed {
			s.data.Sessions[sess.ID] = previous
		} else {
			delete(s.data.Sessions, sess.ID)
		}
		return err
	}
	return nil
}

func (s *Store) Session(id string) (model.Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.data.Sessions[id]
	if ok && time.Now().After(sess.ExpiresAt) {
		return model.Session{}, false
	}
	return sess, ok
}

func (s *Store) DeleteSession(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.data.Sessions[id]
	delete(s.data.Sessions, id)
	if err := s.persistLocked(); err != nil {
		if existed {
			s.data.Sessions[id] = previous
		}
		return err
	}
	return nil
}

func (s *Store) PruneSessions(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := make(map[string]model.Session)
	for id, sess := range s.data.Sessions {
		if !sess.ExpiresAt.After(now) {
			removed[id] = sess
			delete(s.data.Sessions, id)
		}
	}
	if len(removed) == 0 {
		return nil
	}
	if err := s.persistLocked(); err != nil {
		for id, sess := range removed {
			s.data.Sessions[id] = sess
		}
		return err
	}
	return nil
}

func (s *Store) SaveUploadJob(job model.UploadJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.data.UploadQueue[job.ID]
	s.data.UploadQueue[job.ID] = job
	if err := s.persistLocked(); err != nil {
		if existed {
			s.data.UploadQueue[job.ID] = previous
		} else {
			delete(s.data.UploadQueue, job.ID)
		}
		return err
	}
	return nil
}

func (s *Store) DeleteUploadJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.data.UploadQueue[id]
	delete(s.data.UploadQueue, id)
	if err := s.persistLocked(); err != nil {
		if existed {
			s.data.UploadQueue[id] = previous
		}
		return err
	}
	return nil
}

func (s *Store) UploadJobs() []model.UploadJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.UploadJob, 0, len(s.data.UploadQueue))
	for _, j := range s.data.UploadQueue {
		out = append(out, j)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *Store) persistLocked() error {
	d := diskState{Version: s.data.Version, Cameras: map[string]encryptedCamera{}, Sessions: s.data.Sessions, UploadQueue: s.data.UploadQueue}
	for id, c := range s.data.Cameras {
		ec := encryptedCamera{Camera: c}
		ec.Password = ""
		if c.Password != "" {
			enc, err := s.encrypt(c.Password)
			if err != nil {
				return err
			}
			ec.PasswordCipher = enc
		}
		d.Cameras[id] = ec
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	f, err := os.OpenFile(tmp, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(s.path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func (s *Store) encrypt(plain string) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.RawStdEncoding.EncodeToString(out), nil
}

func (s *Store) decrypt(encoded string) (string, error) {
	b, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(b) < gcm.NonceSize() {
		return "", errors.New("ciphertext corto")
	}
	nonce, ciphertext := b[:gcm.NonceSize()], b[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
