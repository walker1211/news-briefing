package fetcher

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

func loadExampleConfigForFinanceTest(t *testing.T) (*config.Config, filterContext) {
	t.Helper()

	path := filepath.Join("..", "..", "configs", "config.example.yaml")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load(%q) error = %v", path, err)
	}

	return cfg, newFilterContext(cfg)
}

func requireFilterKeywords(t *testing.T, got []string, want ...string) {
	t.Helper()

	for _, expected := range want {
		found := false
		for _, actual := range got {
			if strings.EqualFold(actual, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("keywords = %#v, want keyword %q", got, expected)
		}
	}
}

func TestFinanceExampleConfigSelectsGlobalFinanceStories(t *testing.T) {
	_, filters := loadExampleConfigForFinanceTest(t)

	tests := []struct {
		name          string
		title         string
		wantMatched   []string
		wantExcluded  []string
		wantNoMatched bool
	}{
		{
			name:        "english gold and bullion",
			title:       "Gold prices rise as bullion demand strengthens",
			wantMatched: []string{"gold", "bullion"},
		},
		{
			name:        "federal reserve",
			title:       "Federal Reserve signals a cautious policy path",
			wantMatched: []string{"Federal Reserve"},
		},
		{
			name:        "plural and hyphenated rate phrases",
			title:       "Interest rates stay high as rate-cut hopes fade",
			wantMatched: []string{"interest rates", "rate-cut"},
		},
		{
			name:        "fed needs financial context",
			title:       "Fed holds rates steady",
			wantMatched: []string{"Fed", "rates"},
		},
		{
			name:        "global stocks use two weak keywords",
			title:       "Global stocks and bonds retreat after overseas session",
			wantMatched: []string{"stocks", "bonds"},
		},
		{
			name:          "ordinary fed usage",
			title:         "The puppy was fed before the flight",
			wantNoMatched: true,
		},
		{
			name:          "only one weak keyword",
			title:         "Global stocks rally",
			wantNoMatched: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			matched, excluded := filterCandidate(
				model.Article{Category: "新闻财经", Title: tc.title},
				config.Source{},
				filters,
			)
			if tc.wantNoMatched {
				if len(matched) != 0 {
					t.Fatalf("matched = %#v, want no matched keywords", matched)
				}
			} else {
				requireFilterKeywords(t, matched, tc.wantMatched...)
			}
			if len(excluded) != len(tc.wantExcluded) {
				t.Fatalf("excluded = %#v, want %d keywords", excluded, len(tc.wantExcluded))
			}
			requireFilterKeywords(t, excluded, tc.wantExcluded...)
		})
	}
}

func TestFinanceExampleConfigNarrowsExclusions(t *testing.T) {
	_, filters := loadExampleConfigForFinanceTest(t)

	tests := []struct {
		name         string
		title        string
		wantMatched  []string
		wantExcluded []string
	}{
		{
			name:        "gold futures limit up is finance news",
			title:       "黄金期货涨停",
			wantMatched: []string{"黄金"},
		},
		{
			name:        "intraday stock decline is finance news",
			title:       "美股盘中大跌",
			wantMatched: []string{"美股"},
		},
		{
			name:         "stock recommendation remains excluded",
			title:        "个股推荐：美股投资机会",
			wantMatched:  []string{"美股"},
			wantExcluded: []string{"个股推荐"},
		},
		{
			name:         "sports noise remains excluded",
			title:        "体育赛事新闻",
			wantExcluded: []string{"体育"},
		},
		{
			name:         "world cup noise remains excluded",
			title:        "世界杯赛程",
			wantExcluded: []string{"世界杯"},
		},
		{
			name:         "football noise remains excluded",
			title:        "足球联赛",
			wantExcluded: []string{"足球"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			matched, excluded := filterCandidate(
				model.Article{Category: "新闻财经", Title: tc.title},
				config.Source{},
				filters,
			)
			requireFilterKeywords(t, matched, tc.wantMatched...)
			if len(excluded) != len(tc.wantExcluded) {
				t.Fatalf("excluded = %#v, want %d keywords", excluded, len(tc.wantExcluded))
			}
			requireFilterKeywords(t, excluded, tc.wantExcluded...)
		})
	}
}

func TestFinanceExampleConfigKeepsAIFilterBehavior(t *testing.T) {
	_, filters := loadExampleConfigForFinanceTest(t)

	tests := []struct {
		name        string
		title       string
		wantMatched []string
		wantNoMatch bool
	}{
		{
			name:        "strong keyword",
			title:       "OpenAI publishes a new capability",
			wantMatched: []string{"OpenAI"},
		},
		{
			name:        "one weak keyword",
			title:       "Google publishes a search update",
			wantNoMatch: true,
		},
		{
			name:        "two weak keywords",
			title:       "Google 发布开源模型",
			wantMatched: []string{"Google", "开源", "模型"},
		},
		{
			name:        "unrelated story",
			title:       "普通消费电子新闻",
			wantNoMatch: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			matched, excluded := filterCandidate(
				model.Article{Category: "AI/科技", Title: tc.title},
				config.Source{},
				filters,
			)
			if len(excluded) != 0 {
				t.Fatalf("excluded = %#v, want no excluded keywords", excluded)
			}
			if tc.wantNoMatch {
				if len(matched) != 0 {
					t.Fatalf("matched = %#v, want no matched keywords", matched)
				}
				return
			}
			requireFilterKeywords(t, matched, tc.wantMatched...)
		})
	}
}
