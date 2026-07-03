package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/logutil"
	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/output"
	"github.com/walker1211/news-briefing/internal/scheduler"
	"github.com/walker1211/news-briefing/internal/summarizer"
	"github.com/walker1211/news-briefing/internal/watch"
)

type app struct {
	cfg                 *config.Config
	now                 func() time.Time
	scheduler           schedulerDeps
	fetch               fetchDeps
	watch               watchDeps
	ai                  aiDeps
	output              outputDeps
	email               emailDeps
	publishHook         func(context.Context, config.PublishHookConfig, publishHookRequest) error
	suppressPublishHook bool
}

type schedulerDeps struct {
	startCron          func(*config.Config, func(scheduler.Window)) error
	startCronContext   func(context.Context, *config.Config, func(scheduler.Window)) error
	waitForever        func()
	waitForeverContext func(context.Context)
}

type fetchDeps struct {
	fetchAll                          func(*config.Config, bool) ([]model.Article, []fetcher.FailedSource, error)
	fetchAllContext                   func(context.Context, *config.Config, bool) ([]model.Article, []fetcher.FailedSource, error)
	fetchAllDetailedContext           func(context.Context, *config.Config, bool) (fetcher.FetchResult, error)
	fetchWindow                       func(*config.Config, time.Time, time.Time, bool, bool) ([]model.Article, []fetcher.FailedSource, error)
	fetchWindowContext                func(context.Context, *config.Config, time.Time, time.Time, bool, bool) ([]model.Article, []fetcher.FailedSource, error)
	fetchWindowDetailedContext        func(context.Context, *config.Config, time.Time, time.Time, bool, bool) (fetcher.FetchResult, error)
	fetchWindowDetailedHistoryContext func(context.Context, *config.Config, time.Time, time.Time, bool, bool, string) (fetcher.FetchResult, error)
	fetchXAlertsContext               func(context.Context, *config.Config, time.Time) (fetcher.FetchResult, error)
	markSeen                          func([]model.Article) error
}

type watchDeps struct {
	fetchWatch        func(*config.Config, time.Time) ([]model.Article, *model.WatchReport, error)
	fetchWatchContext func(context.Context, *config.Config, time.Time) ([]model.Article, *model.WatchReport, error)
}

type aiDeps struct {
	summarize                func([]model.Article, []string, *time.Location) (string, error)
	summarizeContext         func(context.Context, []model.Article, []string, *time.Location) (string, error)
	summarizeBriefingContext func(context.Context, []model.Article, []string, *time.Location) (model.BriefingSummary, string, error)
	translate                func([]model.Article, []string, *time.Location) (string, error)
	translateContext         func(context.Context, []model.Article, []string, *time.Location) (string, error)
	deepDive                 func(string, []model.Article, *time.Location) (string, error)
	deepDiveContext          func(context.Context, string, []model.Article, *time.Location) (string, error)
}

type outputDeps struct {
	composeBody        func(string, model.OutputMode, model.OutputContent) (string, error)
	printText          func(string)
	printFailed        func([]fetcher.FailedSource)
	printArticles      func([]model.Article)
	printCLI           func(*model.Briefing)
	writeMarkdown      func(*model.Briefing, string) (string, error)
	writeWatchMarkdown func(*model.WatchReport, string, string, string) (string, error)
	writeDeepDive      func(string, string, string, string) (string, error)
}

type emailDeps struct {
	sendEmail           func(*model.Briefing, *config.Config, []fetcher.FailedSource) error
	sendDeepEmail       func(string, *model.Briefing, *config.Config, []fetcher.FailedSource) error
	resendMarkdownEmail func(string, *config.Config) error
	sendAlertEmail      func(string, string, *config.Config) error
}

func newApp(cfg *config.Config) *app {
	httpClient := fetcher.NewHTTPClient(cfg.Proxy, cfg.Fetch.Timeout)
	fetchClient := fetcher.NewClient(httpClient)
	watchRunner := watch.NewRunner(httpClient)
	aiRunner := summarizer.NewRunnerWithRetryDelays(cfg.AI.Command, cfg.AI.Args, cfg.AI.ExtraFlags, cfg.AI.ShouldAppendSystemPrompt(), cfg.Proxy.HTTP, cfg.Proxy.Socks5, cfg.AI.Retry.Delays)
	emailSender := output.NewEmailSender()
	return &app{
		cfg: cfg,
		now: time.Now,
		scheduler: schedulerDeps{
			startCron:        scheduler.Start,
			startCronContext: scheduler.StartContext,
			waitForever: func() {
				select {}
			},
			waitForeverContext: func(ctx context.Context) {
				<-ctx.Done()
			},
		},
		fetch: fetchDeps{
			fetchAll:                          fetchClient.FetchAll,
			fetchAllContext:                   fetchClient.FetchAllContext,
			fetchAllDetailedContext:           fetchClient.FetchAllDetailedContext,
			fetchWindow:                       fetchClient.FetchWindow,
			fetchWindowContext:                fetchClient.FetchWindowContext,
			fetchWindowDetailedContext:        fetchClient.FetchWindowDetailedContext,
			fetchWindowDetailedHistoryContext: fetchClient.FetchWindowDetailedWithXVisibleHistoryContext,
			fetchXAlertsContext:               fetcher.FetchXAlertsContext,
			markSeen: func(articles []model.Article) error {
				return fetcher.MarkArticlesSeen(cfg.Output.Dir, articles)
			},
		},
		watch: watchDeps{
			fetchWatch:        watchRunner.Run,
			fetchWatchContext: watchRunner.RunContext,
		},
		ai: aiDeps{
			summarize:                aiRunner.Summarize,
			summarizeContext:         aiRunner.SummarizeContext,
			summarizeBriefingContext: aiRunner.SummarizeBriefingContext,
			translate:                aiRunner.Translate,
			translateContext:         aiRunner.TranslateContext,
			deepDive:                 aiRunner.DeepDive,
			deepDiveContext:          aiRunner.DeepDiveContext,
		},
		output: outputDeps{
			composeBody: output.FormatBody,
			printText:   defaultPrintText,
			printFailed: fetcher.PrintFailed,
			printArticles: func(articles []model.Article) {
				loc := cfg.ScheduleLocation
				if loc == nil {
					loc = time.Local
				}
				printArticles(articles, categoryOrderFromSources(cfg.Sources), loc)
			},
			printCLI:           output.PrintCLI,
			writeMarkdown:      output.WriteMarkdown,
			writeWatchMarkdown: output.WriteWatchMarkdown,
			writeDeepDive:      output.WriteDeepDive,
		},
		email: emailDeps{
			sendEmail:           emailSender.SendEmail,
			sendDeepEmail:       emailSender.SendDeepEmail,
			resendMarkdownEmail: emailSender.SendMarkdownFile,
			sendAlertEmail:      emailSender.SendAlertEmail,
		},
		publishHook: runPublishHook,
	}
}

func defaultPrintText(s string) {
	fmt.Println(s)
}

func (app *app) ensureTextOutputDeps() {
	if app.output.composeBody == nil {
		app.output.composeBody = output.FormatBody
	}
	if app.output.printText == nil {
		app.output.printText = defaultPrintText
	}
}

func (app *app) ensureBriefingOutputDeps() {
	if app.output.composeBody == nil {
		app.output.composeBody = output.FormatBody
	}
}

func (app *app) ensureResendEmailDeps() {
	if app.email.resendMarkdownEmail == nil {
		app.email.resendMarkdownEmail = output.SendMarkdownFile
	}
	if app.output.printText == nil {
		app.output.printText = defaultPrintText
	}
}

func (app *app) ensureDeepEmailDeps() {
	if app.email.sendDeepEmail == nil {
		app.email.sendDeepEmail = output.SendDeepEmail
	}
}

func categoryOrderFromSources(sources []config.Source) []string {
	seen := make(map[string]struct{}, len(sources))
	ordered := make([]string, 0, len(sources))
	for _, source := range sources {
		category := strings.TrimSpace(source.Category)
		if _, ok := seen[category]; ok {
			continue
		}
		seen[category] = struct{}{}
		ordered = append(ordered, category)
	}
	return ordered
}

