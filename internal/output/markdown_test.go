package output

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestWriteMarkdownPreservesBodyOrderAndSingleTitle(t *testing.T) {
	outputDir := t.TempDir()
	path, err := WriteMarkdown(&model.Briefing{
		Date:       "26.03.27",
		Period:     "1400",
		RawContent: "TRANSLATED\n\n---\n\nORIGINAL",
	}, outputDir)
	if err != nil {
		t.Fatalf("WriteMarkdown() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(data)
	title := "# 国际资讯简报 26.03.27 午间 14:00"
	if strings.Count(got, title) != 1 {
		t.Fatalf("WriteMarkdown() title count = %d, want 1 in %q", strings.Count(got, title), got)
	}
	if !strings.Contains(got, "TRANSLATED\n\n---\n\nORIGINAL") {
		t.Fatalf("WriteMarkdown() body = %q", got)
	}
}

func TestWriteDeepDiveUsesTopicDeepDivePackHeader(t *testing.T) {
	outputDir := t.TempDir()
	path, err := WriteDeepDive("OpenAI", "正文", outputDir, "26.03.26")
	if err != nil {
		t.Fatalf("WriteDeepDive() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "# 话题深挖包：OpenAI") {
		t.Fatalf("WriteDeepDive() header = %q", got)
	}
	if strings.Contains(got, "# 深度素材包：OpenAI") {
		t.Fatalf("WriteDeepDive() kept legacy header: %q", got)
	}
}
