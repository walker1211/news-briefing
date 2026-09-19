package fetcher

import (
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/walker1211/news-briefing/internal/model"
)

const (
	maxKeywordScore = 400
	incidentScore   = 600
	responseScore   = 100
	roundupPenalty  = 450
)

var securityIncidentKeywords = []string{
	"数据泄露", "隐私泄露", "代码泄露", "偷传代码", "静默上传", "未经授权上传", "未经同意上传",
	"安全漏洞", "零日漏洞", "远程代码执行",
	"data breach", "data leak", "privacy breach", "code exfiltration", "unauthorized upload",
	"vulnerability", "zero-day", "remote code execution", "security incident",
}

var ambiguousSecurityIncidentKeywords = []string{"入侵", "越狱", "jailbreak"}

var digitalTechnologyContextKeywords = []string{
	"AI", "模型", "代码", "软件", "网络", "数据", "账号", "服务器", "Claude", "Gemini", "LLM",
	"cyber", "software", "server", "model", "Android", "iOS",
}

var incidentResponseKeywords = []string{
	"官方回应", "致歉", "已修复", "修复", "通报", "补丁", "apologizes", "apology", "patch", "fixed",
}

var roundupTitlePrefixes = []string{
	"IT早报", "早报", "晚报", "日报", "周报", "科技早报", "科技晚报", "AI早报", "AI日报", "GPT周报",
	"weekly roundup", "daily roundup", "news roundup",
}

// ArticleRankingScore contains the bounded source-limit ranking components.
type ArticleRankingScore struct {
	Keywords       int `json:"keywords"`
	Event          int `json:"event"`
	Freshness      int `json:"freshness"`
	RoundupPenalty int `json:"roundup_penalty"`
	Total          int `json:"total"`
}

// ScoreArticle scores an article using the same keyword-boundary rules as
// source filtering. It deliberately scores only visible title and summary text.
func ScoreArticle(article model.Article, strong, weak []string, newest time.Time) ArticleRankingScore {
	title := visibleText(article.Title)
	summary := visibleText(article.Summary)

	keywords := len(MatchKeywords(title, strong))*120 +
		len(MatchKeywords(summary, strong))*50 +
		len(MatchKeywords(title, weak))*35 +
		len(MatchKeywords(summary, weak))*15
	if keywords > maxKeywordScore {
		keywords = maxKeywordScore
	}

	isRoundup := isRoundupTitle(title)
	event := 0
	if !isRoundup && isSecurityIncidentTitle(title) {
		event = incidentScore
		if len(MatchKeywords(title, incidentResponseKeywords)) > 0 {
			event += responseScore
		}
	}

	freshness := 0
	if !newest.IsZero() && !article.Published.IsZero() {
		hours := max(0, int(newest.Sub(article.Published)/time.Hour))
		freshness = max(0, 100-5*hours)
	}

	penalty := 0
	if isRoundup {
		penalty = roundupPenalty
	}
	return ArticleRankingScore{
		Keywords:       keywords,
		Event:          event,
		Freshness:      freshness,
		RoundupPenalty: penalty,
		Total:          keywords + event + freshness - penalty,
	}
}

func isSecurityIncidentTitle(title string) bool {
	if len(MatchKeywords(title, securityIncidentKeywords)) > 0 {
		return true
	}
	return len(MatchKeywords(title, ambiguousSecurityIncidentKeywords)) > 0 &&
		len(MatchKeywords(title, digitalTechnologyContextKeywords)) > 0
}

func visibleText(value string) string {
	node, err := html.Parse(strings.NewReader(value))
	if err != nil {
		return value
	}
	var parts []string
	var visit func(*html.Node)
	visit = func(current *html.Node) {
		if current.Type == html.ElementNode && (current.Data == "script" || current.Data == "style") {
			return
		}
		if current.Type == html.TextNode {
			if text := strings.TrimSpace(current.Data); text != "" {
				parts = append(parts, text)
			}
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return strings.Join(parts, " ")
}

func isRoundupTitle(title string) bool {
	normalized := strings.ToLower(strings.TrimSpace(title))
	for _, prefix := range roundupTitlePrefixes {
		if strings.HasPrefix(normalized, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}
