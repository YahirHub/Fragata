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
	"strings"
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
	Version      int                             `json:"version"`
	Cameras      map[string]encryptedCamera      `json:"cameras"`
	Sessions     map[string]model.Session        `json:"sessions"`
	UploadQueue  map[string]model.UploadJob      `json:"upload_queue"`
	SFTPProfiles map[string]encryptedSFTPProfile `json:"sftp_profiles"`
	Retention    model.RetentionPolicy           `json:"retention"`
	Events       map[string]model.DetectionEvent `json:"events"`
}

type encryptedCamera struct {
	model.Camera
	PasswordCipher    string `json:"password_cipher,omitempty"`
	SnapshotURLCipher string `json:"snapshot_url_cipher,omitempty"`
}

type encryptedSFTPProfile struct {
	model.SFTPProfile
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
	s.data = model.State{Version: 3, Cameras: map[string]model.Camera{}, Sessions: map[string]model.Session{}, UploadQueue: map[string]model.UploadJob{}, SFTPProfiles: map[string]model.SFTPProfile{}, Retention: model.RetentionPolicy{Value: 30, Unit: "days"}, Events: map[string]model.DetectionEvent{}}
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
	if d.SFTPProfiles == nil {
		d.SFTPProfiles = map[string]encryptedSFTPProfile{}
	}
	if d.Events == nil {
		d.Events = map[string]model.DetectionEvent{}
	}
	state := model.State{Version: d.Version, Cameras: map[string]model.Camera{}, Sessions: d.Sessions, UploadQueue: d.UploadQueue, SFTPProfiles: map[string]model.SFTPProfile{}, Retention: d.Retention, Events: d.Events}
	if state.Version < 3 {
		state.Version = 3
	}
	if state.Retention.Value < 1 || (state.Retention.Unit != "days" && state.Retention.Unit != "months" && state.Retention.Unit != "years") {
		state.Retention = model.RetentionPolicy{Value: 30, Unit: "days"}
	}
	for id, ec := range d.Cameras {
		c := ec.Camera
		if ec.PasswordCipher != "" {
			plain, err := s.decrypt(ec.PasswordCipher)
			if err != nil {
				return fmt.Errorf("descifrar contraseña de cámara %s: %w", id, err)
			}
			c.Password = plain
		}
		if ec.SnapshotURLCipher != "" {
			plain, err := s.decrypt(ec.SnapshotURLCipher)
			if err != nil {
				return fmt.Errorf("descifrar snapshot de cámara %s: %w", id, err)
			}
			c.SnapshotURL = plain
		}
		state.Cameras[id] = c
	}
	for id, encrypted := range d.SFTPProfiles {
		profile := encrypted.SFTPProfile
		if encrypted.PasswordCipher != "" {
			plain, err := s.decrypt(encrypted.PasswordCipher)
			if err != nil {
				return fmt.Errorf("descifrar perfil SFTP %s: %w", id, err)
			}
			profile.Password = plain
		}
		state.SFTPProfiles[id] = profile
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

func (s *Store) SFTPProfiles() []model.SFTPProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.SFTPProfile, 0, len(s.data.SFTPProfiles))
	for _, profile := range s.data.SFTPProfiles {
		out = append(out, profile)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func (s *Store) SFTPProfile(id string) (model.SFTPProfile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile, ok := s.data.SFTPProfiles[id]
	return profile, ok
}

func (s *Store) SaveSFTPProfile(profile model.SFTPProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.data.SFTPProfiles[profile.ID]
	s.data.SFTPProfiles[profile.ID] = profile
	if err := s.persistLocked(); err != nil {
		if existed {
			s.data.SFTPProfiles[profile.ID] = previous
		} else {
			delete(s.data.SFTPProfiles, profile.ID)
		}
		return err
	}
	return nil
}

func (s *Store) DeleteSFTPProfile(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.data.SFTPProfiles[id]
	delete(s.data.SFTPProfiles, id)
	if err := s.persistLocked(); err != nil {
		if existed {
			s.data.SFTPProfiles[id] = previous
		}
		return err
	}
	return nil
}

func (s *Store) Retention() model.RetentionPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Retention
}

func (s *Store) SaveRetention(policy model.RetentionPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.data.Retention
	s.data.Retention = policy
	if err := s.persistLocked(); err != nil {
		s.data.Retention = previous
		return err
	}
	return nil
}

func (s *Store) SaveDetectionEvent(event model.DetectionEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.data.Events[event.ID]
	s.data.Events[event.ID] = event
	if err := s.persistLocked(); err != nil {
		if existed {
			s.data.Events[event.ID] = previous
		} else {
			delete(s.data.Events, event.ID)
		}
		return err
	}
	return nil
}

func (s *Store) DetectionEvents(cameraID string, limit int) []model.DetectionEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit < 1 || limit > 1000 {
		limit = 200
	}
	out := make([]model.DetectionEvent, 0, len(s.data.Events))
	for _, event := range s.data.Events {
		if cameraID == "" || event.CameraID == cameraID {
			out = append(out, event)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Store) DetectionEventsBetween(cameraID string, start, end time.Time, limit int) []model.DetectionEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit < 1 || limit > 5000 {
		limit = 5000
	}
	out := make([]model.DetectionEvent, 0)
	for _, event := range s.data.Events {
		if cameraID != "" && event.CameraID != cameraID {
			continue
		}
		if event.CreatedAt.Before(start) || !event.CreatedAt.Before(end) {
			continue
		}
		out = append(out, event)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Store) DetectionEvent(id string) (model.DetectionEvent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	event, ok := s.data.Events[id]
	return event, ok
}

func (s *Store) DetectionEventsBefore(cutoff time.Time) []model.DetectionEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.DetectionEvent, 0)
	for _, event := range s.data.Events {
		if event.CreatedAt.Before(cutoff) {
			out = append(out, event)
		}
	}
	return out
}

func (s *Store) DeleteDetectionEvents(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := make(map[string]model.DetectionEvent, len(ids))
	for _, id := range ids {
		if event, ok := s.data.Events[id]; ok {
			removed[id] = event
			delete(s.data.Events, id)
		}
	}
	if len(removed) == 0 {
		return nil
	}
	if err := s.persistLocked(); err != nil {
		for id, event := range removed {
			s.data.Events[id] = event
		}
		return err
	}
	return nil
}

func (s *Store) persistLocked() error {
	d := diskState{Version: s.data.Version, Cameras: map[string]encryptedCamera{}, Sessions: s.data.Sessions, UploadQueue: s.data.UploadQueue, SFTPProfiles: map[string]encryptedSFTPProfile{}, Retention: s.data.Retention, Events: s.data.Events}
	for id, c := range s.data.Cameras {
		ec := encryptedCamera{Camera: c}
		ec.Password = ""
		ec.SnapshotURL = ""
		if c.Password != "" {
			enc, err := s.encrypt(c.Password)
			if err != nil {
				return err
			}
			ec.PasswordCipher = enc
		}
		if c.SnapshotURL != "" {
			enc, err := s.encrypt(c.SnapshotURL)
			if err != nil {
				return err
			}
			ec.SnapshotURLCipher = enc
		}
		d.Cameras[id] = ec
	}
	for id, profile := range s.data.SFTPProfiles {
		encrypted := encryptedSFTPProfile{SFTPProfile: profile}
		encrypted.Password = ""
		if profile.Password != "" {
			value, err := s.encrypt(profile.Password)
			if err != nil {
				return err
			}
			encrypted.PasswordCipher = value
		}
		d.SFTPProfiles[id] = encrypted
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
