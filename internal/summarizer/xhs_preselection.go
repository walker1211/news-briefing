package summarizer

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

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
	"据报", "报道称", "媒体报道", "援引知情人士", "知情人士称", "消息人士称", "调查称", "被曝", "被指", "传出",
}

var xhsPreselectionOfficialLabels = []string{
	"官方公告", "公司公告", "监管公告", "国务院", "人民银行", "发改委", "能源局", "国家统计局",
	"证监会", "交易所", "法院", "检察院", "公安", "政府网站",
}

var xhsPreselectionDefaultOfficialHosts = []string{
	"gov.cn", "pbc.gov.cn", "ndrc.gov.cn", "nea.gov.cn", "csrc.gov.cn", "sse.com.cn",
	"szse.cn", "bse.cn", "hkexnews.hk", "hkex.com.hk", "sfc.hk", "sec.gov",
}

var xhsBackfillDisallowedTerms = []string{
	"fbi", "监管", "执法", "执法机构", "公共安全", "警方", "警察", "枪击", "枪支", "犯罪", "刑事", "逮捕",
	"中东局势", "地缘政治", "战争", "战事", "军事", "武装", "导弹", "核武", "空袭",
	"袭击", "伤亡", "死亡", "恐怖", "制裁", "冲突",
}

// XHSContextualExclusionRule keeps contextual safety policy independent from
// configuration loading. A story is excluded only when both lists match.
type XHSContextualExclusionRule struct {
	AnchorKeywords  []string
	ContextKeywords []string
}

func cloneXHSContextualExclusionRules(rules []XHSContextualExclusionRule) []XHSContextualExclusionRule {
	cloned := make([]XHSContextualExclusionRule, 0, len(rules))
	for _, rule := range rules {
		cloned = append(cloned, XHSContextualExclusionRule{
			AnchorKeywords:  append([]string(nil), rule.AnchorKeywords...),
			ContextKeywords: append([]string(nil), rule.ContextKeywords...),
		})
	}
	return cloned
}

func (r *Runner) applyXHSPreselection(summary *model.BriefingSummary, candidates []model.BriefingStory, articles []model.Article) {
	if summary == nil || !r.xhsPreselectionEnabled {
		return
	}
	summary.XHSStories = preselectXHSStories(
		summary.Stories,
		candidates,
		articles,
		r.xhsPreselectionEmailOnly,
		r.xhsPreselectionCategories,
		r.xhsPreselectionTargetItems,
		r.xhsPreselectionMinSources,
		r.xhsPreselectionOfficialHosts,
		r.xhsPreselectionExcludedTerms,
		r.xhsPreselectionContextualExclusions,
	)
	// XHSTopics is only consumed by the card manifest. Rebuild it from the
	// selected XHS subset so topics from omitted briefing categories cannot leak
	// into the XHS post.
	summary.XHSTopics = xhsTopicsForStories(summary.XHSStories)
}

func preselectXHSStories(finalStories, candidates []model.BriefingStory, articles []model.Article, emailOnly bool, categories []string, targetItems, minimumSources int, officialHosts, excludedTerms []string, contextualExclusions []XHSContextualExclusionRule) []model.BriefingStory {
	if targetItems <= 0 {
		return []model.BriefingStory{}
	}
	if minimumSources <= 0 {
		minimumSources = 2
	}
	allowed := make(map[string]struct{}, len(categories))
	for _, category := range categories {
		category = strings.TrimSpace(category)
		if category == "" {
			continue
		}
		if _, exists := allowed[category]; exists {
			continue
		}
		allowed[category] = struct{}{}
	}
	selected := make([]model.BriefingStory, 0, targetItems)
	seen := map[string]struct{}{}
	appendEligible := func(story model.BriefingStory, trace *model.XHSSelectionTrace) bool {
		if len(selected) >= targetItems || !xhsStoryEligible(story, articles, allowed, minimumSources, officialHosts, excludedTerms, contextualExclusions) {
			return false
		}
		key := xhsStoryIdentity(story)
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
		story = cloneBriefingStory(story)
		story.XHSSelection = cloneXHSSelectionTrace(trace)
		selected = append(selected, story)
		return true
	}
	for _, story := range finalStories {
		appendEligible(story, &model.XHSSelectionTrace{Origin: "email"})
	}
	if emailOnly || len(selected) >= targetItems {
		return selected
	}

	type rankedCandidate struct {
		story     model.BriefingStory
		selection model.XHSSelectionTrace
	}
	ranked := make([]rankedCandidate, 0, len(candidates))
	newest := time.Time{}
	for _, story := range candidates {
		category := strings.TrimSpace(story.Category)
		if _, ok := allowed[category]; !ok {
			continue
		}
		if !xhsBackfillEligible(story, articles, allowed, minimumSources, officialHosts, excludedTerms, contextualExclusions) {
			continue
		}
		if _, exists := seen[xhsStoryIdentity(story)]; exists {
			continue
		}
		for _, source := range xhsStorySourceArticles(story, articles) {
			if source.Published.After(newest) {
				newest = source.Published
			}
		}
		ranked = append(ranked, rankedCandidate{story: story})
	}
	for index := range ranked {
		ranked[index].selection = xhsBackfillSelection(ranked[index].story, articles, newest, officialHosts)
	}
	// The category workers already order their stories by importance. Preserve
	// that order for equal scores, but compare eligible backfills across
	// categories instead of filling a fixed quota from each category.
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].selection.Score > ranked[j].selection.Score
	})
	for _, candidate := range ranked {
		if len(selected) >= targetItems {
			break
		}
		appendEligible(candidate.story, &candidate.selection)
	}
	return selected
}

