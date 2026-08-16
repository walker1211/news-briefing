package summarizer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/output"
)

const argSep = "\x1f"

func TestBriefingPromptRequiresStructuredJSONAndImageRules(t *testing.T) {
	for _, want := range []string{
		"只输出合法 JSON",
		"不要输出 Markdown",
		"overview_groups",
		"stories",
		"image_url",
		"source_article_ids",
		"每个分类只列该分类新闻",
		"Image 字段原值",
		"不要编造、改写或重新托管图片 URL",
	} {
		if !strings.Contains(briefingPrompt, want) {
			t.Fatalf("briefingPrompt missing %q", want)
		}
	}
}

func TestBriefingPromptRequiresOverviewEmojisAndDetailedStoryFields(t *testing.T) {
	for _, want := range []string{
		"每条 items 开头使用一个贴切 emoji",
		"summary 用2-4句话说明关键事实、背景、数字、参与方和最新进展",
		"impact 用2-3句话说明为什么重要、影响哪些人或机构、后续观察变量",
		"source_article_ids 必须使用输入新闻条目的 1-based 编号",
	} {
		if !strings.Contains(briefingPrompt, want) {
			t.Fatalf("briefingPrompt missing detail rule %q", want)
		}
	}
}

func TestBriefingPromptRequestsReusableXHSTopics(t *testing.T) {
	for _, want := range []string{
		`"xhs_topics": ["话题1", "话题2", "话题3"]`,
		"xhs_topics 输出 3 个适合整篇简报的小红书话题",
		"每项不要带 #、空格或特殊符号",
	} {
		if !strings.Contains(briefingPrompt, want) {
			t.Fatalf("briefingPrompt missing XHS topic rule %q", want)
		}
	}
}

func TestBriefingPromptUsesFollowupDirectionsInsteadOfTopicSuggestions(t *testing.T) {
	for _, want := range []string{
		"directions",
		"why",
		"next",
		"deep_command",
		"./news-briefing deep \"关键词\" --ignore-seen",
	} {
		if !strings.Contains(briefingPrompt, want) {
			t.Fatalf("briefingPrompt missing %q", want)
		}
	}
}

func TestBriefingPromptDefinesDirectionCountRule(t *testing.T) {
	for _, want := range []string{
		"输出 2-4 个最值得普通用户继续关注的新闻方向",
		"不要少于 2 个，也不要多于 4 个",
	} {
		if !strings.Contains(briefingPrompt, want) {
			t.Fatalf("briefingPrompt missing count rule %q", want)
		}
	}
	for _, unwanted := range []string{
		"必须恰好输出 3 个方向",
		"不要输出 2 个或 4 个",
	} {
		if strings.Contains(briefingPrompt, unwanted) {
			t.Fatalf("briefingPrompt unexpectedly contains %q", unwanted)
		}
	}
}

func TestBriefingPromptDefinesSelectionAndMergeRules(t *testing.T) {
	for _, want := range []string{
		"按下面新闻条目里出现的分类顺序输出",
		"如果两个候选方向需要使用同一个 deep 关键词，默认应优先考虑合并",
	} {
		if !strings.Contains(briefingPrompt, want) {
			t.Fatalf("briefingPrompt missing rule %q", want)
		}
	}
	for _, unwanted := range []string{
		"默认至少 2/3 方向来自 AI/科技",
		"最多 1 个国际政治方向作为补位",
		"## AI/科技",
		"## 国际政治",
	} {
		if strings.Contains(briefingPrompt, unwanted) {
			t.Fatalf("briefingPrompt unexpectedly contains %q", unwanted)
		}
	}
}

func TestBriefingPromptRequiresEnglishEntityStyleDeepCommands(t *testing.T) {
	for _, want := range []string{
		"深挖命令里的关键词默认优先使用英文实体或英文新闻短语",
		"长度控制在 2-6 个词",
		"优先包含公司名、产品名、人物名、法案/政策名、机构名等明确锚点",
		"避免使用纯中文概括题目",
		"避免只用过泛词",
		"Sanders AOC AI data center bill",
		"ICE data brokers surveillance",
	} {
		if !strings.Contains(briefingPrompt, want) {
			t.Fatalf("briefingPrompt missing deep command rule %q", want)
		}
	}
}

func TestParseBriefingSummaryJSONStripsCodeFence(t *testing.T) {
	raw := "```json\n{\"stories\":[{\"category\":\"AI/科技\",\"title\":\"OpenAI 发布\"}]}\n```"

	got, err := parseBriefingSummaryJSON(raw)
	if err != nil {
		t.Fatalf("parseBriefingSummaryJSON() error = %v", err)
	}
	if len(got.Stories) != 1 || got.Stories[0].Title != "OpenAI 发布" {
		t.Fatalf("parseBriefingSummaryJSON() = %#v", got)
	}
}

func TestValidateBriefingSummaryImagesKeepsAllowedImageURL(t *testing.T) {
	articles := []model.Article{{ImageURL: "https://example.com/openai.jpg"}}
	summary := model.BriefingSummary{Stories: []model.BriefingStory{{ImageURL: "https://example.com/openai.jpg"}}}

	got := validateBriefingSummaryImages(summary, articles)
	if got.Stories[0].ImageURL != "https://example.com/openai.jpg" {
		t.Fatalf("ImageURL = %q, want allowed URL", got.Stories[0].ImageURL)
	}
}

func TestValidateBriefingSummaryImagesClearsInventedImageURL(t *testing.T) {
	articles := []model.Article{{ImageURL: "https://example.com/openai.jpg"}}
	summary := model.BriefingSummary{Stories: []model.BriefingStory{{ImageURL: "https://evil.example.com/fake.jpg"}}}

	got := validateBriefingSummaryImages(summary, articles)
	if got.Stories[0].ImageURL != "" {
		t.Fatalf("ImageURL = %q, want empty for invented URL", got.Stories[0].ImageURL)
	}
}

func TestValidateBriefingSummaryImagesReplacesImageFromOtherSourceArticle(t *testing.T) {
	articles := []model.Article{
		{ImageURL: "https://example.com/claude.jpg"},
		{ImageURL: "https://example.com/rocket.jpg"},
	}
	summary := model.BriefingSummary{Stories: []model.BriefingStory{{ImageURL: "https://example.com/rocket.jpg", SourceArticleIDs: []int{1}}}}

	got := validateBriefingSummaryImages(summary, articles)
	if got.Stories[0].ImageURL != "https://example.com/claude.jpg" {
		t.Fatalf("ImageURL = %q, want source article image", got.Stories[0].ImageURL)
	}
}

func TestValidateBriefingSummaryImagesBackfillsFromSourceArticleIDs(t *testing.T) {
	articles := []model.Article{
		{ImageURL: ""},
		{ImageURL: "https://example.com/source.jpg"},
	}
	summary := model.BriefingSummary{Stories: []model.BriefingStory{{SourceArticleIDs: []int{1, 2}}}}

	got := validateBriefingSummaryImages(summary, articles)
	if got.Stories[0].ImageURL != "https://example.com/source.jpg" {
		t.Fatalf("ImageURL = %q, want source article image", got.Stories[0].ImageURL)
	}
}

func TestValidateAndNormalizeBriefingSummaryReferencesRejectsCrossCategorySource(t *testing.T) {
	articles := []model.Article{
		{Category: "AI/科技", Source: "AI Source", Title: "AI story"},
		{Category: "国际政治", Source: "World Source", Title: "World story"},
	}
	summary := model.BriefingSummary{Stories: []model.BriefingStory{{
		Category:         "AI/科技",
		Title:            "Mismatched story",
		SourceArticleIDs: []int{2},
	}}}

	_, err := validateAndNormalizeBriefingSummaryReferences(summary, articles, time.UTC)
	if err == nil || !strings.Contains(err.Error(), `category "AI/科技" references article 2 from category "国际政治"`) {
		t.Fatalf("validate references error = %v, want category mismatch", err)
	}
}

