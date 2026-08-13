package summarizer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/walker1211/news-briefing/internal/imageutil"
	"github.com/walker1211/news-briefing/internal/logutil"
	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/output"
)

const briefingPrompt = `你是一个国际新闻编辑。

请将以下英文新闻条目整理成中文结构化简报。只输出合法 JSON，不要输出 Markdown，不要用代码块包裹，不要输出过程说明、自我说明或额外备注。

JSON 顶层结构必须是：
{
  "overview_groups": [
    {"category": "分类名", "items": ["emoji 新闻要点"]}
  ],
  "stories": [
    {
      "category": "分类名",
      "title": "中文标题",
      "image_url": "输入 Image 字段原值或空字符串",
      "summary": "2-4句话摘要",
      "impact": "2-3句话影响分析",
      "source_article_ids": [1]
    }
  ],
  "situation": "3句话今日整体态势",
  "xhs_topics": ["话题1", "话题2", "话题3"],
  "directions": [
    {
      "title": "方向标题",
      "why": "1-2句话说明为什么值得追",
      "next": "1句话说明后续观察变量或节点",
      "deep_command": "./news-briefing deep \"关键词\" --ignore-seen"
    }
  ]
}

overview_groups 是今日速览：按分类分组，每个分类只列该分类新闻；每条 items 开头使用一个贴切 emoji，不要堆叠多个 emoji。
stories 按下面新闻条目里出现的分类顺序输出；每个分类内部按重要程度排序，相关联的新闻合并为同一 story。
summary 用2-4句话说明关键事实、背景、数字、参与方和最新进展，不要只做一句话标题复述；如果多条相关新闻合并为同一 story，要交代各条新闻之间的关系。
impact 用2-3句话说明为什么重要、影响哪些人或机构、后续观察变量，避免空泛地写“值得关注后续进展”。
source_article_ids 必须使用输入新闻条目的 1-based 编号；合并多条新闻时列出所有来源编号。
每个 story 只能引用与自身 category 完全相同的新闻条目。不要输出 source_line；来源名称和时间会由程序根据 source_article_ids 确定性生成。
image_url 只能使用输入新闻条目中的 Image 字段原值；没有可用图片时使用空字符串。不要编造、改写或重新托管图片 URL。不要使用 tracking pixel、RSS 统计图、透明占位图或与 story 主体事件无直接关系的配图。多条新闻合并为同一 story 时，如果无法明确判断哪张图直接对应主标题事件，就把 image_url 设为空字符串。AI/科技 分类下的重要 story 如果 source_article_ids 对应新闻有 Image 字段，应优先使用其中最相关且确定对应的一张。
xhs_topics 输出 3 个适合整篇简报的小红书话题，最多 4 个；每项不要带 #、空格或特殊符号，优先使用平台上容易识别的通用话题，不要直接复制冗长标题。

directions 输出 2-4 个最值得普通用户继续关注的新闻方向，不要少于 2 个，也不要多于 4 个。
如果高质量独立方向不足，允许合并相近新闻形成更上位但仍然具体的方向；不要为了凑数输出重复方向。
如果两个候选方向需要使用同一个 deep 关键词，默认应优先考虑合并，而不是拆成两个方向。
deep_command 格式为 ./news-briefing deep "关键词" --ignore-seen。
深挖命令里的关键词默认优先使用英文实体或英文新闻短语，而不是中文概括题目。
长度控制在 2-6 个词，优先包含公司名、产品名、人物名、法案/政策名、机构名等明确锚点。
避免使用纯中文概括题目，也避免只用过泛词，例如不要只写 AI、美国科技、数据中心新闻。
优先参考这种风格：Sanders AOC AI data center bill、ICE data brokers surveillance。
关键词应尽量具体且可直接用于 deep 命令。`

const nonInteractiveBriefingSystemPrompt = `这是一次无人值守的单轮批处理任务，不是对话。
你不能向用户提问，不能请求确认口径、风格或范围，不能给出 A/B 选项，不能输出“如果你愿意我可以……”之类的引导语。
如有风格歧义，默认按提示词要求的结构化中文简报数据输出，语言自然，偏自然、可直接阅读的中文研究简报风格。
只输出提示词要求的最终 JSON，不要输出任何过程说明、任务说明、自我介绍或额外备注。`