func (app *app) displayLocation() *time.Location {
	if app.cfg != nil && app.cfg.ScheduleLocation != nil {
		return app.cfg.ScheduleLocation
	}
	return time.Local
}

func execute(app *app, cmd command) error {
	return executeContext(context.Background(), app, cmd)
}

func executeContext(ctx context.Context, app *app, cmd command) error {
	if err := app.preflightCommand(cmd); err != nil {
		return err
	}
	app.suppressPublishHook = commandSuppressesPublishHook(cmd)
	switch c := cmd.(type) {
	case runCommand:
		return app.runBriefingContext(ctx, "run", currentPeriod(), c.raw, !c.noEmail)
	case regenCommand:
		return app.runRegenContext(ctx, c)
	case fetchCommand:
		return app.runFetchContext(ctx, c)
	case alertsCommand:
		return app.runAlertsContext(ctx)
	case xRoutesCommand:
		return app.runXRoutesContext(ctx)
	case xReadyCommand:
		return app.runXReadyContext(ctx, c)
	case serveCommand:
		logutil.Println("Starting news aggregator in scheduled mode...")
		if err := app.startScheduler(ctx, app.cfg, func(window scheduler.Window) {
			if err := app.runScheduledBriefingOnceContext(ctx, window, "cron", true); err != nil {
				logutil.Errorf("scheduled run failed: %v", err)
			}
		}); err != nil {
			return err
		}
		app.wait(ctx)
		return nil
	case deepCommand:
		return app.runDeepDiveContext(ctx, c)
	case resendMDCommand:
		app.ensureResendEmailDeps()
		if err := app.email.resendMarkdownEmail(c.file, app.cfg); err != nil {
			return err
		}
		app.output.printText(fmt.Sprintf("Email resent to %s", app.cfg.Email.To))
		return nil
	case helpCommand:
		printUsage()
		return nil
	default:
		return fmt.Errorf("unsupported command: %T", cmd)
	}
}

func outputNeedsTranslatedContent(mode model.OutputMode) bool {
	return mode != model.OutputModeOriginalOnly
}

func (app *app) preflightCommand(cmd command) error {
	if !commandSendsEmail(cmd) {
		return nil
	}
	return app.preflightEmailSending()
}

func commandSendsEmail(cmd command) bool {
	switch c := cmd.(type) {
	case runCommand:
		return !c.noEmail
	case serveCommand, xReadyCommand:
		return true
	case regenCommand:
		return c.sendEmail
	case deepCommand:
		return c.sendEmail
	case resendMDCommand:
		return true
	default:
		return false
	}
}

func commandSuppressesPublishHook(cmd command) bool {
	switch c := cmd.(type) {
	case runCommand:
		return c.noPublish
	case regenCommand:
		return c.noPublish
	case serveCommand:
		return c.noPublish
	case xReadyCommand:
		return c.noPublish
	default:
		return false
	}
}

func (app *app) preflightEmailSending() error {
	if app == nil || app.cfg == nil {
		return fmt.Errorf("preflight email: config is nil")
	}
	return output.ValidateEmailReadyForSending(app.cfg)
}

func (app *app) startScheduler(ctx context.Context, cfg *config.Config, run func(scheduler.Window)) error {
	if app.scheduler.startCronContext != nil {
		return app.scheduler.startCronContext(ctx, cfg, run)
	}
	if app.scheduler.startCron != nil {
		return app.scheduler.startCron(cfg, run)
	}
	return scheduler.StartContext(ctx, cfg, run)
}

func (app *app) wait(ctx context.Context) {
	if app.scheduler.waitForeverContext != nil {
		app.scheduler.waitForeverContext(ctx)
		return
	}
	if app.scheduler.waitForever != nil {
		app.scheduler.waitForever()
	}
}

func (app *app) fetchAllArticles(ctx context.Context, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
	if app.fetch.fetchAllContext != nil {
		return app.fetch.fetchAllContext(ctx, app.cfg, markSeen)
	}
	if app.fetch.fetchAll != nil {
		return app.fetch.fetchAll(app.cfg, markSeen)
	}
	return fetcher.FetchAllContext(ctx, app.cfg, markSeen)
}

func (app *app) fetchWindowArticles(ctx context.Context, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
	if app.fetch.fetchWindowContext != nil {
		return app.fetch.fetchWindowContext(ctx, app.cfg, from, to, markSeen, ignoreSeen)
	}
	if app.fetch.fetchWindow != nil {
		return app.fetch.fetchWindow(app.cfg, from, to, markSeen, ignoreSeen)
	}
	return fetcher.FetchWindowContext(ctx, app.cfg, from, to, markSeen, ignoreSeen)
}

func (app *app) fetchAllArticlesDetailed(ctx context.Context, markSeen bool) (fetcher.FetchResult, error) {
	if app.fetch.fetchAllDetailedContext != nil {
		return app.fetch.fetchAllDetailedContext(ctx, app.cfg, markSeen)
	}
	articles, failed, err := app.fetchAllArticles(ctx, markSeen)
	return fetcher.FetchResult{Articles: articles, Failed: failed}, err
}

func (app *app) fetchWindowArticlesDetailed(ctx context.Context, from, to time.Time, markSeen bool, ignoreSeen bool) (fetcher.FetchResult, error) {
	if app.fetch.fetchWindowDetailedContext != nil {
		return app.fetch.fetchWindowDetailedContext(ctx, app.cfg, from, to, markSeen, ignoreSeen)
	}
	articles, failed, err := app.fetchWindowArticles(ctx, from, to, markSeen, ignoreSeen)
	return fetcher.FetchResult{Articles: articles, Failed: failed}, err
}

func (app *app) fetchWindowArticlesDetailedWithXVisibleHistory(ctx context.Context, from, to time.Time, markSeen bool, ignoreSeen bool, historyDir string) (fetcher.FetchResult, error) {
	if app.fetch.fetchWindowDetailedHistoryContext != nil {
		return app.fetch.fetchWindowDetailedHistoryContext(ctx, app.cfg, from, to, markSeen, ignoreSeen, historyDir)
	}
	return fetcher.FetchWindowDetailedWithXVisibleHistoryContext(ctx, app.cfg, from, to, markSeen, ignoreSeen, historyDir)
}

func (app *app) fetchXAlerts(ctx context.Context, now time.Time) (fetcher.FetchResult, error) {
	if app.fetch.fetchXAlertsContext != nil {
		return app.fetch.fetchXAlertsContext(ctx, app.cfg, now)
	}
	return fetcher.FetchXAlertsContext(ctx, app.cfg, now)
}

func (app *app) fetchWatchArticles(ctx context.Context, now time.Time) ([]model.Article, *model.WatchReport, error) {
	if app.watch.fetchWatchContext != nil {
		return app.watch.fetchWatchContext(ctx, app.cfg, now)
	}
	if app.watch.fetchWatch != nil {
		return app.watch.fetchWatch(app.cfg, now)
	}
	return nil, nil, nil
}

type watchFetchResult struct {
	articles []model.Article
	report   *model.WatchReport
	err      error
}

type briefingFetchResult struct {
	articles              []model.Article
	filteredArticles      []model.Article
	seenArticles          []model.Article
	failed                []fetcher.FailedSource
	watchSiteErrorNotices []string
}

func (app *app) startWatchFetch(ctx context.Context, watchTime time.Time) func() watchFetchResult {
	if app.watch.fetchWatchContext == nil && app.watch.fetchWatch == nil {
		return func() watchFetchResult { return watchFetchResult{} }
	}
	resultCh := make(chan watchFetchResult, 1)
	go func() {
		articles, report, err := app.fetchWatchArticles(ctx, watchTime)
		resultCh <- watchFetchResult{articles: articles, report: report, err: err}
	}()
	return func() watchFetchResult { return <-resultCh }
}

