package output

import "fmt"

func briefingClock(period string) string {
	if len(period) != 4 {
		return period
	}
	return period[:2] + ":" + period[2:]
}

func briefingTitle(date, period string) string {
	return fmt.Sprintf("国际资讯简报 %s %s %s", date, periodPrefix(period), briefingClock(period))
}

func briefingMarkdownHeader(date, period string) string {
	return "# " + briefingTitle(date, period)
}

func briefingEmailSubject(date, period string) string {
	return fmt.Sprintf("[资讯简报] %s %s %s", date, periodPrefix(period), briefingClock(period))
}

func briefingFileName(date, period string) string {
	return fmt.Sprintf("%s-%s-%s.md", date, periodPrefix(period), period)
}

func deepEmailSubject(topic string) string {
	return fmt.Sprintf("[资讯简报] 话题深挖 | %s", topic)
}

func deepEmailTitle(topic string) string {
	return fmt.Sprintf("国际资讯话题深挖 | %s", topic)
}
