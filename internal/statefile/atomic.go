package statefile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// AtomicWriteDurability controls whether an atomic write also asks the filesystem for durability.
type AtomicWriteDurability int

const (
	// AtomicWriteDurable syncs the temp file and parent directory; use it for state files.
	AtomicWriteDurable AtomicWriteDurability = iota
	// AtomicWriteReplaceOnly uses same-directory temp-file rename without fsync; use it for generated outputs.
	AtomicWriteReplaceOnly
)

// AtomicWriteOptions configures an atomic write.
type AtomicWriteOptions struct {
	Perm       fs.FileMode
	Durability AtomicWriteDurability
}

// WriteAtomic writes durable state-style data via same-directory rename.
// Final-path symlinks are replaced, not followed.
func WriteAtomic(path string, data []byte, perm fs.FileMode) error {
	return WriteAtomicWithOptions(path, data, AtomicWriteOptions{Perm: perm, Durability: AtomicWriteDurable})
}

// WriteAtomicReplaceOnly writes generated output via rename without fsyncing the file or parent directory.
// Final-path symlinks are replaced, not followed.
func WriteAtomicReplaceOnly(path string, data []byte, perm fs.FileMode) error {
	return WriteAtomicWithOptions(path, data, AtomicWriteOptions{Perm: perm, Durability: AtomicWriteReplaceOnly})
}

// WriteAtomicWithOptions writes data via same-directory rename according to opts.
// Final-path symlinks are replaced, not followed.
func WriteAtomicWithOptions(path string, data []byte, opts AtomicWriteOptions) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	perm := opts.Perm
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			perm = info.Mode().Perm()
		}
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
	if opts.Durability == AtomicWriteDurable {
		if err := file.Sync(); err != nil {
			_ = file.Close()
			cleanup()
			return fmt.Errorf("sync temp file: %w", err)
		}
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
	if opts.Durability == AtomicWriteDurable {
		if err := syncDir(dir); err != nil {
			return err
		}
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
