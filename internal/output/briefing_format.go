package output

import (
	"fmt"

	"github.com/walker1211/news-briefing/internal/model"
)

func briefingClock(period string) string {
	if len(period) != 4 {
		return period
	}
	return period[:2] + ":" + period[2:]
}

func briefingTitle(date, period string) string {
	return fmt.Sprintf("国际资讯简报 %s %s %s", date, periodPrefix(period), briefingClock(period))
}

func aiTechBriefingTitle(date, period string) string {
	label := "早报"
	if len(period) >= 2 && period[:2] >= "18" {
		label = "晚报"
	}
	return fmt.Sprintf("AI科技%s | %s %s", label, date, briefingClock(period))
}

func briefingMarkdownHeader(date, period string) string {
	return "# " + briefingTitle(date, period)
}

func briefingEmailSubject(date, period string) string {
	return fmt.Sprintf("[资讯简报] %s %s %s", date, periodPrefix(period), briefingClock(period))
}

func briefingHeaderBlock(briefing *model.Briefing) string {
	if briefing == nil {
		return ""
	}
	return briefingMarkdownHeader(briefing.Date, briefing.Period) + "\n\n"
}

func briefingFileName(date, period string) string {
	return fmt.Sprintf("%s-%s-%s.md", date, periodPrefix(period), period)
}

func briefingIndexFileName(date, period string) string {
	return fmt.Sprintf("%s-%s-%s.json", date, periodPrefix(period), period)
}

func watchTitle(date, period string) string {
	return fmt.Sprintf("非新闻监听 %s %s %s", date, periodPrefix(period), briefingClock(period))
}

func watchFileName(date, period string) string {
	return fmt.Sprintf("%s-%s-%s-watch.md", date, periodPrefix(period), period)
}

func deepEmailSubject(topic string) string {
	return fmt.Sprintf("[资讯简报] 话题深挖 | %s", topic)
}

func deepEmailTitle(topic string) string {
	return fmt.Sprintf("国际资讯话题深挖 | %s", topic)
}
