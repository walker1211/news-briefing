package summarizer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/output"
)

func TestApplyXHSPreselectionKeepsEmailStoriesAndBackfillsSafeCandidates(t *testing.T) {
	articles := []model.Article{
		{Title: "Codex", Source: "Readhub", Category: "AI/科技", Link: "https://example.com/codex", Summary: "产品能力更新。"},
		{Title: "Anthropic", Source: "媒体甲", Category: "AI/科技", Link: "https://example.com/revenue", Summary: "媒体报道私营公司营收。"},
		{Title: "Cursor", Source: "产品博客", SourceRole: model.SourceRolePrimary, Category: "AI/科技", Link: "https://example.com/cursor", Summary: "产品发布。"},
		{Title: "投资数据", Source: "媒体甲", Category: "新闻财经", Link: "https://example.com/macro", Summary: "国家统计局数据显示投资变化。"},
		{Title: "数字支付", Source: "媒体甲", Category: "新闻财经", Link: "https://example.com/pay", Summary: "人民银行宣布运营机构扩容。"},
		{Title: "国际", Source: "媒体甲", Category: "国际政治", Link: "https://example.com/world", Summary: "国际新闻。"},
	}
	finalStories := []model.BriefingStory{
		{Category: "AI/科技", Title: "Anthropic 年化营收突破新高", Summary: "据报营收增长。", SourceArticleIDs: []int{2}},
		{Category: "AI/科技", Title: "Codex 开放 1M 上下文", Summary: "产品能力更新。", SourceArticleIDs: []int{1}},
		{Category: "国际政治", Title: "国际新闻", Summary: "国际新闻。", SourceArticleIDs: []int{6}},
	}
	candidates := append(append([]model.BriefingStory(nil), finalStories...),
		model.BriefingStory{Category: "AI/科技", Title: "Cursor 推出托管平台", Summary: "产品发布。", SourceArticleIDs: []int{3}},
		model.BriefingStory{Category: "新闻财经", Title: "前7月投资数据发布", Summary: "国家统计局数据。", SourceArticleIDs: []int{4}},
		model.BriefingStory{Category: "新闻财经", Title: "数字支付扩容", Summary: "人民银行宣布扩容。", SourceArticleIDs: []int{5}},
	)
	summary := model.BriefingSummary{Stories: append([]model.BriefingStory(nil), finalStories...)}
	wantEmail := append([]model.BriefingStory(nil), summary.Stories...)
	runner := &Runner{}
	runner.SetXHSPreselectionOptions(true, []string{"AI/科技", "新闻财经"}, 4, 2, nil, nil, nil)
	runner.applyXHSPreselection(&summary, candidates, articles)

	if !reflect.DeepEqual(summary.Stories, wantEmail) {
		t.Fatalf("email stories changed: got %#v want %#v", summary.Stories, wantEmail)
	}
	gotTitles := make([]string, 0, len(summary.XHSStories))
	for _, story := range summary.XHSStories {
		gotTitles = append(gotTitles, story.Title)
	}
	wantTitles := []string{"Codex 开放 1M 上下文", "Cursor 推出托管平台", "前7月投资数据发布", "数字支付扩容"}
	if !reflect.DeepEqual(gotTitles, wantTitles) {
		t.Fatalf("XHS titles = %#v, want %#v", gotTitles, wantTitles)
	}
}

