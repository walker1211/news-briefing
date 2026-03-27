package fetcher

import (
	"testing"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestDedupInBatchKeepsFirstUniqueLink(t *testing.T) {
	articles := []model.Article{
		{Title: "first", Link: "https://example.com/a"},
		{Title: "second", Link: "https://example.com/a"},
		{Title: "third", Link: "https://example.com/b"},
	}

	got := DedupInBatch(articles)
	if len(got) != 2 {
		t.Fatalf("len(DedupInBatch(...)) = %d, want 2", len(got))
	}
	if got[0].Title != "first" || got[1].Title != "third" {
		t.Fatalf("DedupInBatch(...) = %#v", got)
	}
}

func TestDedupInBatchUsesTitleAndSourceWhenLinkEmpty(t *testing.T) {
	articles := []model.Article{
		{Title: "same", Source: "src"},
		{Title: "same", Source: "src"},
		{Title: "same", Source: "other"},
	}

	got := DedupInBatch(articles)
	if len(got) != 2 {
		t.Fatalf("len(DedupInBatch(...)) = %d, want 2", len(got))
	}
}
