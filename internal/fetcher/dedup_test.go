package fetcher

import (
	"path/filepath"
	"testing"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestDedupInBatchKeepsFirstUniqueLink(t *testing.T) {
	articles := []model.Article{
		{Title: "first", Link: "https://example.com/a"},
		{Title: "second", Link: "https://example.com/a"},
		{Title: "third", Link: "https://example.com/b"},
	}

	outcome := DedupInBatch(articles)
	if len(outcome.Articles) != 2 {
		t.Fatalf("len(DedupInBatch(...).Articles) = %d, want 2", len(outcome.Articles))
	}
	if outcome.Articles[0].Title != "first" || outcome.Articles[1].Title != "third" {
		t.Fatalf("DedupInBatch(...) = %#v", outcome.Articles)
	}
	if _, ok := outcome.DuplicateKeys["https://example.com/a"]; !ok {
		t.Fatalf("DuplicateKeys = %#v, want duplicate link recorded", outcome.DuplicateKeys)
	}
}

func TestDedupInBatchUsesTitleAndSourceWhenLinkEmpty(t *testing.T) {
	articles := []model.Article{
		{Title: "same", Source: "src"},
		{Title: "same", Source: "src"},
		{Title: "same", Source: "other"},
	}

	outcome := DedupInBatch(articles)
	if len(outcome.Articles) != 2 {
		t.Fatalf("len(DedupInBatch(...).Articles) = %d, want 2", len(outcome.Articles))
	}
	if _, ok := outcome.DuplicateKeys["same|src"]; !ok {
		t.Fatalf("DuplicateKeys = %#v, want title/source key recorded", outcome.DuplicateKeys)
	}
}

func TestDedupSeparatesBatchDuplicatesFromHistoricalSeen(t *testing.T) {
	dir := t.TempDir()
	store := NewSeenStore(filepath.Join(dir, "output"))
	if err := store.Save([]seenEntry{{URL: "https://example.com/already-seen"}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	articles := []model.Article{
		{Title: "first", Link: "https://example.com/in-batch"},
		{Title: "second", Link: "https://example.com/in-batch"},
		{Title: "seen", Link: "https://example.com/already-seen"},
	}

	outcome, err := Dedup(articles, false, store)
	if err != nil {
		t.Fatalf("Dedup() error = %v", err)
	}
	if len(outcome.Articles) != 1 || outcome.Articles[0].Link != "https://example.com/in-batch" {
		t.Fatalf("outcome.Articles = %#v", outcome.Articles)
	}
	if _, ok := outcome.DuplicateKeys["https://example.com/in-batch"]; !ok {
		t.Fatalf("DuplicateKeys = %#v, want batch duplicate recorded", outcome.DuplicateKeys)
	}
	if _, ok := outcome.SeenBeforeKeys["https://example.com/already-seen"]; !ok {
		t.Fatalf("SeenBeforeKeys = %#v, want historical seen recorded", outcome.SeenBeforeKeys)
	}
	if _, ok := outcome.SeenBeforeKeys["https://example.com/in-batch"]; ok {
		t.Fatalf("SeenBeforeKeys = %#v, want batch duplicate excluded", outcome.SeenBeforeKeys)
	}
}