func TestXHSStoryEligibilityRequiresOfficialSourcesForMaterialClaims(t *testing.T) {
	articles := []model.Article{
		{Source: "媒体甲", Link: "https://example.com/a", Summary: "second quarter results"},
		{Source: "媒体乙", Link: "https://example.com/b"},
		{Source: "交易所公告", Link: "https://www.sse.com.cn/c"},
	}
	allowed := map[string]struct{}{"AI/科技": {}}
	tests := []struct {
		name  string
		story model.BriefingStory
		want  bool
	}{
		{name: "ordinary single-source product update", story: model.BriefingStory{Category: "AI/科技", Title: "Codex 开放新上下文", SourceArticleIDs: []int{1}}, want: true},
		{name: "media-only revenue", story: model.BriefingStory{Category: "AI/科技", Title: "公司年化营收突破新高", SourceArticleIDs: []int{1, 2}}, want: false},
		{name: "media-only acquisition", story: model.BriefingStory{Category: "AI/科技", Title: "公司据报完成收购", SourceArticleIDs: []int{1, 2}}, want: false},
		{name: "official IPO", story: model.BriefingStory{Category: "AI/科技", Title: "公司上市安排", SourceArticleIDs: []int{3}}, want: true},
		{name: "single-source reported claim", story: model.BriefingStory{Category: "AI/科技", Title: "企业被指拆解稀有书籍", SourceArticleIDs: []int{1}}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := xhsStoryEligible(tc.story, articles, allowed, 2, nil, nil, nil); got != tc.want {
				t.Fatalf("xhsStoryEligible() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestApplyXHSPreselectionExcludesConfiguredRiskTermsAndRebuildsTopics(t *testing.T) {
	articles := []model.Article{
		{Source: "官方甲", SourceRole: model.SourceRolePrimary, Category: "新闻财经", Link: "https://example.com/statement"},
		{Source: "官方乙", SourceRole: model.SourceRolePrimary, Category: "新闻财经", Link: "https://example.com/fire"},
		{Source: "媒体甲", Category: "新闻财经", Link: "https://example.com/payment"},
		{Source: "产品博客", SourceRole: model.SourceRolePrimary, Category: "AI/科技", Link: "https://example.com/claude"},
		{Source: "产品博客", SourceRole: model.SourceRolePrimary, Category: "AI/科技", Link: "https://example.com/copilot"},
		{Source: "芯片厂商", SourceRole: model.SourceRolePrimary, Category: "AI/科技", Link: "https://example.com/chip"},
		{Source: "央行", SourceRole: model.SourceRolePrimary, Category: "新闻财经", Link: "https://example.com/rates"},
	}
	summary := model.BriefingSummary{
		Stories: []model.BriefingStory{
			{Category: "新闻财经", Title: "中方反对威胁叙事", Summary: "官方表态。", SourceArticleIDs: []int{1}},
			{Category: "新闻财经", Title: "重大火灾调查进展", Summary: "调查发布。", SourceArticleIDs: []int{2}},
			{Category: "新闻财经", Title: "中小企业回款难", Summary: "拖欠问题持续。", SourceArticleIDs: []int{3}},
			{Category: "AI/科技", Title: "Claude 发布新能力", Summary: "模型产品更新。", SourceArticleIDs: []int{4}},
			{Category: "AI/科技", Title: "Copilot 改进开发者体验", Summary: "开发者工具更新。", SourceArticleIDs: []int{5}},
			{Category: "AI/科技", Title: "芯片厂商发布新 GPU", Summary: "提升算力效率。", SourceArticleIDs: []int{6}},
			{Category: "新闻财经", Title: "央行维持利率", Summary: "宏观经济数据稳定。", SourceArticleIDs: []int{7}},
		},
		XHSTopics: []string{"中东局势", "油价", "战争"},
	}
	wantStories := append([]model.BriefingStory(nil), summary.Stories...)
	canonicalMarkdown := output.StructuredBriefingMarkdown(summary, []string{"AI/科技", "新闻财经"})
	runner := &Runner{}
	runner.SetXHSPreselectionOptions(true, []string{"AI/科技", "新闻财经"}, 10, 1, nil, []string{"威胁叙事", "火灾调查", "回款难", "拖欠"}, nil)
	runner.applyXHSPreselection(&summary, summary.Stories, articles)

	if !reflect.DeepEqual(summary.Stories, wantStories) {
		t.Fatalf("briefing stories changed: got %#v want %#v", summary.Stories, wantStories)
	}
	if got := output.StructuredBriefingMarkdown(summary, []string{"AI/科技", "新闻财经"}); got != canonicalMarkdown {
		t.Fatalf("canonical Markdown changed by XHS preselection:\n got: %q\nwant: %q", got, canonicalMarkdown)
	}
	gotTitles := make([]string, 0, len(summary.XHSStories))
	for _, story := range summary.XHSStories {
		gotTitles = append(gotTitles, story.Title)
	}
	wantTitles := []string{"Claude 发布新能力", "Copilot 改进开发者体验", "芯片厂商发布新 GPU", "央行维持利率"}
	if !reflect.DeepEqual(gotTitles, wantTitles) {
		t.Fatalf("XHS titles = %#v, want %#v", gotTitles, wantTitles)
	}
	for _, topic := range summary.XHSTopics {
		if topic == "中东局势" || topic == "油价" || topic == "战争" {
			t.Fatalf("unrelated original topic leaked into XHS topics: %#v", summary.XHSTopics)
		}
		if strings.ContainsAny(topic, "# \t\n\r") {
			t.Fatalf("invalid XHS topic %q", topic)
		}
	}
	if len(summary.XHSTopics) < 3 || len(summary.XHSTopics) > 4 {
		t.Fatalf("XHS topics = %#v, want 3-4 topics", summary.XHSTopics)
	}
	for _, want := range []string{"人工智能", "开发者工具", "芯片与算力", "宏观经济"} {
		if !containsXHSTerm(strings.Join(summary.XHSTopics, "\n"), []string{want}) {
			t.Fatalf("XHS topics = %#v, want %q", summary.XHSTopics, want)
		}
	}
}

func TestApplyXHSPreselectionDoesNotBackfillExcludedStories(t *testing.T) {
	articles := []model.Article{
		{Source: "官方", SourceRole: model.SourceRolePrimary, Category: "新闻财经", Link: "https://example.com/a"},
		{Source: "官方", SourceRole: model.SourceRolePrimary, Category: "新闻财经", Link: "https://example.com/b"},
	}
	summary := model.BriefingSummary{Stories: []model.BriefingStory{
		{Category: "新闻财经", Title: "中性央行声明", Summary: "利率维持不变。", SourceArticleIDs: []int{1}},
	}}
	candidates := append(append([]model.BriefingStory(nil), summary.Stories...), model.BriefingStory{Category: "新闻财经", Title: "回款难仍在持续", Summary: "拖欠问题。", SourceArticleIDs: []int{2}})
	runner := &Runner{}
	runner.SetXHSPreselectionOptions(true, []string{"新闻财经"}, 5, 1, nil, []string{"回款难", "拖欠"}, nil)
	runner.applyXHSPreselection(&summary, candidates, articles)
	if got := len(summary.XHSStories); got != 1 {
		t.Fatalf("XHS story count = %d, want 1; unsafe backfill must not fill target", got)
	}
}

func TestApplyXHSPreselectionDisabledLeavesNilStories(t *testing.T) {
	summary := model.BriefingSummary{Stories: []model.BriefingStory{{Category: "AI/科技", Title: "Story"}}}
	(&Runner{}).applyXHSPreselection(&summary, summary.Stories, []model.Article{{Source: "Source"}})
	if summary.XHSStories != nil {
		t.Fatalf("XHSStories = %#v, want nil when disabled", summary.XHSStories)
	}
}

func TestApplyXHSPreselectionExcludesChinaOnlyWithConfiguredRiskContext(t *testing.T) {
	articles := []model.Article{
		{Source: "官方", SourceRole: model.SourceRolePrimary, Category: "AI/科技", Link: "https://example.com/cac"},
		{Source: "官方", SourceRole: model.SourceRolePrimary, Category: "新闻财经", Link: "https://example.com/investment"},
		{Source: "官方", SourceRole: model.SourceRolePrimary, Category: "新闻财经", Link: "https://example.com/statement"},
		{Source: "产品博客", SourceRole: model.SourceRolePrimary, Category: "AI/科技", Link: "https://example.com/compute"},
		{Source: "产品博客", SourceRole: model.SourceRolePrimary, Category: "AI/科技", Link: "https://example.com/devtools"},
		{Source: "欧盟委员会", SourceRole: model.SourceRolePrimary, Category: "AI/科技", Link: "https://example.com/eu"},
	}
	summary := model.BriefingSummary{Stories: []model.BriefingStory{
		{Category: "AI/科技", Title: "中国网信部门公布执法案例", Summary: "加强平台监管。", SourceArticleIDs: []int{1}},
		{Category: "新闻财经", Title: "中国投资降幅扩大", Summary: "经济压力仍在。", SourceArticleIDs: []int{2}},
		{Category: "新闻财经", Title: "中方反对威胁叙事", Summary: "涉及政治对抗。", SourceArticleIDs: []int{3}},
		{Category: "AI/科技", Title: "中国算力平台完成接入", Summary: "提升开发者计算效率。", SourceArticleIDs: []int{4}},
		{Category: "AI/科技", Title: "中国开发者工具发布", Summary: "支持代码协作。", SourceArticleIDs: []int{5}},
		{Category: "AI/科技", Title: "欧盟推进 AI 监管", Summary: "仅涉及当地规则。", SourceArticleIDs: []int{6}},
	}}
	wantStories := append([]model.BriefingStory(nil), summary.Stories...)
	canonicalMarkdown := output.StructuredBriefingMarkdown(summary, []string{"AI/科技", "新闻财经"})
	rules := []XHSContextualExclusionRule{{
		AnchorKeywords:  []string{"中国", "中方"},
		ContextKeywords: []string{"执法", "监管", "投资降幅", "经济压力", "威胁叙事", "政治对抗"},
	}}
	runner := &Runner{}
	runner.SetXHSPreselectionOptions(true, []string{"AI/科技", "新闻财经"}, 10, 1, nil, nil, rules)
	runner.applyXHSPreselection(&summary, summary.Stories, articles)

	if !reflect.DeepEqual(summary.Stories, wantStories) {
		t.Fatalf("briefing stories changed: got %#v want %#v", summary.Stories, wantStories)
	}
	if got := output.StructuredBriefingMarkdown(summary, []string{"AI/科技", "新闻财经"}); got != canonicalMarkdown {
		t.Fatalf("canonical Markdown changed by XHS preselection:\n got: %q\nwant: %q", got, canonicalMarkdown)
	}
	gotTitles := make([]string, 0, len(summary.XHSStories))
	for _, story := range summary.XHSStories {
		gotTitles = append(gotTitles, story.Title)
	}
	wantTitles := []string{"中国算力平台完成接入", "中国开发者工具发布", "欧盟推进 AI 监管"}
	if !reflect.DeepEqual(gotTitles, wantTitles) {
		t.Fatalf("XHS titles = %#v, want %#v", gotTitles, wantTitles)
	}
}

func TestXHSTopicsForCustomCategoryStillReturnsThreeSafeTopics(t *testing.T) {
	topics := xhsTopicsForStories([]model.BriefingStory{{Category: "自定义", Title: "例行信息更新"}})
	want := []string{"每日资讯", "资讯速览", "今日观察"}
	if !reflect.DeepEqual(topics, want) {
		t.Fatalf("xhsTopicsForStories() = %#v, want %#v", topics, want)
	}
}
