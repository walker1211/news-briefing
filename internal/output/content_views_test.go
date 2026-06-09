package output

import (
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestArticleListViewUsesProvidedLocation(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}

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

	want := "1. [AI/科技] OpenAI ships feature\n   Feature summary\n   Source: Example | 2026-03-18 07:00\n   Link: https://example.com/openai\n\n2. [国际政治] Policy update\n   Policy summary\n   Source: Example Politics | 2026-03-18 08:30\n   Link: https://example.com/policy\n\n"
	if got := ArticleListView(articles, loc); got != want {
		t.Fatalf("ArticleListView() = %q, want %q", got, want)
	}
}

func TestArticleListViewIncludesImageOnlyWhenPresent(t *testing.T) {
	articles := []model.Article{
		{
			Title:     "OpenAI ships feature",
			Summary:   "Feature summary",
			ImageURL:  "https://example.com/cover.jpg",
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

	got := ArticleListView(articles, time.UTC)
	if !strings.Contains(got, "   Image: https://example.com/cover.jpg\n") {
		t.Fatalf("ArticleListView() = %q, want image line", got)
	}
	if strings.Contains(got, "Policy update\n   Policy summary\n   Source: Example Politics | 2026-03-18 15:30\n   Link: https://example.com/policy\n   Image:") {
		t.Fatalf("ArticleListView() = %q, should not include empty image line", got)
	}
}

func TestGroupedArticleListViewIncludesImageOnlyWhenPresent(t *testing.T) {
	articles := []model.Article{
		{
			Title:     "OpenAI ships feature",
			Summary:   "Feature summary",
			ImageURL:  "https://example.com/cover.jpg",
			Source:    "Example",
			Link:      "https://example.com/openai",
			Category:  "AI/科技",
			Published: time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC),
		},
	}

	got := GroupedArticleListView(articles, []string{"AI/科技"}, time.UTC)
	if !strings.Contains(got, "   Image: https://example.com/cover.jpg\n") {
		t.Fatalf("GroupedArticleListView() = %q, want image line", got)
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

	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}

	categoryOrder := []string{"AI/科技", "国际政治"}
	want := "== AI/科技 (1篇) ==\n\n1. OpenAI ships feature\n   Feature summary\n   Source: Example | 2026-03-18 07:00\n   Link: https://example.com/openai\n\n== 国际政治 (1篇) ==\n\n2. Policy update\n   Policy summary\n   Source: Example Politics | 2026-03-18 08:30\n   Link: https://example.com/policy\n\n"
	if got := GroupedArticleListView(articles, categoryOrder, loc); got != want {
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

	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}

	categoryOrder := []string{"国际政治", "AI/科技"}
	want := "== 国际政治 (1篇) ==\n\n1. Policy update\n   Policy summary\n   Source: Example Politics | 2026-03-18 08:30\n   Link: https://example.com/policy\n\n== AI/科技 (1篇) ==\n\n2. OpenAI ships feature\n   Feature summary\n   Source: Example | 2026-03-18 07:00\n   Link: https://example.com/openai\n\n== 开源工具 (1篇) ==\n\n3. Tooling launch\n   Tooling summary\n   Source: Example Tools | 2026-03-18 09:00\n   Link: https://example.com/tools\n\n"
	if got := GroupedArticleListView(articles, categoryOrder, loc); got != want {
		t.Fatalf("GroupedArticleListView() = %q, want %q", got, want)
	}
}

func TestOrderedArticleListUsesGroupedPromptOrder(t *testing.T) {
	articles := []model.Article{
		{Title: "Tooling launch", Category: "开源工具"},
		{Title: "Policy update", Category: "国际政治"},
		{Title: "OpenAI ships feature", Category: "AI/科技"},
		{Title: "Claude ships feature", Category: "AI/科技"},
	}

	got := OrderedArticleList(articles, []string{"AI/科技", "国际政治"})
	wantTitles := []string{"OpenAI ships feature", "Claude ships feature", "Policy update", "Tooling launch"}
	if len(got) != len(wantTitles) {
		t.Fatalf("len(OrderedArticleList()) = %d, want %d", len(got), len(wantTitles))
	}
	for i, want := range wantTitles {
		if got[i].Title != want {
			t.Fatalf("OrderedArticleList()[%d].Title = %q, want %q", i, got[i].Title, want)
		}
	}
}

func TestStructuredBriefingMarkdownRendersStoryImageBelowTitle(t *testing.T) {
	summary := model.BriefingSummary{
		OverviewGroups: []model.BriefingOverviewGroup{{Category: "AI/科技", Items: []string{"🤖 OpenAI 发布新功能"}}},
		Stories: []model.BriefingStory{{
			Category:   "AI/科技",
			Title:      "OpenAI 发布新功能",
			ImageURL:   "https://example.com/openai.jpg",
			Summary:    "功能摘要。",
			Impact:     "影响分析。",
			SourceLine: "来源: Example | 2026-03-18 14:00",
		}},
		Situation: "今日态势。",
		Directions: []model.BriefingDirection{{
			Title:       "OpenAI roadmap",
			Why:         "值得继续追。",
			Next:        "观察发布节奏。",
			DeepCommand: "./news-briefing deep \"OpenAI roadmap\" --ignore-seen",
		}},
	}

	got := StructuredBriefingMarkdown(summary, []string{"AI/科技"})
	want := "### OpenAI 发布新功能\n![OpenAI 发布新功能](https://example.com/openai.jpg)\n**摘要：** 功能摘要。"
	if !strings.Contains(got, want) {
		t.Fatalf("StructuredBriefingMarkdown() = %q, want story image under title", got)
	}
}

func TestStructuredBriefingMarkdownOmitsEmptyStoryImage(t *testing.T) {
	summary := model.BriefingSummary{Stories: []model.BriefingStory{{Category: "AI/科技", Title: "OpenAI 发布新功能", Summary: "功能摘要。"}}}

	got := StructuredBriefingMarkdown(summary, []string{"AI/科技"})
	if strings.Contains(got, "![") {
		t.Fatalf("StructuredBriefingMarkdown() = %q, should not render empty image", got)
	}
}

func TestStructuredBriefingMarkdownUsesCategoryOrder(t *testing.T) {
	summary := model.BriefingSummary{Stories: []model.BriefingStory{
		{Category: "国际政治", Title: "Policy update", Summary: "Policy summary."},
		{Category: "AI/科技", Title: "OpenAI ships", Summary: "AI summary."},
		{Category: "开源工具", Title: "Tooling launch", Summary: "Tool summary."},
	}}

	got := StructuredBriefingMarkdown(summary, []string{"AI/科技", "国际政治"})
	ai := strings.Index(got, "## AI/科技")
	politics := strings.Index(got, "## 国际政治")
	tools := strings.Index(got, "## 开源工具")
	if ai < 0 || politics < 0 || tools < 0 || !(ai < politics && politics < tools) {
		t.Fatalf("StructuredBriefingMarkdown() order = %q", got)
	}
}
