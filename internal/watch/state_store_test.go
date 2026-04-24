package watch

import (
	"os"
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

func TestStateStoresSaveWithoutLeavingTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	if err := NewIndexStore(dir).Save(IndexState{}); err != nil {
		t.Fatalf("IndexStore.Save() error = %v", err)
	}
	if err := NewArticleStore(dir).Save(ArticleState{}); err != nil {
		t.Fatalf("ArticleStore.Save() error = %v", err)
	}

	for _, name := range []string{"watch-index.json", "watch-articles.json"} {
		path := filepath.Join(dir, "state", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Stat(%q) error = %v", path, err)
		}
		matches, err := filepath.Glob(filepath.Join(dir, "state", "."+name+"-*.tmp"))
		if err != nil {
			t.Fatalf("Glob() error = %v", err)
		}
		if len(matches) != 0 {
			t.Fatalf("temporary files for %s = %v, want none", name, matches)
		}
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
