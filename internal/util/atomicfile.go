package util

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes body to a temporary file in the target directory, fsyncs it,
// and renames it atomically to path.
func WriteFileAtomic(path string, body []byte) error {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create parent dir: %w", err)
		}
	}
	tmpFile, err := os.CreateTemp(dir, ".atomic-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanup := func() error {
		if err := os.Remove(tmpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove temp file: %w", err)
		}
		return nil
	}
	withCleanup := func(primary error, closeErr error) error {
		errs := []error{primary}
		if closeErr != nil {
			errs = append(errs, fmt.Errorf("close temp file: %w", closeErr))
		}
		if cleanupErr := cleanup(); cleanupErr != nil {
			errs = append(errs, cleanupErr)
		}
		return errors.Join(errs...)
	}
	if _, err := tmpFile.Write(body); err != nil {
		return withCleanup(fmt.Errorf("write temp file: %w", err), tmpFile.Close())
	}
	if err := tmpFile.Sync(); err != nil {
		return withCleanup(fmt.Errorf("sync temp file: %w", err), tmpFile.Close())
	}
	if err := tmpFile.Close(); err != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			return errors.Join(fmt.Errorf("close temp file: %w", err), cleanupErr)
		}
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			return errors.Join(fmt.Errorf("promote temp file: %w", err), cleanupErr)
		}
		return fmt.Errorf("promote temp file: %w", err)
	}
	return nil
}