const nonInteractiveDeepDiveSystemPrompt = `这是一次无人值守的单轮批处理任务，不是对话。
你不能向用户提问，不能请求确认口径、风格或范围，不能给出 A/B 选项，不能输出“如果你愿意我可以……”之类的引导语。
只输出最终深挖正文，不要输出任何过程说明、自我介绍、提问或额外备注。`

const deepDivePrompt = `你是一个资深新闻调研员和话题研究助手。

基于以下关于「%s」的新闻素材，生成一份详细的话题深挖包：

## 事件时间线
按时间顺序列出关键事件节点。

## 各方立场
列出事件中主要各方的立场和动机。

## 关键引用
提取可以直接在文章中使用的关键引述（标注来源）。

## 研究建议
- 推荐的研究切入点
- 值得继续跟踪的关键信号
- 需要注意的敏感点
- 可以继续追踪的延伸问题`

type Runner struct {
	commandName        string
	commandArgs        []string
	defaultModel       string
	translationModel   string
	appendSystemPrompt bool
	proxyEnv           []string
	retrySleep         sleepFunc
	retryDelays        []time.Duration
	failureLogPath     string
}

type callKind string

// InvalidPromptError reports the first byte that prevents an AI prompt from
// being encoded as UTF-8. It is deterministic and should not be retried with a
// smaller article set.
type InvalidPromptError struct {
	Offset int
}

func (e *InvalidPromptError) Error() string {
	return fmt.Sprintf("ai prompt is not valid UTF-8 (invalid byte at offset %d)", e.Offset)
}

// IsInvalidPromptError reports whether err was caused by invalid prompt bytes.
func IsInvalidPromptError(err error) bool {
	var target *InvalidPromptError
	return errors.As(err, &target)
}

const (
	callKindSummarize       callKind = "summarize"
	callKindTranslate       callKind = "translate"
	callKindDeepDive        callKind = "deep"
	defaultModel                     = "gpt-5.6-sol"
	defaultTranslationModel          = "gpt-5.3-codex-spark"
)

var (
	defaultCommand     = "codex"
	defaultCommandArgs = []string{
		"exec",
		"--ignore-user-config",
		"--ephemeral",
		"--skip-git-repo-check",
		"--sandbox", "read-only",
		"--color", "never",
		"--disable", "apps",
		"--disable", "plugins",
		"--disable", "remote_plugin",
	}
	legacyClaudeCommandArgs   = []string{"--bare", "--disable-slash-commands"}
	defaultAppendSystemPrompt = true
	defaultFailureLogPath     = filepath.Join("logs", "ai-cli-failures.log")
	defaultRetryDelays        = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 9 * time.Second, 17 * time.Second}
	legacyDefaultMu           sync.RWMutex
	legacyDefaultConfig       = newDefaultRunnerConfig()
	requestIDPattern          = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
)

type runnerConfig struct {
	command            string
	args               []string
	appendSystemPrompt bool
	httpProxy          string
	socks5Proxy        string
	retryDelays        []time.Duration
	failureLogPath     string
}

func newDefaultRunnerConfig() runnerConfig {
	return runnerConfig{
		command:            defaultCommand,
		args:               cloneStrings(defaultCommandArgs),
		appendSystemPrompt: defaultAppendSystemPrompt,
		retryDelays:        cloneDurations(defaultRetryDelays),
		failureLogPath:     defaultFailureLogPath,
	}
}

