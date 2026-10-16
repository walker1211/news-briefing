package fetcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestDedupInBatchOfficialArticleAliasesKeepFirstLink(t *testing.T) {
	old := "https://deepmind.google/blog/model-release/?utm_source=feed#intro"
	newURL := "https://blog.google/innovation-and-ai/models-and-research/gemini-models/model-release/"
	for _, links := range [][]string{{old, newURL}, {newURL, old}} {
		articles := []model.Article{{Title: "first", Link: links[0]}, {Title: "second", Link: links[1]}}
		outcome := DedupInBatch(articles)
		if len(outcome.Articles) != 1 || outcome.Articles[0].Link != links[0] {
			t.Fatalf("DedupInBatch(%q first) = %#v, want original first link", links[0], outcome.Articles)
		}
		if _, ok := outcome.DuplicateKeys["https://deepmind.google/blog/model-release"]; !ok {
			t.Fatalf("DuplicateKeys = %#v, want canonical article key", outcome.DuplicateKeys)
		}
	}
}

func TestDedupOfficialArticleAliasesMatchHistoricalRawSeenBothWays(t *testing.T) {
	old := "https://deepmind.google/blog/model-release/?utm_source=feed"
	newURL := "https://blog.google/innovation-and-ai/models-and-research/google-deepmind/model-release/#summary"
	for _, pair := range [][2]string{{old, newURL}, {newURL, old}} {
		store := NewSeenStore(t.TempDir())
		if err := store.Save([]seenEntry{{URL: pair[0], Time: time.Now()}}); err != nil {
			t.Fatal(err)
		}
		outcome, err := Dedup([]model.Article{{Link: pair[1]}}, false, store)
		if err != nil {
			t.Fatal(err)
		}
		if len(outcome.Articles) != 0 || len(outcome.SeenBeforeKeys) != 1 {
			t.Fatalf("historical %q, incoming %q: outcome = %#v", pair[0], pair[1], outcome)
		}
		loaded, err := store.Load()
		if err != nil || len(loaded) != 1 || loaded[0].URL != pair[0] {
			t.Fatalf("read-only Dedup altered historical store: %#v, %v", loaded, err)
		}
	}
}

func TestCanonicalArticleURLBoundaries(t *testing.T) {
	tests := []struct{ name, link, want string }{
		{"deepmind legacy", "https://blog.google/technology/ai/model-release/?gclid=one#section", "https://deepmind.google/blog/model-release"},
		{"nvidia tracking", "https://developer.nvidia.com/blog/gpu-release/?utm_campaign=launch&fbclid=one#section", "https://developer.nvidia.com/blog/gpu-release"},
		{"semantic query", "https://deepmind.google/blog/model-release/?lang=zh&utm_source=feed", "https://deepmind.google/blog/model-release?lang=zh"},
		{"case-sensitive slug", "https://deepmind.google/blog/Model-Release/", "https://deepmind.google/blog/Model-Release"},
		{"different google section", "https://blog.google/technology/ai/robot-release/", "https://deepmind.google/blog/robot-release"},
		{"unrelated section", "https://blog.google/products/gemini/model-release/?utm_source=feed#section", "https://blog.google/products/gemini/model-release/?utm_source=feed#section"},
		{"nested google path", "https://blog.google/technology/ai/series/model-release/", "https://blog.google/technology/ai/series/model-release/"},
		{"similar host", "https://deepmind.google.evil.test/blog/model-release/?utm_source=feed", "https://deepmind.google.evil.test/blog/model-release/?utm_source=feed"},
		{"userinfo", "https://user@deepmind.google/blog/model-release/?utm_source=feed", "https://user@deepmind.google/blog/model-release/?utm_source=feed"},
		{"unsupported scheme", "ftp://deepmind.google/blog/model-release/?utm_source=feed", "ftp://deepmind.google/blog/model-release/?utm_source=feed"},
		{"port", "https://deepmind.google:443/blog/model-release/?utm_source=feed", "https://deepmind.google:443/blog/model-release/?utm_source=feed"},
		{"unrelated nvidia path", "https://developer.nvidia.com/guides/gpu-release/?utm_source=feed", "https://developer.nvidia.com/guides/gpu-release/?utm_source=feed"},
		{"other host", "https://example.com/blog/model-release/?utm_source=feed#section", "https://example.com/blog/model-release/?utm_source=feed#section"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canonicalArticleURL(tt.link); got != tt.want {
				t.Fatalf("canonicalArticleURL(%q) = %q, want %q", tt.link, got, tt.want)
			}
		})
	}
	articles := []model.Article{
		{Link: "https://deepmind.google/blog/model-release/"},
		{Link: "https://deepmind.google/blog/model-release/?lang=zh"},
		{Link: "https://deepmind.google/blog/model-safety-update/"},
		{Link: "https://blog.google/innovation-and-ai/models-and-research/google-deepmind/robotics-companion/"},
		{Link: "https://blog.google/products/gemini/model-release/"},
	}
	if got := len(DedupInBatch(articles).Articles); got != len(articles) {
		t.Fatalf("distinct articles reduced to %d, want %d", got, len(articles))
	}
}

func TestDedupOfficialAliasesReadOnlyAndCancelled(t *testing.T) {
	store := NewSeenStore(t.TempDir())
	article := model.Article{Link: "https://deepmind.google/blog/model-release/"}
	outcome, err := Dedup([]model.Article{article}, false, store)
	if err != nil || len(outcome.Articles) != 1 {
		t.Fatalf("read-only Dedup = %#v, %v", outcome, err)
	}
	if _, err := os.Stat(store.Path); !os.IsNotExist(err) {
		t.Fatalf("read-only Dedup wrote seen store: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = DedupContext(ctx, []model.Article{article}, true, store)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled DedupContext error = %v", err)
	}
	if _, err := os.Stat(store.Path); !os.IsNotExist(err) {
		t.Fatalf("cancelled DedupContext wrote seen store: %v", err)
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
