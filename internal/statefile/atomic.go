package statefile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func WriteAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat target file: %w", err)
	}

	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := file.Name()
	cleaned := false
	cleanup := func() {
		if !cleaned {
			_ = os.Remove(tmpPath)
			cleaned = true
		}
	}

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := file.Chmod(perm); err != nil {
		_ = file.Close()
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		cleanup()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp file: %w", err)
	}
	cleaned = true
	if err := syncDir(dir); err != nil {
		return err
	}
	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open parent dir: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync parent dir: %w", err)
	}
	return nil
}
