package watch

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestSeenStoreUsesOutputStateDirectory(t *testing.T) {
	store := NewSeenStore("output")
	if store.Path != filepath.Join("output", "state", "watch-seen.json") {
		t.Fatalf("store.Path = %q", store.Path)
	}
}

func TestSeenStoreSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewSeenStore(dir)
	state := model.WatchSeenState{
		Items: []model.WatchSeenArticle{{
			ID:               "https://support.claude.com/a",
			URL:              "https://support.claude.com/a",
			Title:            "Claude 上的身份验证",
			Source:           "Anthropic Claude Support",
			BriefingCategory: "AI/科技",
			WatchCategory:    "安全保障",
			Summary:          "摘要",
			Body:             "正文",
			EventType:        "content_changed",
			DetectedAt:       time.Date(2026, 4, 15, 21, 22, 0, 0, time.UTC),
		}},
	}
	if err := store.Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("len(got.Items) = %d, want 1", len(got.Items))
	}
	item := got.Items[0]
	if item.Title != "Claude 上的身份验证" || item.WatchCategory != "安全保障" || item.EventType != "content_changed" {
		t.Fatalf("Load() item = %#v", item)
	}
}

func TestSeenStoreLoadInitializesEmptyMap(t *testing.T) {
	store := NewSeenStore(t.TempDir())
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Items == nil {
		t.Fatal("Load() Items = nil")
	}
	if len(got.Items) != 0 {
		t.Fatalf("len(got.Items) = %d, want 0", len(got.Items))
	}
}