func TestValidateAndNormalizeBriefingSummaryReferencesRejectsMissingAndInvalidIDs(t *testing.T) {
	articles := []model.Article{{Category: "AI/科技", Source: "AI Source"}}
	tests := []struct {
		name    string
		ids     []int
		wantErr string
	}{
		{name: "missing", wantErr: "has no source_article_ids"},
		{name: "zero", ids: []int{0}, wantErr: "outside 1..1"},
		{name: "too large", ids: []int{2}, wantErr: "outside 1..1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			summary := model.BriefingSummary{Stories: []model.BriefingStory{{Category: "AI/科技", Title: "Story", SourceArticleIDs: tc.ids}}}
			_, err := validateAndNormalizeBriefingSummaryReferences(summary, articles, time.UTC)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validate references error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateAndNormalizeBriefingSummaryReferencesBuildsSourceLine(t *testing.T) {
	articles := []model.Article{
		{Category: "AI/科技", Source: "Source B", Published: time.Date(2026, 8, 11, 9, 30, 0, 0, time.UTC)},
		{Category: "AI/科技", Source: "Source A", Published: time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)},
		{Category: "AI/科技", Source: "Source B", Published: time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)},
	}
	summary := model.BriefingSummary{Stories: []model.BriefingStory{{
		Category:         " AI/科技 ",
		Title:            "Story",
		SourceArticleIDs: []int{1, 2, 1, 3},
		SourceLine:       "来源: AI 自由生成的错误来源",
	}}}

	got, err := validateAndNormalizeBriefingSummaryReferences(summary, articles, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	story := got.Stories[0]
	if !reflect.DeepEqual(story.SourceArticleIDs, []int{1, 2, 3}) {
		t.Fatalf("source ids = %v, want deduplicated ids", story.SourceArticleIDs)
	}
	if story.SourceLine != "来源: Source B、Source A | 2026-08-11 08:00 至 2026-08-11 09:30" {
		t.Fatalf("source line = %q", story.SourceLine)
	}
}

func TestValidateBriefingSummaryImagesSkipsTrackingPixelBackfill(t *testing.T) {
	articles := []model.Article{{ImageURL: "https://media.npr.org/include/images/tracking/npr-rss-pixel.png?story=nx-s1"}}
	summary := model.BriefingSummary{Stories: []model.BriefingStory{{SourceArticleIDs: []int{1}}}}

	got := validateBriefingSummaryImages(summary, articles)
	if got.Stories[0].ImageURL != "" {
		t.Fatalf("ImageURL = %q, want empty for tracking pixel", got.Stories[0].ImageURL)
	}
}

func TestValidateBriefingSummaryImagesKeepsChosenImageFromAmbiguousSourceArticles(t *testing.T) {
	articles := []model.Article{
		{ImageURL: "https://example.com/fire.jpg"},
		{ImageURL: "https://example.com/satellite.jpg"},
	}
	summary := model.BriefingSummary{Stories: []model.BriefingStory{{ImageURL: "https://example.com/fire.jpg", SourceArticleIDs: []int{1, 2}}}}

	got := validateBriefingSummaryImages(summary, articles)
	if got.Stories[0].ImageURL != "https://example.com/fire.jpg" {
		t.Fatalf("ImageURL = %q, want chosen source image", got.Stories[0].ImageURL)
	}
}

func TestValidateBriefingSummaryImagesDoesNotBackfillAmbiguousMultiSourceImages(t *testing.T) {
	articles := []model.Article{
		{ImageURL: "https://example.com/fire.jpg"},
		{ImageURL: "https://example.com/satellite.jpg"},
	}
	summary := model.BriefingSummary{Stories: []model.BriefingStory{{SourceArticleIDs: []int{1, 2}}}}

	got := validateBriefingSummaryImages(summary, articles)
	if got.Stories[0].ImageURL != "" {
		t.Fatalf("ImageURL = %q, want empty for ambiguous multi-source backfill", got.Stories[0].ImageURL)
	}
}

func TestRunnerSummarizeValidatesImagesUsingPromptArticleOrder(t *testing.T) {
	const aiImage = "https://example.com/ai.jpg"
	const politicsImage = "https://example.com/politics.jpg"
	setupFakeCLIOutput(t, "claude", `{"overview_groups":[],"stories":[{"category":"AI/科技","title":"AI story","image_url":"https://example.com/ai.jpg","summary":"AI summary.","impact":"AI impact.","source_article_ids":[1],"source_line":"来源: AI Source | 2026-03-18 14:00"}],"situation":"","directions":[]}`)
	runner := NewRunner("claude", nil, false, "", "")
	articles := []model.Article{
		{
			Title:     "Politics story",
			Summary:   "Politics summary.",
			Source:    "Politics Source",
			Category:  "国际政治",
			ImageURL:  politicsImage,
			Published: time.Date(2026, 3, 18, 15, 0, 0, 0, time.UTC),
		},
		{
			Title:     "AI story",
			Summary:   "AI summary.",
			Source:    "AI Source",
			Category:  "AI/科技",
			ImageURL:  aiImage,
			Published: time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC),
		},
	}

	got, err := runner.SummarizeContext(context.Background(), articles, []string{"AI/科技", "国际政治"}, time.UTC)
	if err != nil {
		t.Fatalf("SummarizeContext() error = %v", err)
	}
	if !strings.Contains(got, "]("+aiImage+")") {
		t.Fatalf("SummarizeContext() = %q, want AI image from prompt source id", got)
	}
	if strings.Contains(got, politicsImage) {
		t.Fatalf("SummarizeContext() = %q, should not use image from original article index", got)
	}
}

func TestRunnerSummarizeRemapsPromptSourceIDsToOriginalArticleOrder(t *testing.T) {
	setupFakeCLIOutput(t, "claude", `{"overview_groups":[],"stories":[{"category":"AI/科技","title":"AI story","summary":"AI summary.","impact":"AI impact.","source_article_ids":[1]}],"situation":"","directions":[]}`)
	runner := NewRunner("claude", nil, false, "", "")
	articles := []model.Article{
		{Title: "Politics story", Source: "Politics Source", Category: "国际政治"},
		{Title: "AI story", Source: "AI Source", Category: "AI/科技"},
	}

	got, _, err := runner.SummarizeBriefingContext(context.Background(), articles, []string{"AI/科技", "国际政治"}, time.UTC)
	if err != nil {
		t.Fatalf("SummarizeBriefingContext() error = %v", err)
	}
	if want := []int{2}; !reflect.DeepEqual(got.Stories[0].SourceArticleIDs, want) {
		t.Fatalf("source ids = %v, want original article ids %v", got.Stories[0].SourceArticleIDs, want)
	}
	if got.Stories[0].SourceLine != "来源: AI Source" {
		t.Fatalf("source line = %q", got.Stories[0].SourceLine)
	}
}

func TestDeepDivePromptUsesTopicDeepDivePackWording(t *testing.T) {
	for _, want := range []string{
		"你是一个资深新闻调研员和话题研究助手。",
		"生成一份详细的话题深挖包：",
		"## 研究建议",
		"- 推荐的研究切入点",
		"- 值得继续跟踪的关键信号",
	} {
		if !strings.Contains(deepDivePrompt, want) {
			t.Fatalf("deepDivePrompt missing wording %q", want)
		}
	}
}

func TestRunnerUsesConfiguredArgsForDeepDive(t *testing.T) {
	setupFakeCLI(t, "claude")
	runner := NewRunner("claude", []string{"--model", "claude-opus-4-6", "--bare", "--disable-slash-commands"}, true, "", "")

	articles := sampleArticles()
	got, err := runner.DeepDive("OpenAI", articles, time.Local)
	if err != nil {
		t.Fatalf("DeepDive() error = %v", err)
	}

	want := []string{
		"--model",
		"claude-opus-4-6",
		"--bare",
		"--disable-slash-commands",
		"--append-system-prompt",
		nonInteractiveDeepDiveSystemPrompt,
		"-p",
		fmt.Sprintf(deepDivePrompt, "OpenAI") + "\n\n---\n话题: OpenAI\n\n相关新闻:\n" + output.ArticleListView(articles, time.Local),
	}
	if args := splitArgs(got); !reflect.DeepEqual(args, want) {
		t.Fatalf("DeepDive() args = %#v, want %#v", args, want)
	}
}

func TestRunnerUsesDefaultConfiguredCommand(t *testing.T) {
	setupFakeCLI(t, "codex")
	runner := NewRunner("", nil, true, "", "")

	got, err := runner.callClaude("hello world", runner.summarizeRuntimeArgs()...)
	if err != nil {
		t.Fatalf("callClaude() error = %v", err)
	}

	want := []string{
		"exec",
		"--ignore-user-config",
		"--ephemeral",
		"--skip-git-repo-check",
		"--sandbox", "read-only",
		"--color", "never",
		"--disable", "apps",
		"--disable", "plugins",
		"--disable", "remote_plugin",
		"--model", defaultModel,
		"-c", "developer_instructions=" + strconv.Quote(nonInteractiveBriefingSystemPrompt),
		"-",
	}
	if args := splitArgs(got); !reflect.DeepEqual(args, want) {
		t.Fatalf("callClaude() args = %#v, want %#v", args, want)
	}
}

func TestRunnerUsesCodexStdinAndDeveloperInstructions(t *testing.T) {
	argsPath, stdinPath := setupFakeCLIOutputAndInput(t, "codex", validBriefingJSON())
	runner := NewRunner("codex", []string{"exec", "--model", "gpt-5.6-sol"}, true, "", "")

	articles := sampleArticles()
	if _, err := runner.Summarize(articles, []string{"AI/科技", "国际政治"}, time.Local); err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}

	wantArgs := []string{
		"exec",
		"--model", "gpt-5.6-sol",
		"-c", "developer_instructions=" + strconv.Quote(nonInteractiveBriefingSystemPrompt),
		"-",
	}
	if args := readFakeCLIArgs(t, argsPath); !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("Summarize() args = %#v, want %#v", args, wantArgs)
	}
	wantStdin := briefingPrompt + "\n\n---\n以下是今日新闻条目：\n\n" + output.GroupedArticleListView(articles, []string{"AI/科技", "国际政治"}, time.Local)
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read fake CLI stdin: %v", err)
	}
	if got := string(stdin); got != wantStdin {
		t.Fatalf("Summarize() stdin = %q, want %q", got, wantStdin)
	}
}

