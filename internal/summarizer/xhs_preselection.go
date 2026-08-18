package summarizer

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/walker1211/news-briefing/internal/model"
)

var xhsPreselectionNegativeTerms = []string{
	"兑付危机", "爆雷", "账户被冻结", "资金被冻结", "资金无法兑付", "无法兑付",
	"投资者维权", "赴港维权", "跑路", "诈骗", "欺诈", "造假", "贿赂", "内幕交易",
	"非法获利", "被捕", "欠薪", "破产", "清盘", "违约", "销毁", "负面信息",
}

var xhsPreselectionOfficialOnlyTerms = []string{
	"年化营收", "季度营收", "营业利润", "利润率", "估值", "融资", "募资", "收购", "并购",
	"IPO", "上市", "发行价", "发行市值", "市值", "担保", "账户被冻结", "资金被冻结",
}

var xhsPreselectionReportedTerms = []string{
	"据报", "报道称", "媒体报道", "援引知情人士", "知情人士称", "消息人士称", "调查称", "被曝", "传出",
}

var xhsPreselectionOfficialLabels = []string{
	"官方公告", "公司公告", "监管公告", "国务院", "人民银行", "发改委", "能源局", "国家统计局",
	"证监会", "交易所", "法院", "检察院", "公安", "政府网站", "SEC", "HKEX",
}

var xhsPreselectionDefaultOfficialHosts = []string{
	"gov.cn", "pbc.gov.cn", "ndrc.gov.cn", "nea.gov.cn", "csrc.gov.cn", "sse.com.cn",
	"szse.cn", "bse.cn", "hkexnews.hk", "hkex.com.hk", "sfc.hk", "sec.gov",
}

func (r *Runner) applyXHSPreselection(summary *model.BriefingSummary, candidates []model.BriefingStory, articles []model.Article) {
	if summary == nil || !r.xhsPreselectionEnabled {
		return
	}
	summary.XHSStories = preselectXHSStories(
		summary.Stories,
		candidates,
		articles,
		r.xhsPreselectionCategories,
		r.xhsPreselectionTargetItems,
		r.xhsPreselectionMinSources,
		r.xhsPreselectionOfficialHosts,
	)
}

func preselectXHSStories(finalStories, candidates []model.BriefingStory, articles []model.Article, categories []string, targetItems, minimumSources int, officialHosts []string) []model.BriefingStory {
	if targetItems <= 0 {
		return []model.BriefingStory{}
	}
	if minimumSources <= 0 {
		minimumSources = 2
	}
	allowed := make(map[string]struct{}, len(categories))
	orderedCategories := make([]string, 0, len(categories))
	for _, category := range categories {
		category = strings.TrimSpace(category)
		if category == "" {
			continue
		}
		if _, exists := allowed[category]; exists {
			continue
		}
		allowed[category] = struct{}{}
		orderedCategories = append(orderedCategories, category)
	}
	selected := make([]model.BriefingStory, 0, targetItems)
	seen := map[string]struct{}{}
	appendEligible := func(story model.BriefingStory) bool {
		if len(selected) >= targetItems || !xhsStoryEligible(story, articles, allowed, minimumSources, officialHosts) {
			return false
		}
		key := xhsStoryIdentity(story)
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
		selected = append(selected, cloneBriefingStory(story))
		return true
	}
	for _, story := range finalStories {
		appendEligible(story)
	}
	if len(selected) >= targetItems {
		return selected
	}

	queues := make(map[string][]model.BriefingStory, len(orderedCategories))
	for _, story := range candidates {
		category := strings.TrimSpace(story.Category)
		if _, ok := allowed[category]; !ok {
			continue
		}
		if !xhsStoryEligible(story, articles, allowed, minimumSources, officialHosts) {
			continue
		}
		if _, exists := seen[xhsStoryIdentity(story)]; exists {
			continue
		}
		queues[category] = append(queues[category], story)
	}
	indexes := make(map[string]int, len(orderedCategories))
	for len(selected) < targetItems {
		progressed := false
		for _, category := range orderedCategories {
			queue := queues[category]
			for indexes[category] < len(queue) {
				story := queue[indexes[category]]
				indexes[category]++
				if appendEligible(story) {
					progressed = true
					break
				}
			}
			if len(selected) >= targetItems {
				break
			}
		}
		if !progressed {
			break
		}
	}
	return selected
}

func xhsStoryEligible(story model.BriefingStory, articles []model.Article, allowed map[string]struct{}, minimumSources int, officialHosts []string) bool {
	if _, ok := allowed[strings.TrimSpace(story.Category)]; !ok {
		return false
	}
	sources := xhsStorySourceArticles(story, articles)
	if len(sources) == 0 {
		return false
	}
	combined := strings.Join([]string{story.Title, story.Summary, story.Impact}, "\n")
	official := xhsStoryHasOfficialSource(sources, officialHosts)
	material := containsXHSTerm(combined, xhsPreselectionOfficialOnlyTerms) || containsXHSTerm(combined, xhsPreselectionNegativeTerms)
	if material && !official {
		return false
	}
	if containsXHSTerm(combined, xhsPreselectionReportedTerms) && !official && xhsIndependentSourceCount(sources) < minimumSources {
		return false
	}
	return true
}

func xhsStorySourceArticles(story model.BriefingStory, articles []model.Article) []model.Article {
	result := make([]model.Article, 0, len(story.SourceArticleIDs))
	seen := map[int]struct{}{}
	for _, id := range story.SourceArticleIDs {
		index := id - 1
		if index < 0 || index >= len(articles) {
			continue
		}
		if _, exists := seen[index]; exists {
			continue
		}
		seen[index] = struct{}{}
		result = append(result, articles[index])
	}
	return result
}

func xhsStoryHasOfficialSource(articles []model.Article, configuredHosts []string) bool {
	hosts := append(append([]string(nil), xhsPreselectionDefaultOfficialHosts...), configuredHosts...)
	for _, article := range articles {
		if containsXHSTerm(article.Source+"\n"+article.Summary, xhsPreselectionOfficialLabels) || strings.TrimSpace(article.SourceRole) == model.SourceRolePrimary {
			return true
		}
		parsed, err := url.Parse(strings.TrimSpace(article.Link))
		if err != nil {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		for _, candidate := range hosts {
			candidate = strings.ToLower(strings.TrimSpace(candidate))
			if candidate != "" && (host == candidate || strings.HasSuffix(host, "."+candidate)) {
				return true
			}
		}
	}
	return false
}

func xhsIndependentSourceCount(articles []model.Article) int {
	seen := map[string]struct{}{}
	for _, article := range articles {
		if source := strings.ToLower(strings.TrimSpace(article.Source)); source != "" {
			seen[source] = struct{}{}
		}
	}
	return len(seen)
}

func containsXHSTerm(value string, terms []string) bool {
	value = strings.ToLower(value)
	for _, term := range terms {
		if strings.Contains(value, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func xhsStoryIdentity(story model.BriefingStory) string {
	parts := []string{strings.TrimSpace(story.Category), strings.TrimSpace(story.Title)}
	for _, id := range story.SourceArticleIDs {
		parts = append(parts, strconv.Itoa(id))
	}
	return strings.Join(parts, "\x00")
}

func cloneBriefingStory(story model.BriefingStory) model.BriefingStory {
	story.SourceArticleIDs = append([]int(nil), story.SourceArticleIDs...)
	return story
}
