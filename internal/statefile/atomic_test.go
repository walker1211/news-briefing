package statefile

import (
	"os"
	"path/filepath"
	"testing"
)

type atomicWriteCase struct {
	name  string
	write func(string, []byte, os.FileMode) error
}

var atomicWriteCases = []atomicWriteCase{
	{name: "durable", write: WriteAtomic},
	{name: "replace only", write: WriteAtomicReplaceOnly},
}

func TestWriteAtomicCreatesParentDirectory(t *testing.T) {
	for _, tt := range atomicWriteCases {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nested", "state.json")
			if err := tt.write(path, []byte(`{"ok":true}`), 0o644); err != nil {
				t.Fatalf("write() error = %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if string(data) != `{"ok":true}` {
				t.Fatalf("data = %q", string(data))
			}
		})
	}
}

func TestWriteAtomicReplacesExistingFile(t *testing.T) {
	for _, tt := range atomicWriteCases {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			if err := tt.write(path, []byte("new"), 0o644); err != nil {
				t.Fatalf("write() error = %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if string(data) != "new" {
				t.Fatalf("data = %q, want new", string(data))
			}
		})
	}
}

func TestWriteAtomicPreservesExistingFileMode(t *testing.T) {
	for _, tt := range atomicWriteCases {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			if err := tt.write(path, []byte("new"), 0o644); err != nil {
				t.Fatalf("write() error = %v", err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("Stat() error = %v", err)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Fatalf("mode = %v, want 0600", got)
			}
		})
	}
}

func TestWriteAtomicUsesRequestedFileMode(t *testing.T) {
	for _, tt := range atomicWriteCases {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if err := tt.write(path, []byte("new"), 0o600); err != nil {
				t.Fatalf("write() error = %v", err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("Stat() error = %v", err)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Fatalf("mode = %v, want 0600", got)
			}
		})
	}
}

func TestWriteAtomicLeavesNoTemporaryFiles(t *testing.T) {
	for _, tt := range atomicWriteCases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state.json")
			if err := tt.write(path, []byte("new"), 0o644); err != nil {
				t.Fatalf("write() error = %v", err)
			}
			matches, err := filepath.Glob(filepath.Join(dir, ".state.json-*.tmp"))
			if err != nil {
				t.Fatalf("Glob() error = %v", err)
			}
			if len(matches) != 0 {
				t.Fatalf("temporary files = %v, want none", matches)
			}
		})
	}
}

func TestWriteAtomicReplacesFinalSymlinkWithoutFollowingTarget(t *testing.T) {
	for _, tt := range atomicWriteCases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			targetPath := filepath.Join(dir, "target.md")
			if err := os.WriteFile(targetPath, []byte("target"), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			linkPath := filepath.Join(dir, "briefing.md")
			if err := os.Symlink(targetPath, linkPath); err != nil {
				t.Skipf("Symlink() unsupported: %v", err)
			}

			if err := tt.write(linkPath, []byte("new"), 0o644); err != nil {
				t.Fatalf("write() error = %v", err)
			}
			linkInfo, err := os.Lstat(linkPath)
			if err != nil {
				t.Fatalf("Lstat() error = %v", err)
			}
			if linkInfo.Mode()&os.ModeSymlink != 0 {
				t.Fatalf("%s is still a symlink", linkPath)
			}
			if got := linkInfo.Mode().Perm(); got != 0o644 {
				t.Fatalf("mode = %v, want 0644", got)
			}
			data, err := os.ReadFile(linkPath)
			if err != nil {
				t.Fatalf("ReadFile(linkPath) error = %v", err)
			}
			if string(data) != "new" {
				t.Fatalf("link path data = %q, want new", string(data))
			}
			targetData, err := os.ReadFile(targetPath)
			if err != nil {
				t.Fatalf("ReadFile(targetPath) error = %v", err)
			}
			if string(targetData) != "target" {
				t.Fatalf("target data = %q, want target", string(targetData))
			}
		})
	}
}
