package output

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/walker1211/news-briefing/internal/model"
)

func SourceIndexPath(markdownPath string) string {
	dir := filepath.Dir(markdownPath)
	base := strings.TrimSuffix(filepath.Base(markdownPath), filepath.Ext(markdownPath))
	return filepath.Join(dir, "index", base+filepath.Ext(briefingIndexFileName("x", "0000")))
}

func WriteSourceIndex(markdownPath string, index model.SourceIndex) (string, error) {
	path := SourceIndexPath(markdownPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create source index dir: %w", err)
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal source index: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write source index: %w", err)
	}
	return path, nil
}
