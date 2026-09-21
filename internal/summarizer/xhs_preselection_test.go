package summarizer

import (
	"reflect"
	"strings"
	"testing"
	"time"

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
	wantTitles := []string{"Codex 开放 1M 上下文", "Cursor 推出托管平台"}
	if !reflect.DeepEqual(gotTitles, wantTitles) {
		t.Fatalf("XHS titles = %#v, want %#v", gotTitles, wantTitles)
	}
}

func TestXHSBackfillRanksEligibleStoriesByTechnologyEvidenceAndFreshness(t *testing.T) {
	latest := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)
	articles := []model.Article{
		{Source: "财经媒体", SourceRole: model.SourceRoleOriginal, Category: "新闻财经", Link: "https://example.com/rates", Published: latest},
		{Source: "科技媒体", SourceRole: model.SourceRoleOriginal, Category: "AI/科技", Link: "https://example.com/fbi", Published: latest},
		{Source: "芯片厂商", SourceRole: model.SourceRolePrimary, Category: "新闻财经", Link: "https://example.com/chips", Published: latest.Add(-2 * time.Hour)},
		{Source: "独立媒体", SourceRole: model.SourceRoleOriginal, Category: "新闻财经", Link: "https://example.com/chip-analysis", Published: latest.Add(-3 * time.Hour)},
		{Source: "产品博客", SourceRole: model.SourceRolePrimary, Category: "AI/科技", Link: "https://example.com/cursor", Published: latest.Add(-18 * time.Hour)},
		{Source: "官方", SourceRole: model.SourceRolePrimary, Category: "AI/科技", Link: "https://example.com/war", Published: latest},
	}
	final := model.BriefingStory{Category: "新闻财经", Title: "央行维持利率", SourceArticleIDs: []int{1}}
	candidates := []model.BriefingStory{
		{Category: "AI/科技", Title: "FBI称AI使用量增长", Summary: "执法机构采用AI。", SourceArticleIDs: []int{2}},
		{Category: "新闻财经", Title: "芯片厂商发布新工艺", Summary: "制造技术更新。", SourceArticleIDs: []int{3, 4}},
		{Category: "AI/科技", ContentType: model.ContentTypeTool, Title: "Cursor发布开发者API", Summary: "新工具发布。", SourceArticleIDs: []int{5}},
		{Category: "AI/科技", ContentType: model.ContentTypeTool, Title: "AI工具涉及战争", SourceArticleIDs: []int{6}},
	}
	got := preselectXHSStories([]model.BriefingStory{final}, candidates, articles, []string{"AI/科技", "新闻财经"}, 3, 2, nil, []string{"战争"}, nil)
	want := []string{"央行维持利率", "Cursor发布开发者API", "芯片厂商发布新工艺"}
	if len(got) != len(want) {
		t.Fatalf("XHS stories = %#v, want %d eligible stories", got, len(want))
	}
	for index, title := range want {
		if got[index].Title != title {
			t.Fatalf("XHS story %d = %q, want %q", index, got[index].Title, title)
		}
	}
}

func TestXHSBackfillPrefersCorroborationAndUsesFreshnessThenInputOrder(t *testing.T) {
	latest := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)
	articles := []model.Article{
		{Source: "厂商甲", SourceRole: model.SourceRolePrimary, Category: "AI/科技", Link: "https://example.com/old", Published: latest.Add(-36 * time.Hour)},
		{Source: "厂商乙", SourceRole: model.SourceRolePrimary, Category: "AI/科技", Link: "https://example.com/new", Published: latest},
		{Source: "媒体丙", SourceRole: model.SourceRoleOriginal, Category: "AI/科技", Link: "https://example.com/corroboration", Published: latest.Add(-time.Hour)},
		{Source: "无关来源", SourceRole: model.SourceRoleOriginal, Category: "国际政治", Link: "https://example.com/unrelated", Published: latest.Add(72 * time.Hour)},
	}
	candidates := []model.BriefingStory{
		{Category: "AI/科技", Title: "旧款芯片更新", SourceArticleIDs: []int{1}},
		{Category: "AI/科技", Title: "新版芯片更新甲", SourceArticleIDs: []int{2}},
		{Category: "AI/科技", Title: "新版芯片更新乙", SourceArticleIDs: []int{2}},
		{Category: "AI/科技", Title: "新版芯片获多方验证", SourceArticleIDs: []int{2, 3}},
	}
	got := preselectXHSStories(nil, candidates, articles, []string{"AI/科技"}, 4, 2, nil, nil, nil)
	want := []string{"新版芯片获多方验证", "新版芯片更新甲", "新版芯片更新乙", "旧款芯片更新"}
	for index, title := range want {
		if got[index].Title != title {
			t.Fatalf("XHS story %d = %q, want %q", index, got[index].Title, title)
		}
	}
}

