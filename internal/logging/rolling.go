package logging

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const DefaultMaxSize int64 = 1 << 20

// RollingWriter keeps a single log file below maxSize by removing its oldest
// complete lines. The newest half is retained whenever compaction is needed.
type RollingWriter struct {
	mu      sync.Mutex
	path    string
	maxSize int64
	file    *os.File
	size    int64
}

func Open(path string, maxSize int64) (*RollingWriter, error) {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	writer := &RollingWriter{path: path, maxSize: maxSize, file: file, size: info.Size()}
	if writer.size > maxSize {
		if err := writer.compactLocked(nil); err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	return writer, nil
}

func (w *RollingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, os.ErrClosed
	}
	originalLength := len(p)
	if int64(len(p)) > w.maxSize {
		p = p[len(p)-int(w.maxSize):]
		if index := bytes.IndexByte(p, '\n'); index >= 0 && index+1 < len(p) {
			p = p[index+1:]
		}
	}
	if w.size+int64(len(p)) > w.maxSize {
		if err := w.compactLocked(p); err != nil {
			return 0, err
		}
		return originalLength, nil
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	if err != nil {
		return n, err
	}
	return originalLength, nil
}

func (w *RollingWriter) compactLocked(pending []byte) error {
	if w.file == nil {
		return os.ErrClosed
	}
	if err := w.file.Sync(); err != nil {
		return err
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	raw, err := os.ReadFile(w.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	keepLimit := w.maxSize / 2
	if pendingSize := int64(len(pending)); pendingSize >= w.maxSize {
		raw = nil
	} else if pendingSize > keepLimit {
		keepLimit = w.maxSize - pendingSize
	}
	if int64(len(raw)) > keepLimit {
		raw = raw[len(raw)-int(keepLimit):]
		if index := bytes.IndexByte(raw, '\n'); index >= 0 && index+1 < len(raw) {
			raw = raw[index+1:]
		}
	}
	content := make([]byte, 0, len(raw)+len(pending))
	content = append(content, raw...)
	content = append(content, pending...)
	if int64(len(content)) > w.maxSize {
		content = content[len(content)-int(w.maxSize):]
	}
	tmp := w.path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o640); err != nil {
		return err
	}
	if err := os.Rename(tmp, w.path); err != nil {
		return err
	}
	file, err := os.OpenFile(w.path, os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	w.file = file
	w.size = int64(len(content))
	return nil
}

func (w *RollingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func MultiOutput(file io.Writer) io.Writer {
	return io.MultiWriter(os.Stdout, file)
}
