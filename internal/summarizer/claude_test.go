package summarizer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
	runner := NewRunner("claude", nil, nil, false, "", "")
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

func TestRunnerUsesConfiguredExtraFlagsForDeepDive(t *testing.T) {
	setupFakeCLI(t, "claude")
	runner := NewRunner("claude", []string{"--model", "claude-opus-4-6"}, []string{"--bare", "--disable-slash-commands"}, true, "", "")

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
	setupFakeCLI(t, "ccs")
	runner := NewRunner("", nil, nil, true, "", "")

	got, err := runner.callClaude("hello world")
	if err != nil {
		t.Fatalf("callClaude() error = %v", err)
	}

	want := []string{"codex", "-p", "hello world"}
	if args := splitArgs(got); !reflect.DeepEqual(args, want) {
		t.Fatalf("callClaude() args = %#v, want %#v", args, want)
	}
}

func TestRunnerUsesConfiguredCommandAndArgs(t *testing.T) {
	setupFakeCLI(t, "my-ai")
	runner := NewRunner("my-ai", []string{"foo", "bar"}, nil, true, "", "")

	got, err := runner.callClaude("hello world", "--model", "haiku")
	if err != nil {
		t.Fatalf("callClaude() error = %v", err)
	}

	want := []string{"foo", "bar", "--model", "haiku", "-p", "hello world"}
	if args := splitArgs(got); !reflect.DeepEqual(args, want) {
		t.Fatalf("callClaude() args = %#v, want %#v", args, want)
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

	runner := NewRunner("failing-ai", nil, nil, true, "", "")
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

	runner := NewRunner("flaky-ai", nil, nil, true, "", "")
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

	runner := NewRunner("stream-error-ai", nil, nil, true, "", "")
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

	runner := NewRunner("locked-ai", nil, nil, true, "", "")
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

	runner := NewRunnerWithRetryDelays("configured-delay-ai", nil, nil, true, "", "", []time.Duration{2 * time.Second, 5 * time.Second})
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
	runner := NewRunner("unused-ai", nil, nil, true, "", "")
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
	runner := NewRunner("cancel-retry-ai", nil, nil, true, "", "")
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

	runner := NewRunner("always-fail-ai", nil, nil, true, "", "")
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

	runner := NewRunner("log-fail-ai", nil, nil, true, "", "")
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

	runner := NewRunner("ccs", []string{"codex"}, nil, true, "", "")
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

	runner := NewRunner("ccs", []string{"codex"}, nil, true, "", "")
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

	runner := NewRunner("ccs", []string{"codex"}, nil, true, "", "")
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

	runner := NewRunner("ccs", []string{"codex"}, nil, true, "", "")
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

	runner := NewRunner("ccs", []string{"codex"}, nil, true, "", "")
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

func TestRunnerUsesConfiguredExtraFlagsForSummarize(t *testing.T) {
	argsPath := setupFakeCLIOutput(t, "claude", validBriefingJSON())
	runner := NewRunner("claude", []string{"--model", "claude-opus-4-6"}, []string{"--bare", "--disable-slash-commands"}, true, "", "")

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

func TestRunnerUsesConfiguredExtraFlagsForTranslate(t *testing.T) {
	setupFakeCLI(t, "claude")
	runner := NewRunner("claude", []string{"--model", "claude-opus-4-6"}, []string{"--bare", "--disable-slash-commands"}, true, "", "")

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

	ccsRunner := NewRunner("ccs", []string{"codex"}, []string{"--bare"}, true, "http://127.0.0.1:7897", "")
	otherRunner := NewRunner("my-ai", []string{"foo"}, nil, false, "", "")

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
	if !NewRunner("ccs", []string{"codex"}, nil, true, "", "").shouldSanitizeCLIOutput() {
		t.Fatalf("shouldSanitizeCLIOutput() = false, want true for ccs codex")
	}
	if NewRunner("ccs", []string{"gemini"}, nil, true, "", "").shouldSanitizeCLIOutput() {
		t.Fatalf("shouldSanitizeCLIOutput() = true, want false for ccs gemini")
	}
	if NewRunner("my-ai", []string{"codex"}, nil, true, "", "").shouldSanitizeCLIOutput() {
		t.Fatalf("shouldSanitizeCLIOutput() = true, want false for non-ccs command")
	}
}

func TestLegacyShouldSanitizeCLIOutputUsesDefaultConfig(t *testing.T) {
	ResetCommandForTest()
	t.Cleanup(ResetCommandForTest)

	if !shouldSanitizeCLIOutput() {
		t.Fatalf("shouldSanitizeCLIOutput() = false, want true for default ccs codex")
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

	if !shouldSanitizeCLIOutput() {
		t.Fatalf("shouldSanitizeCLIOutput() = false, want true after reset")
	}
	if runner := legacyDefaultRunner(); len(runner.proxyEnv) != 0 {
		t.Fatalf("proxy env after reset = %#v, want empty", runner.proxyEnv)
	}
}

func TestDefaultRunnerConcurrentMutationDoesNotRace(t *testing.T) {
	ResetCommandForTest()
	t.Cleanup(ResetCommandForTest)
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
