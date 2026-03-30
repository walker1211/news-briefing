package fetcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestSeenStorePathUsesOutputDir(t *testing.T) {
	store := NewSeenStore("output")
	want := filepath.Join("output", "state", "seen.json")
	if store.Path != want {
		t.Fatalf("Path = %q, want %q", store.Path, want)
	}
	if store.LegacyPath != "seen.json" {
		t.Fatalf("LegacyPath = %q, want %q", store.LegacyPath, "seen.json")
	}
}

func TestSeenStoreFallsBackToLegacyRootSeen(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "seen.json")
	data := `[
  {"url":"https://example.com/a","time":"2026-03-22T08:00:00Z"}
]`
	if err := os.WriteFile(legacyPath, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	entries, err := NewSeenStore("output").Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(entries) != 1 || entries[0].URL != "https://example.com/a" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestSeenStoreLoadRejectsCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	canonicalDir := filepath.Join(dir, "output", "state")
	if err := os.MkdirAll(canonicalDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	canonicalPath := filepath.Join(canonicalDir, "seen.json")
	if err := os.WriteFile(canonicalPath, []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	_, err = NewSeenStore("output").Load()
	if err == nil {
		t.Fatalf("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Fatalf("error = %q, want parse error", err.Error())
	}
}

func TestDedupWithLegacySeenWritesCanonicalStore(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "seen.json")
	legacy := fmt.Sprintf(`[
  {"url":"https://example.com/a","time":%q}
]`, time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339))
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	articles := []model.Article{{Title: "new", Link: "https://example.com/b"}}
	_, err = Dedup(articles, true, NewSeenStore("output"))
	if err != nil {
		t.Fatalf("Dedup() error = %v", err)
	}

	canonicalPath := filepath.Join(dir, "output", "state", "seen.json")
	data, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("ReadFile() canonical error = %v", err)
	}
	if !strings.Contains(string(data), "https://example.com/a") {
		t.Fatalf("canonical data = %q, want legacy entry", string(data))
	}
	if !strings.Contains(string(data), "https://example.com/b") {
		t.Fatalf("canonical data = %q, want new entry", string(data))
	}
}
