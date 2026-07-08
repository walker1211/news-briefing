package output

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/statefile"
)

func WriteSourceStatsSidecar(report model.SourceStatsReport, markdownPath string) (string, error) {
	path := SourceStatsPath(markdownPath)
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal source stats: %w", err)
	}
	data = append(data, '\n')
	if err := statefile.WriteAtomicReplaceOnly(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write source stats: %w", err)
	}
	return path, nil
}

func SourceStatsPath(markdownPath string) string {
	return strings.TrimSuffix(markdownPath, filepath.Ext(markdownPath)) + ".source-stats.json"
}
