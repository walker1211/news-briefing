package output

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestWriteWatchMarkdownWritesGroupedEvents(t *testing.T) {
	outputDir := t.TempDir()
	report := &model.WatchReport{
		GeneratedAt: time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC),
		Events: []model.WatchEvent{{
			EventType:         "new_article",
			Source:            "Anthropic Claude Support",
			Category:          "安全保障",
			ArticleTitle:      "Claude 上的身份验证",
			ArticleURL:        "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
			IncludeInBriefing: true,
			Reason:            "命中高价值关键词：身份验证",
		}},
	}
	path, err := WriteWatchMarkdown(report, outputDir, "26.04.15", "1600")
	if err != nil {
		t.Fatalf("WriteWatchMarkdown() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "# 非新闻监听 26.04.15 午间 16:00") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "Claude 上的身份验证") || !strings.Contains(text, "new_article") {
		t.Fatalf("text = %q", text)
	}
}

func TestRenderWatchMarkdownShowsNoChangesMessage(t *testing.T) {
	text := RenderWatchMarkdown(&model.WatchReport{GeneratedAt: time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC)}, "26.04.15", "1600")
	if !strings.Contains(text, "- 本次未检测到变化") {
		t.Fatalf("text = %q", text)
	}
}

func TestRenderWatchMarkdownShowsSiteErrorsInFullChanges(t *testing.T) {
	text := RenderWatchMarkdown(&model.WatchReport{
		GeneratedAt: time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC),
		Events: []model.WatchEvent{{
			EventType:         "site_error",
			Source:            "Claude Platform Release Notes",
			Category:          "Claude Platform Release Notes",
			Reason:            "抓取失败：EOF",
			IncludeInBriefing: false,
		}},
	}, "26.04.15", "1600")
	if !strings.Contains(text, "site_error") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "抓取失败：EOF") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "Claude Platform Release Notes / Claude Platform Release Notes") {
		t.Fatalf("text = %q", text)
	}
}