func (c runnerConfig) clone() runnerConfig {
	return runnerConfig{
		command:            c.command,
		args:               cloneStrings(c.args),
		appendSystemPrompt: c.appendSystemPrompt,
		httpProxy:          c.httpProxy,
		socks5Proxy:        c.socks5Proxy,
		retryDelays:        cloneDurations(c.retryDelays),
		failureLogPath:     c.failureLogPath,
	}
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func cloneDurations(values []time.Duration) []time.Duration {
	return append([]time.Duration(nil), values...)
}

func (c runnerConfig) newRunner() *Runner {
	runner := NewRunnerWithRetryDelays(c.command, c.args, c.appendSystemPrompt, c.httpProxy, c.socks5Proxy, c.retryDelays)
	runner.failureLogPath = c.failureLogPath
	return runner
}

func (c runnerConfig) shouldSanitizeCLIOutput() bool {
	return shouldSanitizeCommand(c.command, c.args)
}

func legacyDefaultRunnerConfig() runnerConfig {
	legacyDefaultMu.RLock()
	cfg := legacyDefaultConfig.clone()
	legacyDefaultMu.RUnlock()
	return cfg
}

func legacyDefaultRunner() *Runner {
	return legacyDefaultRunnerConfig().newRunner()
}

func NewRunner(command string, args []string, appendSystemPrompt bool, httpProxy, socks5Proxy string) *Runner {
	return NewRunnerWithRetryDelays(command, args, appendSystemPrompt, httpProxy, socks5Proxy, nil)
}

func NewRunnerWithRetryDelays(command string, args []string, appendSystemPrompt bool, httpProxy, socks5Proxy string, retryDelays []time.Duration) *Runner {
	name := command
	if name == "" {
		name = defaultCommand
	}
	runnerArgs := cloneStrings(args)
	if len(runnerArgs) == 0 {
		switch {
		case isDirectCodexCommand(name):
			runnerArgs = cloneStrings(defaultCommandArgs)
		case isClaudeCommand(name):
			runnerArgs = cloneStrings(legacyClaudeCommandArgs)
		}
	}
	configuredDefaultModel := ""
	if isDirectCodexCommand(name) {
		runnerArgs, configuredDefaultModel = withoutModelArgs(runnerArgs)
	}
	if configuredDefaultModel == "" {
		configuredDefaultModel = defaultModel
	}
	if retryDelays == nil {
		retryDelays = defaultRetryDelays
	}

	var proxyEnv []string
	if httpProxy != "" {
		proxyEnv = append(proxyEnv,
			"http_proxy="+httpProxy,
			"https_proxy="+httpProxy,
			"HTTP_PROXY="+httpProxy,
			"HTTPS_PROXY="+httpProxy,
		)
	}
	if socks5Proxy != "" {
		proxyEnv = append(proxyEnv, "all_proxy="+socks5Proxy, "ALL_PROXY="+socks5Proxy)
	}

	return &Runner{
		commandName:        name,
		commandArgs:        runnerArgs,
		defaultModel:       configuredDefaultModel,
		translationModel:   defaultTranslationModel,
		appendSystemPrompt: appendSystemPrompt,
		proxyEnv:           proxyEnv,
		retrySleep:         retrySleep,
		retryDelays:        cloneDurations(retryDelays),
		failureLogPath:     defaultFailureLogPath,
	}
}

// SetModels configures task-specific models for direct Codex execution.
// Other AI commands keep receiving only their configured base arguments.
func (r *Runner) SetModels(defaultTaskModel, translationTaskModel string) {
	if model := strings.TrimSpace(defaultTaskModel); model != "" {
		r.defaultModel = model
	}
	if model := strings.TrimSpace(translationTaskModel); model != "" {
		r.translationModel = model
	}
}

func withoutModelArgs(args []string) ([]string, string) {
	filtered := make([]string, 0, len(args))
	model := ""
	for i := 0; i < len(args); i++ {
		switch {
		case (args[i] == "--model" || args[i] == "-m") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-"):
			model = args[i+1]
			i++
		case args[i] == "--model" || args[i] == "-m":
			// Drop a malformed model flag so the task-specific value below remains unique.
		case strings.HasPrefix(args[i], "--model="):
			model = strings.TrimPrefix(args[i], "--model=")
		case strings.HasPrefix(args[i], "-m="):
			model = strings.TrimPrefix(args[i], "-m=")
		default:
			filtered = append(filtered, args[i])
		}
	}
	return filtered, strings.TrimSpace(model)
}

// SetProxy 配置默认 Runner 的代理环境变量
func SetProxy(httpProxy, socks5Proxy string) {
	legacyDefaultMu.Lock()
	defer legacyDefaultMu.Unlock()

	cfg := legacyDefaultConfig.clone()
	cfg.httpProxy = httpProxy
	cfg.socks5Proxy = socks5Proxy
	legacyDefaultConfig = cfg
}

func SetCommand(command string, args []string) {
	legacyDefaultMu.Lock()
	defer legacyDefaultMu.Unlock()

	cfg := legacyDefaultConfig.clone()
	cfg.command = command
	cfg.args = cloneStrings(args)
	legacyDefaultConfig = cfg
}

func ResetCommandForTest() {
	legacyDefaultMu.Lock()
	defer legacyDefaultMu.Unlock()

	legacyDefaultConfig = newDefaultRunnerConfig()
}

func isRetryableAICLIError(err error, stdout string, stderr string) bool {
	if err == nil {
		return false
	}
	combined := strings.ToLower(strings.Join([]string{err.Error(), stdout, stderr}, "\n"))
	for _, marker := range []string{
		"server_error",
		"internal_error",
		"stream error",
		"you can retry your request",
		"status: 500",
		"status: 502",
		"status: 503",
		"status: 504",
		"context canceled",
		"timeout",
		"timed out",
		"i/o timeout",
		"connection reset",
		"lock file is already being held",
		"failed to acquire startup lock",
		"another ccs process may be starting cliproxy",
		"elocked",
		"enotacquired",
		"eof",
		"ai cli returned empty content",
	} {
		if strings.Contains(combined, marker) {
			return true
		}
	}
	return false
}

type sleepFunc func(context.Context, time.Duration) error

func retrySleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *Runner) callClaudeWithKind(kind callKind, prompt string, runtimeArgs ...string) (string, error) {
	return r.callClaudeWithKindContext(context.Background(), kind, prompt, runtimeArgs...)
}