func (app *app) mergeWatchFetchResult(ctx context.Context, articles []model.Article, result watchFetchResult, sidecarDate string, period string) ([]model.Article, []string, error) {
	if result.err != nil {
		notices := []string{fmt.Sprintf("Watch 抓取失败：%v", result.err)}
		app.printWatchSiteErrorNotices(notices)
		return articles, notices, nil
	}
	articles = append(articles, result.articles...)
	notices := watchSiteErrorNotices(result.report)
	app.printWatchSiteErrorNotices(notices)
	if app.output.writeWatchMarkdown != nil && result.report != nil {
		if err := runIfActive(ctx, func() error {
			_, err := app.output.writeWatchMarkdown(result.report, app.cfg.Output.Dir, sidecarDate, period)
			return err
		}); err != nil {
			return nil, nil, err
		}
	}
	return articles, notices, nil
}

func (app *app) fetchBriefingArticlesWithWatch(ctx context.Context, watchTime time.Time, sidecarDate string, period string, fetchMain func(context.Context) (fetcher.FetchResult, error)) (briefingFetchResult, error) {
	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()
	waitWatch := app.startWatchFetch(watchCtx, watchTime)

	result, err := fetchMain(ctx)
	if err != nil {
		return briefingFetchResult{}, err
	}
	seenArticles := append([]model.Article(nil), result.Articles...)
	watchSiteErrorNotices := []string(nil)
	result.Articles, watchSiteErrorNotices, err = app.mergeWatchFetchResult(ctx, result.Articles, waitWatch(), sidecarDate, period)
	if err != nil {
		return briefingFetchResult{}, err
	}
	return briefingFetchResult{articles: result.Articles, filteredArticles: result.FilteredArticles, seenArticles: seenArticles, failed: result.Failed, watchSiteErrorNotices: watchSiteErrorNotices}, nil
}

func (app *app) summarizeArticles(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
	_, markdown, err := app.summarizeBriefing(ctx, articles, categoryOrder, loc)
	return markdown, err
}

func (app *app) summarizeBriefing(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (*model.BriefingSummary, string, error) {
	if app.ai.summarizeBriefingContext != nil {
		structured, markdown, err := app.ai.summarizeBriefingContext(ctx, articles, categoryOrder, loc)
		if err != nil {
			return nil, "", err
		}
		return &structured, markdown, nil
	}
	if app.ai.summarizeContext != nil {
		markdown, err := app.ai.summarizeContext(ctx, articles, categoryOrder, loc)
		return nil, markdown, err
	}
	if app.ai.summarize != nil {
		markdown, err := app.ai.summarize(articles, categoryOrder, loc)
		return nil, markdown, err
	}
	structured, markdown, err := summarizer.SummarizeBriefingContext(ctx, articles, categoryOrder, loc)
	if err != nil {
		return nil, "", err
	}
	return &structured, markdown, nil
}

func (app *app) translateArticles(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
	if app.ai.translateContext != nil {
		return app.ai.translateContext(ctx, articles, categoryOrder, loc)
	}
	if app.ai.translate != nil {
		return app.ai.translate(articles, categoryOrder, loc)
	}
	return summarizer.TranslateContext(ctx, articles, categoryOrder, loc)
}

func (app *app) deepDiveArticles(ctx context.Context, topic string, articles []model.Article, loc *time.Location) (string, error) {
	if app.ai.deepDiveContext != nil {
		return app.ai.deepDiveContext(ctx, topic, articles, loc)
	}
	if app.ai.deepDive != nil {
		return app.ai.deepDive(topic, articles, loc)
	}
	return summarizer.DeepDiveContext(ctx, topic, articles, loc)
}

func runIfActive(ctx context.Context, run func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return run()
}

func (app *app) runFetch(cmd fetchCommand) error {
	return app.runFetchContext(context.Background(), cmd)
}

func (app *app) runXRoutesContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	app.ensureTextOutputDeps()
	var builder strings.Builder
	for _, account := range app.cfg.XAccounts.Accounts {
		handle := strings.TrimPrefix(strings.TrimSpace(account.Handle), "@")
		if handle == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString("/twitter/user/")
		builder.WriteString(handle)
	}
	app.output.printText(builder.String())
	return nil
}

func (app *app) runAlertsContext(ctx context.Context) error {
	now := app.currentTime()
	logutil.Println("Fetching X alerts for last 24h...")
	result, err := app.fetchXAlerts(ctx, now)
	if err != nil {
		return err
	}
	app.ensureTextOutputDeps()
	app.output.printArticles(result.Articles)
	app.output.printFailed(result.Failed)
	if len(result.Articles) == 0 {
		app.output.printText("No X alerts found in the last 24h.")
	}
	return nil
}

func (app *app) runFetchContext(ctx context.Context, cmd fetchCommand) error {
	logutil.Println("Fetching news...")
	articles, failed, err := app.fetchAllArticles(ctx, false)
	if err != nil {
		return err
	}
	logutil.Printf("Found %d articles after filtering.", len(articles))
	app.ensureTextOutputDeps()

	if !cmd.zh {
		app.output.printArticles(articles)
		app.output.printFailed(failed)
		return nil
	}
	if len(articles) == 0 {
		app.output.printFailed(failed)
		return nil
	}

	categoryOrder := categoryOrderFromSources(app.cfg.Sources)
	content := model.OutputContent{
		Original: output.GroupedArticleListView(articles, categoryOrder, app.displayLocation()),
	}
	if outputNeedsTranslatedContent(app.cfg.Output.Mode) {
		logutil.Println("Translating with AI CLI...")
		translated, err := app.translateArticles(ctx, articles, categoryOrder, app.displayLocation())
		if err != nil {
			return err
		}
		content.Translated = translated
	}
	body, err := app.output.composeBody("fetch --zh", app.cfg.Output.Mode, content)
	if err != nil {
		return err
	}
	fmt.Println()
	app.output.printText(body)
	app.output.printFailed(failed)
	return nil
}

func (app *app) runBriefing(commandPath string, period string, showRaw bool, sendEmail bool) error {
	return app.runBriefingContext(context.Background(), commandPath, period, showRaw, sendEmail)
}

func (app *app) runBriefingContext(ctx context.Context, commandPath string, period string, showRaw bool, sendEmail bool) error {
	logutil.Println("Fetching news...")
	now := app.currentTime()
	date := now.Format("06.01.02")
	fetchStarted := time.Now()
	result, err := app.fetchBriefingArticlesWithWatch(ctx, now, date, period, func(ctx context.Context) (fetcher.FetchResult, error) {
		return app.fetchAllArticlesDetailed(ctx, false)
	})
	if err != nil {
		return err
	}
	logutil.Printf("Stage fetch completed in %s", time.Since(fetchStarted).Round(time.Second))
	limitStarted := time.Now()
	result.articles, result.filteredArticles, _, err = applyBriefingArticleLimits(result.articles, result.filteredArticles, app.configuredArticleLimits())
	if err != nil {
		return err
	}
	logutil.Printf("Stage article_limit completed in %s", time.Since(limitStarted).Round(time.Second))
	return app.renderBriefingContext(ctx, commandPath, date, period, result.articles, result.filteredArticles, result.seenArticles, result.failed, showRaw, sendEmail, result.watchSiteErrorNotices...)
}

func (app *app) runScheduledBriefing(window scheduler.Window, sendEmail bool) error {
	return app.runScheduledBriefingContext(context.Background(), window, sendEmail)
}

func (app *app) runScheduledBriefingContext(ctx context.Context, window scheduler.Window, sendEmail bool) error {
	return app.runScheduledBriefingContextWithReporter(ctx, window, sendEmail, nil)
}

func (app *app) runScheduledBriefingContextWithReporter(ctx context.Context, window scheduler.Window, sendEmail bool, reporter *scheduledRunReporter) error {
	logutil.Println("Fetching news...")
	loc := app.displayLocation()
	date := window.To.In(loc).Format("06.01.02")
	fetchStarted := time.Now()
	result, err := app.fetchBriefingArticlesWithWatch(ctx, window.To, date, window.Period, func(ctx context.Context) (fetcher.FetchResult, error) {
		return app.fetchWindowArticlesDetailed(ctx, window.From, window.To, false, false)
	})
	if err != nil {
		return err
	}
	logutil.Printf("Stage fetch completed in %s", time.Since(fetchStarted).Round(time.Second))
	limitStarted := time.Now()
	var limitReport articleLimitReport
	result.articles, result.filteredArticles, limitReport, err = applyBriefingArticleLimits(result.articles, result.filteredArticles, app.configuredArticleLimits())
	if reporter != nil {
		reporter.updateArticleLimits(limitReport)
	}
	if err != nil {
		return err
	}
	logutil.Printf("Stage article_limit completed in %s", time.Since(limitStarted).Round(time.Second))
	return app.renderBriefingContextWithReporter(ctx, "serve", date, window.Period, result.articles, result.filteredArticles, result.seenArticles, result.failed, false, sendEmail, reporter, result.watchSiteErrorNotices...)
}

