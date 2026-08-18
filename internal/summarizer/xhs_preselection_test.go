package summarizer

import (
	"reflect"
	"testing"

	"github.com/walker1211/news-briefing/internal/model"
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
	runner.SetXHSPreselectionOptions(true, []string{"AI/科技", "新闻财经"}, 4, 2, nil)
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
		{Source: "媒体甲", Link: "https://example.com/a"},
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
		{name: "single-source reported claim", story: model.BriefingStory{Category: "AI/科技", Title: "媒体据报发布新功能", SourceArticleIDs: []int{1}}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := xhsStoryEligible(tc.story, articles, allowed, 2, nil); got != tc.want {
				t.Fatalf("xhsStoryEligible() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestApplyXHSPreselectionDisabledLeavesNilStories(t *testing.T) {
	summary := model.BriefingSummary{Stories: []model.BriefingStory{{Category: "AI/科技", Title: "Story"}}}
	(&Runner{}).applyXHSPreselection(&summary, summary.Stories, []model.Article{{Source: "Source"}})
	if summary.XHSStories != nil {
		t.Fatalf("XHSStories = %#v, want nil when disabled", summary.XHSStories)
	}
}
