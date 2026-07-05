package retention

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fragata/internal/store"
)

type Cleaner struct {
	BaseDir   string
	EventsDir string
	Store     *store.Store
	Logger    *slog.Logger
	Interval  time.Duration
}

type Result struct {
	Deleted               int   `json:"deleted"`
	Freed                 int64 `json:"freed_bytes"`
	EventSnapshotsDeleted int   `json:"event_snapshots_deleted"`
}

func (c Cleaner) Run(ctx context.Context) {
	if c.Store == nil || strings.TrimSpace(c.BaseDir) == "" {
		return
	}
	interval := c.Interval
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	c.Cleanup(time.Now())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			c.Cleanup(now)
		}
	}
}

// Cleanup applies the current global policy. It only removes finalized MKV
// files, never partial recordings or files that remain in the upload queue.
func (c Cleaner) Cleanup(now time.Time) Result {
	result := Result{}
	if c.Store == nil || strings.TrimSpace(c.BaseDir) == "" {
		return result
	}
	policy := c.Store.Retention()
	cutoff, ok := policy.Cutoff(now)
	if !ok {
		return result
	}
	pending := make(map[string]struct{})
	for _, job := range c.Store.UploadJobs() {
		absolute, err := filepath.Abs(job.LocalPath)
		if err == nil {
			pending[filepath.Clean(absolute)] = struct{}{}
		}
	}
	err := filepath.WalkDir(c.BaseDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if c.Logger != nil {
				c.Logger.Warn("retention could not inspect path", "path", path, "error", walkErr)
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		lower := strings.ToLower(entry.Name())
		if !strings.HasSuffix(lower, ".mkv") {
			return nil
		}
		absolute, err := filepath.Abs(path)
		if err == nil {
			if _, queued := pending[filepath.Clean(absolute)]; queued {
				return nil
			}
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			if c.Logger != nil {
				c.Logger.Warn("retention could not remove recording", "path", path, "error", err)
			}
			return nil
		}
		result.Deleted++
		result.Freed += info.Size()
		return nil
	})
	if err != nil && c.Logger != nil {
		c.Logger.Warn("retention scan failed", "error", err)
	}
	removeEmptyDirectories(c.BaseDir)
	result.EventSnapshotsDeleted = c.cleanupEvents(cutoff)
	if result.Deleted > 0 && c.Logger != nil {
		c.Logger.Info("retention cleanup completed", "files", result.Deleted, "bytes", result.Freed, "cutoff", cutoff.UTC())
	}
	return result
}

func (c Cleaner) cleanupEvents(cutoff time.Time) int {
	if c.Store == nil || strings.TrimSpace(c.EventsDir) == "" {
		return 0
	}
	root, err := filepath.Abs(c.EventsDir)
	if err != nil {
		return 0
	}
	removedIDs := make([]string, 0)
	deleted := 0
	for _, event := range c.Store.DetectionEventsBefore(cutoff) {
		if event.SnapshotPath == "" {
			removedIDs = append(removedIDs, event.ID)
			continue
		}
		relative := filepath.FromSlash(event.SnapshotPath)
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) > 0 && parts[0] == "events" {
			relative = filepath.Join(parts[1:]...)
		}
		absolute, err := filepath.Abs(filepath.Join(root, relative))
		if err != nil || (absolute != root && !strings.HasPrefix(absolute, root+string(os.PathSeparator))) {
			continue
		}
		err = os.Remove(absolute)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			if c.Logger != nil {
				c.Logger.Warn("retention could not remove event snapshot", "path", absolute, "error", err)
			}
			continue
		}
		removedIDs = append(removedIDs, event.ID)
		deleted++
	}
	if err := c.Store.DeleteDetectionEvents(removedIDs); err != nil {
		if c.Logger != nil {
			c.Logger.Warn("retention could not prune event metadata", "error", err)
		}
		return 0
	}
	removeEmptyDirectories(root)
	return deleted
}

func removeEmptyDirectories(base string) {
	var directories []string
	_ = filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() && path != base {
			directories = append(directories, path)
		}
		return nil
	})
	for index := len(directories) - 1; index >= 0; index-- {
		_ = os.Remove(directories[index])
	}
}
