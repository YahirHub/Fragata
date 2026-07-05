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

type Uploader struct {
	cfg     config.SFTPConfig
	store   *store.Store
	claimed map[string]struct{}
	mu      sync.Mutex
	onError func(error)
}

func New(cfg config.SFTPConfig, state *store.Store, onError func(error)) *Uploader {
	return &Uploader{cfg: cfg, store: state, claimed: make(map[string]struct{}), onError: onError}
}

func (u *Uploader) Enabled() bool { return u != nil && u.cfg.Enabled }

func (u *Uploader) Enqueue(cameraID, localPath, relativeRemotePath string) error {
	if !u.Enabled() {
		return nil
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return err
	}
	id, err := randomID()
	if err != nil {
		return err
	}
	remote := path.Join(u.cfg.RemoteBaseDir, filepath.ToSlash(relativeRemotePath))
	job := model.UploadJob{
		ID: id, CameraID: cameraID, LocalPath: localPath, RemotePath: remote,
		Size: info.Size(), NextAttempt: time.Now(), CreatedAt: time.Now().UTC(),
	}
	return u.store.SaveUploadJob(job)
}

func (u *Uploader) Run(ctx context.Context) {
	if !u.Enabled() {
		return
	}
	var wg sync.WaitGroup
	for i := 0; i < u.cfg.Workers; i++ {
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

	sshClient, sftpClient, err := u.connect(ctx)
	if err != nil {
		return err
	}
	defer sshClient.Close()
	defer sftpClient.Close()

	if remoteMatches(sftpClient, job) {
		return u.complete(job)
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
	return u.complete(job)
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

func (u *Uploader) complete(job model.UploadJob) error {
	if err := u.store.DeleteUploadJob(job.ID); err != nil {
		return err
	}
	if u.cfg.DeleteLocal {
		if err := os.Remove(job.LocalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (u *Uploader) connect(ctx context.Context) (*ssh.Client, *sftp.Client, error) {
	callback, err := knownhosts.New(u.cfg.KnownHostsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("cargar known_hosts: %w", err)
	}
	auth, err := u.authMethods()
	if err != nil {
		return nil, nil, err
	}
	sshConfig := &ssh.ClientConfig{
		User: u.cfg.User, Auth: auth, HostKeyCallback: callback, Timeout: u.cfg.Timeout,
	}
	address := net.JoinHostPort(u.cfg.Host, fmt.Sprintf("%d", u.cfg.Port))
	dialer := net.Dialer{Timeout: u.cfg.Timeout}
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

func (u *Uploader) authMethods() ([]ssh.AuthMethod, error) {
	methods := make([]ssh.AuthMethod, 0, 2)
	if u.cfg.PrivateKeyPath != "" {
		raw, err := os.ReadFile(u.cfg.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("leer llave SFTP: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(raw)
		if err != nil {
			return nil, fmt.Errorf("leer llave privada SFTP: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if u.cfg.Password != "" {
		methods = append(methods, ssh.Password(u.cfg.Password))
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
