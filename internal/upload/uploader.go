package upload

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fragata/internal/config"
	"fragata/internal/model"
	"fragata/internal/store"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const EnvironmentProfileID = "__environment__"

type Uploader struct {
	legacy  config.SFTPConfig
	store   *store.Store
	claimed map[string]struct{}
	mu      sync.Mutex
	onError func(error)
	workers int
}

func New(legacy config.SFTPConfig, state *store.Store, onError func(error)) *Uploader {
	workers := legacy.Workers
	if workers < 1 {
		workers = 1
	}
	return &Uploader{legacy: legacy, store: state, claimed: make(map[string]struct{}), onError: onError, workers: workers}
}

func (u *Uploader) Enabled(profileID string) bool {
	if u == nil {
		return false
	}
	_, ok := u.profileConfig(profileID)
	return ok
}

func (u *Uploader) Enqueue(cameraID, profileID, localPath, relativeRemotePath string) error {
	cfg, ok := u.profileConfig(profileID)
	if !ok {
		return errors.New("la cámara no tiene un servidor SFTP habilitado")
	}
	resolvedID := profileID
	if resolvedID == "" && u.legacy.Enabled {
		resolvedID = EnvironmentProfileID
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return err
	}
	id, err := randomID()
	if err != nil {
		return err
	}
	remote := path.Join(cfg.RemoteBaseDir, filepath.ToSlash(relativeRemotePath))
	job := model.UploadJob{
		ID: id, CameraID: cameraID, SFTPProfileID: resolvedID, LocalPath: localPath, RemotePath: remote,
		Size: info.Size(), NextAttempt: time.Now(), CreatedAt: time.Now().UTC(),
	}
	return u.store.SaveUploadJob(job)
}

func (u *Uploader) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < u.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u.worker(ctx)
		}()
	}
	<-ctx.Done()
	wg.Wait()
}

func (u *Uploader) worker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if job, ok := u.nextJob(); ok {
			err := u.process(ctx, job)
			u.release(job.ID)
			if err != nil && ctx.Err() == nil {
				u.fail(job, err)
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (u *Uploader) nextJob() (model.UploadJob, bool) {
	now := time.Now()
	jobs := u.store.UploadJobs()
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, job := range jobs {
		if job.NextAttempt.After(now) {
			continue
		}
		if _, busy := u.claimed[job.ID]; busy {
			continue
		}
		u.claimed[job.ID] = struct{}{}
		return job, true
	}
	return model.UploadJob{}, false
}

func (u *Uploader) release(id string) {
	u.mu.Lock()
	delete(u.claimed, id)
	u.mu.Unlock()
}

func (u *Uploader) fail(job model.UploadJob, err error) {
	job.Attempts++
	job.LastError = truncate(err.Error(), 800)
	job.NextAttempt = time.Now().Add(backoff(job.Attempts))
	_ = u.store.SaveUploadJob(job)
	if u.onError != nil {
		u.onError(fmt.Errorf("subida SFTP %s: %w", filepath.Base(job.LocalPath), err))
	}
}

func (u *Uploader) process(ctx context.Context, job model.UploadJob) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	cfg, ok := u.profileConfig(job.SFTPProfileID)
	if !ok {
		return errors.New("el perfil SFTP de la cola ya no existe o está deshabilitado")
	}
	local, err := os.Open(job.LocalPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return u.store.DeleteUploadJob(job.ID)
		}
		return err
	}
	defer local.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, local); err != nil {
		return err
	}
	job.SHA256 = hex.EncodeToString(hash.Sum(nil))
	if _, err := local.Seek(0, io.SeekStart); err != nil {
		return err
	}

	sshClient, sftpClient, err := connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer sshClient.Close()
	defer sftpClient.Close()

	if remoteMatches(sftpClient, job) {
		return u.complete(job, cfg.DeleteLocal)
	}
	if err := sftpClient.MkdirAll(path.Dir(job.RemotePath)); err != nil {
		return fmt.Errorf("crear directorio remoto: %w", err)
	}
	partial := job.RemotePath + ".part"
	_ = sftpClient.Remove(partial)
	remote, err := sftpClient.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY)
	if err != nil {
		return fmt.Errorf("crear archivo remoto: %w", err)
	}
	written, copyErr := io.Copy(remote, local)
	closeErr := remote.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != job.Size {
		return fmt.Errorf("tamaño enviado %d, esperado %d", written, job.Size)
	}
	stat, err := sftpClient.Stat(partial)
	if err != nil || stat.Size() != job.Size {
		return errors.New("el servidor remoto no confirmó el tamaño del archivo")
	}
	_ = sftpClient.Remove(job.RemotePath)
	if err := sftpClient.Rename(partial, job.RemotePath); err != nil {
		return fmt.Errorf("finalizar archivo remoto: %w", err)
	}
	checksumPath := job.RemotePath + ".sha256"
	checksumPartial := checksumPath + ".part"
	_ = sftpClient.Remove(checksumPartial)
	checksum, err := sftpClient.Create(checksumPartial)
	if err != nil {
		return fmt.Errorf("crear checksum remoto: %w", err)
	}
	if _, err := io.WriteString(checksum, job.SHA256+"  "+path.Base(job.RemotePath)+"\n"); err != nil {
		_ = checksum.Close()
		return fmt.Errorf("escribir checksum remoto: %w", err)
	}
	if err := checksum.Close(); err != nil {
		return fmt.Errorf("cerrar checksum remoto: %w", err)
	}
	_ = sftpClient.Remove(checksumPath)
	if err := sftpClient.Rename(checksumPartial, checksumPath); err != nil {
		return fmt.Errorf("finalizar checksum remoto: %w", err)
	}
	return u.complete(job, cfg.DeleteLocal)
}