func TestXHSBackfillDoesNotTreatMediaMentionAsDirectSource(t *testing.T) {
	articles := []model.Article{
		{Source: "科技媒体", SourceRole: model.SourceRoleOriginal, Link: "https://example.com/report", Summary: "报道提及国务院的公告。"},
		{Source: "发布机构", SourceRole: model.SourceRolePrimary, Link: "https://example.com/release"},
	}
	media := model.BriefingStory{Category: "AI/科技", Title: "AI产品更新", SourceArticleIDs: []int{1}}
	primary := model.BriefingStory{Category: "AI/科技", Title: "AI产品发布", SourceArticleIDs: []int{2}}
	if !xhsStoryHasOfficialSource(articles[:1], nil) {
		t.Fatal("existing eligibility should still accept an official mention")
	}
	if xhsStoryHasDirectSource(articles[:1], nil) {
		t.Fatal("a media mention must not count as direct provenance")
	}
	if xhsBackfillSelection(media, articles, time.Time{}, nil).Score >= xhsBackfillSelection(primary, articles, time.Time{}, nil).Score {
		t.Fatal("a media mention must not outrank a direct primary source")
	}
	if !xhsStoryHasDirectSource([]model.Article{{Link: "https://sub.gov.cn/release"}}, nil) {
		t.Fatal("an official host should count as a direct source even without a source role")
	}
}

func TestXHSBackfillRequiresLowRiskTechnologyAndReliableSources(t *testing.T) {
	articles := []model.Article{
		{Source: "科技媒体", SourceRole: model.SourceRoleOriginal, Link: "https://example.com/fbi"},
		{Source: "厂商", SourceRole: model.SourceRolePrimary, Link: "https://example.com/product"},
		{Source: "媒体甲", SourceRole: model.SourceRoleOriginal, Link: "https://example.com/a"},
		{Source: "媒体乙", SourceRole: model.SourceRoleOriginal, Link: "https://example.com/b"},
	}
	allowed := map[string]struct{}{"AI/科技": {}, "新闻财经": {}}
	tests := []struct {
		name  string
		story model.BriefingStory
		want  bool
	}{
		{name: "single source public safety AI", story: model.BriefingStory{Category: "AI/科技", Title: "FBI扩大AI使用", Summary: "执法机构称协助阻止枪击。", SourceArticleIDs: []int{1}}, want: false},
		{name: "direct regulatory technology", story: model.BriefingStory{Category: "AI/科技", Title: "AI监管平台上线", Summary: "执法流程数字化。", SourceArticleIDs: []int{2}}, want: false},
		{name: "direct product source", story: model.BriefingStory{Category: "AI/科技", Title: "Cursor发布开发者API", SourceArticleIDs: []int{2}}, want: true},
		{name: "corroborated chip reporting", story: model.BriefingStory{Category: "新闻财经", Title: "芯片制造工艺更新", SourceArticleIDs: []int{3, 4}}, want: true},
		{name: "non technology finance", story: model.BriefingStory{Category: "新闻财经", Title: "短债收益率发生变化", SourceArticleIDs: []int{3, 4}}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := xhsBackfillEligible(tc.story, articles, allowed, 2, nil, nil, nil); got != tc.want {
				t.Fatalf("xhsBackfillEligible() = %v, want %v", got, tc.want)
			}
		})
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
