package output

import (
	"fmt"
	"strings"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
)

func formatArticlePublishedAt(published time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	return published.In(loc).Format("2006-01-02 15:04")
}

func ArticleListView(articles []model.Article, loc *time.Location) string {
	var sb strings.Builder
	for i, a := range articles {
		writeArticleListItem(&sb, i+1, a, loc, true)
	}
	return sb.String()
}

func writeArticleListItem(sb *strings.Builder, number int, article model.Article, loc *time.Location, includeCategory bool) {
	if includeCategory {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s\n", number, article.Category, article.Title))
	} else {
		sb.WriteString(fmt.Sprintf("%d. %s\n", number, article.Title))
	}
	sb.WriteString(fmt.Sprintf("   %s\n   Source: %s | %s\n   Link: %s\n",
		article.Summary, article.Source, formatArticlePublishedAt(article.Published, loc), article.Link))
	if strings.TrimSpace(article.ImageURL) != "" {
		sb.WriteString("   Image: " + strings.TrimSpace(article.ImageURL) + "\n")
	}
	sb.WriteString("\n")
}

func OrderedCategories(articles []model.Article, categoryOrder []string) []string {
	seen := make(map[string]struct{})
	ordered := make([]string, 0, len(categoryOrder))
	for _, cat := range categoryOrder {
		cat = strings.TrimSpace(cat)
		if _, ok := seen[cat]; ok {
			continue
		}
		seen[cat] = struct{}{}
		ordered = append(ordered, cat)
	}
	for _, article := range articles {
		cat := strings.TrimSpace(article.Category)
		if _, ok := seen[cat]; ok {
			continue
		}
		seen[cat] = struct{}{}
		ordered = append(ordered, cat)
	}
	return ordered
}

func OrderedArticleList(articles []model.Article, categoryOrder []string) []model.Article {
	grouped := make(map[string][]model.Article)
	for _, article := range articles {
		grouped[article.Category] = append(grouped[article.Category], article)
	}
	ordered := make([]model.Article, 0, len(articles))
	for _, category := range OrderedCategories(articles, categoryOrder) {
		ordered = append(ordered, grouped[category]...)
	}
	return ordered
}

func GroupedArticleListView(articles []model.Article, categoryOrder []string, loc *time.Location) string {
	grouped := make(map[string][]model.Article)
	for _, a := range articles {
		grouped[a.Category] = append(grouped[a.Category], a)
	}

	var sb strings.Builder
	n := 1
	for _, cat := range OrderedCategories(articles, categoryOrder) {
		items := grouped[cat]
		if len(items) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("== %s (%d篇) ==\n\n", cat, len(items)))
		for _, a := range items {
			writeArticleListItem(&sb, n, a, loc, false)
			n++
		}
	}
	return sb.String()
}

func StructuredBriefingMarkdown(summary model.BriefingSummary, categoryOrder []string) string {
	var sb strings.Builder
	writeStructuredOverview(&sb, summary.OverviewGroups)
	writeStructuredStories(&sb, summary.Stories, categoryOrder)
	writeStructuredSituation(&sb, summary.Situation)
	writeStructuredDirections(&sb, summary.Directions)
	return strings.TrimSpace(sb.String())
}

func writeStructuredOverview(sb *strings.Builder, groups []model.BriefingOverviewGroup) {
	sb.WriteString("## 今日速览\n\n")
	for _, group := range groups {
		category := strings.TrimSpace(group.Category)
		if category == "" || len(group.Items) == 0 {
			continue
		}
		sb.WriteString("### " + category + "\n\n")
		for _, item := range group.Items {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			sb.WriteString("- " + strings.TrimPrefix(item, "- ") + "\n")
		}
		sb.WriteString("\n")
	}
}

func writeStructuredStories(sb *strings.Builder, stories []model.BriefingStory, categoryOrder []string) {
	grouped := make(map[string][]model.BriefingStory)
	for _, story := range stories {
		category := strings.TrimSpace(story.Category)
		if category == "" {
			category = "未分类"
		}
		story.Category = category
		grouped[category] = append(grouped[category], story)
	}
	for _, category := range orderedStoryCategories(stories, categoryOrder) {
		items := grouped[category]
		if len(items) == 0 {
			continue
		}
		sb.WriteString("## " + category + "\n\n")
		for _, story := range items {
			writeStructuredStory(sb, story)
		}
	}
}

func orderedStoryCategories(stories []model.BriefingStory, categoryOrder []string) []string {
	seen := make(map[string]struct{})
	ordered := make([]string, 0, len(categoryOrder))
	for _, category := range categoryOrder {
		category = strings.TrimSpace(category)
		if category == "" {
			continue
		}
		if _, ok := seen[category]; ok {
			continue
		}
		seen[category] = struct{}{}
		ordered = append(ordered, category)
	}
	for _, story := range stories {
		category := strings.TrimSpace(story.Category)
		if category == "" {
			category = "未分类"
		}
		if _, ok := seen[category]; ok {
			continue
		}
		seen[category] = struct{}{}
		ordered = append(ordered, category)
	}
	return ordered
}

func writeStructuredStory(sb *strings.Builder, story model.BriefingStory) {
	title := strings.TrimSpace(story.Title)
	if title == "" {
		title = "未命名新闻"
	}
	sb.WriteString("### " + title + "\n")
	if imageURL := strings.TrimSpace(story.ImageURL); imageURL != "" {
		sb.WriteString("![" + markdownImageAlt(title) + "](" + imageURL + ")\n")
	}
	if summary := strings.TrimSpace(story.Summary); summary != "" {
		sb.WriteString("**摘要：** " + summary + "  \n")
	}
	if impact := strings.TrimSpace(story.Impact); impact != "" {
		sb.WriteString("**影响：** " + impact + "  \n")
	}
	if source := strings.TrimSpace(story.SourceLine); source != "" {
		sb.WriteString("> " + strings.TrimPrefix(source, "> ") + "\n")
	}
	sb.WriteString("\n")
}

func markdownImageAlt(value string) string {
	value = strings.ReplaceAll(value, "[", "")
	value = strings.ReplaceAll(value, "]", "")
	return strings.TrimSpace(value)
}

func writeStructuredSituation(sb *strings.Builder, situation string) {
	sb.WriteString("---\n## 今日态势\n\n")
	if situation = strings.TrimSpace(situation); situation != "" {
		sb.WriteString(situation + "\n\n")
	}
}

func writeStructuredDirections(sb *strings.Builder, directions []model.BriefingDirection) {
	sb.WriteString("---\n## 今日最值得追的方向\n\n")
	for i, direction := range directions {
		title := strings.TrimSpace(direction.Title)
		if title == "" {
			title = "未命名方向"
		}
		sb.WriteString(fmt.Sprintf("### 方向%d：%s\n", i+1, title))
		if why := strings.TrimSpace(direction.Why); why != "" {
			sb.WriteString("**为什么值得追：** " + why + "\n")
		}
		if next := strings.TrimSpace(direction.Next); next != "" {
			sb.WriteString("**接下来关注什么：** " + next + "\n")
		}
		if command := strings.TrimSpace(direction.DeepCommand); command != "" {
			sb.WriteString("**深挖命令：** " + command + "\n")
		}
		sb.WriteString("\n")
	}
}
