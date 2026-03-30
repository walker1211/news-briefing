package summarizer

import (
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

func TestBriefingPromptUsesFollowupDirectionsInsteadOfTopicSuggestions(t *testing.T) {
	if !strings.Contains(briefingPrompt, "## 今日最值得追的方向") {
		t.Fatalf("briefingPrompt missing new section title")
	}

	for _, want := range []string{
		"**为什么值得追：**",
		"**接下来关注什么：**",
		"**深挖命令：**",
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
	got, err := runner.DeepDive("OpenAI", articles)
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
		fmt.Sprintf(deepDivePrompt, "OpenAI") + "\n\n---\n话题: OpenAI\n\n相关新闻:\n" + output.ArticleListView(articles),
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

func TestRunnerUsesConfiguredExtraFlagsForSummarize(t *testing.T) {
	setupFakeCLI(t, "claude")
	runner := NewRunner("claude", []string{"--model", "claude-opus-4-6"}, []string{"--bare", "--disable-slash-commands"}, true, "", "")

	articles := sampleArticles()
	got, err := runner.Summarize(articles, []string{"AI/科技", "国际政治"})
	if err != nil {
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
		briefingPrompt + "\n\n---\n以下是今日新闻条目：\n\n" + output.GroupedArticleListView(articles, []string{"AI/科技", "国际政治"}),
	}
	if args := splitArgs(got); !reflect.DeepEqual(args, want) {
		t.Fatalf("Summarize() args = %#v, want %#v", args, want)
	}
}

func TestRunnerUsesConfiguredExtraFlagsForTranslate(t *testing.T) {
	setupFakeCLI(t, "claude")
	runner := NewRunner("claude", []string{"--model", "claude-opus-4-6"}, []string{"--bare", "--disable-slash-commands"}, true, "", "")

	articles := sampleArticles()
	got, err := runner.Translate(articles, []string{"AI/科技", "国际政治"})
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
		translatePrompt + "\n\n" + output.GroupedArticleListView(articles, []string{"AI/科技", "国际政治"}),
	}
	if args := splitArgs(got); !reflect.DeepEqual(args, want) {
		t.Fatalf("Translate() args = %#v, want %#v", args, want)
	}
}

func TestRunnerInstancesDoNotShareCommandOrProxyState(t *testing.T) {
	setupFakeCLI(t, "ccs")
	setupFakeCLI(t, "my-ai")

	ccsRunner := NewRunner("ccs", []string{"codex"}, []string{"--bare"}, true, "http://127.0.0.1:7897", "")
	otherRunner := NewRunner("my-ai", []string{"foo"}, nil, false, "", "")

	articles := sampleArticles()
	first, err := ccsRunner.Summarize(articles, []string{"AI/科技", "国际政治"})
	if err != nil {
		t.Fatalf("ccsRunner.Summarize() error = %v", err)
	}
	second, err := otherRunner.Summarize(articles, []string{"AI/科技", "国际政治"})
	if err != nil {
		t.Fatalf("otherRunner.Summarize() error = %v", err)
	}

	firstArgs := splitArgs(first)
	secondArgs := splitArgs(second)
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

func TestShouldSanitizeCLIOutputOnlyForCCSCodex(t *testing.T) {
	ResetCommandForTest()
	t.Cleanup(func() {
		ResetCommandForTest()
	})

	if !shouldSanitizeCLIOutput() {
		t.Fatalf("shouldSanitizeCLIOutput() = false, want true for default ccs codex")
	}

	SetCommand("ccs", []string{"gemini"})
	if shouldSanitizeCLIOutput() {
		t.Fatalf("shouldSanitizeCLIOutput() = true, want false for ccs gemini")
	}

	SetCommand("my-ai", []string{"codex"})
	if shouldSanitizeCLIOutput() {
		t.Fatalf("shouldSanitizeCLIOutput() = true, want false for non-ccs command")
	}
}

func TestSetProxyPreservesConfiguredCommand(t *testing.T) {
	ResetCommandForTest()
	setupFakeCLI(t, "my-ai")
	SetCommand("my-ai", []string{"foo"})
	SetProxy("http://127.0.0.1:7897", "socks5://127.0.0.1:7898")

	got, err := Summarize(sampleArticles(), []string{"AI/科技", "国际政治"})
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}

	want := []string{"foo", "--append-system-prompt", nonInteractiveBriefingSystemPrompt, "-p", briefingPrompt + "\n\n---\n以下是今日新闻条目：\n\n" + output.GroupedArticleListView(sampleArticles(), []string{"AI/科技", "国际政治"})}
	if args := splitArgs(got); !reflect.DeepEqual(args, want) {
		t.Fatalf("Summarize() args = %#v, want %#v", args, want)
	}
}

func TestDefaultRunnerConcurrentMutationDoesNotRace(t *testing.T) {
	ResetCommandForTest()
	setupFakeCLI(t, "ccs")
	setupFakeCLI(t, "my-ai")

	articles := sampleArticles()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			SetCommand("ccs", []string{"codex"})
		}()
		go func() {
			defer wg.Done()
			SetProxy("http://127.0.0.1:7897", "")
		}()
		go func() {
			defer wg.Done()
			_, _ = Summarize(articles, []string{"AI/科技", "国际政治"})
		}()
	}
	wg.Wait()
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
