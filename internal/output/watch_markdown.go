package output

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/statefile"
)

func WriteWatchMarkdown(report *model.WatchReport, outputDir string, date string, period string) (string, error) {
	watchDir := filepath.Join(outputDir, "watch")
	path := filepath.Join(watchDir, watchFileName(date, period))
	body := RenderWatchMarkdown(report, date, period)
	if err := statefile.WriteAtomicReplaceOnly(path, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("write watch markdown: %w", err)
	}
	return path, nil
}

func RenderWatchMarkdown(report *model.WatchReport, date string, period string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(watchTitle(date, period))
	b.WriteString("\n\n")
	b.WriteString("## 主简报候选\n\n")
	writeWatchSection(&b, filterWatchEvents(report, true))
	b.WriteString("\n## 完整变化\n\n")
	writeWatchSection(&b, filterWatchEvents(report, false))
	return b.String()
}

func filterWatchEvents(report *model.WatchReport, briefingOnly bool) []model.WatchEvent {
	if report == nil || len(report.Events) == 0 {
		return nil
	}
	events := make([]model.WatchEvent, 0, len(report.Events))
	for _, event := range report.Events {
		if briefingOnly && !event.IncludeInBriefing {
			continue
		}
		events = append(events, event)
	}
	return events
}

func writeWatchSection(b *strings.Builder, events []model.WatchEvent) {
	if len(events) == 0 {
		b.WriteString("- 本次未检测到变化\n")
		return
	}

	sorted := append([]model.WatchEvent(nil), events...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Source != sorted[j].Source {
			return sorted[i].Source < sorted[j].Source
		}
		if sorted[i].Category != sorted[j].Category {
			return sorted[i].Category < sorted[j].Category
		}
		if sorted[i].ArticleTitle != sorted[j].ArticleTitle {
			return sorted[i].ArticleTitle < sorted[j].ArticleTitle
		}
		return sorted[i].EventType < sorted[j].EventType
	})

	lastGroup := ""
	for _, event := range sorted {
		group := event.Source + "::" + event.Category
		if group != lastGroup {
			if lastGroup != "" {
				b.WriteString("\n")
			}
			b.WriteString("### ")
			if event.Category != "" {
				b.WriteString(event.Source)
				b.WriteString(" / ")
				b.WriteString(event.Category)
			} else {
				b.WriteString(event.Source)
			}
			b.WriteString("\n\n")
			lastGroup = group
		}
		b.WriteString("- `")
		b.WriteString(event.EventType)
		b.WriteString("`")
		if event.ArticleTitle != "" {
			b.WriteString(" ")
			b.WriteString(event.ArticleTitle)
		}
		if event.Reason != "" {
			b.WriteString(" — ")
			b.WriteString(event.Reason)
		}
		if event.ArticleURL != "" {
			b.WriteString("\n  - URL: ")
			b.WriteString(event.ArticleURL)
		}
		b.WriteString("\n  - 进入主简报: ")
		if event.IncludeInBriefing {
			b.WriteString("是")
		} else {
			b.WriteString("否")
		}
		b.WriteString("\n")
	}
}