func TestCodexRuntimeArgsMapSystemPromptsToDeveloperInstructions(t *testing.T) {
	runner := NewRunner("codex", []string{"exec"}, true, "", "")
	runner.SetModelOptions("summary-model", "medium", "translation-model", "high")
	runner.SetSummaryEditorOptions(true, "editor-model", "high", 12, 17, 20)
	tests := []struct {
		name       string
		got        []string
		wantModel  string
		wantEffort string
		want       string
	}{
		{name: "summarize", got: runner.summarizeRuntimeArgs(), wantModel: "summary-model", wantEffort: "medium", want: nonInteractiveBriefingSystemPrompt},
		{name: "editor", got: runner.summaryEditorRuntimeArgs(), wantModel: "editor-model", wantEffort: "high", want: nonInteractiveBriefingSystemPrompt},
		{name: "deep", got: runner.deepDiveRuntimeArgs(), wantModel: "summary-model", wantEffort: "medium", want: nonInteractiveDeepDiveSystemPrompt},
		{name: "translate", got: runner.translateRuntimeArgs(), wantModel: "translation-model", wantEffort: "high", want: nonInteractiveBriefingSystemPrompt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := []string{"--model", tt.wantModel, "-c", "model_reasoning_effort=" + strconv.Quote(tt.wantEffort), "-c", "developer_instructions=" + strconv.Quote(tt.want)}
			if !reflect.DeepEqual(tt.got, want) {
				t.Fatalf("runtime args = %#v, want %#v", tt.got, want)
			}
		})
	}

	withoutSystemPrompt := NewRunner("codex", []string{"exec"}, false, "", "")
	withoutSystemPrompt.SetModels("summary-model", "translation-model")
	if got, want := withoutSystemPrompt.summarizeRuntimeArgs(), []string{"--model", "summary-model"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime args with append_system_prompt=false = %#v, want %#v", got, want)
	}
}

func TestRunnerUsesConfiguredTranslationModelExactlyOnce(t *testing.T) {
	argsPath, stdinPath := setupFakeCLIOutputAndInput(t, "codex", "final body")
	runner := NewRunner("codex", []string{"exec", "--model", "legacy-model", "--ephemeral"}, false, "", "")
	runner.SetModels("configured-default-model", "configured-translation-model")
	articles := sampleArticles()
	categoryOrder := []string{"AI/科技", "国际政治"}

	got, err := runner.Translate(articles, categoryOrder, time.Local)
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	if got != "final body" {
		t.Fatalf("Translate() = %q, want %q", got, "final body")
	}

	want := []string{"exec", "--ephemeral", "--model", "configured-translation-model", "-"}
	if args := readFakeCLIArgs(t, argsPath); !reflect.DeepEqual(args, want) {
		t.Fatalf("Translate() args = %#v, want %#v", args, want)
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read fake CLI stdin: %v", err)
	}
	wantStdin := translatePrompt + "\n\n" + output.GroupedArticleListView(articles, categoryOrder, time.Local)
	if got := string(stdin); got != wantStdin {
		t.Fatalf("Translate() stdin = %q, want %q", got, wantStdin)
	}
}

func TestRunnerSummarizesCategoriesConcurrentlyThenSynthesizes(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })

	aiOutput := filepath.Join(dir, "ai.json")
	financeOutput := filepath.Join(dir, "finance.json")
	synthesisOutput := filepath.Join(dir, "synthesis.json")
	files := map[string]string{
		aiOutput:        `{"overview_groups":[{"category":"AI/科技","items":["🤖 AI 要点"]}],"stories":[{"category":"AI/科技","title":"AI 新闻","summary":"AI 摘要。","impact":"AI 影响。","source_article_ids":[1]}]}`,
		financeOutput:   `{"overview_groups":[{"category":"新闻财经","items":["💹 财经要点"]}],"stories":[{"category":"新闻财经","title":"财经新闻","summary":"财经摘要。","impact":"财经影响。","source_article_ids":[1]}]}`,
		synthesisOutput: `{"situation":"两个分类共同呈现结构变化。","xhs_topics":["人工智能","财经观察","每日新闻"],"directions":[{"title":"AI roadmap","why":"产品持续演进。","next":"观察发布节奏。","deep_command":"./news-briefing deep \"AI roadmap\" --ignore-seen"},{"title":"Market outlook","why":"市场预期变化。","next":"观察政策与价格。","deep_command":"./news-briefing deep \"Market outlook\" --ignore-seen"}]}`,
	}
	for path, body := range files {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write fake output: %v", err)
		}
	}

	commandPath := filepath.Join(dir, "codex")
	script := `#!/bin/sh
input="` + dir + `/input.$$"
cat > "$input"
if grep -q '分类摘要 JSON' "$input"; then
  cat "` + synthesisOutput + `"
elif grep -q '当前分类：AI/科技' "$input"; then
  touch "` + dir + `/ai.ready"
  i=0; while [ ! -f "` + dir + `/finance.ready" ] && [ "$i" -lt 100 ]; do sleep 0.01; i=$((i+1)); done
  [ -f "` + dir + `/finance.ready" ] || exit 9
  cat "` + aiOutput + `"
elif grep -q '当前分类：新闻财经' "$input"; then
  touch "` + dir + `/finance.ready"
  i=0; while [ ! -f "` + dir + `/ai.ready" ] && [ "$i" -lt 100 ]; do sleep 0.01; i=$((i+1)); done
  [ -f "` + dir + `/ai.ready" ] || exit 9
  cat "` + financeOutput + `"
else
  exit 8
fi
rm -f "$input"
`
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	articles := []model.Article{
		{Title: "AI", Source: "AI Source", Category: "AI/科技", Published: time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)},
		{Title: "Finance", Source: "Finance Source", Category: "新闻财经", Published: time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)},
	}
	runner := NewRunner("codex", []string{"exec"}, false, "", "")
	runner.SetSummaryOptions(true, 2)
	summary, markdown, err := runner.SummarizeBriefingContext(context.Background(), articles, []string{"AI/科技", "新闻财经"}, time.UTC)
	if err != nil {
		t.Fatalf("SummarizeBriefingContext() error = %v", err)
	}
	if got, want := len(summary.Stories), 2; got != want {
		t.Fatalf("stories = %d, want %d", got, want)
	}
	if got := summary.Stories[1].SourceArticleIDs; !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("finance source ids = %#v, want [2]", got)
	}
	if len(summary.Directions) != 2 || len(summary.XHSTopics) != 3 {
		t.Fatalf("synthesis = %#v", summary)
	}
	if !strings.Contains(markdown, "两个分类共同呈现结构变化") {
		t.Fatalf("markdown = %q, want synthesized situation", markdown)
	}
}

