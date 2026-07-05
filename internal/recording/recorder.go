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
	CameraID                string
	BaseDir                 string
	SegmentDuration         time.Duration
	SegmentDurationProvider func() time.Duration
	Hub                     *stream.Hub
	OnStarted               func(path string, started time.Time)
	OnCompleted             func(CompletedFile)
	OnError                 func(error)
}

type finalizeRequest struct {
	segment *segment
	endedAt time.Time
}

func (r *Recorder) Run(ctx context.Context) error {
	if r.Hub == nil {
		return errors.New("hub requerido")
	}
	if r.currentDuration() <= 0 {
		return errors.New("duración de segmento inválida")
	}

	// A large buffer protects the recorder from short disk stalls. Segment
	// finalization runs separately so Sync/Close never blocks incoming video.
	accessUnits, unsubscribe := r.Hub.SubscribeAccessUnitsReliable(2048)
	defer unsubscribe()

	finalizeQueue := make(chan finalizeRequest, 8)
	finalizerDone := make(chan struct{})
	go func() {
		defer close(finalizerDone)
		for request := range finalizeQueue {
			if request.segment == nil {
				continue
			}
			if err := request.segment.finish(request.endedAt); err != nil {
				r.report(fmt.Errorf("finalizar MKV: %w", err))
				continue
			}
			r.completed(request.segment)
		}
	}()

	finishAll := func(current *segment) {
		if current != nil {
			finalizeQueue <- finalizeRequest{segment: current, endedAt: time.Now()}
		}
		close(finalizeQueue)
		<-finalizerDone
	}

	var current *segment
	for {
		select {
		case <-ctx.Done():
			finishAll(current)
			return nil
		case au, ok := <-accessUnits:
			if !ok {
				finishAll(current)
				return nil
			}

			if au.Discontinuity {
				if current != nil && (au.Generation == 0 || current.generation == au.Generation) {
					finalizeQueue <- finalizeRequest{segment: current, endedAt: time.Now()}
					current = nil
				}
				continue
			}

			if current != nil && au.Generation != 0 && current.generation != 0 && current.generation != au.Generation {
				finalizeQueue <- finalizeRequest{segment: current, endedAt: time.Now()}
				current = nil
			}

			info := r.Hub.Info()
			if current == nil {
				if !au.KeyFrame || !ready(info) {
					continue
				}
				seg, err := r.start(info, au.Generation)
				if err != nil {
					r.report(err)
					continue
				}
				if err := seg.writer.WriteAccessUnit(au); err != nil {
					_ = seg.discard()
					r.report(fmt.Errorf("iniciar MKV: %w", err))
					continue
				}
				seg.announce(r.OnStarted)
				current = seg
				continue
			}

			duration := r.currentDuration()
			if au.KeyFrame && duration > 0 && time.Since(current.startedAt) >= duration {
				// Open and write the next segment before closing the previous one.
				// The boundary keyframe belongs to the new file, so both files remain
				// independently decodable without dropping an access unit.
				next, err := r.start(info, au.Generation)
				if err == nil {
					err = next.writer.WriteAccessUnit(au)
				}
				if err == nil {
					next.announce(r.OnStarted)
					previous := current
					current = next
					finalizeQueue <- finalizeRequest{segment: previous, endedAt: time.Now()}
					continue
				}
				if next != nil {
					_ = next.discard()
				}
				r.report(fmt.Errorf("rotar MKV sin interrumpir el stream: %w", err))
				// If opening the next file fails, keep the current file running so
				// video is not discarded merely because rotation failed.
			}

			if err := current.writer.WriteAccessUnit(au); err != nil {
				r.report(fmt.Errorf("escribir MKV: %w", err))
				_ = current.preservePartial()
				current = nil
			}
		}
	}
}

func (r *Recorder) currentDuration() time.Duration {
	if r.SegmentDurationProvider != nil {
		if value := r.SegmentDurationProvider(); value > 0 {
			return value
		}
	}
	return r.SegmentDuration
}

func ready(info stream.Info) bool {
	if info.Width <= 0 || info.Height <= 0 {
		return false
	}
	if info.Codec == "H264" {
		return len(info.SPS) >= 4 && len(info.PPS) > 0
	}
	if info.Codec == "H265" {
		return len(info.VPS) > 0 && len(info.SPS) > 0 && len(info.PPS) > 0
	}
	return false
}

func (r *Recorder) start(info stream.Info, generation uint64) (*segment, error) {
	now := time.Now()
	dir := filepath.Join(r.BaseDir, safePart(r.CameraID), now.Format("2006"), now.Format("01"), now.Format("02"))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("crear directorio de grabación: %w", err)
	}
	baseName := now.Format("15-04-05.000")
	var partial, final string
	var file *os.File
	var err error
	for attempt := 0; attempt < 100; attempt++ {
		name := baseName
		if attempt > 0 {
			name = fmt.Sprintf("%s-%02d", baseName, attempt)
		}
		partial = filepath.Join(dir, name+".mkv.partial")
		final = filepath.Join(dir, name+".mkv")
		file, err = os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("crear grabación: %w", err)
		}
	}
	if file == nil {
		return nil, errors.New("no se pudo generar un nombre único para la grabación")
	}
	writer, err := matroska.New(file, info)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(partial)
		return nil, err
	}
	return &segment{
		partialPath: partial,
		finalPath:   final,
		file:        file,
		writer:      writer,
		startedAt:   now,
		generation:  generation,
	}, nil
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
	generation  uint64
	announced   bool
	finished    bool
}

func (s *segment) announce(callback func(string, time.Time)) {
	if s == nil || s.announced {
		return
	}
	s.announced = true
	if callback != nil {
		callback(s.partialPath, s.startedAt)
	}
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

func (s *segment) discard() error {
	if s == nil {
		return nil
	}
	_ = s.file.Close()
	return os.Remove(s.partialPath)
}

func (s *segment) preservePartial() error {
	if s == nil {
		return nil
	}
	_ = s.writer.Close()
	_ = s.file.Sync()
	return s.file.Close()
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