const scheduledRunRunningTTL = 2 * time.Hour

func (app *app) runXReadyContext(ctx context.Context, cmd xReadyCommand) error {
	window, err := app.xReadyWindow(cmd)
	if err != nil {
		return err
	}
	loc := app.displayLocation()
	logutil.Printf("[scheduler] X ready callback: [%s -> %s]", window.From.In(loc).Format(time.RFC3339), window.To.In(loc).Format(time.RFC3339))
	return app.runScheduledBriefingOnceContext(ctx, window, "x-ready-callback", true)
}

func (app *app) xReadyWindow(cmd xReadyCommand) (scheduler.Window, error) {
	loc := app.displayLocation()
	from, err := parseXReadyTime(cmd.fromRaw, loc)
	if err != nil {
		return scheduler.Window{}, fmt.Errorf("parse --from: %w", err)
	}
	to, err := parseXReadyTime(cmd.toRaw, loc)
	if err != nil {
		return scheduler.Window{}, fmt.Errorf("parse --to: %w", err)
	}
	if !to.After(from) {
		return scheduler.Window{}, fmt.Errorf("--to must be after --from")
	}
	period := strings.TrimSpace(cmd.period)
	if period == "" {
		period = defaultPeriodFrom(to.In(loc))
	}
	if err := validatePeriod(period); err != nil {
		return scheduler.Window{}, err
	}
	return scheduler.Window{Expr: "x-ready-callback", Period: period, From: from, To: to}, nil
}

func parseXReadyTime(value string, loc *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, nil
	}
	return parseRegenTime(value, loc)
}

func (app *app) runScheduledBriefingOnceContext(ctx context.Context, window scheduler.Window, trigger string, sendEmail bool) error {
	paths, acquired, err := app.acquireScheduledRunWindow(window, trigger)
	if err != nil {
		return err
	}
	if !acquired {
		return nil
	}
	reporter := &scheduledRunReporter{paths: paths, trigger: trigger, window: window}
	err = app.runScheduledBriefingContextWithReporter(ctx, window, sendEmail, reporter)
	if err != nil {
		if markErr := markScheduledRunFailed(paths, trigger, window, err); markErr != nil {
			logutil.Errorf("mark scheduled run failed: %v", markErr)
		}
		return err
	}
	return markScheduledRunDone(paths, trigger, window, reporter.stateFields())
}

type scheduledRunPaths struct {
	running string
	done    string
	failed  string
}

type scheduledRunReporter struct {
	paths   scheduledRunPaths
	trigger string
	window  scheduler.Window
	fields  map[string]string
}

func (r *scheduledRunReporter) updateArticleLimits(report articleLimitReport) {
	if r == nil || strings.TrimSpace(r.paths.running) == "" || !report.applied {
		return
	}
	r.updateFields("article_limit", report.stateFields())
}

func (r *scheduledRunReporter) updateAIStage(attempt aiBriefingAttempt, stage string, elapsed time.Duration) {
	if r == nil || strings.TrimSpace(r.paths.running) == "" {
		return
	}
	fields := map[string]string{
		"ai_attempt":    attempt.name,
		"ai_articles":   fmt.Sprintf("%d", len(attempt.articles)),
		"ai_categories": articleCategoryCountsString(attempt.articles),
	}
	if attempt.fallback {
		fields["ai_fallback"] = "true"
	}
	if elapsed > 0 {
		fields["ai_elapsed"] = elapsed.Round(time.Second).String()
	}
	r.updateFields(stage, fields)
}

func (r *scheduledRunReporter) updateFields(stage string, fields map[string]string) {
	if r.fields == nil {
		r.fields = make(map[string]string)
	}
	for key, value := range fields {
		r.fields[key] = value
	}
	r.fields["stage"] = stage
	if err := os.WriteFile(r.paths.running, []byte(scheduledRunStateContentWithFields("running", r.trigger, r.window, nil, r.fields)), 0o644); err != nil {
		logutil.Errorf("update scheduled run marker: %v", err)
	}
}

func (r *scheduledRunReporter) stateFields() map[string]string {
	if r == nil || len(r.fields) == 0 {
		return nil
	}
	fields := make(map[string]string, len(r.fields))
	for key, value := range r.fields {
		fields[key] = value
	}
	return fields
}