func TestRunnerCategoryFailureDoesNotCancelSiblingSummary(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })

	financeDonePath := filepath.Join(dir, "finance.done")
	commandPath := filepath.Join(dir, "codex")
	script := `#!/bin/sh
input="` + dir + `/input.$$"
cat > "$input"
if grep -q '当前分类：AI/科技' "$input"; then
  rm -f "$input"
  exit 7
elif grep -q '当前分类：新闻财经' "$input"; then
  sleep 0.1
  touch "` + financeDonePath + `"
  printf '%s' '{"overview_groups":[{"category":"新闻财经","items":["💹 财经要点"]}],"stories":[{"category":"新闻财经","title":"财经新闻","summary":"财经摘要。","impact":"财经影响。","source_article_ids":[1]}]}'
  rm -f "$input"
  exit 0
fi
rm -f "$input"
exit 8
`
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	articles := []model.Article{
		{Title: "AI", Source: "AI Source", Category: "AI/科技", Published: time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)},
		{Title: "Finance", Source: "Finance Source", Category: "新闻财经", Published: time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)},
	}
	runner := NewRunner("codex", []string{"exec"}, false, "", "")
	runner.failureLogPath = ""
	runner.SetSummaryOptions(true, 2)

	_, _, err := runner.SummarizeBriefingContext(context.Background(), articles, []string{"AI/科技", "新闻财经"}, time.UTC)
	if err == nil {
		t.Fatal("SummarizeBriefingContext() error = nil, want category failure")
	}
	if !strings.Contains(err.Error(), "summarize category AI/科技") || strings.Contains(err.Error(), "signal: killed") {
		t.Fatalf("SummarizeBriefingContext() error = %q, want original AI category error", err)
	}
	if _, statErr := os.Stat(financeDonePath); statErr != nil {
		t.Fatalf("finance sibling did not finish after AI failure: %v", statErr)
	}
}

func TestRunnerUsesFinalEditorToSelectExistingCategoryStories(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })

	aiOutput := filepath.Join(dir, "ai.json")
	financeOutput := filepath.Join(dir, "finance.json")
	editorOutput := filepath.Join(dir, "editor.json")
	files := map[string]string{
		aiOutput:      `{"overview_groups":[{"category":"AI/科技","items":["🤖 AI 要点"]}],"stories":[{"category":"AI/科技","title":"AI 新闻","summary":"AI 摘要。","impact":"AI 影响。","source_article_ids":[1]}]}`,
		financeOutput: `{"overview_groups":[{"category":"新闻财经","items":["💹 财经要点"]}],"stories":[{"category":"新闻财经","title":"财经新闻","summary":"财经摘要。","impact":"财经影响。","source_article_ids":[1]}]}`,
		editorOutput:  `{"selected_story_ids":[2,1],"overview_groups":[{"category":"AI/科技","items":["🤖 AI 新闻"]},{"category":"新闻财经","items":["💹 财经新闻"]}],"situation":"两个分类共同呈现结构变化。","xhs_topics":["人工智能","财经观察","每日新闻"],"directions":[{"title":"AI roadmap","why":"产品持续演进。","next":"观察发布节奏。","deep_command":"./news-briefing deep \"AI roadmap\" --ignore-seen"},{"title":"Market outlook","why":"市场预期变化。","next":"观察政策与价格。","deep_command":"./news-briefing deep \"Market outlook\" --ignore-seen"}]}`,
	}
	for path, body := range files {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write fake output: %v", err)
		}
	}

	commandPath := filepath.Join(dir, "codex")
	script := `#!/bin/sh
input="` + dir + `/input.$$"
cat > "$input"
if grep -q '候选 stories JSON' "$input"; then
  grep -q '"story_id":1' "$input" || exit 7
  grep -q '"story_id":2' "$input" || exit 7
  cat "` + editorOutput + `"
elif grep -q '当前分类：AI/科技' "$input"; then
  touch "` + dir + `/ai.ready"
  i=0; while [ ! -f "` + dir + `/finance.ready" ] && [ "$i" -lt 100 ]; do sleep 0.01; i=$((i+1)); done
  cat "` + aiOutput + `"
elif grep -q '当前分类：新闻财经' "$input"; then
  touch "` + dir + `/finance.ready"
  i=0; while [ ! -f "` + dir + `/ai.ready" ] && [ "$i" -lt 100 ]; do sleep 0.01; i=$((i+1)); done
  cat "` + financeOutput + `"
else
  exit 8
fi
rm -f "$input"
`
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	articles := []model.Article{
		{Title: "AI", Source: "AI Source", Category: "AI/科技", Published: time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)},
		{Title: "Finance", Source: "Finance Source", Category: "新闻财经", Published: time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)},
	}
	runner := NewRunner("codex", []string{"exec"}, false, "", "")
	runner.SetModelOptions("category-model", "medium", "translation-model", "high")
	runner.SetSummaryOptions(true, 2)
	runner.SetSummaryEditorOptions(true, "editor-model", "high", 1, 2, 2)
	summary, _, err := runner.SummarizeBriefingContext(context.Background(), articles, []string{"AI/科技", "新闻财经"}, time.UTC)
	if err != nil {
		t.Fatalf("SummarizeBriefingContext() error = %v", err)
	}
	if got := []string{summary.Stories[0].Title, summary.Stories[1].Title}; !reflect.DeepEqual(got, []string{"财经新闻", "AI 新闻"}) {
		t.Fatalf("selected story order = %#v", got)
	}
	if got := summary.Stories[0].SourceArticleIDs; !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("selected finance source ids = %#v, want [2]", got)
	}
	if len(summary.OverviewGroups) != 2 || summary.OverviewGroups[0].Category != "AI/科技" {
		t.Fatalf("overview groups = %#v", summary.OverviewGroups)
	}
}

func TestCallClaudeIncludesStdoutAndStderrOnExitError(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	commandName := "failing-ai"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\nprintf 'partial output'\n>&2 printf 'stderr detail'\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunner("failing-ai", nil, true, "", "")
	_, err := runner.callClaude("hello world")
	if err == nil {
		t.Fatal("callClaude() error = nil, want exit error")
	}
	for _, want := range []string{"ai cli failed after 1 attempts:", "stdout: partial output", "stderr: stderr detail"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("callClaude() error = %q, want substring %q", err.Error(), want)
		}
	}
}

func TestCallClaudeRejectsInvalidUTF8PromptBeforeStartingCLI(t *testing.T) {
	runner := NewRunner("command-must-not-run", nil, true, "", "")
	runner.failureLogPath = ""
	runner.retrySleep = func(context.Context, time.Duration) error {
		t.Fatal("invalid prompt must not be retried")
		return nil
	}

	prompt := "abc" + string([]byte{0xff}) + "中文"
	_, err := runner.callClaude(prompt)
	if err == nil {
		t.Fatal("callClaude() error = nil, want invalid prompt error")
	}
	if !IsInvalidPromptError(err) {
		t.Fatalf("IsInvalidPromptError(%v) = false, want true", err)
	}
	for _, want := range []string{"ai cli failed after 1 attempts", "invalid byte at offset 3"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("callClaude() error = %q, want substring %q", err.Error(), want)
		}
	}
}

func TestCallClaudeRetriesRetryableFailureAndEventuallySucceeds(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	statePath := filepath.Join(dir, "attempts.txt")
	commandName := "flaky-ai"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\n" +
		"COUNT=0\n" +
		"if [ -f \"" + statePath + "\" ]; then COUNT=$(cat \"" + statePath + "\"); fi\n" +
		"COUNT=$((COUNT+1))\n" +
		"printf '%s' \"$COUNT\" > \"" + statePath + "\"\n" +
		"if [ \"$COUNT\" -lt 3 ]; then\n" +
		"  >&2 printf 'server_error request req-%s' \"$COUNT\"\n" +
		"  exit 1\n" +
		"fi\n" +
		"printf 'final body'\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunner("flaky-ai", nil, true, "", "")
	runner.retrySleep = func(context.Context, time.Duration) error { return nil }
	got, err := runner.callClaudeWithKind(callKindSummarize, "hello world")
	if err != nil {
		t.Fatalf("callClaudeWithKind() error = %v", err)
	}
	if got != "final body" {
		t.Fatalf("callClaudeWithKind() = %q, want %q", got, "final body")
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile() attempts error = %v", err)
	}
	if strings.TrimSpace(string(data)) != "3" {
		t.Fatalf("attempt count = %q, want %q", strings.TrimSpace(string(data)), "3")
	}
}

