package fetcher

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestSourceIndexTypesMarshalExpectedFields(t *testing.T) {
	index := model.SourceIndex{
		SourceRuns: []model.SourceRun{{
			Name:             "RSS",
			Type:             "rss",
			Category:         "AI/科技",
			Status:           "success",
			Error:            "",
			FetchedCount:     3,
			KeywordMissCount: 1,
			WindowMissCount:  1,
			DedupedCount:     0,
			IncludedCount:    1,
		}},
		ArticleTraces: []model.ArticleTrace{{
			Title:           "story",
			Link:            "https://example.com/story",
			Source:          "RSS",
			SourceType:      "rss",
			Category:        "AI/科技",
			Published:       time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC),
			MatchedKeywords: []string{"AI", "Copilot"},
			Status:          model.TraceStatusIncluded,
			RejectionReason: "",
		}},
	}

	data, err := json.Marshal(index)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	text := string(data)
	for _, field := range []string{"source_runs", "article_traces", "fetched_count", "keyword_miss_count", "window_miss_count", "deduped_count", "included_count", "matched_keywords", "rejection_reason", "source_type"} {
		if !strings.Contains(text, `"`+field+`"`) {
			t.Fatalf("marshal output missing field %q: %s", field, text)
		}
	}
}

func TestSourceIndexTypesStatuses(t *testing.T) {
	statuses := []model.TraceStatus{
		model.TraceStatusIncluded,
		model.TraceStatusKeywordMiss,
		model.TraceStatusOutOfWindow,
		model.TraceStatusDuplicateInBatch,
		model.TraceStatusSeenBefore,
		model.TraceStatusMissingAcceptableTime,
		model.TraceStatusNonReleasePage,
	}
	want := []string{"included", "keyword_miss", "out_of_window", "duplicate_in_batch", "seen_before", "missing_acceptable_time", "non_release_page"}
	for i, status := range statuses {
		if string(status) != want[i] {
			t.Fatalf("status[%d] = %q, want %q", i, string(status), want[i])
		}
	}
}