func (app *app) acquireScheduledRunWindow(window scheduler.Window, trigger string) (scheduledRunPaths, bool, error) {
	paths := app.scheduledRunPaths(window)
	if err := os.MkdirAll(filepath.Dir(paths.running), 0o755); err != nil {
		return paths, false, err
	}
	if _, err := os.Stat(paths.done); err == nil {
		logutil.Printf("[scheduler] 跳过窗口 %s：已完成", window.Period)
		return paths, false, nil
	} else if !os.IsNotExist(err) {
		return paths, false, err
	}
	if info, err := os.Stat(paths.running); err == nil {
		if time.Since(info.ModTime()) < scheduledRunRunningTTL {
			logutil.Printf("[scheduler] 跳过窗口 %s：已有执行中任务", window.Period)
			return paths, false, nil
		}
		logutil.Printf("[scheduler] 替换过期执行标记: %s", paths.running)
		if err := os.Remove(paths.running); err != nil && !os.IsNotExist(err) {
			return paths, false, err
		}
	} else if !os.IsNotExist(err) {
		return paths, false, err
	}
	file, err := os.OpenFile(paths.running, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if os.IsExist(err) {
		logutil.Printf("[scheduler] 跳过窗口 %s：已有执行中任务", window.Period)
		return paths, false, nil
	}
	if err != nil {
		return paths, false, err
	}
	if _, err := file.WriteString(scheduledRunStateContent("running", trigger, window, nil)); err != nil {
		_ = file.Close()
		return paths, false, err
	}
	if err := file.Close(); err != nil {
		return paths, false, err
	}
	if err := os.Remove(paths.failed); err != nil && !os.IsNotExist(err) {
		return paths, false, err
	}
	return paths, true, nil
}

func (app *app) scheduledRunPaths(window scheduler.Window) scheduledRunPaths {
	outputDir := "output"
	if app != nil && app.cfg != nil && strings.TrimSpace(app.cfg.Output.Dir) != "" {
		outputDir = app.cfg.Output.Dir
	}
	key := scheduledRunWindowKey(window)
	dir := filepath.Join(outputDir, "state", "scheduled-runs")
	return scheduledRunPaths{
		running: filepath.Join(dir, key+".running"),
		done:    filepath.Join(dir, key+".done"),
		failed:  filepath.Join(dir, key+".failed"),
	}
}

func scheduledRunWindowKey(window scheduler.Window) string {
	return window.From.UTC().Format("20060102T150405Z") + "_" + window.To.UTC().Format("20060102T150405Z")
}

func markScheduledRunDone(paths scheduledRunPaths, trigger string, window scheduler.Window, fields map[string]string) error {
	if err := os.WriteFile(paths.done, []byte(scheduledRunStateContentWithFields("done", trigger, window, nil, fields)), 0o644); err != nil {
		return err
	}
	if err := os.Remove(paths.running); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func markScheduledRunFailed(paths scheduledRunPaths, trigger string, window scheduler.Window, runErr error) error {
	if err := os.WriteFile(paths.failed, []byte(scheduledRunStateContent("failed", trigger, window, runErr)), 0o644); err != nil {
		return err
	}
	if err := os.Remove(paths.running); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func scheduledRunStateContent(status string, trigger string, window scheduler.Window, runErr error) string {
	return scheduledRunStateContentWithFields(status, trigger, window, runErr, nil)
}

func scheduledRunStateContentWithFields(status string, trigger string, window scheduler.Window, runErr error, fields map[string]string) string {
	var builder strings.Builder
	builder.WriteString("status: ")
	builder.WriteString(status)
	builder.WriteByte('\n')
	builder.WriteString("trigger: ")
	builder.WriteString(trigger)
	builder.WriteByte('\n')
	builder.WriteString("period: ")
	builder.WriteString(window.Period)
	builder.WriteByte('\n')
	builder.WriteString("from: ")
	builder.WriteString(window.From.Format(time.RFC3339))
	builder.WriteByte('\n')
	builder.WriteString("to: ")
	builder.WriteString(window.To.Format(time.RFC3339))
	builder.WriteByte('\n')
	builder.WriteString("updated_at: ")
	builder.WriteString(time.Now().Format(time.RFC3339))
	builder.WriteByte('\n')
	for key, value := range fields {
		builder.WriteString(key)
		builder.WriteString(": ")
		builder.WriteString(value)
		builder.WriteByte('\n')
	}
	if runErr != nil {
		builder.WriteString("error: ")
		builder.WriteString(runErr.Error())
		builder.WriteByte('\n')
	}
	return builder.String()
}

func (app *app) runRegen(cmd regenCommand) error {
	return app.runRegenContext(context.Background(), cmd)
}

func (app *app) runRegenContext(ctx context.Context, cmd regenCommand) error {
	loc := app.displayLocation()
	from, err := parseRegenTime(cmd.fromRaw, loc)
	if err != nil {
		return fmt.Errorf("parse --from: %w", err)
	}
	to, err := parseRegenTime(cmd.toRaw, loc)
	if err != nil {
		return fmt.Errorf("parse --to: %w", err)
	}
	if to.Before(from) {
		return fmt.Errorf("--to must be after or equal to --from")
	}
	period := cmd.period
	if period == "" {
		period = defaultPeriodFrom(to)
	}

	historyDir := ""
	if cmd.xVisibleHistoryDays > 0 {
		historyDir = strings.TrimSpace(cmd.xVisibleHistoryDir)
		if historyDir == "" && app.cfg != nil {
			historyDir = strings.TrimSpace(app.cfg.XAccounts.HistoryDir)
		}
		if historyDir == "" {
			return fmt.Errorf("x visible history dir is required when --x-visible-history-days is set")
		}
		maxWindow := time.Duration(cmd.xVisibleHistoryDays) * 24 * time.Hour
		if to.Sub(from) > maxWindow {
			return fmt.Errorf("requested window exceeds --x-visible-history-days=%d", cmd.xVisibleHistoryDays)
		}
	}

	logutil.Printf("Fetching news for window %s ~ %s...", from.Format("2006-01-02 15:04"), to.Format("2006-01-02 15:04"))
	fetchStarted := time.Now()
	var result fetcher.FetchResult
	if cmd.xVisibleHistoryDays > 0 {
		result, err = app.fetchWindowArticlesDetailedWithXVisibleHistory(ctx, from, to, false, cmd.ignoreSeen, historyDir)
	} else {
		result, err = app.fetchWindowArticlesDetailed(ctx, from, to, false, cmd.ignoreSeen)
	}
	if err != nil {
		return err
	}
	logutil.Printf("Stage fetch completed in %s", time.Since(fetchStarted).Round(time.Second))
	limits := app.configuredArticleLimits()
	if len(cmd.maxArticlesByCategory) > 0 {
		limits = cmd.maxArticlesByCategory
	}
	limitStarted := time.Now()
	result.Articles, result.FilteredArticles, _, err = applyBriefingArticleLimits(result.Articles, result.FilteredArticles, limits)
	if err != nil {
		return err
	}
	logutil.Printf("Stage article_limit completed in %s", time.Since(limitStarted).Round(time.Second))
	return app.renderBriefingContext(ctx, "regen", to.Format("06.01.02"), period, result.Articles, result.FilteredArticles, nil, result.Failed, cmd.raw, cmd.sendEmail)
}

func (app *app) configuredArticleLimits() map[string]int {
	if app == nil || app.cfg == nil {
		return nil
	}
	return app.cfg.Output.MaxArticlesByCategory
}

type articleLimitCategoryReport struct {
	category string
	before   int
	after    int
	limit    int
	hasLimit bool
}

type articleLimitReport struct {
	applied     bool
	totalBefore int
	totalAfter  int
	categories  []articleLimitCategoryReport
}

func applyBriefingArticleLimits(articles []model.Article, filteredArticles []model.Article, limits map[string]int) ([]model.Article, []model.Article, articleLimitReport, error) {
	if len(limits) == 0 {
		return articles, filteredArticles, articleLimitReport{}, nil
	}
	original := articles
	articles = filterArticlesByCategoryLimits(articles, limits)
	filteredArticles = nil
	report := buildArticleLimitReport(original, articles, limits)
	logutil.Printf("Article limits by category: %s", report.summaryString())
	if len(articles) == 0 {
		return nil, nil, report, fmt.Errorf("no articles remain after article limits")
	}
	return articles, filteredArticles, report, nil
}

func buildArticleLimitReport(original []model.Article, limited []model.Article, limits map[string]int) articleLimitReport {
	beforeCounts := make(map[string]int)
	afterCounts := make(map[string]int)
	categories := make(map[string]struct{})
	for _, article := range original {
		category := normalizedArticleCategory(article.Category)
		beforeCounts[category]++
		categories[category] = struct{}{}
	}
	for _, article := range limited {
		category := normalizedArticleCategory(article.Category)
		afterCounts[category]++
		categories[category] = struct{}{}
	}
	for category := range limits {
		categories[normalizedArticleCategory(category)] = struct{}{}
	}
	return articleLimitReport{applied: true, totalBefore: len(original), totalAfter: len(limited), categories: articleLimitCategoryReports(beforeCounts, afterCounts, categories, limits)}
}

func articleLimitCategoryReports(beforeCounts map[string]int, afterCounts map[string]int, categories map[string]struct{}, limits map[string]int) []articleLimitCategoryReport {
	names := make([]string, 0, len(categories))
	for category := range categories {
		names = append(names, category)
	}
	sort.Strings(names)
	reports := make([]articleLimitCategoryReport, 0, len(names))
	for _, category := range names {
		limit, hasLimit := limits[category]
		reports = append(reports, articleLimitCategoryReport{category: category, before: beforeCounts[category], after: afterCounts[category], limit: limit, hasLimit: hasLimit})
	}
	return reports
}

func (r articleLimitReport) summaryString() string {
	if !r.applied {
		return ""
	}
	parts := make([]string, 0, len(r.categories)+1)
	for _, category := range r.categories {
		limitText := "no limit"
		if category.hasLimit {
			limitText = fmt.Sprintf("limit %d", category.limit)
		}
		parts = append(parts, fmt.Sprintf("%s %d -> %d (%s)", category.category, category.before, category.after, limitText))
	}
	parts = append(parts, fmt.Sprintf("total %d -> %d", r.totalBefore, r.totalAfter))
	return strings.Join(parts, ", ")
}

func (r articleLimitReport) stateFields() map[string]string {
	if !r.applied {
		return nil
	}
	fields := map[string]string{
		"article_limit_total_before": fmt.Sprintf("%d", r.totalBefore),
		"article_limit_total_after":  fmt.Sprintf("%d", r.totalAfter),
	}
	categorySummary := r.summaryString()
	if prefix, ok := strings.CutSuffix(categorySummary, fmt.Sprintf(", total %d -> %d", r.totalBefore, r.totalAfter)); ok {
		fields["article_limit_categories"] = prefix
	} else {
		fields["article_limit_categories"] = categorySummary
	}
	return fields
}

func normalizedArticleCategory(category string) string {
	category = strings.TrimSpace(category)
	if category == "" {
		return "未分类"
	}
	return category
}

func filterArticlesByCategoryLimits(articles []model.Article, limits map[string]int) []model.Article {
	out := make([]model.Article, 0, len(articles))
	counts := make(map[string]int, len(limits))
	for _, article := range articles {
		category := normalizedArticleCategory(article.Category)
		limit, ok := limits[category]
		if !ok || counts[category] >= limit {
			continue
		}
		out = append(out, article)
		counts[category]++
	}
	return out
}

type aiBriefingAttempt struct {
	name     string
	articles []model.Article
	fallback bool
}

type aiBriefingResult struct {
	articles          []model.Article
	structuredSummary *model.BriefingSummary
	summary           string
}

func (app *app) summarizeBriefingWithFallback(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location, alertEnabled bool, reporter *scheduledRunReporter) (aiBriefingResult, error) {
	attempts, err := app.aiBriefingAttempts(articles)
	if err != nil {
		return aiBriefingResult{}, err
	}
	timeout := app.aiTimeoutConfig()
	aiCtx := ctx
	cancelTotal := func() {}
	aiStarted := time.Now()
	if timeout.TotalBudget > 0 {
		aiCtx, cancelTotal = context.WithTimeout(ctx, timeout.TotalBudget)
	}
	defer cancelTotal()

	var lastErr error
	for index, attempt := range attempts {
		if err := aiCtx.Err(); err != nil {
			return aiBriefingResult{}, app.wrapAITotalBudgetError(err, aiStarted, timeout.TotalBudget)
		}
		structuredSummary, summary, err := app.runAISummaryAttempt(aiCtx, attempt, categoryOrder, loc, timeout, reporter)
		if err == nil {
			return aiBriefingResult{articles: attempt.articles, structuredSummary: structuredSummary, summary: summary}, nil
		}
		lastErr = err
		if index == len(attempts)-1 {
			break
		}
		next := attempts[index+1]
		app.sendAIFallbackAlert(alertEnabled, attempt, next, err)
		logutil.Warnf("AI summary attempt %q failed; retrying with fallback %q: %v", attempt.name, next.name, err)
	}
	if err := aiCtx.Err(); err != nil {
		return aiBriefingResult{}, app.wrapAITotalBudgetError(err, aiStarted, timeout.TotalBudget)
	}
	app.sendAIFinalFailureAlert(alertEnabled, lastErr)
	return aiBriefingResult{}, lastErr
}

func (app *app) aiBriefingAttempts(articles []model.Article) ([]aiBriefingAttempt, error) {
	attempts := []aiBriefingAttempt{{name: "primary", articles: articles}}
	if app == nil || app.cfg == nil || !app.cfg.Output.Fallback.Enabled {
		return attempts, nil
	}
	for _, level := range app.cfg.Output.Fallback.Levels {
		limited := filterArticlesByCategoryLimits(articles, level.MaxArticlesByCategory)
		if len(limited) == 0 {
			return nil, fmt.Errorf("fallback %q leaves no articles", level.Name)
		}
		attempts = append(attempts, aiBriefingAttempt{name: level.Name, articles: limited, fallback: true})
	}
	return attempts, nil
}

func (app *app) aiTimeoutConfig() config.AITimeoutCfg {
	if app == nil || app.cfg == nil {
		return config.AITimeoutCfg{}
	}
	return app.cfg.AI.Timeout
}

func (app *app) runAISummaryAttempt(ctx context.Context, attempt aiBriefingAttempt, categoryOrder []string, loc *time.Location, timeout config.AITimeoutCfg, reporter *scheduledRunReporter) (*model.BriefingSummary, string, error) {
	attemptCtx := ctx
	cancelAttempt := func() {}
	if timeout.AttemptTimeout > 0 {
		attemptCtx, cancelAttempt = context.WithTimeout(ctx, timeout.AttemptTimeout)
	}
	defer cancelAttempt()
	reporter.updateAIStage(attempt, "ai_summary", 0)
	stopWarning := app.startAIWarningTimer(attemptCtx, attempt, timeout.WarningAfter, reporter)
	defer stopWarning()

	started := time.Now()
	logutil.Printf("AI summary attempt %q started: articles=%d categories=%s", attempt.name, len(attempt.articles), articleCategoryCountsString(attempt.articles))
	structuredSummary, summary, err := app.summarizeBriefing(attemptCtx, attempt.articles, categoryOrder, loc)
	duration := time.Since(started).Round(time.Second)
	if err != nil {
		if errors.Is(attemptCtx.Err(), context.DeadlineExceeded) && timeout.AttemptTimeout > 0 {
			logutil.Warnf("AI summary attempt %q timed out after %s", attempt.name, duration)
			return nil, "", fmt.Errorf("ai summary attempt %q timed out after %s: %w", attempt.name, timeout.AttemptTimeout, attemptCtx.Err())
		}
		logutil.Warnf("AI summary attempt %q failed after %s: %v", attempt.name, duration, err)
		return nil, "", err
	}
	logutil.Printf("AI summary attempt %q completed in %s", attempt.name, duration)
	return structuredSummary, summary, nil
}

func (app *app) startAIWarningTimer(ctx context.Context, attempt aiBriefingAttempt, warningAfter time.Duration, reporter *scheduledRunReporter) func() {
	if warningAfter <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	timer := time.NewTimer(warningAfter)
	go func() {
		select {
		case <-timer.C:
			reporter.updateAIStage(attempt, "ai_summary_warning", warningAfter)
			logutil.Warnf("AI summary attempt %q still running after %s: articles=%d categories=%s", attempt.name, warningAfter, len(attempt.articles), articleCategoryCountsString(attempt.articles))
		case <-ctx.Done():
		case <-done:
		}
	}()
	return func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		close(done)
	}
}

func (app *app) wrapAITotalBudgetError(err error, started time.Time, budget time.Duration) error {
	if errors.Is(err, context.DeadlineExceeded) && budget > 0 {
		return fmt.Errorf("ai summary total budget exceeded after %s: %w", time.Since(started).Round(time.Second), err)
	}
	return err
}

func articleCategoryCountsString(articles []model.Article) string {
	counts := make(map[string]int)
	for _, article := range articles {
		category := strings.TrimSpace(article.Category)
		if category == "" {
			category = "未分类"
		}
		counts[category]++
	}
	parts := make([]string, 0, len(counts))
	for category, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", category, count))
	}
	return strings.Join(parts, ",")
}

func (app *app) sendAIFallbackAlert(enabled bool, failed aiBriefingAttempt, next aiBriefingAttempt, runErr error) {
	if !enabled {
		return
	}
	body := fmt.Sprintf("AI summary attempt %q failed and fallback %q is starting.\n\nFailed articles: %d\nFallback articles: %d\nError: %v", failed.name, next.name, len(failed.articles), len(next.articles), runErr)
	app.sendAIAlert("[news-briefing] AI summary fallback started", body)
}

func (app *app) sendAIFinalFailureAlert(enabled bool, runErr error) {
	if !enabled {
		return
	}
	app.sendAIAlert("[news-briefing] AI summary failed", fmt.Sprintf("AI summary failed after all configured attempts.\n\nError: %v", runErr))
}

func (app *app) sendAIAlert(subject string, body string) {
	if app == nil || app.email.sendAlertEmail == nil || app.cfg == nil || strings.TrimSpace(app.cfg.Email.SMTPHost) == "" {
		return
	}
	if err := app.email.sendAlertEmail(subject, body, app.cfg); err != nil {
		logutil.Errorf("send AI alert email failed: %v", err)
	}
}

func (app *app) renderBriefing(commandPath string, date string, period string, articles []model.Article, filteredArticles []model.Article, seenArticles []model.Article, failed []fetcher.FailedSource, showRaw bool, sendEmail bool) error {
	return app.renderBriefingContext(context.Background(), commandPath, date, period, articles, filteredArticles, seenArticles, failed, showRaw, sendEmail)
}

func (app *app) appendFilteredArticlesAppendix(ctx context.Context, body string, filteredArticles []model.Article, categoryOrder []string) (string, error) {
	if app.cfg == nil || !app.cfg.Output.IncludeFilteredArticles || len(filteredArticles) == 0 {
		return body, nil
	}
	appendix, err := app.translateArticles(ctx, filteredArticles, categoryOrder, app.displayLocation())
	if err != nil {
		return "", fmt.Errorf("translate filtered articles: %w", err)
	}
	appendix = strings.TrimSpace(appendix)
	if appendix == "" {
		return body, nil
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return "## 未命中关键词的候选新闻\n\n" + appendix, nil
	}
	return body + "\n\n## 未命中关键词的候选新闻\n\n" + appendix, nil
}

func (app *app) renderBriefingContext(ctx context.Context, commandPath string, date string, period string, articles []model.Article, filteredArticles []model.Article, seenArticles []model.Article, failed []fetcher.FailedSource, showRaw bool, sendEmail bool, watchSiteErrorNotices ...string) error {
	return app.renderBriefingContextWithReporter(ctx, commandPath, date, period, articles, filteredArticles, seenArticles, failed, showRaw, sendEmail, nil, watchSiteErrorNotices...)
}

func (app *app) renderBriefingContextWithReporter(ctx context.Context, commandPath string, date string, period string, articles []model.Article, filteredArticles []model.Article, seenArticles []model.Article, failed []fetcher.FailedSource, showRaw bool, sendEmail bool, reporter *scheduledRunReporter, watchSiteErrorNotices ...string) error {
	logutil.Printf("Found %d articles after filtering.", len(articles))
	app.output.printFailed(failed)
	app.ensureBriefingOutputDeps()

	if showRaw {
		fmt.Println("\n--- Raw Articles ---")
		app.output.printArticles(articles)
		fmt.Println("--- End Raw Articles ---")
		fmt.Println()
	}

	categoryOrder := categoryOrderFromSources(app.cfg.Sources)
	content := model.OutputContent{
		Original: output.GroupedArticleListView(articles, categoryOrder, app.displayLocation()),
	}
	summary := ""
	var structuredSummary *model.BriefingSummary
	if outputNeedsTranslatedContent(app.cfg.Output.Mode) {
		logutil.Println("Generating summary with AI CLI...")
		aiStarted := time.Now()
		result, err := app.summarizeBriefingWithFallback(ctx, articles, categoryOrder, app.displayLocation(), sendEmail, reporter)
		if err != nil {
			return err
		}
		logutil.Printf("Stage ai_summary completed in %s", time.Since(aiStarted).Round(time.Second))
		articles = result.articles
		structuredSummary = result.structuredSummary
		summary = result.summary
		content.Original = output.GroupedArticleListView(articles, categoryOrder, app.displayLocation())
		content.Translated = summary
	}
	body, err := app.output.composeBody(commandPath, app.cfg.Output.Mode, content)
	if err != nil {
		return err
	}
	body, err = app.appendFilteredArticlesAppendix(ctx, body, filteredArticles, categoryOrder)
	if err != nil {
		return err
	}
	body = appendWatchSiteErrorNotices(body, watchSiteErrorNotices)
	body = output.AppendFailedSection(body, failed)

	briefing := &model.Briefing{
		Date:              date,
		Period:            period,
		Articles:          articles,
		Summary:           summary,
		StructuredSummary: structuredSummary,
		RawContent:        body,
	}

	if err := runIfActive(ctx, func() error {
		app.output.printCLI(briefing)
		return nil
	}); err != nil {
		return err
	}

	var path string
	if err := runIfActive(ctx, func() error {
		var writeErr error
		path, writeErr = app.output.writeMarkdown(briefing, app.cfg.Output.Dir)
		return writeErr
	}); err != nil {
		return fmt.Errorf("write markdown: %w", err)
	}
	logutil.Printf("Markdown saved: %s", path)

	if app.fetch.markSeen != nil && len(seenArticles) > 0 {
		if err := runIfActive(ctx, func() error {
			return app.fetch.markSeen(seenArticles)
		}); err != nil {
			return fmt.Errorf("mark seen: %w", err)
		}
	}

	return app.runPostMarkdownActions(ctx, briefing, path, sendEmail, failed)
}

func (app *app) runPostMarkdownActions(ctx context.Context, briefing *model.Briefing, markdownPath string, sendEmail bool, _ []fetcher.FailedSource) error {
	hookDone := make(chan error, 1)
	if app.suppressPublishHook {
		logutil.Println("Skipping publish hook")
		hookDone <- nil
	} else if app.cfg != nil && app.cfg.PublishHook.Enabled {
		publishMarkdownPath := markdownPath
		if absPath, err := filepath.Abs(markdownPath); err == nil {
			publishMarkdownPath = absPath
		}
		runHook := app.publishHook
		if runHook == nil {
			runHook = runPublishHook
		}
		go func() {
			hookDone <- runHook(ctx, app.cfg.PublishHook, publishHookRequest{
				MarkdownFile:     publishMarkdownPath,
				CardManifestFile: cardManifestPathForMarkdown(publishMarkdownPath),
				SourceApp:        "news-briefing",
				Date:             briefing.Date,
				Period:           briefing.Period,
			})
		}()
	} else {
		hookDone <- nil
	}

	var emailErr error
	if !sendEmail {
		logutil.Println("Skipping email")
	} else {
		sendMarkdownEmail := app.email.resendMarkdownEmail
		err := runIfActive(ctx, func() error {
			if sendMarkdownEmail != nil {
				return sendMarkdownEmail(markdownPath, app.cfg)
			}
			return app.email.sendEmail(briefing, app.cfg, nil)
		})
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				emailErr = ctxErr
			} else {
				logutil.Errorf("Error sending email: %v", err)
			}
		} else {
			logutil.Printf("Email sent to %s", app.cfg.Email.To)
		}
	}

	if err := <-hookDone; err != nil {
		logutil.Errorf("publish hook failed: %v", err)
	}
	return emailErr
}