func TestCallClaudeRetriesInternalStreamError(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	statePath := filepath.Join(dir, "attempts.txt")
	commandName := "stream-error-ai"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\n" +
		"COUNT=0\n" +
		"if [ -f \"" + statePath + "\" ]; then COUNT=$(cat \"" + statePath + "\"); fi\n" +
		"COUNT=$((COUNT+1))\n" +
		"printf '%s' \"$COUNT\" > \"" + statePath + "\"\n" +
		"if [ \"$COUNT\" -eq 1 ]; then\n" +
		"  >&2 printf 'API Error: stream error: stream ID 1; INTERNAL_ERROR; received from peer'\n" +
		"  exit 1\n" +
		"fi\n" +
		"printf 'final body'\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunner("stream-error-ai", nil, true, "", "")
	runner.retrySleep = func(context.Context, time.Duration) error { return nil }
	got, err := runner.callClaudeWithKind(callKindSummarize, "hello world")
	if err != nil {
		t.Fatalf("callClaudeWithKind() error = %v", err)
	}
	if got != "final body" {
		t.Fatalf("callClaudeWithKind() = %q, want %q", got, "final body")
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile() attempts error = %v", err)
	}
	if strings.TrimSpace(string(data)) != "2" {
		t.Fatalf("attempt count = %q, want %q", strings.TrimSpace(string(data)), "2")
	}
}

func TestCallClaudeRetriesStreamingTimedOutError(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	statePath := filepath.Join(dir, "attempts.txt")
	commandName := "streaming-timeout-ai"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\n" +
		"COUNT=0\n" +
		"if [ -f \"" + statePath + "\" ]; then COUNT=$(cat \"" + statePath + "\"); fi\n" +
		"COUNT=$((COUNT+1))\n" +
		"printf '%s' \"$COUNT\" > \"" + statePath + "\"\n" +
		"if [ \"$COUNT\" -eq 1 ]; then\n" +
		"  printf 'API Error: Upstream response timed out while streaming response body'\n" +
		"  exit 1\n" +
		"fi\n" +
		"printf 'final body'\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunner("streaming-timeout-ai", nil, true, "", "")
	runner.retrySleep = func(context.Context, time.Duration) error { return nil }
	got, err := runner.callClaudeWithKind(callKindSummarize, "hello world")
	if err != nil {
		t.Fatalf("callClaudeWithKind() error = %v", err)
	}
	if got != "final body" {
		t.Fatalf("callClaudeWithKind() = %q, want %q", got, "final body")
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile() attempts error = %v", err)
	}
	if strings.TrimSpace(string(data)) != "2" {
		t.Fatalf("attempt count = %q, want %q", strings.TrimSpace(string(data)), "2")
	}
}

func TestCallClaudeRetriesGenericRetryableAPIError(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	statePath := filepath.Join(dir, "attempts.txt")
	commandName := "generic-api-error-ai"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\n" +
		"COUNT=0\n" +
		"if [ -f \"" + statePath + "\" ]; then COUNT=$(cat \"" + statePath + "\"); fi\n" +
		"COUNT=$((COUNT+1))\n" +
		"printf '%s' \"$COUNT\" > \"" + statePath + "\"\n" +
		"if [ \"$COUNT\" -eq 1 ]; then\n" +
		"  printf 'API Error: An error occurred while processing your request. You can retry your request, or contact us through our help center if the error persists.'\n" +
		"  exit 1\n" +
		"fi\n" +
		"printf 'final body'\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunner("generic-api-error-ai", nil, true, "", "")
	runner.retrySleep = func(context.Context, time.Duration) error { return nil }
	got, err := runner.callClaudeWithKind(callKindSummarize, "hello world")
	if err != nil {
		t.Fatalf("callClaudeWithKind() error = %v", err)
	}
	if got != "final body" {
		t.Fatalf("callClaudeWithKind() = %q, want %q", got, "final body")
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile() attempts error = %v", err)
	}
	if strings.TrimSpace(string(data)) != "2" {
		t.Fatalf("attempt count = %q, want %q", strings.TrimSpace(string(data)), "2")
	}
}

func TestIsRetryableOAuthCredentialErrorMatchesOnlyKnownRotationFailures(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		stderr string
		want   bool
	}{
		{name: "refresh token reused", stderr: "Refresh token has already been used", want: true},
		{name: "revoked code", stdout: `{"code":"token_revoked"}`, want: true},
		{name: "invalidated token", stderr: "Encountered invalidated OAuth token for user", want: true},
		{name: "generic unauthorized", stderr: "401 Unauthorized", want: false},
		{name: "unrelated token error", stderr: "token budget exceeded", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := isRetryableOAuthCredentialError(errors.New("exit status 1"), test.stdout, test.stderr)
			if got != test.want {
				t.Fatalf("isRetryableOAuthCredentialError() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestCallClaudeRetriesOAuthCredentialErrorOnceThenSucceeds(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	statePath := filepath.Join(dir, "attempts.txt")
	commandName := "oauth-refresh-ai"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\n" +
		"COUNT=0\n" +
		"if [ -f \"" + statePath + "\" ]; then COUNT=$(cat \"" + statePath + "\"); fi\n" +
		"COUNT=$((COUNT+1))\n" +
		"printf '%s' \"$COUNT\" > \"" + statePath + "\"\n" +
		"if [ \"$COUNT\" -eq 1 ]; then\n" +
		"  >&2 printf 'Encountered invalidated oauth token for user, failing request'\n" +
		"  exit 1\n" +
		"fi\n" +
		"printf 'final body'\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunnerWithRetryDelays(commandName, nil, true, "", "", []time.Duration{time.Minute})
	var slept []time.Duration
	runner.retrySleep = func(_ context.Context, delay time.Duration) error {
		slept = append(slept, delay)
		return nil
	}
	got, err := runner.callClaudeWithKind(callKindSummarize, "hello world")
	if err != nil {
		t.Fatalf("callClaudeWithKind() error = %v", err)
	}
	if got != "final body" {
		t.Fatalf("callClaudeWithKind() = %q, want final body", got)
	}
	if want := []time.Duration{oauthCredentialRetryDelay}; !reflect.DeepEqual(slept, want) {
		t.Fatalf("retry sleeps = %v, want %v", slept, want)
	}
}

func TestCallClaudeStopsAfterOneOAuthCredentialRetry(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	statePath := filepath.Join(dir, "attempts.txt")
	commandName := "revoked-oauth-ai"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\n" +
		"COUNT=0\n" +
		"if [ -f \"" + statePath + "\" ]; then COUNT=$(cat \"" + statePath + "\"); fi\n" +
		"COUNT=$((COUNT+1))\n" +
		"printf '%s' \"$COUNT\" > \"" + statePath + "\"\n" +
		" >&2 printf 'token_revoked'\n" +
		"exit 1\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunnerWithRetryDelays(commandName, nil, true, "", "", []time.Duration{time.Minute})
	runner.failureLogPath = filepath.Join(dir, "ai-cli-failures.log")
	var slept []time.Duration
	runner.retrySleep = func(_ context.Context, delay time.Duration) error {
		slept = append(slept, delay)
		return nil
	}
	_, err := runner.callClaudeWithKind(callKindSummarize, "hello world")
	if err == nil || !strings.Contains(err.Error(), "after 2 attempts") {
		t.Fatalf("callClaudeWithKind() error = %v, want two-attempt failure", err)
	}
	data, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatalf("ReadFile() attempts error = %v", readErr)
	}
	if got := strings.TrimSpace(string(data)); got != "2" {
		t.Fatalf("attempt count = %q, want 2", got)
	}
	if want := []time.Duration{oauthCredentialRetryDelay}; !reflect.DeepEqual(slept, want) {
		t.Fatalf("retry sleeps = %v, want %v", slept, want)
	}
}

func TestCallClaudeSerializesConcurrentOAuthCredentialRetries(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	commandName := "concurrent-oauth-ai"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\n>&2 printf 'refresh token already used'\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunner(commandName, nil, true, "", "")
	runner.failureLogPath = ""
	var mu sync.Mutex
	activeSleeps := 0
	maxActiveSleeps := 0
	runner.retrySleep = func(_ context.Context, _ time.Duration) error {
		mu.Lock()
		activeSleeps++
		if activeSleeps > maxActiveSleeps {
			maxActiveSleeps = activeSleeps
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		activeSleeps--
		mu.Unlock()
		return nil
	}

	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			_, _ = runner.callClaudeWithKind(callKindSummarize, "hello world")
		})
	}
	wg.Wait()

	if maxActiveSleeps != 1 {
		t.Fatalf("max concurrent OAuth retry sleeps = %d, want 1", maxActiveSleeps)
	}
}

