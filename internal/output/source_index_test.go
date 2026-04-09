package output

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestSourceIndexPath(t *testing.T) {
	markdownPath := filepath.Join("output", "26.04.08-早间-0700.md")
	got := SourceIndexPath(markdownPath)
	want := filepath.Join("output", "index", "26.04.08-早间-0700.json")
	if got != want {
		t.Fatalf("SourceIndexPath() = %q, want %q", got, want)
	}
}

func TestWriteSourceIndexCreatesDirectoryAndUsesStableJSONIndent(t *testing.T) {
	dir := t.TempDir()
	markdownPath := filepath.Join(dir, "26.04.08-早间-0700.md")
	index := model.SourceIndex{
		SourceRuns: []model.SourceRun{{
			Name:         "RSS",
			Type:         "rss",
			Category:     "AI/科技",
			Status:       "success",
			FetchedCount: 1,
		}},
		ArticleTraces: []model.ArticleTrace{{
			Title:           "Claude Mythos",
			Link:            "https://example.com/a",
			Source:          "RSS",
			SourceType:      "rss",
			Category:        "AI/科技",
			MatchedKeywords: []string{"Claude Mythos"},
			Status:          model.TraceStatusIncluded,
		}},
	}

	path, err := WriteSourceIndex(markdownPath, index)
	if err != nil {
		t.Fatalf("WriteSourceIndex() error = %v", err)
	}
	wantPath := filepath.Join(dir, "index", "26.04.08-早间-0700.json")
	if path != wantPath {
		t.Fatalf("WriteSourceIndex() path = %q, want %q", path, wantPath)
	}
	if _, err := os.Stat(filepath.Join(dir, "index")); err != nil {
		t.Fatalf("Stat(index dir) error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "\n  \"source_runs\": [") {
		t.Fatalf("WriteSourceIndex() missing stable indent: %q", got)
	}
	if !strings.Contains(got, "\n      \"name\": \"RSS\"") {
		t.Fatalf("WriteSourceIndex() missing nested indent: %q", got)
	}
}
