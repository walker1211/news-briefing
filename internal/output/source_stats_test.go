package output

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestWriteSourceStatsSidecar(t *testing.T) {
	dir := t.TempDir()
	markdownPath := filepath.Join(dir, "26.07.08-早间-0800.md")
	report := model.SourceStatsReport{
		SchemaVersion: "source-stats/v1",
		SourceApp:     "news-briefing",
		Date:          "26.07.08",
		Period:        "0800",
		GeneratedAt:   time.Date(2026, 7, 8, 8, 0, 0, 0, time.UTC),
		Sources: []model.SourceStatsEntry{{
			Source:   "Hacker News",
			Type:     "hackernews",
			Category: "AI/科技",
			Fetched:  2,
		}},
	}
	report.RecalculateTotals()

	path, err := WriteSourceStatsSidecar(report, markdownPath)
	if err != nil {
		t.Fatalf("WriteSourceStatsSidecar() error = %v", err)
	}
	wantPath := filepath.Join(dir, "26.07.08-早间-0800.source-stats.json")
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var got model.SourceStatsReport
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got.SchemaVersion != "source-stats/v1" || got.Totals.Fetched != 2 || len(got.Sources) != 1 {
		t.Fatalf("decoded report = %#v", got)
	}
}