func TestCallClaudeRetriesCCSStartupLockError(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	statePath := filepath.Join(dir, "attempts.txt")
	commandName := "locked-ai"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\n" +
		"COUNT=0\n" +
		"if [ -f \"" + statePath + "\" ]; then COUNT=$(cat \"" + statePath + "\"); fi\n" +
		"COUNT=$((COUNT+1))\n" +
		"printf '%s' \"$COUNT\" > \"" + statePath + "\"\n" +
		"if [ \"$COUNT\" -eq 1 ]; then\n" +
		"  >&2 printf '[X] Lock file is already being held'\n" +
		"  exit 1\n" +
		"fi\n" +
		"printf 'final body'\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunner("locked-ai", nil, true, "", "")
	runner.retrySleep = func(context.Context, time.Duration) error { return nil }
	got, err := runner.callClaudeWithKind(callKindSummarize, "hello world")
	if err != nil {
		t.Fatalf("callClaudeWithKind() error = %v", err)
	}
	if got != "final body" {
		t.Fatalf("callClaudeWithKind() = %q, want %q", got, "final body")
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile() attempts error = %v", err)
	}
	if strings.TrimSpace(string(data)) != "2" {
		t.Fatalf("attempt count = %q, want %q", strings.TrimSpace(string(data)), "2")
	}
}

func TestCallClaudeUsesConfiguredRetryDelays(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	statePath := filepath.Join(dir, "attempts.txt")
	commandName := "configured-delay-ai"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\n" +
		"COUNT=0\n" +
		"if [ -f \"" + statePath + "\" ]; then COUNT=$(cat \"" + statePath + "\"); fi\n" +
		"COUNT=$((COUNT+1))\n" +
		"printf '%s' \"$COUNT\" > \"" + statePath + "\"\n" +
		"if [ \"$COUNT\" -lt 3 ]; then\n" +
		"  >&2 printf 'server_error request req-%s' \"$COUNT\"\n" +
		"  exit 1\n" +
		"fi\n" +
		"printf 'final body'\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunnerWithRetryDelays("configured-delay-ai", nil, true, "", "", []time.Duration{2 * time.Second, 5 * time.Second})
	var slept []time.Duration
	runner.retrySleep = func(ctx context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	got, err := runner.callClaudeWithKind(callKindSummarize, "hello world")
	if err != nil {
		t.Fatalf("callClaudeWithKind() error = %v", err)
	}
	if got != "final body" {
		t.Fatalf("callClaudeWithKind() = %q, want final body", got)
	}
	wantSleeps := []time.Duration{2 * time.Second, 5 * time.Second}
	if !reflect.DeepEqual(slept, wantSleeps) {
		t.Fatalf("retry sleeps = %v, want %v", slept, wantSleeps)
	}
}

func TestCallClaudeWithKindContextReturnsContextErrorWithoutRetry(t *testing.T) {
	runner := NewRunner("unused-ai", nil, true, "", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := runner.callClaudeWithKindContext(ctx, callKindSummarize, "hello world")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("callClaudeWithKindContext() error = %v, want context.Canceled", err)
	}
}

func TestCallClaudeWithKindContextStopsDuringRetrySleep(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	commandName := "cancel-retry-ai"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\n>&2 printf 'server_error'\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runner := NewRunner("cancel-retry-ai", nil, true, "", "")
	runner.retrySleep = func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}
	_, err := runner.callClaudeWithKindContext(ctx, callKindSummarize, "hello world")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("callClaudeWithKindContext() error = %v, want context.Canceled", err)
	}
}

func TestIsRetryableAICLIErrorRejectsNonRetryableFailure(t *testing.T) {
	err := fmt.Errorf("ai cli: exit status 1")
	if isRetryableAICLIError(err, "", "flag provided but not defined") {
		t.Fatal("isRetryableAICLIError() = true, want false for argument error")
	}
}

func TestCallClaudeReturnsAggregatedErrorAfterRetryExhaustion(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	statePath := filepath.Join(dir, "attempts.txt")
	commandName := "always-fail-ai"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\n" +
		"COUNT=0\n" +
		"if [ -f \"" + statePath + "\" ]; then COUNT=$(cat \"" + statePath + "\"); fi\n" +
		"COUNT=$((COUNT+1))\n" +
		"printf '%s' \"$COUNT\" > \"" + statePath + "\"\n" +
		"printf 'stdout attempt %s' \"$COUNT\"\n" +
		" >&2 printf 'server_error request req-%s' \"$COUNT\"\n" +
		"exit 1\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunner("always-fail-ai", nil, true, "", "")
	runner.retrySleep = func(context.Context, time.Duration) error { return nil }
	_, err := runner.callClaudeWithKind(callKindSummarize, "hello world")
	if err == nil {
		t.Fatal("callClaudeWithKind() error = nil, want retry exhaustion")
	}
	for _, want := range []string{"after 6 attempts", "stdout attempt 6", "req-6"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("callClaudeWithKind() error = %q, want substring %q", err.Error(), want)
		}
	}
}

func TestExtractRequestIDFindsOpenAIStyleRequestID(t *testing.T) {
	stderr := "server_error request ID 19318a28-85ad-423c-a7cd-9b262bcb6741"
	got := extractRequestID("", stderr)
	if got != "19318a28-85ad-423c-a7cd-9b262bcb6741" {
		t.Fatalf("extractRequestID() = %q", got)
	}
}

func TestCallClaudeWritesFailureLogAfterRetryExhaustion(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "ai-cli-failures.log")
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	commandName := "log-fail-ai"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\nprintf 'stdout body'\n>&2 printf 'server_error request ID 19318a28-85ad-423c-a7cd-9b262bcb6741'\nexit 1\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunner("log-fail-ai", nil, true, "", "")
	runner.retrySleep = func(context.Context, time.Duration) error { return nil }
	runner.failureLogPath = logPath
	_, err := runner.callClaudeWithKind(callKindTranslate, "hello world")
	if err == nil {
		t.Fatal("callClaudeWithKind() error = nil, want failure")
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() log error = %v", err)
	}
	for _, want := range []string{"translate", "19318a28-85ad-423c-a7cd-9b262bcb6741", "attempts=6"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("failure log = %q, want substring %q", string(data), want)
		}
	}
}

func TestCallClaudeRetrySuccessStillSanitizesCCSOutput(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	statePath := filepath.Join(dir, "attempts.txt")
	commandName := "ccs"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\n" +
		"COUNT=0\n" +
		"if [ -f \"" + statePath + "\" ]; then COUNT=$(cat \"" + statePath + "\"); fi\n" +
		"COUNT=$((COUNT+1))\n" +
		"printf '%s' \"$COUNT\" > \"" + statePath + "\"\n" +
		"if [ \"$COUNT\" -lt 2 ]; then\n" +
		"  >&2 printf 'server_error request ID 19318a28-85ad-423c-a7cd-9b262bcb6741'\n" +
		"  exit 1\n" +
		"fi\n" +
		"printf '[i] Joined existing CLIProxy on port 8317 (http)\n最终正文'\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunner("ccs", []string{"codex"}, true, "", "")
	runner.retrySleep = func(context.Context, time.Duration) error { return nil }
	got, err := runner.callClaudeWithKind(callKindSummarize, "hello world")
	if err != nil {
		t.Fatalf("callClaudeWithKind() error = %v", err)
	}
	if got != "最终正文" {
		t.Fatalf("callClaudeWithKind() = %q, want %q", got, "最终正文")
	}
}

