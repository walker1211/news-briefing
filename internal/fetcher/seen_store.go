package fetcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type SeenStore struct {
	Path       string
	LegacyPath string
}

func NewSeenStore(outputDir string) SeenStore {
	if outputDir == "" {
		outputDir = "output"
	}
	return SeenStore{
		Path:       filepath.Join(outputDir, "state", "seen.json"),
		LegacyPath: "seen.json",
	}
}

func (s SeenStore) Load() ([]seenEntry, error) {
	path := s.Path
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat seen store: %w", err)
		}
		if _, legacyErr := os.Stat(s.LegacyPath); legacyErr == nil {
			path = s.LegacyPath
		} else if legacyErr != nil && !os.IsNotExist(legacyErr) {
			return nil, fmt.Errorf("stat legacy seen store: %w", legacyErr)
		} else {
			return nil, nil
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read seen store: %w", err)
	}
	var entries []seenEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse seen store: %w", err)
	}
	return entries, nil
}

func (s SeenStore) Save(entries []seenEntry) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fmt.Errorf("create seen state dir: %w", err)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal seen store: %w", err)
	}
	if err := os.WriteFile(s.Path, data, 0o644); err != nil {
		return fmt.Errorf("write seen store: %w", err)
	}
	return nil
}
