package fetcher

import (
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestScoreArticleCapsKeywordBoostAndRecognizesIncidents(t *testing.T) {
	newest := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	score := ScoreArticle(model.Article{
		Title:     "数据泄露后官方回应并已修复",
		Summary:   "OpenAI OpenAI OpenAI OpenAI",
		Published: newest,
	}, []string{"OpenAI", "数据泄露", "官方回应", "已修复"}, []string{"OpenAI"}, newest)
	if score.Keywords != maxKeywordScore {
		t.Fatalf("Keywords = %d, want %d", score.Keywords, maxKeywordScore)
	}
	if score.Event != incidentScore+responseScore {
		t.Fatalf("Event = %d, want %d", score.Event, incidentScore+responseScore)
	}
	if score.Freshness != 100 {
		t.Fatalf("Freshness = %d, want 100", score.Freshness)
	}

	financial := ScoreArticle(model.Article{Title: "公司回应季度财报争议", Published: newest}, nil, nil, newest)
	if financial.Event != 0 {
		t.Fatalf("generic financial controversy Event = %d, want 0", financial.Event)
	}
	for _, title := range []string{"军队入侵邻国", "囚犯越狱"} {
		if got := ScoreArticle(model.Article{Title: title, Published: newest}, nil, nil, newest).Event; got != 0 {
			t.Fatalf("%q Event = %d, want 0", title, got)
		}
	}
	if got := ScoreArticle(model.Article{Title: "Gemini 越狱后入侵三家公司", Published: newest}, nil, nil, newest).Event; got != incidentScore {
		t.Fatalf("technical-context incident Event = %d, want %d", got, incidentScore)
	}
}

func TestScoreArticleIgnoresHTMLAttributesAndPenalizesAnchoredRoundups(t *testing.T) {
	newest := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	attributeOnly := ScoreArticle(model.Article{
		Title:     `<img alt="OpenAI" src="https://example.test/OpenAI.png">routine update`,
		Summary:   `<a href="https://example.test/OpenAI">read more</a>`,
		Published: newest,
	}, []string{"OpenAI"}, nil, newest)
	if attributeOnly.Keywords != 0 {
		t.Fatalf("HTML attribute Keywords = %d, want 0", attributeOnly.Keywords)
	}
	if got := ScoreArticle(model.Article{Title: "AI早报：今日模型动态", Published: newest}, nil, nil, newest); got.RoundupPenalty != roundupPenalty {
		t.Fatalf("anchored roundup penalty = %d, want %d", got.RoundupPenalty, roundupPenalty)
	}
	if got := ScoreArticle(model.Article{Title: "这份报道提到 daily roundup 的编辑方式", Published: newest}, nil, nil, newest); got.RoundupPenalty != 0 {
		t.Fatalf("non-anchored roundup penalty = %d, want 0", got.RoundupPenalty)
	}
}

func TestScoreArticleFreshnessUsesWholeHours(t *testing.T) {
	newest := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if got := ScoreArticle(model.Article{Published: newest.Add(-19*time.Hour - 59*time.Minute)}, nil, nil, newest).Freshness; got != 5 {
		t.Fatalf("Freshness = %d, want 5", got)
	}
	if got := ScoreArticle(model.Article{}, nil, nil, newest).Freshness; got != 0 {
		t.Fatalf("zero publication freshness = %d, want 0", got)
	}
	if got := ScoreArticle(model.Article{Published: newest.Add(time.Hour)}, nil, nil, newest).Freshness; got != 100 {
		t.Fatalf("future publication freshness = %d, want 100", got)
	}
}

func TestScoreArticleRoundupDoesNotDoubleCountIncident(t *testing.T) {
	article := model.Article{Title: "IT早报：ExampleCode 偷传代码事件致歉；AI 模型更新"}
	score := ScoreArticle(article, []string{"AI"}, nil, time.Time{})
	if score.Event != 0 || score.RoundupPenalty != roundupPenalty {
		t.Fatalf("roundup score = %+v, want no event bonus and roundup penalty", score)
	}
}