func TestCallClaudeRetriesWhenSanitizedOutputIsEmpty(t *testing.T) {
	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	statePath := filepath.Join(dir, "attempts.txt")
	commandName := "ccs"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\n" +
		"COUNT=0\n" +
		"if [ -f \"" + statePath + "\" ]; then COUNT=$(cat \"" + statePath + "\"); fi\n" +
		"COUNT=$((COUNT+1))\n" +
		"printf '%s' \"$COUNT\" > \"" + statePath + "\"\n" +
		"if [ \"$COUNT\" -lt 2 ]; then\n" +
		"  printf '[i] Joined existing CLIProxy on port 8317 (http)\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '[i] Joined existing CLIProxy on port 8317 (http)\n最终正文'\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunner("ccs", []string{"codex"}, true, "", "")
	runner.retrySleep = func(context.Context, time.Duration) error { return nil }
	got, err := runner.callClaudeWithKind(callKindSummarize, "hello world")
	if err != nil {
		t.Fatalf("callClaudeWithKind() error = %v", err)
	}
	if got != "最终正文" {
		t.Fatalf("callClaudeWithKind() = %q, want %q", got, "最终正文")
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile() attempts error = %v", err)
	}
	if strings.TrimSpace(string(data)) != "2" {
		t.Fatalf("attempt count = %q, want %q", strings.TrimSpace(string(data)), "2")
	}
}

func TestTranslateWritesFailureLogWhenSanitizedOutputStaysEmpty(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "ai-cli-failures.log")
	statePath := filepath.Join(dir, "attempts.txt")
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	commandName := "ccs"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\n" +
		"COUNT=0\n" +
		"if [ -f \"" + statePath + "\" ]; then COUNT=$(cat \"" + statePath + "\"); fi\n" +
		"COUNT=$((COUNT+1))\n" +
		"printf '%s' \"$COUNT\" > \"" + statePath + "\"\n" +
		"printf '[i] Joined existing CLIProxy on port 8317 (http)\n'\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunner("ccs", []string{"codex"}, true, "", "")
	runner.retrySleep = func(context.Context, time.Duration) error { return nil }
	runner.failureLogPath = logPath
	_, err := runner.Translate(sampleArticles(), []string{"AI/科技"}, time.Local)
	if err == nil {
		t.Fatal("Translate() error = nil, want retry exhaustion")
	}
	for _, want := range []string{"after 6 attempts", "ai cli returned empty content"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Translate() error = %q, want substring %q", err.Error(), want)
		}
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile() attempts error = %v", err)
	}
	if strings.TrimSpace(string(data)) != "6" {
		t.Fatalf("attempt count = %q, want %q", strings.TrimSpace(string(data)), "6")
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() log error = %v", err)
	}
	for _, want := range []string{"translate", "attempts=6"} {
		if !strings.Contains(string(logData), want) {
			t.Fatalf("failure log = %q, want substring %q", string(logData), want)
		}
	}
}

func TestSummarizeWritesFailureLogWhenSanitizedOutputStaysEmpty(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "ai-cli-failures.log")
	statePath := filepath.Join(dir, "attempts.txt")
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	commandName := "ccs"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\n" +
		"COUNT=0\n" +
		"if [ -f \"" + statePath + "\" ]; then COUNT=$(cat \"" + statePath + "\"); fi\n" +
		"COUNT=$((COUNT+1))\n" +
		"printf '%s' \"$COUNT\" > \"" + statePath + "\"\n" +
		"printf '[i] Joined existing CLIProxy on port 8317 (http)\n'\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunner("ccs", []string{"codex"}, true, "", "")
	runner.retrySleep = func(context.Context, time.Duration) error { return nil }
	runner.failureLogPath = logPath
	_, err := runner.Summarize(sampleArticles(), []string{"AI/科技"}, time.Local)
	if err == nil {
		t.Fatal("Summarize() error = nil, want retry exhaustion")
	}
	for _, want := range []string{"after 6 attempts", "ai cli returned empty content"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Summarize() error = %q, want substring %q", err.Error(), want)
		}
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile() attempts error = %v", err)
	}
	if strings.TrimSpace(string(data)) != "6" {
		t.Fatalf("attempt count = %q, want %q", strings.TrimSpace(string(data)), "6")
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() log error = %v", err)
	}
	for _, want := range []string{"summarize", "attempts=6"} {
		if !strings.Contains(string(logData), want) {
			t.Fatalf("failure log = %q, want substring %q", string(logData), want)
		}
	}
}

func TestDeepDiveWritesFailureLogWhenSanitizedOutputStaysEmpty(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "ai-cli-failures.log")
	statePath := filepath.Join(dir, "attempts.txt")
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	commandName := "ccs"
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\n" +
		"COUNT=0\n" +
		"if [ -f \"" + statePath + "\" ]; then COUNT=$(cat \"" + statePath + "\"); fi\n" +
		"COUNT=$((COUNT+1))\n" +
		"printf '%s' \"$COUNT\" > \"" + statePath + "\"\n" +
		"printf '[i] Joined existing CLIProxy on port 8317 (http)\n'\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	runner := NewRunner("ccs", []string{"codex"}, true, "", "")
	runner.retrySleep = func(context.Context, time.Duration) error { return nil }
	runner.failureLogPath = logPath
	_, err := runner.DeepDive("OpenAI", sampleArticles(), time.Local)
	if err == nil {
		t.Fatal("DeepDive() error = nil, want retry exhaustion")
	}
	for _, want := range []string{"after 6 attempts", "ai cli returned empty content"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("DeepDive() error = %q, want substring %q", err.Error(), want)
		}
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile() attempts error = %v", err)
	}
	if strings.TrimSpace(string(data)) != "6" {
		t.Fatalf("attempt count = %q, want %q", strings.TrimSpace(string(data)), "6")
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() log error = %v", err)
	}
	for _, want := range []string{"deep", "attempts=6"} {
		if !strings.Contains(string(logData), want) {
			t.Fatalf("failure log = %q, want substring %q", string(logData), want)
		}
	}
}

func TestRunnerUsesConfiguredArgsForSummarize(t *testing.T) {
	argsPath := setupFakeCLIOutput(t, "claude", validBriefingJSON())
	runner := NewRunner("claude", []string{"--model", "claude-opus-4-6", "--bare", "--disable-slash-commands"}, true, "", "")

	articles := sampleArticles()
	if _, err := runner.Summarize(articles, []string{"AI/科技", "国际政治"}, time.Local); err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}

	want := []string{
		"--model",
		"claude-opus-4-6",
		"--bare",
		"--disable-slash-commands",
		"--append-system-prompt",
		nonInteractiveBriefingSystemPrompt,
		"-p",
		briefingPrompt + "\n\n---\n以下是今日新闻条目：\n\n" + output.GroupedArticleListView(articles, []string{"AI/科技", "国际政治"}, time.Local),
	}
	if args := readFakeCLIArgs(t, argsPath); !reflect.DeepEqual(args, want) {
		t.Fatalf("Summarize() args = %#v, want %#v", args, want)
	}
}

func TestRunnerUsesConfiguredArgsForTranslate(t *testing.T) {
	setupFakeCLI(t, "claude")
	runner := NewRunner("claude", []string{"--model", "claude-opus-4-6", "--bare", "--disable-slash-commands"}, true, "", "")

	articles := sampleArticles()
	got, err := runner.Translate(articles, []string{"AI/科技", "国际政治"}, time.Local)
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}

	want := []string{
		"--model",
		"claude-opus-4-6",
		"--bare",
		"--disable-slash-commands",
		"--append-system-prompt",
		nonInteractiveBriefingSystemPrompt,
		"-p",
		translatePrompt + "\n\n" + output.GroupedArticleListView(articles, []string{"AI/科技", "国际政治"}, time.Local),
	}
	if args := splitArgs(got); !reflect.DeepEqual(args, want) {
		t.Fatalf("Translate() args = %#v, want %#v", args, want)
	}
}