func cardManifestPathForMarkdown(markdownPath string) string {
	return strings.TrimSuffix(markdownPath, filepath.Ext(markdownPath)) + ".card-manifest.json"
}

func (app *app) runDeepDive(cmd deepCommand) error {
	return app.runDeepDiveContext(context.Background(), cmd)
}

func (app *app) runDeepDiveContext(ctx context.Context, cmd deepCommand) error {
	logutil.Printf("Deep diving into: %s", cmd.topic)

	req, err := app.resolveDeepDiveRequest(cmd)
	if err != nil {
		return err
	}
	articles, failed, err := app.loadDeepDiveArticles(ctx, req)
	if err != nil {
		return err
	}
	app.output.printFailed(failed)
	app.ensureTextOutputDeps()

	relevant, body, err := app.buildDeepDiveBody(ctx, cmd.topic, articles)
	if err != nil {
		return err
	}
	return app.finalizeDeepDive(cmd, req.briefingDate, relevant, body, failed)
}

type deepDiveRequest struct {
	from         time.Time
	to           time.Time
	briefingDate string
	useWindow    bool
	ignoreSeen   bool
}

func (app *app) resolveDeepDiveRequest(cmd deepCommand) (deepDiveRequest, error) {
	now := app.currentTime()
	loc := app.displayLocation()
	req := deepDiveRequest{
		from:         now.Add(-12 * time.Hour),
		to:           now,
		briefingDate: now.In(loc).Format("06.01.02"),
	}
	if cmd.fromRaw != "" || cmd.toRaw != "" {
		from, err := parseRegenTime(cmd.fromRaw, loc)
		if err != nil {
			return deepDiveRequest{}, fmt.Errorf("parse --from: %w", err)
		}
		to, err := parseRegenTime(cmd.toRaw, loc)
		if err != nil {
			return deepDiveRequest{}, fmt.Errorf("parse --to: %w", err)
		}
		if to.Before(from) {
			return deepDiveRequest{}, fmt.Errorf("--to must be after or equal to --from")
		}
		req.from = from
		req.to = to
		req.briefingDate = to.In(loc).Format("06.01.02")
		req.useWindow = true
		req.ignoreSeen = cmd.ignoreSeen
		return req, nil
	}
	if cmd.ignoreSeen {
		req.useWindow = true
		req.ignoreSeen = true
	}
	return req, nil
}

