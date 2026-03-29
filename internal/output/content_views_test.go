package output

import (
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestArticleListView(t *testing.T) {
	articles := []model.Article{
		{
			Title:     "OpenAI ships feature",
			Summary:   "Feature summary",
			Source:    "Example",
			Link:      "https://example.com/openai",
			Category:  "AI/科技",
			Published: time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC),
		},
		{
			Title:     "Policy update",
			Summary:   "Policy summary",
			Source:    "Example Politics",
			Link:      "https://example.com/policy",
			Category:  "国际政治",
			Published: time.Date(2026, 3, 18, 15, 30, 0, 0, time.UTC),
		},
	}

	want := "1. [AI/科技] OpenAI ships feature\n   Feature summary\n   Source: Example | 2026-03-18 22:00\n   Link: https://example.com/openai\n\n2. [国际政治] Policy update\n   Policy summary\n   Source: Example Politics | 2026-03-18 23:30\n   Link: https://example.com/policy\n\n"
	if got := ArticleListView(articles); got != want {
		t.Fatalf("ArticleListView() = %q, want %q", got, want)
	}
}

func TestGroupedArticleListViewUsesConfiguredCategoryOrder(t *testing.T) {
	articles := []model.Article{
		{
			Title:     "Policy update",
			Summary:   "Policy summary",
			Source:    "Example Politics",
			Link:      "https://example.com/policy",
			Category:  "国际政治",
			Published: time.Date(2026, 3, 18, 15, 30, 0, 0, time.UTC),
		},
		{
			Title:     "OpenAI ships feature",
			Summary:   "Feature summary",
			Source:    "Example",
			Link:      "https://example.com/openai",
			Category:  "AI/科技",
			Published: time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC),
		},
	}

	categoryOrder := []string{"AI/科技", "国际政治"}
	want := "== AI/科技 (1篇) ==\n\n1. OpenAI ships feature\n   Feature summary\n   Source: Example | 2026-03-18 22:00\n   Link: https://example.com/openai\n\n== 国际政治 (1篇) ==\n\n2. Policy update\n   Policy summary\n   Source: Example Politics | 2026-03-18 23:30\n   Link: https://example.com/policy\n\n"
	if got := GroupedArticleListView(articles, categoryOrder); got != want {
		t.Fatalf("GroupedArticleListView() = %q, want %q", got, want)
	}
}

func TestGroupedArticleListViewAppendsUnknownCategoriesAfterConfiguredOnes(t *testing.T) {
	articles := []model.Article{
		{
			Title:     "Tooling launch",
			Summary:   "Tooling summary",
			Source:    "Example Tools",
			Link:      "https://example.com/tools",
			Category:  "开源工具",
			Published: time.Date(2026, 3, 18, 16, 0, 0, 0, time.UTC),
		},
		{
			Title:     "Policy update",
			Summary:   "Policy summary",
			Source:    "Example Politics",
			Link:      "https://example.com/policy",
			Category:  "国际政治",
			Published: time.Date(2026, 3, 18, 15, 30, 0, 0, time.UTC),
		},
		{
			Title:     "OpenAI ships feature",
			Summary:   "Feature summary",
			Source:    "Example",
			Link:      "https://example.com/openai",
			Category:  "AI/科技",
			Published: time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC),
		},
	}

	categoryOrder := []string{"国际政治", "AI/科技"}
	want := "== 国际政治 (1篇) ==\n\n1. Policy update\n   Policy summary\n   Source: Example Politics | 2026-03-18 23:30\n   Link: https://example.com/policy\n\n== AI/科技 (1篇) ==\n\n2. OpenAI ships feature\n   Feature summary\n   Source: Example | 2026-03-18 22:00\n   Link: https://example.com/openai\n\n== 开源工具 (1篇) ==\n\n3. Tooling launch\n   Tooling summary\n   Source: Example Tools | 2026-03-19 00:00\n   Link: https://example.com/tools\n\n"
	if got := GroupedArticleListView(articles, categoryOrder); got != want {
		t.Fatalf("GroupedArticleListView() = %q, want %q", got, want)
	}
}