// xhsBackfillSelection ranks already-eligible stories, never raw articles. Source
// reliability and freshness are measured from the story's referenced articles;
// a title's keyword count cannot outweigh independently verified sources.
func xhsBackfillSelection(story model.BriefingStory, articles []model.Article, newest time.Time, officialHosts []string) model.XHSSelectionTrace {
	components := map[string]int{}
	if strings.TrimSpace(story.Category) == "AI/科技" {
		components["technology_category"] = 24
	}
	if xhsTechnologyTitle(story.Title) {
		components["technology_title"] = 12
	}
	switch strings.TrimSpace(story.ContentType) {
	case model.ContentTypeTool:
		components["content_type"] = 12
	case model.ContentTypeCase:
		components["content_type"] = 8
	case model.ContentTypeInsight:
		components["content_type"] = 6
	}

	sources := xhsStorySourceArticles(story, articles)
	switch briefingEvidenceLevel(sources) {
	case model.EvidenceCorroborated:
		components["evidence"] = 24
	case model.EvidenceSupported:
		components["evidence"] = 12
	}
	if xhsStoryHasDirectSource(sources, officialHosts) {
		components["direct_source"] = 12
	}
	if !newest.IsZero() {
		latestSource := time.Time{}
		for _, source := range sources {
			if source.Published.After(latestSource) {
				latestSource = source.Published
			}
		}
		if !latestSource.IsZero() {
			switch age := newest.Sub(latestSource); {
			case age <= 6*time.Hour:
				components["freshness"] = 8
			case age <= 24*time.Hour:
				components["freshness"] = 4
			}
		}
	}
	score := 0
	for _, value := range components {
		score += value
	}
	return model.XHSSelectionTrace{Origin: "backfill", Score: score, ScoreComponents: components}
}

func xhsBackfillEligible(story model.BriefingStory, articles []model.Article, allowed map[string]struct{}, minimumSources int, officialHosts, excludedTerms []string, contextualExclusions []XHSContextualExclusionRule) bool {
	if !xhsStoryEligible(story, articles, allowed, minimumSources, officialHosts, excludedTerms, contextualExclusions) {
		return false
	}
	if !xhsTechnologyTitle(story.Title) {
		return false
	}
	combined := strings.Join([]string{story.Title, story.Summary, story.Impact}, "\n")
	if containsXHSTerm(combined, xhsBackfillDisallowedTerms) {
		return false
	}
	sources := xhsStorySourceArticles(story, articles)
	return xhsStoryHasDirectSource(sources, officialHosts) || briefingEvidenceLevel(sources) == model.EvidenceCorroborated
}

func xhsTechnologyTitle(title string) bool {
	for _, rule := range xhsSpecificTopicRules {
		switch rule.topic {
		case "人工智能", "开发者工具", "机器人", "芯片与算力":
			if containsXHSTopicTerm(title, rule.keywords) {
				return true
			}
		}
	}
	return false
}