func (app *app) loadDeepDiveArticles(ctx context.Context, req deepDiveRequest) ([]model.Article, []fetcher.FailedSource, error) {
	var (
		articles []model.Article
		failed   []fetcher.FailedSource
		err      error
	)
	if req.useWindow {
		articles, failed, err = app.fetchWindowArticles(ctx, req.from, req.to, false, req.ignoreSeen)
	} else {
		articles, failed, err = app.fetchAllArticles(ctx, false)
	}
	if err != nil {
		return nil, nil, err
	}
	watchArticles, err := loadWatchSeenArticles(app.cfg.Output.Dir, req.from, req.to)
	if err != nil {
		return nil, nil, err
	}
	articles = append(articles, watchArticles...)
	return articles, failed, nil
}

func (app *app) buildDeepDiveBody(ctx context.Context, topic string, articles []model.Article) ([]model.Article, string, error) {
	relevant, err := selectDeepDiveArticles(topic, articles)
	if err != nil {
		return nil, "", err
	}
	formattedContent := model.OutputContent{
		Original: output.ArticleListView(relevant, app.displayLocation()),
	}
	if outputNeedsTranslatedContent(app.cfg.Output.Mode) {
		logutil.Printf("Found %d relevant articles. Generating deep dive...", len(relevant))
		content, err := app.deepDiveArticles(ctx, topic, relevant, app.displayLocation())
		if err != nil {
			return nil, "", err
		}
		if looksLikeInteractiveFollowUp(content) {
			return nil, "", fmt.Errorf("deep dive returned interactive follow-up instead of final content")
		}
		formattedContent.Translated = content
	} else {
		logutil.Printf("Found %d relevant articles.", len(relevant))
	}
	body, err := app.output.composeBody("deep", app.cfg.Output.Mode, formattedContent)
	if err != nil {
		return nil, "", err
	}
	return relevant, body, nil
}

