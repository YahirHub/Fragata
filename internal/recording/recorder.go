package recording

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fragata/internal/matroska"
	"fragata/internal/stream"
)

type CompletedFile struct {
	Path      string
	StartedAt time.Time
	EndedAt   time.Time
	Size      int64
}

type Recorder struct {
	CameraID        string
	BaseDir         string
	SegmentDuration time.Duration
	Hub             *stream.Hub
	OnStarted       func(path string, started time.Time)
	OnCompleted     func(CompletedFile)
	OnError         func(error)
}

func (r *Recorder) Run(ctx context.Context) error {
	if r.Hub == nil {
		return errors.New("hub requerido")
	}
	if r.SegmentDuration < 10*time.Second {
		return errors.New("segmento mínimo: 10s")
	}
	accessUnits, unsubscribe := r.Hub.SubscribeAccessUnits(256)
	defer unsubscribe()

	var current *segment
	for {
		select {
		case <-ctx.Done():
			if current != nil {
				if err := current.finish(time.Now()); err != nil {
					r.report(err)
				}
				r.completed(current)
			}
			return nil
		case au, ok := <-accessUnits:
			if !ok {
				return nil
			}
			info := r.Hub.Info()
			if current == nil {
				if !au.KeyFrame || !ready(info) {
					continue
				}
				seg, err := r.start(info)
				if err != nil {
					return err
				}
				current = seg
			}
			if au.KeyFrame && time.Since(current.startedAt) >= r.SegmentDuration {
				if err := current.finish(time.Now()); err != nil {
					r.report(err)
				} else {
					r.completed(current)
				}
				seg, err := r.start(info)
				if err != nil {
					return err
				}
				current = seg
			}
			if err := current.writer.WriteAccessUnit(au); err != nil {
				r.report(fmt.Errorf("escribir MKV: %w", err))
				_ = current.abort()
				current = nil
			}
		}
	}
}

func ready(info stream.Info) bool {
	if info.Codec == "H264" {
		return len(info.SPS) >= 4 && len(info.PPS) > 0
	}
	if info.Codec == "H265" {
		return len(info.VPS) > 0 && len(info.SPS) > 0 && len(info.PPS) > 0
	}
	return false
}

func (r *Recorder) start(info stream.Info) (*segment, error) {
	now := time.Now()
	dir := filepath.Join(r.BaseDir, safePart(r.CameraID), now.Format("2006"), now.Format("01"), now.Format("02"))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("crear directorio de grabación: %w", err)
	}
	name := now.Format("15-04-05.000")
	partial := filepath.Join(dir, name+".mkv.partial")
	final := filepath.Join(dir, name+".mkv")
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, fmt.Errorf("crear grabación: %w", err)
	}
	writer, err := matroska.New(file, info)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(partial)
		return nil, err
	}
	seg := &segment{partialPath: partial, finalPath: final, file: file, writer: writer, startedAt: now}
	if r.OnStarted != nil {
		r.OnStarted(partial, now)
	}
	return seg, nil
}

func (r *Recorder) completed(seg *segment) {
	if r.OnCompleted == nil || seg == nil || !seg.finished {
		return
	}
	info, err := os.Stat(seg.finalPath)
	if err != nil {
		r.report(err)
		return
	}
	r.OnCompleted(CompletedFile{Path: seg.finalPath, StartedAt: seg.startedAt, EndedAt: seg.endedAt, Size: info.Size()})
}

func (r *Recorder) report(err error) {
	if r.OnError != nil && err != nil {
		r.OnError(err)
	}
}

type segment struct {
	partialPath string
	finalPath   string
	file        *os.File
	writer      *matroska.Writer
	startedAt   time.Time
	endedAt     time.Time
	finished    bool
}

func (s *segment) finish(ended time.Time) error {
	if s.finished {
		return nil
	}
	if err := s.writer.Close(); err != nil {
		_ = s.file.Close()
		return err
	}
	if err := s.file.Sync(); err != nil {
		_ = s.file.Close()
		return err
	}
	if err := s.file.Close(); err != nil {
		return err
	}
	if err := os.Rename(s.partialPath, s.finalPath); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(s.finalPath)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	s.endedAt = ended
	s.finished = true
	return nil
}

func (s *segment) abort() error {
	_ = s.file.Close()
	return os.Remove(s.partialPath)
}

func safePart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "camera"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
