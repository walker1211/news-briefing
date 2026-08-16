package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/scheduler"
)

func TestWriteSourceShadowReportIsolatedAndSanitized(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 16, 8, 3, 0, 0, time.FixedZone("CST", 8*60*60))
	app := &app{
		cfg: &config.Config{
			Output:       config.OutputCfg{Dir: dir},
			SourceShadow: config.SourceShadowConfig{Retention: 72 * time.Hour},
		},
		now: func() time.Time { return now },
	}
	window := scheduler.Window{From: now.Add(-10 * time.Hour), To: now, Period: "0800"}
	result := fetcher.FetchResult{
		Articles: []model.Article{{
			Title: "Official release", Link: "https://example.com/release", Source: "Example", SourceRole: model.SourceRolePrimary, Category: "AI/科技", Published: now.Add(-time.Hour),
		}},
		Failed: []fetcher.FailedSource{{Name: "Private Feed", Err: errors.New("Get https://private.invalid/feed: timeout")}},
	}

	if err := app.writeSourceShadowReport(window, result, nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state", "source-shadow", "2026-08-16-0800.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{sourceShadowSchemaVersion, "Official release", `"source_role": "primary"`, `"class": "timeout"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("report missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "private.invalid") {
		t.Fatalf("report leaked failure endpoint: %s", text)
	}
}

func TestPruneSourceShadowReportsKeepsRecentReport(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)
	oldPath := filepath.Join(dir, "old.json")
	recentPath := filepath.Join(dir, "recent.json")
	if err := os.WriteFile(oldPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recentPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldPath, now.Add(-73*time.Hour), now.Add(-73*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(recentPath, now.Add(-71*time.Hour), now.Add(-71*time.Hour)); err != nil {
		t.Fatal(err)
	}
	app := &app{cfg: &config.Config{SourceShadow: config.SourceShadowConfig{Retention: 72 * time.Hour}}}
	app.pruneSourceShadowReports(dir, now)
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old report still exists: %v", err)
	}
	if _, err := os.Stat(recentPath); err != nil {
		t.Fatalf("recent report removed: %v", err)
	}
}