func (app *app) finalizeDeepDive(cmd deepCommand, briefingDate string, relevant []model.Article, body string, failed []fetcher.FailedSource) error {
	briefing := &model.Briefing{
		Date:       briefingDate,
		Articles:   relevant,
		RawContent: body,
	}
	path, err := app.output.writeDeepDive(cmd.topic, body, app.cfg.Output.Dir, briefing.Date)
	if err != nil {
		logutil.Errorf("Error writing deep dive: %v", err)
	} else {
		logutil.Printf("Deep dive saved: %s", path)
	}
	if cmd.sendEmail {
		app.ensureDeepEmailDeps()
		if err := app.email.sendDeepEmail(cmd.topic, briefing, app.cfg, failed); err != nil {
			logutil.Errorf("Error sending email: %v", err)
		} else {
			logutil.Printf("Email sent to %s", app.cfg.Email.To)
		}
	}
	fmt.Println()
	app.output.printText(body)
	return nil
}

func loadWatchSeenArticles(outputDir string, from, to time.Time) ([]model.Article, error) {
	store := watch.NewSeenStore(outputDir)
	state, err := store.Load()
	if err != nil {
		return nil, err
	}
	articles := make([]model.Article, 0, len(state.Items))
	for _, item := range state.Items {
		if !item.DetectedAt.After(from) || item.DetectedAt.After(to) {
			continue
		}
		articles = append(articles, annotateWatchDeepArticle(item))
	}
	return articles, nil
}

func annotateWatchDeepArticle(item model.WatchSeenArticle) model.Article {
	summary := item.Summary
	if summary == "" {
		summary = item.Body
	}
	if summary == "" {
		summary = item.EventType
	}
	if summary != "" {
		summary = "[Watch][" + item.WatchCategory + "] " + summary
	}
	return model.Article{
		Title:     item.Title,
		Link:      item.URL,
		Summary:   summary,
		Source:    item.Source + " Watch",
		Category:  item.BriefingCategory,
		Published: item.DetectedAt,
	}
}

func selectDeepDiveArticles(topic string, articles []model.Article) ([]model.Article, error) {
	normalizedTopic := normalizeDeepDiveText(topic)
	if normalizedTopic == "" || allDeepDiveTopicTermsAreWeak(normalizedTopic) {
		return nil, fmt.Errorf("no sufficiently relevant articles found for topic %q; try a more specific keyword", topic)
	}
	var exact []model.Article
	for _, article := range articles {
		text := normalizeDeepDiveText(article.Title + " " + article.Summary)
		if strings.Contains(text, normalizedTopic) {
			exact = append(exact, article)
		}
	}
	if len(exact) > 0 {
		return exact, nil
	}

	keywords := deepDiveKeywords(topic)
	bestScore := 0
	var scored []model.Article
	for _, article := range articles {
		text := normalizeDeepDiveText(article.Title + " " + article.Summary)
		score := 0
		for _, keyword := range keywords {
			if strings.Contains(text, keyword) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			scored = []model.Article{article}
			continue
		}
		if score > 0 && score == bestScore {
			scored = append(scored, article)
		}
	}
	if bestScore >= 2 {
		return scored, nil
	}
	return nil, fmt.Errorf("no sufficiently relevant articles found for topic %q; try a more specific keyword", topic)
}

func deepDiveKeywords(topic string) []string {
	fields := strings.Fields(normalizeDeepDiveText(topic))
	keywords := make([]string, 0, len(fields)*3)
	seen := make(map[string]struct{})
	for _, field := range fields {
		if shouldSkipDeepDiveKeyword(field) {
			continue
		}
		for _, keyword := range deepDiveKeywordAliases(field) {
			normalized := normalizeDeepDiveText(keyword)
			if normalized == "" {
				continue
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			keywords = append(keywords, normalized)
		}
	}
	return keywords
}

func normalizeDeepDiveText(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	lastSpace := true
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func allDeepDiveTopicTermsAreWeak(topic string) bool {
	for _, field := range strings.Fields(topic) {
		if !shouldSkipDeepDiveKeyword(field) {
			return false
		}
	}
	return true
}

func shouldSkipDeepDiveKeyword(field string) bool {
	if len([]rune(field)) < 2 {
		return true
	}
	_, ok := deepDiveEnglishStopwords[field]
	return ok
}

var deepDiveEnglishStopwords = map[string]struct{}{
	"a":    {},
	"an":   {},
	"and":  {},
	"for":  {},
	"from": {},
	"in":   {},
	"is":   {},
	"of":   {},
	"on":   {},
	"the":  {},
	"to":   {},
	"with": {},
}

func deepDiveKeywordAliases(field string) []string {
	aliases := []string{field}
	switch field {
	case "美国":
		aliases = append(aliases, "us", "u.s.", "united states", "america")
	case "数据中心":
		aliases = append(aliases, "data center", "datacenter")
	case "暂停", "暂停法案":
		aliases = append(aliases, "pause", "halt", "bill", "law", "legislation", "moratorium", "restriction", "restrictions")
	case "法案":
		aliases = append(aliases, "bill", "law", "legislation")
	case "ai":
		aliases = append(aliases, "artificial intelligence")
	}
	return aliases
}

func looksLikeInteractiveFollowUp(content string) bool {
	trimmed := strings.TrimSpace(content)
	for _, marker := range []string{
		"你希望我怎么继续？",
		"你希望我怎么继续?",
		"你希望我怎么继续",
		"你希望我",
		"如果你愿意我可以",
		"要不要我继续",
		"是否需要我继续",
	} {
		if strings.Contains(trimmed, marker) {
			return true
		}
	}
	return false
}

func (app *app) currentTime() time.Time {
	if app.now != nil {
		return app.now()
	}
	return time.Now()
}

func watchSiteErrorNotices(report *model.WatchReport) []string {
	if report == nil {
		return nil
	}
	notices := make([]string, 0)
	for _, event := range report.Events {
		if event.EventType != "site_error" {
			continue
		}
		notices = append(notices, fmt.Sprintf("Watch 站点异常：%s — %s", event.Source, event.Reason))
	}
	return notices
}

func appendWatchSiteErrorNotices(body string, notices []string) string {
	if len(notices) == 0 {
		return body
	}
	body = strings.TrimSpace(body)
	var b strings.Builder
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	b.WriteString("## Watch 站点异常\n\n")
	for _, notice := range notices {
		b.WriteString("- ")
		b.WriteString(notice)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func (app *app) printWatchSiteErrorNotices(notices []string) {
	if app.output.printText == nil {
		return
	}
	for _, notice := range notices {
		app.output.printText(notice)
	}
}
