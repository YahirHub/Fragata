package recording

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// RecoverPartials preserves complete clusters written before an unclean shutdown.
// Matroska segments use an unknown segment size, so finalized clusters remain playable.
func RecoverPartials(baseDir string) ([]string, error) {
	var recovered []string
	err := filepath.WalkDir(baseDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".mkv.partial") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() == 0 {
			return os.Remove(path)
		}
		final := strings.TrimSuffix(path, ".partial")
		if _, err := os.Stat(final); err == nil {
			final = strings.TrimSuffix(final, ".mkv") + ".recovered.mkv"
		}
		if err := os.Rename(path, final); err != nil {
			return err
		}
		recovered = append(recovered, final)
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return recovered, nil
	}
	return recovered, err
}
