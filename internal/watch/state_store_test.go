package watch

import (
	"path/filepath"
	"testing"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestStateStoresUseOutputStateDirectory(t *testing.T) {
	indexStore := NewIndexStore("output")
	articleStore := NewArticleStore("output")

	if indexStore.Path != filepath.Join("output", "state", "watch-index.json") {
		t.Fatalf("indexStore.Path = %q", indexStore.Path)
	}
	if articleStore.Path != filepath.Join("output", "state", "watch-articles.json") {
		t.Fatalf("articleStore.Path = %q", articleStore.Path)
	}
}

func TestIndexStoreSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewIndexStore(dir)
	state := IndexState{
		Homes: map[string]model.WatchIndexSnapshot{
			"anthropic": {
				Scope:     "home",
				Source:    "Anthropic Claude Support",
				URL:       "https://support.claude.com/zh-CN",
				ItemCount: 2,
				Hash:      "home-hash",
			},
		},
		Categories: map[string]model.WatchIndexSnapshot{},
	}
	if err := store.Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Homes["anthropic"].Hash != "home-hash" {
		t.Fatalf("Load() = %#v", got)
	}
}
