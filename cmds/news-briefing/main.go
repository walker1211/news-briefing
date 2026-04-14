package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/output"
)

// isTTY 检测 stdout 是否为终端（非终端时不输出颜色码）
var isTTY bool

func init() {
	fi, _ := os.Stdout.Stat()
	isTTY = (fi.Mode() & os.ModeCharDevice) != 0
}

// color 在终端模式下返回 ANSI 颜色码，非终端返回空字符串
func color(code string) string {
	if isTTY {
		return code
	}
	return ""
}

const (
	defaultConfigPath        = "configs/config.yaml"
	defaultConfigExamplePath = "configs/config.example.yaml"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprint(os.Stderr, usageErrorText(err))
		os.Exit(1)
	}
	if _, ok := cmd.(helpCommand); ok {
		printUsage()
		return
	}

	cfg, err := loadDefaultConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	fetcher.InitHTTPClient(cfg.Proxy)

	if err := execute(newApp(cfg), cmd); err != nil {
		exitWithError(err)
	}
}

func loadDefaultConfig() (*config.Config, error) {
	if _, err := os.Stat(defaultConfigPath); err == nil {
		cfg, err := config.Load(defaultConfigPath)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", defaultConfigPath, err)
		}
		return cfg, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat %s: %w", defaultConfigPath, err)
	}

	return nil, fmt.Errorf("missing %s. Copy %s to %s and fill in your real values", defaultConfigPath, defaultConfigExamplePath, defaultConfigPath)
}

func printUsage() {
	fmt.Println(usageText())
}

func usageErrorText(err error) string {
	return fmt.Sprintf("Error: %v\n\n%s\n", err, usageText())
}

func exitWithError(err error) {
	fmt.Fprintf(os.Stderr, "%v\n", err)
	os.Exit(1)
}

func usageText() string {
	return `国际资讯聚合器

Commands:
  news-briefing run [flags]       生成简报（摘要 + 邮件 + MD文件）
  news-briefing regen [flags]     按指定时间窗重生成简报
  news-briefing fetch [--zh]      仅抓取新闻，显示原始文章列表（--zh 翻译成中文）
  news-briefing serve             守护模式，按 configs/config.yaml 中 schedule 配置自动执行
  news-briefing deep <topic>      深挖某话题，生成话题深挖包
  news-briefing resend-md --file <path>  按已有 Markdown 重发邮件
  news-briefing help              显示此帮助

Note:
  可执行文件名为 news-briefing；子命令包括 run / regen / fetch / serve / deep / resend-md / help

Flags (for run):
  --raw                  同时显示原始文章列表
  --no-email             跳过邮件发送

Flags (for regen):
  --from "YYYY-MM-DD HH:MM"   开始时间（按 schedule_timezone 解析，未配置时使用系统本地时区）
  --to "YYYY-MM-DD HH:MM"     结束时间（按 schedule_timezone 解析，未配置时使用系统本地时区）
  --period HHMM               可选，默认取 --to 的 HHMM
  --ignore-seen               跳过已读状态文件（默认 <output.dir>/state/seen.json），仅做本批次内去重
  --send-email                发送邮件
  --raw                       同时显示原始文章列表
  默认不发邮件，默认仍会写出 Markdown 文件

Flags (for fetch):
  --zh                   翻译成中文（调用已配置 AI CLI）

Flags (for deep):
  --from "YYYY-MM-DD HH:MM"   可选开始时间（按 schedule_timezone 解析，未配置时使用系统本地时区）
  --to "YYYY-MM-DD HH:MM"     可选结束时间（按 schedule_timezone 解析，未配置时使用系统本地时区）
  --ignore-seen                跳过已读状态文件（默认 <output.dir>/state/seen.json），仅做本批次内去重
  --send-email                 发送邮件
  --from / --to 要么都不传，要么一起传；且 --to 必须晚于或等于 --from
  默认读取未读池；若仅传 --ignore-seen，则使用最近 12 小时窗口

Examples:
  news-briefing run
  news-briefing run --raw
  news-briefing run --no-email
  news-briefing regen --from "2026-03-18 08:00" --to "2026-03-18 14:00"
  news-briefing regen --from "2026-03-18 08:00" --to "2026-03-18 14:00" --period 1400 --ignore-seen --send-email
  news-briefing fetch
  news-briefing deep "OpenAI"
  news-briefing deep "Claude" --send-email
  news-briefing deep "Claude" --ignore-seen
  news-briefing deep "Claude" --from "2026-03-28 00:00" --to "2026-03-29 23:59"
  news-briefing deep "Claude" --from "2026-03-28 00:00" --to "2026-03-29 23:59" --ignore-seen
  news-briefing resend-md --file output/26.04.13-晚间-1800.md`
}

func currentPeriod() string {
	now := time.Now()
	return fmt.Sprintf("%02d%02d", now.Hour(), now.Minute())
}

func defaultPeriodFrom(t time.Time) string {
	return t.Format("1504")
}

func defaultPeriodFromRaw(value string) string {
	if len(value) >= 5 {
		return strings.ReplaceAll(value[len(value)-5:], ":", "")
	}
	return ""
}

func parseRegenTime(value string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.Local
	}
	return time.ParseInLocation("2006-01-02 15:04", value, loc)
}

func validatePeriod(period string) error {
	if len(period) != 4 {
		return fmt.Errorf("period must be 4 digits in HHMM format")
	}
	for _, ch := range period {
		if ch < '0' || ch > '9' {
			return fmt.Errorf("period must be 4 digits in HHMM format")
		}
	}
	hour := period[:2]
	minute := period[2:]
	if hour < "00" || hour > "23" || minute < "00" || minute > "59" {
		return fmt.Errorf("period must be a valid HHMM time")
	}
	return nil
}

func formatArticlePublishedAt(published time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	return published.In(loc).Format("2006-01-02 15:04")
}

func printArticles(articles []model.Article, categoryOrder []string, loc *time.Location) {
	grouped := groupByCategory(articles)
	n := 1
	for _, cat := range output.OrderedCategories(articles, categoryOrder) {
		items := grouped[cat]
		if len(items) == 0 {
			continue
		}
		fmt.Printf("%s%s== %s (%d篇) ==%s\n\n", color("\033[1m"), color("\033[32m"), cat, len(items), color("\033[0m"))
		for _, a := range items {
			fmt.Printf("%s%d. %s%s\n", color("\033[33m"), n, a.Title, color("\033[0m"))
			fmt.Printf("   %s\n", a.Summary)
			fmt.Printf("   %sSource: %s | %s%s\n", color("\033[36m"), a.Source, formatArticlePublishedAt(a.Published, loc), color("\033[0m"))
			fmt.Printf("   %s\n\n", a.Link)
			n++
		}
	}
}

func groupByCategory(articles []model.Article) map[string][]model.Article {
	m := make(map[string][]model.Article)
	for _, a := range articles {
		m[a.Category] = append(m[a.Category], a)
	}
	return m
}