func (r *Runner) callClaudeWithKindContext(ctx context.Context, kind callKind, prompt string, runtimeArgs ...string) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= len(r.retryDelays); attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if attempt > 0 {
			delay := r.retryDelays[attempt-1]
			if delay > 0 {
				if err := r.retrySleep(ctx, delay); err != nil {
					return "", err
				}
			}
		}
		out, stdoutText, stderrText, err := r.runClaudeCommandContext(ctx, prompt, runtimeArgs...)
		if err == nil {
			body := strings.TrimSpace(out)
			if r.shouldSanitizeCLIOutput() {
				body = sanitizeCLIOutput(out)
			}
			if body != "" {
				if attempt > 0 {
					logutil.Warnf("AI CLI retry succeeded on attempt %d", attempt+1)
				}
				return body, nil
			}
			err = fmt.Errorf("ai cli returned empty content")
			stdoutText = strings.TrimSpace(out)
		}
		lastErr = buildRetryableCallError(attempt+1, err, stdoutText, stderrText)
		if !isRetryableAICLIError(err, stdoutText, stderrText) || attempt == len(r.retryDelays) {
			finalErr := fmt.Errorf("ai cli failed after %d attempts: %w", attempt+1, lastErr)
			r.appendAICLIFailureLog(kind, attempt+1, stdoutText, stderrText, finalErr)
			return "", finalErr
		}
	}
	return "", lastErr
}

func (r *Runner) callClaude(prompt string, runtimeArgs ...string) (string, error) {
	return r.callClaudeWithKind(callKindSummarize, prompt, runtimeArgs...)
}

func (r *Runner) callClaudeContext(ctx context.Context, prompt string, runtimeArgs ...string) (string, error) {
	return r.callClaudeWithKindContext(ctx, callKindSummarize, prompt, runtimeArgs...)
}

func (r *Runner) runClaudeCommand(prompt string, runtimeArgs ...string) (string, string, string, error) {
	return r.runClaudeCommandContext(context.Background(), prompt, runtimeArgs...)
}