func xhsStoryEligible(story model.BriefingStory, articles []model.Article, allowed map[string]struct{}, minimumSources int, officialHosts, excludedTerms []string, contextualExclusions []XHSContextualExclusionRule) bool {
	if _, ok := allowed[strings.TrimSpace(story.Category)]; !ok {
		return false
	}
	sources := xhsStorySourceArticles(story, articles)
	if len(sources) == 0 {
		return false
	}
	combined := strings.Join([]string{story.Title, story.Summary, story.Impact}, "\n")
	if containsXHSTerm(combined, excludedTerms) {
		return false
	}
	if matchesXHSContextualExclusion(combined, contextualExclusions) {
		return false
	}
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

func matchesXHSContextualExclusion(value string, rules []XHSContextualExclusionRule) bool {
	for _, rule := range rules {
		if containsXHSTerm(value, rule.AnchorKeywords) && containsXHSTerm(value, rule.ContextKeywords) {
			return true
		}
	}
	return false
}

type xhsTopicRule struct {
	topic    string
	keywords []string
}

var xhsSpecificTopicRules = []xhsTopicRule{
	{topic: "人工智能", keywords: []string{"人工智能", "大模型", "语言模型", "生成式", "openai", "chatgpt", "claude", "gemini", "copilot", "llm", "ai"}},
	{topic: "开发者工具", keywords: []string{"开发者", "开源", "github", "gitlab", "编程", "代码", "codex", "cursor", "sdk", "api"}},
	{topic: "机器人", keywords: []string{"机器人", "robot", "具身智能", "自动驾驶"}},
	{topic: "芯片与算力", keywords: []string{"芯片", "半导体", "gpu", "npu", "算力", "处理器", "risc-v", "英伟达", "nvidia"}},
	{topic: "宏观经济", keywords: []string{"央行", "利率", "通胀", "经济增长", "gdp", "pmi", "就业", "货币政策", "财政政策"}},
	{topic: "财经观察", keywords: []string{"股市", "股票", "债券", "黄金", "汇率", "银行", "财报", "市场", "金融"}},
}

// xhsTopicsForStories builds manifest-only topics from the final XHS selection.
// The fixed rule order makes the same story set produce the same topics.
func xhsTopicsForStories(stories []model.BriefingStory) []string {
	if len(stories) == 0 {
		return nil
	}
	combined := make([]string, 0, len(stories))
	hasTechnology := false
	hasFinance := false
	for _, story := range stories {
		combined = append(combined, strings.Join([]string{story.Title, story.Summary, story.Impact}, "\n"))
		switch strings.TrimSpace(story.Category) {
		case "AI/科技":
			hasTechnology = true
		case "新闻财经":
			hasFinance = true
		}
	}
	content := strings.Join(combined, "\n")
	topics := make([]string, 0, 4)
	for _, rule := range xhsSpecificTopicRules {
		if containsXHSTopicTerm(content, rule.keywords) {
			topics = appendXHSTopic(topics, rule.topic)
		}
	}
	if len(topics) < 3 && hasTechnology {
		topics = appendXHSTopic(topics, "科技资讯")
	}
	if len(topics) < 3 && hasFinance {
		topics = appendXHSTopic(topics, "财经观察")
	}
	if len(topics) < 3 && hasTechnology {
		topics = appendXHSTopic(topics, "科技动态")
	}
	if len(topics) < 3 && hasFinance {
		topics = appendXHSTopic(topics, "市场动态")
	}
	if len(topics) < 3 {
		topics = appendXHSTopic(topics, "每日资讯")
	}
	if len(topics) < 3 {
		// A selected story from a custom category still needs safe generic topics.
		topics = appendXHSTopic(topics, "资讯速览")
	}
	if len(topics) < 3 {
		topics = appendXHSTopic(topics, "今日观察")
	}
	if len(topics) > 4 {
		return topics[:4]
	}
	return topics
}

func containsXHSTopicTerm(value string, terms []string) bool {
	value = strings.ToLower(value)
	for _, term := range terms {
		term = strings.ToLower(term)
		if term == "ai" {
			for offset := 0; ; {
				index := strings.Index(value[offset:], term)
				if index < 0 {
					break
				}
				index += offset
				beforeOK := index == 0 || !isASCIIAlphaNumeric(rune(value[index-1]))
				after := index + len(term)
				afterOK := after == len(value) || !isASCIIAlphaNumeric(rune(value[after]))
				if beforeOK && afterOK {
					return true
				}
				offset = index + len(term)
			}
			continue
		}
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

func isASCIIAlphaNumeric(value rune) bool {
	return value <= unicode.MaxASCII && (unicode.IsLetter(value) || unicode.IsDigit(value))
}

func appendXHSTopic(topics []string, topic string) []string {
	topic = strings.TrimSpace(strings.TrimPrefix(topic, "#"))
	if topic == "" || strings.ContainsAny(topic, " \t\n\r") {
		return topics
	}
	for _, existing := range topics {
		if existing == topic {
			return topics
		}
	}
	return append(topics, topic)
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
		if xhsArticleHasOfficialHost(article, hosts) {
			return true
		}
	}
	return false
}

// Rank only a direct primary source or an official host. A media article that
// merely mentions an official agency can satisfy the existing eligibility
// rule, but it should not get the same provenance bonus as the direct source.
func xhsStoryHasDirectSource(articles []model.Article, configuredHosts []string) bool {
	hosts := append(append([]string(nil), xhsPreselectionDefaultOfficialHosts...), configuredHosts...)
	for _, article := range articles {
		if strings.TrimSpace(article.SourceRole) == model.SourceRolePrimary || xhsArticleHasOfficialHost(article, hosts) {
			return true
		}
	}
	return false
}

func xhsArticleHasOfficialHost(article model.Article, hosts []string) bool {
	parsed, err := url.Parse(strings.TrimSpace(article.Link))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	for _, candidate := range hosts {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate != "" && (host == candidate || strings.HasSuffix(host, "."+candidate)) {
			return true
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
	story.XHSSelection = cloneXHSSelectionTrace(story.XHSSelection)
	return story
}

func cloneXHSSelectionTrace(trace *model.XHSSelectionTrace) *model.XHSSelectionTrace {
	if trace == nil {
		return nil
	}
	cloned := *trace
	cloned.ScoreComponents = make(map[string]int, len(trace.ScoreComponents))
	for key, value := range trace.ScoreComponents {
		cloned.ScoreComponents[key] = value
	}
	return &cloned
}
