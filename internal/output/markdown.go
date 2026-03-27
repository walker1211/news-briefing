package output

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/walker1211/news-briefing/internal/model"
)

func WriteMarkdown(briefing *model.Briefing, outputDir string) (string, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	filename := briefingFileName(briefing.Date, briefing.Period)
	path := filepath.Join(outputDir, filename)

	header := briefingMarkdownHeader(briefing.Date, briefing.Period) + "\n\n"
	content := header + briefing.RawContent

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write markdown: %w", err)
	}

	return path, nil
}

func WriteDeepDive(topic, content, outputDir string, date string) (string, error) {
	deepDir := filepath.Join(outputDir, "deep")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		return "", fmt.Errorf("create deep dir: %w", err)
	}

	filename := fmt.Sprintf("%s-%s.md", date, sanitize(topic))
	path := filepath.Join(deepDir, filename)

	header := fmt.Sprintf("# 话题深挖包：%s\n\n", topic)
	full := header + content

	if err := os.WriteFile(path, []byte(full), 0644); err != nil {
		return "", fmt.Errorf("write deep dive: %w", err)
	}

	return path, nil
}

func sanitize(s string) string {
	result := make([]byte, 0, len(s))
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			result = append(result, c)
		} else if c == ' ' {
			result = append(result, '-')
		}
	}
	if len(result) > 50 {
		result = result[:50]
	}
	return string(result)
}