func (r *Runner) runClaudeCommandContext(ctx context.Context, prompt string, runtimeArgs ...string) (string, string, string, error) {
	if offset := firstInvalidUTF8Byte(prompt); offset >= 0 {
		return "", "", "", &InvalidPromptError{Offset: offset}
	}
	args := append([]string{}, r.commandArgs...)
	args = append(args, runtimeArgs...)
	directCodex := isDirectCodexCommand(r.commandName)
	if directCodex {
		args = append(args, "-")
	} else {
		args = append(args, "-p", prompt)
	}
	cmd := exec.CommandContext(ctx, r.commandName, args...)
	if directCodex {
		cmd.Stdin = strings.NewReader(prompt)
	}
	env := filterEnv(os.Environ(), "CLAUDECODE")
	env = append(env, r.proxyEnv...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
	}

	return stdout.String(), strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), nil
}

func firstInvalidUTF8Byte(text string) int {
	for offset := 0; offset < len(text); {
		r, size := utf8.DecodeRuneInString(text[offset:])
		if r == utf8.RuneError && size == 1 {
			return offset
		}
		offset += size
	}
	return -1
}

func buildRetryableCallError(attempt int, err error, stdout string, stderr string) error {
	return fmt.Errorf("attempt %d: %w\nstdout: %s\nstderr: %s", attempt, err, stdout, stderr)
}

func extractRequestID(stdout string, stderr string) string {
	combined := strings.ToLower(strings.Join([]string{stdout, stderr}, "\n"))
	return requestIDPattern.FindString(combined)
}

func (r *Runner) appendAICLIFailureLog(kind callKind, attempts int, stdout string, stderr string, err error) {
	appendAICLIFailureLog(r.failureLogPath, kind, attempts, stdout, stderr, err)
}

func appendAICLIFailureLog(path string, kind callKind, attempts int, stdout string, stderr string, err error) {
	if path == "" {
		return
	}
	if logErr := os.MkdirAll(filepath.Dir(path), 0o755); logErr != nil {
		return
	}
	entry := fmt.Sprintf(
		"time=%s kind=%s attempts=%d request_id=%s error=%q\n",
		time.Now().Format(time.RFC3339),
		kind,
		attempts,
		extractRequestID(stdout, stderr),
		err.Error(),
	)
	f, logErr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if logErr != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(entry)
}

func (r *Runner) shouldSanitizeCLIOutput() bool {
	return shouldSanitizeCommand(r.commandName, r.commandArgs)
}

func shouldSanitizeCommand(command string, args []string) bool {
	if command != "ccs" {
		return false
	}
	for _, arg := range args {
		if arg == "codex" {
			return true
		}
	}
	return false
}

func isDirectCodexCommand(command string) bool {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(command)), ".exe")
	return name == "codex"
}

func isClaudeCommand(command string) bool {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(command)), ".exe")
	return name == "claude"
}

func sanitizeCLIOutput(raw string) string {
	lines := strings.Split(raw, "\n")
	idx := 0
	for idx < len(lines) {
		line := strings.TrimSpace(lines[idx])
		if line == "" {
			idx++
			continue
		}
		if strings.HasPrefix(line, "[i] ") ||
			strings.HasPrefix(line, "[OK] ") ||
			strings.HasPrefix(line, "[warn] ") ||
			strings.Contains(line, "CLIProxy") ||
			strings.HasPrefix(line, "Run \"ccs cliproxy stop\"") {
			idx++
			continue
		}
		break
	}
	return strings.TrimSpace(strings.Join(lines[idx:], "\n"))
}

func Summarize(articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
	return SummarizeContext(context.Background(), articles, categoryOrder, loc)
}

func Translate(articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
	return TranslateContext(context.Background(), articles, categoryOrder, loc)
}

func DeepDive(topic string, articles []model.Article, loc *time.Location) (string, error) {
	return DeepDiveContext(context.Background(), topic, articles, loc)
}

func SummarizeContext(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
	return legacyDefaultRunner().SummarizeContext(ctx, articles, categoryOrder, loc)
}

