package summarizer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/output"
)

const briefingPrompt = `你是一个国际新闻编辑。

请将以下英文新闻条目整理成中文简报。按下面新闻条目里出现的分类顺序输出，每个分类使用 ## 分类名 作为标题。

每条新闻格式：
### 标题（中文翻译）
**摘要：** 用1-2句话概括核心事实
**影响：** 用1句话说明对行业/世界的影响
> 来源: [来源名] | [时间]

按重要程度排序，相关联的新闻合并为同一话题。

然后输出：

---
## 今日态势
用3句话总结今日整体态势。

---
## 今日最值得追的方向

输出 2-4 个最值得普通用户继续关注的新闻方向，每个包含：
### 方向N：[方向标题]
**为什么值得追：** 用1-2句话说明这个话题为什么重要，为什么值得继续跟踪
**接下来关注什么：** 用1句话说明后续最值得观察的变量或节点
**深挖命令：** 提供一条可直接复制执行的命令，格式为 ./news-briefing deep "关键词" --ignore-seen

不要少于 2 个，也不要多于 4 个。
如果高质量独立方向不足，允许合并相近新闻形成更上位但仍然具体的方向；不要为了凑数输出重复方向。
如果两个候选方向需要使用同一个 deep 关键词，默认应优先考虑合并，而不是拆成两个方向。
深挖命令里的关键词默认优先使用英文实体或英文新闻短语，而不是中文概括题目。
长度控制在 2-6 个词，优先包含公司名、产品名、人物名、法案/政策名、机构名等明确锚点。
避免使用纯中文概括题目，也避免只用过泛词，例如不要只写 AI、美国科技、数据中心新闻。
优先参考这种风格：Sanders AOC AI data center bill、ICE data brokers surveillance。
关键词应尽量具体且可直接用于 deep 命令。

这是单轮直接生成任务，不是对话。不要提出问题，不要请求确认口径或风格，不要输出过程说明、自我说明或额外备注，只输出最终简报正文。`

const nonInteractiveBriefingSystemPrompt = `这是一次无人值守的单轮批处理任务，不是对话。
你不能向用户提问，不能请求确认口径、风格或范围，不能给出 A/B 选项，不能输出“如果你愿意我可以……”之类的引导语。
如有风格歧义，默认按可直接发送给读者的中文成稿简报输出，语言自然，偏自然、可直接阅读的中文研究简报风格。
只输出最终简报正文，不要输出任何过程说明、任务说明、自我介绍或额外备注。`

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
	extraFlags         []string
	appendSystemPrompt bool
	proxyEnv           []string
	retrySleep         sleepFunc
}

type callKind string

const (
	callKindSummarize callKind = "summarize"
	callKindTranslate callKind = "translate"
	callKindDeepDive  callKind = "deep"
)

var (
	defaultCommand            = "ccs"
	defaultCommandArgs        = []string{"codex"}
	defaultExtraFlags         []string
	defaultAppendSystemPrompt = true
	defaultRunnerMu           sync.RWMutex
	defaultRunner             = NewRunner(defaultCommand, defaultCommandArgs, defaultExtraFlags, defaultAppendSystemPrompt, "", "")
	defaultHTTPProxy          string
	defaultSocks5Proxy        string
	aiCLIFailureLogPath       = filepath.Join("logs", "ai-cli-failures.log")
	requestIDPattern          = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
)

func NewRunner(command string, args []string, extraFlags []string, appendSystemPrompt bool, httpProxy, socks5Proxy string) *Runner {
	name := command
	if name == "" {
		name = defaultCommand
	}
	runnerArgs := append([]string(nil), args...)
	if len(runnerArgs) == 0 {
		runnerArgs = append([]string(nil), defaultCommandArgs...)
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
		extraFlags:         append([]string(nil), extraFlags...),
		appendSystemPrompt: appendSystemPrompt,
		proxyEnv:           proxyEnv,
		retrySleep:         retrySleep,
	}
}

