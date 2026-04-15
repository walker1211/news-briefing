package watch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/walker1211/news-briefing/internal/model"
)

type IndexState struct {
	Homes      map[string]model.WatchIndexSnapshot `json:"homes"`
	Categories map[string]model.WatchIndexSnapshot `json:"categories"`
}

type ArticleState map[string]model.WatchArticleState

type IndexStore struct{ Path string }

type ArticleStore struct{ Path string }

func NewIndexStore(outputDir string) IndexStore {
	if outputDir == "" {
		outputDir = "output"
	}
	return IndexStore{Path: filepath.Join(outputDir, "state", "watch-index.json")}
}

func NewArticleStore(outputDir string) ArticleStore {
	if outputDir == "" {
		outputDir = "output"
	}
	return ArticleStore{Path: filepath.Join(outputDir, "state", "watch-articles.json")}
}

func (s IndexStore) Load() (IndexState, error) {
	var state IndexState
	if err := loadJSONFile(s.Path, &state); err != nil {
		return IndexState{}, err
	}
	if state.Homes == nil {
		state.Homes = map[string]model.WatchIndexSnapshot{}
	}
	if state.Categories == nil {
		state.Categories = map[string]model.WatchIndexSnapshot{}
	}
	return state, nil
}

func (s IndexStore) Save(state IndexState) error {
	if state.Homes == nil {
		state.Homes = map[string]model.WatchIndexSnapshot{}
	}
	if state.Categories == nil {
		state.Categories = map[string]model.WatchIndexSnapshot{}
	}
	return saveJSONFile(s.Path, state)
}

func (s ArticleStore) Load() (ArticleState, error) {
	var state ArticleState
	if err := loadJSONFile(s.Path, &state); err != nil {
		return nil, err
	}
	if state == nil {
		state = ArticleState{}
	}
	return state, nil
}

func (s ArticleStore) Save(state ArticleState) error {
	if state == nil {
		state = ArticleState{}
	}
	return saveJSONFile(s.Path, state)
}

func loadJSONFile(path string, target any) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat watch state: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read watch state: %w", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("parse watch state: %w", err)
	}
	return nil
}

func saveJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create watch state dir: %w", err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal watch state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write watch state: %w", err)
	}
	return nil
}