func SummarizeBriefingContext(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (model.BriefingSummary, string, error) {
	return legacyDefaultRunner().SummarizeBriefingContext(ctx, articles, categoryOrder, loc)
}

func (r *Runner) Summarize(articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
	return r.SummarizeContext(context.Background(), articles, categoryOrder, loc)
}

func (r *Runner) SummarizeContext(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
	_, markdown, err := r.SummarizeBriefingContext(ctx, articles, categoryOrder, loc)
	return markdown, err
}

func (r *Runner) SummarizeBriefingContext(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (model.BriefingSummary, string, error) {
	if len(articles) == 0 {
		return model.BriefingSummary{}, "今日暂无符合筛选条件的新闻。", nil
	}

	promptArticles, promptToOriginal := orderedPromptArticles(articles, categoryOrder)
	input := output.GroupedArticleListView(promptArticles, categoryOrder, loc)
	prompt := briefingPrompt + "\n\n---\n以下是今日新闻条目：\n\n" + input

	raw, err := r.callClaudeContext(ctx, prompt, r.summarizeRuntimeArgs()...)
	if err != nil {
		return model.BriefingSummary{}, "", err
	}
	structured, err := parseBriefingSummaryJSON(raw)
	if err != nil {
		return model.BriefingSummary{}, "", fmt.Errorf("parse structured briefing: %w", err)
	}
	structured, err = validateAndNormalizeBriefingSummaryReferences(structured, promptArticles, loc)
	if err != nil {
		return model.BriefingSummary{}, "", fmt.Errorf("validate structured briefing references: %w", err)
	}
	structured = validateBriefingSummaryImages(structured, promptArticles)
	structured = remapBriefingSummarySourceArticleIDs(structured, promptToOriginal)
	return structured, output.StructuredBriefingMarkdown(structured, categoryOrder), nil
}

// orderedPromptArticles preserves the exact category order shown to the model
// and records where every prompt item came from in the caller's article slice.
// The model returns 1-based prompt indexes; persisted briefing metadata must use
// the caller's ordering because downstream sidecars receive that original slice.
func orderedPromptArticles(articles []model.Article, categoryOrder []string) ([]model.Article, []int) {
	ordered := make([]model.Article, 0, len(articles))
	originalIndexes := make([]int, 0, len(articles))
	for _, category := range output.OrderedCategories(articles, categoryOrder) {
		for index, article := range articles {
			if article.Category != category {
				continue
			}
			ordered = append(ordered, article)
			originalIndexes = append(originalIndexes, index)
		}
	}
	return ordered, originalIndexes
}

func remapBriefingSummarySourceArticleIDs(summary model.BriefingSummary, promptToOriginal []int) model.BriefingSummary {
	for index := range summary.Stories {
		ids := summary.Stories[index].SourceArticleIDs
		remapped := make([]int, 0, len(ids))
		for _, id := range ids {
			promptIndex := id - 1
			if promptIndex < 0 || promptIndex >= len(promptToOriginal) {
				continue
			}
			remapped = append(remapped, promptToOriginal[promptIndex]+1)
		}
		summary.Stories[index].SourceArticleIDs = remapped
	}
	return summary
}

func validateAndNormalizeBriefingSummaryReferences(summary model.BriefingSummary, articles []model.Article, loc *time.Location) (model.BriefingSummary, error) {
	if loc == nil {
		loc = time.Local
	}
	for index := range summary.Stories {
		story := &summary.Stories[index]
		story.Category = strings.TrimSpace(story.Category)
		if story.Category == "" {
			return model.BriefingSummary{}, fmt.Errorf("story %d has an empty category", index+1)
		}
		if len(story.SourceArticleIDs) == 0 {
			return model.BriefingSummary{}, fmt.Errorf("story %d (%s) has no source_article_ids", index+1, story.Title)
		}

		seen := make(map[int]struct{}, len(story.SourceArticleIDs))
		normalizedIDs := make([]int, 0, len(story.SourceArticleIDs))
		sources := make([]model.Article, 0, len(story.SourceArticleIDs))
		for _, id := range story.SourceArticleIDs {
			articleIndex := id - 1
			if articleIndex < 0 || articleIndex >= len(articles) {
				return model.BriefingSummary{}, fmt.Errorf("story %d (%s) references article %d outside 1..%d", index+1, story.Title, id, len(articles))
			}
			if _, ok := seen[id]; ok {
				continue
			}
			article := articles[articleIndex]
			articleCategory := strings.TrimSpace(article.Category)
			if articleCategory != story.Category {
				return model.BriefingSummary{}, fmt.Errorf("story %d (%s) category %q references article %d from category %q", index+1, story.Title, story.Category, id, articleCategory)
			}
			seen[id] = struct{}{}
			normalizedIDs = append(normalizedIDs, id)
			sources = append(sources, article)
		}
		story.SourceArticleIDs = normalizedIDs
		story.SourceLine = deterministicBriefingSourceLine(sources, loc)
	}
	return summary, nil
}

func deterministicBriefingSourceLine(articles []model.Article, loc *time.Location) string {
	sourceNames := make([]string, 0, len(articles))
	seenSources := make(map[string]struct{}, len(articles))
	var earliest time.Time
	var latest time.Time
	for _, article := range articles {
		source := strings.TrimSpace(article.Source)
		if source != "" {
			if _, ok := seenSources[source]; !ok {
				seenSources[source] = struct{}{}
				sourceNames = append(sourceNames, source)
			}
		}
		if article.Published.IsZero() {
			continue
		}
		published := article.Published.In(loc)
		if earliest.IsZero() || published.Before(earliest) {
			earliest = published
		}
		if latest.IsZero() || published.After(latest) {
			latest = published
		}
	}
	if len(sourceNames) == 0 {
		sourceNames = append(sourceNames, "未知来源")
	}
	line := "来源: " + strings.Join(sourceNames, "、")
	if earliest.IsZero() {
		return line
	}
	if earliest.Equal(latest) {
		return line + " | " + earliest.Format("2006-01-02 15:04")
	}
	return line + " | " + earliest.Format("2006-01-02 15:04") + " 至 " + latest.Format("2006-01-02 15:04")
}

func parseBriefingSummaryJSON(raw string) (model.BriefingSummary, error) {
	var summary model.BriefingSummary
	body := stripJSONCodeFence(strings.TrimSpace(raw))
	if body == "" {
		return summary, fmt.Errorf("empty response")
	}
	if err := json.Unmarshal([]byte(body), &summary); err != nil {
		return summary, err
	}
	return summary, nil
}

func stripJSONCodeFence(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "```") {
		return value
	}
	lines := strings.Split(value, "\n")
	if len(lines) < 2 {
		return value
	}
	first := strings.TrimSpace(lines[0])
	last := strings.TrimSpace(lines[len(lines)-1])
	if first != "```" && first != "```json" && first != "```JSON" {
		return value
	}
	if last != "```" {
		return value
	}
	return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
}

func validateBriefingSummaryImages(summary model.BriefingSummary, articles []model.Article) model.BriefingSummary {
	allowed := make(map[string]struct{})
	for _, article := range articles {
		if image := usableArticleImage(article); image != "" {
			allowed[image] = struct{}{}
		}
	}
	for i := range summary.Stories {
		story := &summary.Stories[i]
		sourceImage, sourceImageOK := singleSourceArticleImage(story.SourceArticleIDs, articles)
		image := strings.TrimSpace(story.ImageURL)
		if image != "" {
			_, allowedImage := allowed[image]
			if allowedImage && sourceArticleIDsContainImage(story.SourceArticleIDs, articles, image) {
				story.ImageURL = image
				continue
			}
		}
		if sourceImageOK {
			story.ImageURL = sourceImage
		} else {
			story.ImageURL = ""
		}
	}
	return summary
}

func sourceArticleIDsContainImage(ids []int, articles []model.Article, image string) bool {
	if len(ids) == 0 {
		return true
	}
	for _, id := range ids {
		idx := id - 1
		if idx < 0 || idx >= len(articles) {
			continue
		}
		if usableArticleImage(articles[idx]) == image {
			return true
		}
	}
	return false
}

func singleSourceArticleImage(ids []int, articles []model.Article) (string, bool) {
	var selected string
	for _, id := range ids {
		idx := id - 1
		if idx < 0 || idx >= len(articles) {
			continue
		}
		image := usableArticleImage(articles[idx])
		if image == "" || image == selected {
			continue
		}
		if selected != "" {
			return "", false
		}
		selected = image
	}
	return selected, true
}

func usableArticleImage(article model.Article) string {
	image := strings.TrimSpace(article.ImageURL)
	if image == "" || !imageutil.IsUsableRemoteImageURL(image) {
		return ""
	}
	return image
}

func (r *Runner) summarizeRuntimeArgs() []string {
	return r.taskRuntimeArgs(r.defaultModel, nonInteractiveBriefingSystemPrompt)
}

func shouldSanitizeCLIOutput() bool {
	return legacyDefaultRunnerConfig().shouldSanitizeCLIOutput()
}

func DeepDiveContext(ctx context.Context, topic string, articles []model.Article, loc *time.Location) (string, error) {
	return legacyDefaultRunner().DeepDiveContext(ctx, topic, articles, loc)
}

func (r *Runner) DeepDive(topic string, articles []model.Article, loc *time.Location) (string, error) {
	return r.DeepDiveContext(context.Background(), topic, articles, loc)
}

func (r *Runner) DeepDiveContext(ctx context.Context, topic string, articles []model.Article, loc *time.Location) (string, error) {
	input := output.ArticleListView(articles, loc)
	prompt := fmt.Sprintf(deepDivePrompt, topic) + "\n\n---\n话题: " + topic + "\n\n相关新闻:\n" + input

	return r.callClaudeWithKindContext(ctx, callKindDeepDive, prompt, r.deepDiveRuntimeArgs()...)
}

func (r *Runner) deepDiveRuntimeArgs() []string {
	return r.taskRuntimeArgs(r.defaultModel, nonInteractiveDeepDiveSystemPrompt)
}

const translatePrompt = `将以下新闻列表翻译成中文。要求：
1. 按分类分组输出，格式为 "== 分类名 ==" 作为标题
2. 每条新闻保持编号，只翻译标题和摘要，保留来源名称、时间和链接不变
3. 直接输出翻译结果，不要加任何额外说明`

func TranslateContext(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
	return legacyDefaultRunner().TranslateContext(ctx, articles, categoryOrder, loc)
}

func (r *Runner) Translate(articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
	return r.TranslateContext(context.Background(), articles, categoryOrder, loc)
}

func (r *Runner) TranslateContext(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
	if len(articles) == 0 {
		return "暂无新闻。", nil
	}
	input := output.GroupedArticleListView(articles, categoryOrder, loc)
	prompt := translatePrompt + "\n\n" + input
	return r.callClaudeWithKindContext(ctx, callKindTranslate, prompt, r.translateRuntimeArgs()...)
}

func (r *Runner) translateRuntimeArgs() []string {
	return r.taskRuntimeArgs(r.translationModel, nonInteractiveBriefingSystemPrompt)
}

func (r *Runner) taskRuntimeArgs(model, systemPrompt string) []string {
	var args []string
	if isDirectCodexCommand(r.commandName) && strings.TrimSpace(model) != "" {
		args = append(args, "--model", strings.TrimSpace(model))
	}
	return append(args, r.systemPromptRuntimeArgs(systemPrompt)...)
}

func (r *Runner) systemPromptRuntimeArgs(systemPrompt string) []string {
	if !r.appendSystemPrompt {
		return nil
	}
	if isDirectCodexCommand(r.commandName) {
		return []string{"-c", "developer_instructions=" + strconv.Quote(systemPrompt)}
	}
	return []string{"--append-system-prompt", systemPrompt}
}

func filterEnv(env []string, exclude string) []string {
	prefix := exclude + "="
	var result []string
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			result = append(result, e)
		}
	}
	return result
}