func (u *Uploader) Test(ctx context.Context, profile model.SFTPProfile) error {
	cfg := profileToConfig(profile)
	if err := validateConfig(cfg); err != nil {
		return err
	}
	sshClient, client, err := connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer sshClient.Close()
	defer client.Close()
	if err := client.MkdirAll(cfg.RemoteBaseDir); err != nil {
		return fmt.Errorf("crear o comprobar directorio remoto: %w", err)
	}
	return nil
}

func remoteMatches(client *sftp.Client, job model.UploadJob) bool {
	stat, err := client.Stat(job.RemotePath)
	if err != nil || stat.Size() != job.Size || job.SHA256 == "" {
		return false
	}
	checksum, err := client.OpenFile(job.RemotePath+".sha256", os.O_RDONLY)
	if err != nil {
		return false
	}
	raw, readErr := io.ReadAll(io.LimitReader(checksum, 512))
	closeErr := checksum.Close()
	if readErr != nil || closeErr != nil {
		return false
	}
	fields := strings.Fields(string(raw))
	return len(fields) > 0 && strings.EqualFold(fields[0], job.SHA256)
}

func (u *Uploader) complete(job model.UploadJob, deleteLocal bool) error {
	if err := u.store.DeleteUploadJob(job.ID); err != nil {
		return err
	}
	if deleteLocal {
		if err := os.Remove(job.LocalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (u *Uploader) profileConfig(id string) (config.SFTPConfig, bool) {
	if id == "" || id == EnvironmentProfileID {
		if u.legacy.Enabled && validateConfig(u.legacy) == nil {
			return u.legacy, true
		}
		return config.SFTPConfig{}, false
	}
	profile, ok := u.store.SFTPProfile(id)
	if !ok || !profile.Enabled {
		return config.SFTPConfig{}, false
	}
	cfg := profileToConfig(profile)
	if validateConfig(cfg) != nil {
		return config.SFTPConfig{}, false
	}
	return cfg, true
}

func profileToConfig(profile model.SFTPProfile) config.SFTPConfig {
	timeout := time.Duration(profile.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return config.SFTPConfig{
		Enabled: true, Host: profile.Host, Port: profile.Port, User: profile.User, Password: profile.Password,
		PrivateKeyPath: profile.PrivateKeyPath, KnownHostsPath: profile.KnownHostsPath, RemoteBaseDir: profile.RemoteBaseDir,
		Workers: 1, Timeout: timeout, DeleteLocal: profile.DeleteLocal,
	}
}

func validateConfig(cfg config.SFTPConfig) error {
	if !cfg.Enabled {
		return errors.New("perfil SFTP deshabilitado")
	}
	if strings.TrimSpace(cfg.Host) == "" || strings.TrimSpace(cfg.User) == "" {
		return errors.New("SFTP requiere host y usuario")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return errors.New("puerto SFTP inválido")
	}
	if cfg.PrivateKeyPath == "" && cfg.Password == "" {
		return errors.New("SFTP requiere contraseña o llave privada")
	}
	if cfg.KnownHostsPath == "" {
		return errors.New("SFTP requiere un archivo known_hosts")
	}
	if strings.TrimSpace(cfg.RemoteBaseDir) == "" {
		return errors.New("SFTP requiere un directorio remoto")
	}
	return nil
}

func connect(ctx context.Context, cfg config.SFTPConfig) (*ssh.Client, *sftp.Client, error) {
	callback, err := knownhosts.New(cfg.KnownHostsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("cargar known_hosts: %w", err)
	}
	auth, err := authMethods(cfg)
	if err != nil {
		return nil, nil, err
	}
	sshConfig := &ssh.ClientConfig{User: cfg.User, Auth: auth, HostKeyCallback: callback, Timeout: cfg.Timeout}
	address := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	dialer := net.Dialer{Timeout: cfg.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, nil, err
	}
	clientConn, channels, requests, err := ssh.NewClientConn(conn, address, sshConfig)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	sshClient := ssh.NewClient(clientConn, channels, requests)
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		return nil, nil, err
	}
	return sshClient, sftpClient, nil
}

func authMethods(cfg config.SFTPConfig) ([]ssh.AuthMethod, error) {
	methods := make([]ssh.AuthMethod, 0, 2)
	if cfg.PrivateKeyPath != "" {
		raw, err := os.ReadFile(cfg.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("leer llave SFTP: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(raw)
		if err != nil {
			return nil, fmt.Errorf("leer llave privada SFTP: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if cfg.Password != "" {
		methods = append(methods, ssh.Password(cfg.Password))
	}
	if len(methods) == 0 {
		return nil, errors.New("no hay método de autenticación SFTP")
	}
	return methods, nil
}

func randomID() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func backoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 8 {
		attempts = 8
	}
	return time.Duration(1<<uint(attempts-1)) * 30 * time.Second
}

func truncate(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max]
}