// SetProxy 配置默认 Runner 的代理环境变量
func SetProxy(httpProxy, socks5Proxy string) {
	defaultRunnerMu.Lock()
	defer defaultRunnerMu.Unlock()

	defaultHTTPProxy = httpProxy
	defaultSocks5Proxy = socks5Proxy
	defaultRunner = NewRunner(defaultRunner.commandName, defaultRunner.commandArgs, defaultRunner.extraFlags, defaultRunner.appendSystemPrompt, defaultHTTPProxy, defaultSocks5Proxy)
}

func SetCommand(command string, args []string) {
	defaultRunnerMu.Lock()
	defer defaultRunnerMu.Unlock()

	defaultRunner = NewRunner(command, args, defaultRunner.extraFlags, defaultRunner.appendSystemPrompt, defaultHTTPProxy, defaultSocks5Proxy)
}

func ResetCommandForTest() {
	defaultRunnerMu.Lock()
	defer defaultRunnerMu.Unlock()

	defaultHTTPProxy = ""
	defaultSocks5Proxy = ""
	defaultRunner = NewRunner(defaultCommand, defaultCommandArgs, defaultExtraFlags, defaultAppendSystemPrompt, "", "")
}

func isRetryableAICLIError(err error, stdout string, stderr string) bool {
	if err == nil {
		return false
	}
	combined := strings.ToLower(strings.Join([]string{err.Error(), stdout, stderr}, "\n"))
	for _, marker := range []string{
		"server_error",
		"status: 500",
		"status: 502",
		"status: 503",
		"status: 504",
		"context canceled",
		"timeout",
		"i/o timeout",
		"connection reset",
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

func (r *Runner) callClaudeWithKind(kind callKind, prompt string, extraFlags ...string) (string, error) {
	return r.callClaudeWithKindContext(context.Background(), kind, prompt, extraFlags...)
}

func (r *Runner) callClaudeWithKindContext(ctx context.Context, kind callKind, prompt string, extraFlags ...string) (string, error) {
	attemptDelays := []time.Duration{0, time.Second, 3 * time.Second}
	var lastErr error
	for attempt, delay := range attemptDelays {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if delay > 0 {
			if err := r.retrySleep(ctx, delay); err != nil {
				return "", err
			}
		}
		out, stdoutText, stderrText, err := r.runClaudeCommandContext(ctx, prompt, extraFlags...)
		if err == nil {
			body := strings.TrimSpace(out)
			if r.shouldSanitizeCLIOutput() {
				body = sanitizeCLIOutput(out)
			}
			if body != "" {
				if attempt > 0 {
					fmt.Fprintf(os.Stderr, "AI CLI retry succeeded on attempt %d\n", attempt+1)
				}
				return body, nil
			}
			err = fmt.Errorf("ai cli returned empty content")
			stdoutText = strings.TrimSpace(out)
		}
		lastErr = buildRetryableCallError(attempt+1, err, stdoutText, stderrText)
		if !isRetryableAICLIError(err, stdoutText, stderrText) || attempt == len(attemptDelays)-1 {
			finalErr := fmt.Errorf("ai cli failed after %d attempts: %w", attempt+1, lastErr)
			appendAICLIFailureLog(kind, attempt+1, stdoutText, stderrText, finalErr)
			return "", finalErr
		}
	}
	return "", lastErr
}

func (r *Runner) callClaude(prompt string, extraFlags ...string) (string, error) {
	return r.callClaudeWithKind(callKindSummarize, prompt, extraFlags...)
}

func (r *Runner) callClaudeContext(ctx context.Context, prompt string, extraFlags ...string) (string, error) {
	return r.callClaudeWithKindContext(ctx, callKindSummarize, prompt, extraFlags...)
}

func (r *Runner) runClaudeCommand(prompt string, extraFlags ...string) (string, string, string, error) {
	return r.runClaudeCommandContext(context.Background(), prompt, extraFlags...)
}

func (r *Runner) runClaudeCommandContext(ctx context.Context, prompt string, extraFlags ...string) (string, string, string, error) {
	args := append([]string{}, r.commandArgs...)
	args = append(args, extraFlags...)
	args = append(args, "-p", prompt)
	cmd := exec.CommandContext(ctx, r.commandName, args...)
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

func buildRetryableCallError(attempt int, err error, stdout string, stderr string) error {
	return fmt.Errorf("attempt %d: %w\nstdout: %s\nstderr: %s", attempt, err, stdout, stderr)
}

func extractRequestID(stdout string, stderr string) string {
	combined := strings.ToLower(strings.Join([]string{stdout, stderr}, "\n"))
	return requestIDPattern.FindString(combined)
}

func appendAICLIFailureLog(kind callKind, attempts int, stdout string, stderr string, err error) {
	if aiCLIFailureLogPath == "" {
		return
	}
	if logErr := os.MkdirAll(filepath.Dir(aiCLIFailureLogPath), 0o755); logErr != nil {
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
	f, logErr := os.OpenFile(aiCLIFailureLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if logErr != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(entry)
}

func (r *Runner) shouldSanitizeCLIOutput() bool {
	if r.commandName != "ccs" {
		return false
	}
	for _, arg := range r.commandArgs {
		if arg == "codex" {
			return true
		}
	}
	return false
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
	defaultRunnerMu.RLock()
	runner := defaultRunner
	defaultRunnerMu.RUnlock()
	return runner.SummarizeContext(ctx, articles, categoryOrder, loc)
}

func (r *Runner) Summarize(articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
	return r.SummarizeContext(context.Background(), articles, categoryOrder, loc)
}

func (r *Runner) SummarizeContext(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
	if len(articles) == 0 {
		return "今日暂无符合筛选条件的新闻。", nil
	}

	input := output.GroupedArticleListView(articles, categoryOrder, loc)
	prompt := briefingPrompt + "\n\n---\n以下是今日新闻条目：\n\n" + input

	return r.callClaudeContext(ctx, prompt, r.summarizeExtraFlags()...)
}

func (r *Runner) summarizeExtraFlags() []string {
	flags := append([]string(nil), r.extraFlags...)
	if r.appendSystemPrompt {
		flags = append(flags, "--append-system-prompt", nonInteractiveBriefingSystemPrompt)
	}
	return flags
}

func shouldSanitizeCLIOutput() bool {
	defaultRunnerMu.RLock()
	runner := defaultRunner
	defaultRunnerMu.RUnlock()
	return runner.shouldSanitizeCLIOutput()
}

func DeepDiveContext(ctx context.Context, topic string, articles []model.Article, loc *time.Location) (string, error) {
	defaultRunnerMu.RLock()
	runner := defaultRunner
	defaultRunnerMu.RUnlock()
	return runner.DeepDiveContext(ctx, topic, articles, loc)
}

func (r *Runner) DeepDive(topic string, articles []model.Article, loc *time.Location) (string, error) {
	return r.DeepDiveContext(context.Background(), topic, articles, loc)
}

func (r *Runner) DeepDiveContext(ctx context.Context, topic string, articles []model.Article, loc *time.Location) (string, error) {
	input := output.ArticleListView(articles, loc)
	prompt := fmt.Sprintf(deepDivePrompt, topic) + "\n\n---\n话题: " + topic + "\n\n相关新闻:\n" + input

	return r.callClaudeWithKindContext(ctx, callKindDeepDive, prompt, r.deepDiveExtraFlags()...)
}

func (r *Runner) deepDiveExtraFlags() []string {
	flags := append([]string(nil), r.extraFlags...)
	if r.appendSystemPrompt {
		flags = append(flags, "--append-system-prompt", nonInteractiveDeepDiveSystemPrompt)
	}
	return flags
}

const translatePrompt = `将以下新闻列表翻译成中文。要求：
1. 按分类分组输出，格式为 "== 分类名 ==" 作为标题
2. 每条新闻保持编号，只翻译标题和摘要，保留来源名称、时间和链接不变
3. 直接输出翻译结果，不要加任何额外说明`

func TranslateContext(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
	defaultRunnerMu.RLock()
	runner := defaultRunner
	defaultRunnerMu.RUnlock()
	return runner.TranslateContext(ctx, articles, categoryOrder, loc)
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
	return r.callClaudeWithKindContext(ctx, callKindTranslate, prompt, r.translateExtraFlags()...)
}

func (r *Runner) translateExtraFlags() []string {
	flags := append([]string(nil), r.extraFlags...)
	if r.appendSystemPrompt {
		flags = append(flags, "--append-system-prompt", nonInteractiveBriefingSystemPrompt)
	}
	return flags
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
