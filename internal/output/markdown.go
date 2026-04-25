package output

import (
	"fmt"
	"path/filepath"

	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/statefile"
)

func WriteMarkdown(briefing *model.Briefing, outputDir string) (string, error) {
	filename := briefingFileName(briefing.Date, briefing.Period)
	path := filepath.Join(outputDir, filename)

	header := briefingMarkdownHeader(briefing.Date, briefing.Period) + "\n\n"
	content := header + briefing.RawContent

	if err := statefile.WriteAtomicReplaceOnly(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write markdown: %w", err)
	}

	return path, nil
}

func WriteDeepDive(topic, content, outputDir string, date string) (string, error) {
	deepDir := filepath.Join(outputDir, "deep")
	filename := fmt.Sprintf("%s-%s.md", date, sanitize(topic))
	path := filepath.Join(deepDir, filename)

	header := fmt.Sprintf("# 话题深挖包：%s\n\n", topic)
	full := header + content

	if err := statefile.WriteAtomicReplaceOnly(path, []byte(full), 0644); err != nil {
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