func TestRunnerInstancesDoNotShareCommandOrProxyState(t *testing.T) {
	ccsArgsPath := setupFakeCLIOutput(t, "ccs", validBriefingJSON())
	otherArgsPath := setupFakeCLIOutput(t, "my-ai", validBriefingJSON())

	ccsRunner := NewRunner("ccs", []string{"codex", "--bare"}, true, "http://127.0.0.1:7897", "")
	otherRunner := NewRunner("my-ai", []string{"foo"}, false, "", "")

	articles := sampleArticles()
	if _, err := ccsRunner.Summarize(articles, []string{"AI/科技", "国际政治"}, time.Local); err != nil {
		t.Fatalf("ccsRunner.Summarize() error = %v", err)
	}
	if _, err := otherRunner.Summarize(articles, []string{"AI/科技", "国际政治"}, time.Local); err != nil {
		t.Fatalf("otherRunner.Summarize() error = %v", err)
	}

	firstArgs := readFakeCLIArgs(t, ccsArgsPath)
	secondArgs := readFakeCLIArgs(t, otherArgsPath)
	if firstArgs[0] != "codex" {
		t.Fatalf("first runner args = %#v", firstArgs)
	}
	if secondArgs[0] != "foo" {
		t.Fatalf("second runner args = %#v", secondArgs)
	}
	for _, arg := range secondArgs {
		if arg == "--bare" || arg == "--disable-slash-commands" || arg == "--append-system-prompt" {
			t.Fatalf("second runner unexpectedly inherited config-driven flags: %#v", secondArgs)
		}
	}
}

func TestSanitizeCLIOutputStripsCCSInfraLogs(t *testing.T) {
	raw := `[i] CLIProxy Plus update: v6.8.50-0 -> v6.8.51-0
[i] Run "ccs cliproxy stop" then restart to apply update
[i] Joined existing CLIProxy on port 8317 (http)
收到，我按“重要性排序 + 关联合并”整理如下：

## AI/科技`

	got := sanitizeCLIOutput(raw)
	want := "收到，我按“重要性排序 + 关联合并”整理如下：\n\n## AI/科技"
	if got != want {
		t.Fatalf("sanitizeCLIOutput() = %q, want %q", got, want)
	}
}

func TestRunnerShouldSanitizeCLIOutputOnlyForCCSCodex(t *testing.T) {
	if !NewRunner("ccs", []string{"codex"}, true, "", "").shouldSanitizeCLIOutput() {
		t.Fatalf("shouldSanitizeCLIOutput() = false, want true for ccs codex")
	}
	if NewRunner("ccs", []string{"gemini"}, true, "", "").shouldSanitizeCLIOutput() {
		t.Fatalf("shouldSanitizeCLIOutput() = true, want false for ccs gemini")
	}
	if NewRunner("my-ai", []string{"codex"}, true, "", "").shouldSanitizeCLIOutput() {
		t.Fatalf("shouldSanitizeCLIOutput() = true, want false for non-ccs command")
	}
}

func TestLegacyShouldSanitizeCLIOutputUsesDefaultConfig(t *testing.T) {
	ResetCommandForTest()
	t.Cleanup(ResetCommandForTest)

	if shouldSanitizeCLIOutput() {
		t.Fatalf("shouldSanitizeCLIOutput() = true, want false for default codex")
	}
}

func TestSetProxyPreservesConfiguredCommand(t *testing.T) {
	ResetCommandForTest()
	t.Cleanup(ResetCommandForTest)
	argsPath := setupFakeCLIOutput(t, "my-ai", validBriefingJSON())
	SetCommand("my-ai", []string{"foo"})
	SetProxy("http://127.0.0.1:7897", "socks5://127.0.0.1:7898")

	if _, err := Summarize(sampleArticles(), []string{"AI/科技", "国际政治"}, time.Local); err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}

	want := []string{"foo", "--append-system-prompt", nonInteractiveBriefingSystemPrompt, "-p", briefingPrompt + "\n\n---\n以下是今日新闻条目：\n\n" + output.GroupedArticleListView(sampleArticles(), []string{"AI/科技", "国际政治"}, time.Local)}
	if args := readFakeCLIArgs(t, argsPath); !reflect.DeepEqual(args, want) {
		t.Fatalf("Summarize() args = %#v, want %#v", args, want)
	}
}

func TestResetCommandForTestRestoresDefaultConfig(t *testing.T) {
	SetCommand("my-ai", []string{"foo"})
	SetProxy("http://127.0.0.1:7897", "socks5://127.0.0.1:7898")
	ResetCommandForTest()
	t.Cleanup(ResetCommandForTest)

	if shouldSanitizeCLIOutput() {
		t.Fatalf("shouldSanitizeCLIOutput() = true, want false after reset")
	}
	if runner := legacyDefaultRunner(); len(runner.proxyEnv) != 0 {
		t.Fatalf("proxy env after reset = %#v, want empty", runner.proxyEnv)
	}
}

func TestDefaultRunnerConcurrentMutationDoesNotRace(t *testing.T) {
	ResetCommandForTest()
	t.Cleanup(ResetCommandForTest)
	setupFakeCLI(t, "claude")
	setupFakeCLI(t, "codex")
	setupFakeCLI(t, "ccs")
	setupFakeCLI(t, "my-ai")

	articles := sampleArticles()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			SetCommand("ccs", []string{"codex"})
		})
		wg.Go(func() {
			SetProxy("http://127.0.0.1:7897", "")
		})
		wg.Go(func() {
			_, _ = Summarize(articles, []string{"AI/科技", "国际政治"}, time.Local)
		})
	}
	wg.Wait()
}

func validBriefingJSON() string {
	return `{"overview_groups":[{"category":"AI/科技","items":["🤖 OpenAI 发布新功能"]}],"stories":[{"category":"AI/科技","title":"OpenAI 发布新功能","summary":"功能摘要。","impact":"影响分析。","source_article_ids":[1],"source_line":"来源: Example | 2026-03-18 14:00"}],"situation":"今日态势。","directions":[{"title":"OpenAI roadmap","why":"值得继续追。","next":"观察发布节奏。","deep_command":"./news-briefing deep \"OpenAI roadmap\" --ignore-seen"}]}`
}

func setupFakeCLIOutput(t *testing.T, baseName string, output string) string {
	t.Helper()

	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	commandName := baseName
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	argsPath := filepath.Join(dir, "args.txt")
	outputPath := filepath.Join(dir, "output.txt")
	if err := os.WriteFile(outputPath, []byte(output), 0o644); err != nil {
		t.Fatalf("write fake cli output: %v", err)
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\n" +
		"for arg do printf '%s\037' \"$arg\" >> \"" + argsPath + "\"; done\n" +
		"printf '\n' >> \"" + argsPath + "\"\n" +
		"cat \"" + outputPath + "\"\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	return argsPath
}

func setupFakeCLIOutputAndInput(t *testing.T, baseName string, output string) (string, string) {
	t.Helper()

	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	commandName := baseName
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	argsPath := filepath.Join(dir, "args.txt")
	stdinPath := filepath.Join(dir, "stdin.txt")
	outputPath := filepath.Join(dir, "output.txt")
	if err := os.WriteFile(outputPath, []byte(output), 0o644); err != nil {
		t.Fatalf("write fake CLI output: %v", err)
	}
	commandPath := filepath.Join(dir, commandName)
	script := "#!/bin/sh\n" +
		"for arg do printf '%s\037' \"$arg\" >> \"" + argsPath + "\"; done\n" +
		"printf '\n' >> \"" + argsPath + "\"\n" +
		"cat > \"" + stdinPath + "\"\n" +
		"cat \"" + outputPath + "\"\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake CLI: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	return argsPath, stdinPath
}

func readFakeCLIArgs(t *testing.T, argsPath string) []string {
	t.Helper()
	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read fake cli args: %v", err)
	}
	return splitArgs(strings.TrimSuffix(string(data), "\n"))
}

func setupFakeCLI(t *testing.T, baseName string) {
	t.Helper()

	dir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		ResetCommandForTest()
	})

	commandName := baseName
	if runtime.GOOS == "windows" {
		commandName += ".bat"
	}
	commandPath := filepath.Join(dir, commandName)
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\nprintf '%s\037' \"$@\"\n"), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
}

func splitArgs(raw string) []string {
	parts := strings.Split(raw, argSep)
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func sampleArticles() []model.Article {
	return []model.Article{{
		Title:     "OpenAI ships feature",
		Link:      "https://example.com/news",
		Summary:   "Feature summary",
		Source:    "Example",
		Category:  "AI/科技",
		Published: time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC),
	}}
}
