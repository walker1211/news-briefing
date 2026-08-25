package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/imageutil"
	"github.com/walker1211/news-briefing/internal/logutil"
	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/output"
	"github.com/walker1211/news-briefing/internal/scheduler"
	"github.com/walker1211/news-briefing/internal/sourcehealth"
	"github.com/walker1211/news-briefing/internal/summarizer"
	"github.com/walker1211/news-briefing/internal/watch"
)

type app struct {
	cfg                     *config.Config
	now                     func() time.Time
	scheduler               schedulerDeps
	fetch                   fetchDeps
	watch                   watchDeps
	ai                      aiDeps
	output                  outputDeps
	email                   emailDeps
	publishHook             func(context.Context, config.PublishHookConfig, publishHookRequest) error
	suppressPublishHook     bool
	imageFilter             imageutil.Filter
	scheduledWindowMu       sync.Mutex
	scheduledWindowWatchers map[string]*scheduledWindowWatcher
}

type schedulerDeps struct {
	startCron                   func(*config.Config, func(scheduler.Window)) error
	startCronContext            func(context.Context, *config.Config, func(scheduler.Window)) error
	startCronContextWithTrigger func(context.Context, *config.Config, func(scheduler.Window, time.Time) error, func(scheduler.Window)) error
	waitForever                 func()
	waitForeverContext          func(context.Context)
}

type fetchDeps struct {
	fetchAll                           func(*config.Config, bool) ([]model.Article, []fetcher.FailedSource, error)
	fetchAllContext                    func(context.Context, *config.Config, bool) ([]model.Article, []fetcher.FailedSource, error)
	fetchAllDetailedContext            func(context.Context, *config.Config, bool) (fetcher.FetchResult, error)
	fetchWindow                        func(*config.Config, time.Time, time.Time, bool, bool) ([]model.Article, []fetcher.FailedSource, error)
	fetchWindowContext                 func(context.Context, *config.Config, time.Time, time.Time, bool, bool) ([]model.Article, []fetcher.FailedSource, error)
	fetchWindowDetailedContext         func(context.Context, *config.Config, time.Time, time.Time, bool, bool) (fetcher.FetchResult, error)
	fetchWindowOrdinaryDetailedContext func(context.Context, *config.Config, time.Time, time.Time, bool, bool) (fetcher.FetchResult, error)
	fetchWindowXDetailedContext        func(context.Context, *config.Config, time.Time, time.Time, bool, bool) (fetcher.FetchResult, error)
	fetchWindowDetailedHistoryContext  func(context.Context, *config.Config, time.Time, time.Time, bool, bool, string) (fetcher.FetchResult, error)
	fetchXAlertsContext                func(context.Context, *config.Config, time.Time) (fetcher.FetchResult, error)
	markSeen                           func([]model.Article) error
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
	fetchClient.SetRSSCacheDir(filepath.Join(cfg.Output.Dir, "state", "rss-cache"))
	watchRunner := watch.NewRunner(httpClient)
	aiRunner := summarizer.NewRunnerWithRetryDelays(cfg.AI.Command, cfg.AI.Args, cfg.AI.ShouldAppendSystemPrompt(), cfg.Proxy.HTTP, cfg.Proxy.Socks5, cfg.AI.Retry.Delays)
	aiRunner.SetModelOptions(cfg.AI.Models.Default, cfg.AI.Models.DefaultEffort, cfg.AI.Models.Translation, cfg.AI.Models.TranslationEffort)
	aiRunner.SetSummaryOptions(cfg.AI.Summary.ParallelByCategory, cfg.AI.Summary.MaxConcurrency)
	aiRunner.SetSummaryEditorOptions(
		cfg.AI.Summary.Editor.Enabled,
		cfg.AI.Models.SummaryEditor,
		cfg.AI.Models.SummaryEditorEffort,
		cfg.AI.Summary.Editor.MinStories,
		cfg.AI.Summary.Editor.TargetStories,
		cfg.AI.Summary.Editor.MaxStories,
	)
	aiRunner.SetXHSPreselectionOptions(
		cfg.Output.XHSPreselection.Enabled,
		cfg.Output.XHSPreselection.Categories,
		cfg.Output.XHSPreselection.TargetItems,
		cfg.Output.XHSPreselection.MinimumIndependentSources,
		cfg.Output.XHSPreselection.OfficialSourceHosts,
	)
	emailSender := output.NewEmailSender()
	imageFilter := imageFilterFromConfig(cfg.ImageFilter)
	return &app{
		cfg:         cfg,
		now:         time.Now,
		imageFilter: imageFilter,
		scheduler: schedulerDeps{
			startCron:                   scheduler.Start,
			startCronContext:            scheduler.StartContext,
			startCronContextWithTrigger: scheduler.StartContextWithTrigger,
			waitForever: func() {
				select {}
			},
			waitForeverContext: func(ctx context.Context) {
				<-ctx.Done()
			},
		},
		fetch: fetchDeps{
			fetchAll:                           fetchClient.FetchAll,
			fetchAllContext:                    fetchClient.FetchAllContext,
			fetchAllDetailedContext:            fetchClient.FetchAllDetailedContext,
			fetchWindow:                        fetchClient.FetchWindow,
			fetchWindowContext:                 fetchClient.FetchWindowContext,
			fetchWindowDetailedContext:         fetchClient.FetchWindowDetailedContext,
			fetchWindowOrdinaryDetailedContext: fetchClient.FetchWindowOrdinaryDetailedContext,
			fetchWindowXDetailedContext:        fetchClient.FetchWindowXDetailedContext,
			fetchWindowDetailedHistoryContext:  fetchClient.FetchWindowDetailedWithXVisibleHistoryContext,
			fetchXAlertsContext:                fetcher.FetchXAlertsContext,
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
			printCLI: output.PrintCLI,
			writeMarkdown: func(briefing *model.Briefing, outputDir string) (string, error) {
				return output.WriteMarkdownWithImageFilter(briefing, outputDir, imageFilter)
			},
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

func imageFilterFromConfig(cfg config.ImageFilterConfig) imageutil.Filter {
	rules := make([]imageutil.ExactURLRule, 0, len(cfg.BlockedURLs))
	for _, rule := range cfg.BlockedURLs {
		rules = append(rules, imageutil.ExactURLRule{Host: rule.Host, Path: rule.Path})
	}
	return imageutil.NewFilter(rules)
}

func (app *app) filterArticleImages(articles []model.Article) []model.Article {
	filtered := append([]model.Article(nil), articles...)
	for i := range filtered {
		if !app.imageFilter.IsUsableRemoteImageURL(filtered[i].ImageURL) {
			filtered[i].ImageURL = ""
		}
	}
	return filtered
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
	restoreEmail, err := app.applyCommandEmailRecipientMatch(cmd)
	if err != nil {
		return err
	}
	defer restoreEmail()
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
	case carryoverAddCommand:
		return app.runCarryoverAddContext(ctx, c)
	case carryoverListCommand:
		return app.runCarryoverListContext(ctx)
	case carryoverRemoveCommand:
		return app.runCarryoverRemoveContext(ctx, c)
	case xReadyCommand:
		return app.runXReadyContext(ctx, c)
	case serveCommand:
		logutil.Println("Starting news aggregator in scheduled mode...")
		if err := app.startSchedulerWithTrigger(ctx, app.cfg, func(window scheduler.Window, dueAt time.Time) error {
			if err := app.registerScheduledWindow(window, dueAt); err != nil {
				return err
			}
			app.startScheduledPrefetch(ctx, window)
			if dueAt.After(app.currentTime()) {
				app.startScheduledWindowWatcher(ctx, window)
			}
			return nil
		}, func(window scheduler.Window) {
			if err := app.runScheduledBriefingOnceContext(ctx, window, "cron", true); err != nil {
				logutil.Errorf("scheduled run failed: %v", err)
			}
		}); err != nil {
			return err
		}
		reconcileCtx, stopReconciler := context.WithCancel(ctx)
		defer stopReconciler()
		app.startScheduledWindowReconciler(reconcileCtx)
		app.wait(ctx)
		return nil
	case deepCommand:
		return app.runDeepDiveContext(ctx, c)
	case resendMDCommand:
		app.ensureResendEmailDeps()
		if err := app.email.resendMarkdownEmail(c.file, app.cfg); err != nil {
			return err
		}
		app.output.printText("Email resent to configured recipient(s)")
		return nil
	case helpCommand:
		printUsage()
		return nil
	default:
		return fmt.Errorf("unsupported command: %T", cmd)
	}
}

func (app *app) applyCommandEmailRecipientMatch(cmd command) (func(), error) {
	match := strings.TrimSpace(commandEmailRecipientMatch(cmd))
	if match == "" {
		return func() {}, nil
	}
	if app == nil || app.cfg == nil {
		return nil, fmt.Errorf("select email recipient: config is nil")
	}
	candidates := make([]string, 0, 1+len(app.cfg.Email.Recipients))
	candidates = append(candidates, app.cfg.Email.To)
	candidates = append(candidates, app.cfg.Email.Recipients...)
	needle := strings.ToLower(match)
	matches := make([]string, 0, 1)
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || !strings.Contains(strings.ToLower(candidate), needle) {
			continue
		}
		key := strings.ToLower(candidate)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		matches = append(matches, candidate)
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("select email recipient: match must identify exactly one configured recipient (matched %d)", len(matches))
	}
	original := app.cfg.Email
	app.cfg.Email.To = matches[0]
	app.cfg.Email.Recipients = nil
	return func() { app.cfg.Email = original }, nil
}

func commandEmailRecipientMatch(cmd command) string {
	switch command := cmd.(type) {
	case regenCommand:
		return command.emailRecipientMatch
	case resendMDCommand:
		return command.emailRecipientMatch
	default:
		return ""
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

func (app *app) startSchedulerWithTrigger(ctx context.Context, cfg *config.Config, onTrigger func(scheduler.Window, time.Time) error, run func(scheduler.Window)) error {
	if app.scheduler.startCronContextWithTrigger != nil {
		return app.scheduler.startCronContextWithTrigger(ctx, cfg, onTrigger, run)
	}
	return app.startScheduler(ctx, cfg, func(window scheduler.Window) {
		dueAt := window.To
		if cfg != nil {
			dueAt = dueAt.Add(cfg.ScheduleDelay)
		}
		if err := onTrigger(window, dueAt); err != nil {
			logutil.Errorf("record scheduled window: %v", err)
			return
		}
		run(window)
	})
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
	watchArticles         []model.Article
	failed                []fetcher.FailedSource
	sourceStats           model.SourceStatsReport
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

func filterWatchFetchResultByWindow(result watchFetchResult, from, to time.Time) watchFetchResult {
	if result.report == nil || from.IsZero() || to.IsZero() {
		return result
	}
	excludedURLs := make(map[string]struct{})
	for i := range result.report.Events {
		event := &result.report.Events[i]
		if !event.IncludeInBriefing || event.PublishedAt.IsZero() {
			continue
		}
		if !event.PublishedAt.Before(from) && event.PublishedAt.Before(to) {
			continue
		}
		event.IncludeInBriefing = false
		if event.Reason == "" {
			event.Reason = "文章发布日期不在本次简报时间窗"
		} else {
			event.Reason += "；文章发布日期不在本次简报时间窗"
		}
		excludedURLs[event.ArticleURL] = struct{}{}
	}
	if len(excludedURLs) == 0 {
		return result
	}
	filtered := make([]model.Article, 0, len(result.articles))
	for _, article := range result.articles {
		if _, excluded := excludedURLs[article.Link]; excluded {
			continue
		}
		filtered = append(filtered, article)
	}
	result.articles = filtered
	return result
}

func (app *app) fetchBriefingArticlesWithWatch(ctx context.Context, watchTime, from, to time.Time, sidecarDate string, period string, fetchMain func(context.Context) (fetcher.FetchResult, error)) (briefingFetchResult, error) {
	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()
	waitWatch := app.startWatchFetch(watchCtx, watchTime)

	result, err := fetchMain(ctx)
	if err != nil {
		return briefingFetchResult{}, err
	}
	seenArticles := append([]model.Article(nil), result.Articles...)
	watchSiteErrorNotices := []string(nil)
	watchResult := filterWatchFetchResultByWindow(waitWatch(), from, to)
	result.Articles, watchSiteErrorNotices, err = app.mergeWatchFetchResult(ctx, result.Articles, watchResult, sidecarDate, period)
	if err != nil {
		return briefingFetchResult{}, err
	}
	var watchArticles []model.Article
	if watchResult.err == nil {
		watchArticles = watchResult.articles
	}
	return briefingFetchResult{articles: result.Articles, filteredArticles: result.FilteredArticles, seenArticles: seenArticles, watchArticles: watchArticles, failed: result.Failed, sourceStats: result.SourceStats, watchSiteErrorNotices: watchSiteErrorNotices}, nil
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
	result, err := app.fetchBriefingArticlesWithWatch(ctx, now, now.Add(-12*time.Hour), now, date, period, func(ctx context.Context) (fetcher.FetchResult, error) {
		return app.fetchAllArticlesDetailed(ctx, false)
	})
	if err != nil {
		return err
	}
	logutil.Printf("Stage fetch completed in %s", time.Since(fetchStarted).Round(time.Second))
	limitStarted := time.Now()
	result.articles, result.filteredArticles, _, err = app.applyBriefingArticleLimits(result.articles, result.filteredArticles, app.configuredArticleLimits())
	if err != nil {
		return err
	}
	logutil.Printf("Stage article_limit completed in %s", time.Since(limitStarted).Round(time.Second))
	return app.renderBriefingContextWithSourceStats(ctx, commandPath, date, period, result.articles, result.filteredArticles, result.seenArticles, result.failed, showRaw, sendEmail, &result.sourceStats, result.watchSiteErrorNotices...)
}

func (app *app) runScheduledBriefing(window scheduler.Window, sendEmail bool) error {
	return app.runScheduledBriefingContext(context.Background(), window, sendEmail)
}

func (app *app) runScheduledBriefingContext(ctx context.Context, window scheduler.Window, sendEmail bool) error {
	return app.runScheduledBriefingContextWithReporter(ctx, window, sendEmail, nil)
}

func (app *app) runScheduledBriefingContextWithReporter(ctx context.Context, window scheduler.Window, sendEmail bool, reporter *scheduledRunReporter) error {
	logutil.Println("Fetching news...")
	app.startScheduledSourceShadow(window)
	loc := app.displayLocation()
	date := window.To.In(loc).Format("06.01.02")
	fetchStarted := time.Now()
	result, err := app.fetchScheduledBriefingArticles(ctx, window, date)
	if err != nil {
		return err
	}
	result, carryoverCount, err := app.injectCarryovers(window.To, result)
	if err != nil {
		return err
	}
	if reporter != nil && carryoverCount > 0 {
		reporter.updateFields("carryover", map[string]string{"carryover_pending": fmt.Sprintf("%d", carryoverCount)})
	}
	logutil.Printf("Stage fetch completed in %s", time.Since(fetchStarted).Round(time.Second))
	limitStarted := time.Now()
	var limitReport articleLimitReport
	result.articles, result.filteredArticles, limitReport, err = app.applyBriefingArticleLimits(result.articles, result.filteredArticles, app.configuredArticleLimits())
	if reporter != nil {
		reporter.updateArticleLimits(limitReport)
	}
	if err != nil {
		return err
	}
	result = app.applyScheduledSourceHealthPolicy(window, result)
	logutil.Printf("Stage article_limit completed in %s", time.Since(limitStarted).Round(time.Second))
	return app.renderBriefingContextWithReporterAndSourceStats(ctx, "serve", date, window.Period, result.articles, result.filteredArticles, result.seenArticles, result.failed, false, sendEmail, reporter, &result.sourceStats, result.watchSiteErrorNotices...)
}

func (app *app) applyScheduledSourceHealthPolicy(window scheduler.Window, result briefingFetchResult) briefingFetchResult {
	if app == nil || app.cfg == nil {
		return result
	}
	threshold := app.cfg.SourceHealth.AlertAfterConsecutiveFailures
	if threshold < 1 {
		threshold = 1
	}
	issues := make([]sourcehealth.Issue, 0, len(result.failed)+len(result.watchSiteErrorNotices))
	for _, failed := range result.failed {
		name := strings.TrimSpace(failed.Name)
		if name == "" {
			continue
		}
		key := "fetch:" + name
		issues = append(issues, sourcehealth.Issue{Key: key, Name: name})
	}
	for _, notice := range result.watchSiteErrorNotices {
		name := watchFailureNoticeName(notice)
		if name == "" {
			continue
		}
		key := "watch:" + name
		issues = append(issues, sourcehealth.Issue{Key: key, Name: name})
	}
	windowID := fmt.Sprintf("%s|%s|%s", window.Period, window.From.UTC().Format(time.RFC3339), window.To.UTC().Format(time.RFC3339))
	statePath := filepath.Join(app.cfg.Output.Dir, "state", "source-health.json")
	policy, err := sourcehealth.Update(statePath, windowID, app.currentTime(), threshold, issues)
	if err != nil {
		logutil.Warnf("source health state unavailable; keeping current notices visible: %v", err)
		return result
	}
	visible := make(map[string]struct{}, len(policy.VisibleKeys))
	for _, key := range policy.VisibleKeys {
		visible[key] = struct{}{}
	}
	filteredFailures := result.failed[:0]
	for _, failed := range result.failed {
		if _, ok := visible["fetch:"+strings.TrimSpace(failed.Name)]; ok {
			filteredFailures = append(filteredFailures, failed)
		}
	}
	filteredWatch := result.watchSiteErrorNotices[:0]
	for _, notice := range result.watchSiteErrorNotices {
		if _, ok := visible["watch:"+watchFailureNoticeName(notice)]; ok {
			filteredWatch = append(filteredWatch, notice)
		}
	}
	for _, name := range policy.Recoveries {
		filteredWatch = append(filteredWatch, "来源恢复："+name)
	}
	result.failed = filteredFailures
	result.watchSiteErrorNotices = filteredWatch
	return result
}

func watchFailureNoticeName(notice string) string {
	trimmed := strings.TrimSpace(notice)
	const sitePrefix = "Watch 站点异常："
	if strings.HasPrefix(trimmed, sitePrefix) {
		name, _, _ := strings.Cut(strings.TrimPrefix(trimmed, sitePrefix), " — ")
		return strings.TrimSpace(name)
	}
	if strings.HasPrefix(trimmed, "Watch 抓取失败：") {
		return "Watch"
	}
	return ""
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
	result.Articles, result.FilteredArticles, _, err = app.applyBriefingArticleLimits(result.Articles, result.FilteredArticles, limits)
	if err != nil {
		return err
	}
	logutil.Printf("Stage article_limit completed in %s", time.Since(limitStarted).Round(time.Second))
	restoreOutputDir := app.useManualRegenOutputDir(cmd, period)
	defer restoreOutputDir()
	return app.renderBriefingContextWithSourceStats(ctx, "regen", to.Format("06.01.02"), period, result.Articles, result.FilteredArticles, nil, result.Failed, cmd.raw, cmd.sendEmail, &result.SourceStats)
}

func (app *app) useManualRegenOutputDir(cmd regenCommand, period string) func() {
	if cmd.replaceOutput || app == nil || app.cfg == nil {
		return func() {}
	}
	original := app.cfg.Output.Dir
	runName := app.currentTime().In(app.displayLocation()).Format("20060102T150405.000") + "-" + period
	app.cfg.Output.Dir = filepath.Join(original, "manual", runName)
	logutil.Printf("Manual regen output isolated under %s", app.cfg.Output.Dir)
	return func() { app.cfg.Output.Dir = original }
}

func (app *app) configuredArticleLimits() map[string]int {
	if app == nil || app.cfg == nil {
		return nil
	}
	return app.cfg.Output.MaxArticlesByCategory
}

func (app *app) configuredSourcePriorities() map[string]int {
	if app == nil || app.cfg == nil {
		return nil
	}
	priorities := make(map[string]int)
	for source, filter := range app.cfg.Filters.Sources {
		if filter.Priority > 0 {
			priorities[source] = filter.Priority
		}
	}
	return priorities
}

func (app *app) configuredArticleRanking() articleRankingConfig {
	if app == nil || app.cfg == nil {
		return articleRankingConfig{}
	}
	return articleRankingConfig{
		priorities: app.configuredSourcePriorities(),
		categories: app.cfg.Filters.Categories,
	}
}

func (app *app) applyBriefingArticleLimits(articles []model.Article, filteredArticles []model.Article, limits map[string]int) ([]model.Article, []model.Article, articleLimitReport, error) {
	return applyBriefingArticleLimitsWithRanking(articles, filteredArticles, limits, app.configuredArticleRanking())
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
	return applyBriefingArticleLimitsWithSourcePriorities(articles, filteredArticles, limits, nil)
}

func applyBriefingArticleLimitsWithSourcePriorities(articles []model.Article, filteredArticles []model.Article, limits map[string]int, priorities map[string]int) ([]model.Article, []model.Article, articleLimitReport, error) {
	return applyBriefingArticleLimitsWithRanking(articles, filteredArticles, limits, articleRankingConfig{priorities: priorities})
}

func applyBriefingArticleLimitsWithRanking(articles []model.Article, filteredArticles []model.Article, limits map[string]int, ranking articleRankingConfig) ([]model.Article, []model.Article, articleLimitReport, error) {
	if len(limits) == 0 {
		return articles, filteredArticles, articleLimitReport{}, nil
	}
	original := articles
	articles = filterArticlesByCategoryLimitsWithRanking(articles, limits, ranking)
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
	return filterArticlesByCategoryLimitsWithSourcePriorities(articles, limits, nil)
}

type rankedArticleIndex struct {
	index            int
	priority         int
	baseScore        int
	duplicatePenalty int
	published        time.Time
	titleFingerprint map[string]struct{}
}

type articleRankingConfig struct {
	priorities map[string]int
	categories map[string]config.CategoryFilterConfig
}

func filterArticlesByCategoryLimitsWithSourcePriorities(articles []model.Article, limits map[string]int, priorities map[string]int) []model.Article {
	return filterArticlesByCategoryLimitsWithRanking(articles, limits, articleRankingConfig{priorities: priorities})
}

func filterArticlesByCategoryLimitsWithRanking(articles []model.Article, limits map[string]int, ranking articleRankingConfig) []model.Article {
	byCategory := make(map[string][]rankedArticleIndex, len(limits))
	newestByCategory := make(map[string]time.Time, len(limits))
	for _, article := range articles {
		category := normalizedArticleCategory(article.Category)
		if _, ok := limits[category]; !ok || article.Published.IsZero() {
			continue
		}
		if newest := newestByCategory[category]; newest.IsZero() || article.Published.After(newest) {
			newestByCategory[category] = article.Published
		}
	}
	for index, article := range articles {
		category := normalizedArticleCategory(article.Category)
		if _, ok := limits[category]; !ok {
			continue
		}
		priority := ranking.priorities[strings.TrimSpace(article.Source)]
		carryoverBoost := 0
		if strings.TrimSpace(article.CarryoverID) != "" {
			carryoverBoost = 1_000_000
		}
		byCategory[category] = append(byCategory[category], rankedArticleIndex{
			index:            index,
			priority:         priority,
			baseScore:        carryoverBoost + priority*5 + articleKeywordRankingScore(article, ranking.categories[category]) + articleFreshnessRankingScore(article.Published, newestByCategory[category]),
			published:        article.Published,
			titleFingerprint: articleTitleFingerprint(article.Title),
		})
	}

	selected := make([]bool, len(articles))
	for category, candidates := range byCategory {
		applyNearDuplicateRankingPenalties(candidates)
		sort.SliceStable(candidates, func(i, j int) bool {
			leftScore := candidates[i].baseScore - candidates[i].duplicatePenalty
			rightScore := candidates[j].baseScore - candidates[j].duplicatePenalty
			if leftScore != rightScore {
				return leftScore > rightScore
			}
			if candidates[i].priority != candidates[j].priority {
				return candidates[i].priority > candidates[j].priority
			}
			if !candidates[i].published.Equal(candidates[j].published) {
				return candidates[i].published.After(candidates[j].published)
			}
			return candidates[i].index < candidates[j].index
		})
		limit := limits[category]
		if limit > len(candidates) {
			limit = len(candidates)
		}
		for _, candidate := range candidates[:limit] {
			selected[candidate.index] = true
		}
	}

	out := make([]model.Article, 0, len(articles))
	for index, article := range articles {
		if selected[index] {
			out = append(out, article)
		}
	}
	return out
}

func articleKeywordRankingScore(article model.Article, filter config.CategoryFilterConfig) int {
	titleStrong := len(fetcher.MatchKeywords(article.Title, filter.IncludeKeywords))
	summaryStrong := len(fetcher.MatchKeywords(article.Summary, filter.IncludeKeywords))
	titleWeak := len(fetcher.MatchKeywords(article.Title, filter.WeakKeywords))
	summaryWeak := len(fetcher.MatchKeywords(article.Summary, filter.WeakKeywords))
	score := titleStrong*120 + summaryStrong*50 + titleWeak*35 + summaryWeak*15
	if score > 400 {
		return 400
	}
	return score
}

func articleFreshnessRankingScore(published time.Time, newest time.Time) int {
	if published.IsZero() || newest.IsZero() {
		return 0
	}
	age := newest.Sub(published)
	if age < 0 {
		age = 0
	}
	score := 100 - int(age/time.Hour)*5
	if score < 0 {
		return 0
	}
	return score
}

func applyNearDuplicateRankingPenalties(candidates []rankedArticleIndex) {
	for left := 0; left < len(candidates); left++ {
		for right := left + 1; right < len(candidates); right++ {
			if articleTitleSimilarity(candidates[left].titleFingerprint, candidates[right].titleFingerprint) < 0.72 {
				continue
			}
			loser := right
			if rankedArticleBetter(candidates[right], candidates[left]) {
				loser = left
			}
			candidates[loser].duplicatePenalty += 600
		}
	}
}

func rankedArticleBetter(left rankedArticleIndex, right rankedArticleIndex) bool {
	if left.baseScore != right.baseScore {
		return left.baseScore > right.baseScore
	}
	if left.priority != right.priority {
		return left.priority > right.priority
	}
	if !left.published.Equal(right.published) {
		return left.published.After(right.published)
	}
	return left.index < right.index
}

func articleTitleFingerprint(title string) map[string]struct{} {
	runes := make([]rune, 0, len(title))
	for _, char := range strings.ToLower(title) {
		if unicode.IsLetter(char) || unicode.IsNumber(char) {
			runes = append(runes, char)
		}
	}
	if len(runes) < 4 {
		return nil
	}
	fingerprint := make(map[string]struct{}, len(runes)-1)
	for index := 0; index+1 < len(runes); index++ {
		fingerprint[string(runes[index:index+2])] = struct{}{}
	}
	return fingerprint
}

func articleTitleSimilarity(left map[string]struct{}, right map[string]struct{}) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	intersection := 0
	for gram := range left {
		if _, ok := right[gram]; ok {
			intersection++
		}
	}
	union := len(left) + len(right) - intersection
	return float64(intersection) / float64(union)
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

type aiFailureDiagnostic struct {
	Stage      string
	Code       string
	Categories []string
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
	aiCtx = summarizer.WithBriefingCategorySummaryCache(aiCtx)

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
		if summarizer.IsInvalidPromptError(err) {
			logutil.Warnf("AI summary attempt %q has an invalid UTF-8 prompt; article-count fallback skipped: %v", attempt.name, err)
			break
		}
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
		limited := filterArticlesByCategoryLimitsWithRanking(articles, level.MaxArticlesByCategory, app.configuredArticleRanking())
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
		diagnostic := classifyAIFailure(err)
		reporter.updateAIFailure(attempt, diagnostic, duration)
		if errors.Is(attemptCtx.Err(), context.DeadlineExceeded) && timeout.AttemptTimeout > 0 {
			logutil.Warnf("AI summary attempt %q timed out after %s", attempt.name, duration)
			return nil, "", fmt.Errorf("ai summary attempt %q timed out after %s: %w", attempt.name, timeout.AttemptTimeout, attemptCtx.Err())
		}
		logutil.Warnf("AI summary attempt %q failed after %s: %v", attempt.name, duration, err)
		return nil, "", err
	}
	reporter.updateAISuccess(attempt, duration)
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
	categories := make([]string, 0, len(counts))
	for category := range counts {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	for _, category := range categories {
		count := counts[category]
		parts = append(parts, fmt.Sprintf("%s=%d", category, count))
	}
	return strings.Join(parts, ",")
}

func (app *app) sendAIFallbackAlert(enabled bool, failed aiBriefingAttempt, next aiBriefingAttempt, runErr error) {
	if !enabled || app == nil || app.cfg == nil || !app.cfg.Output.Fallback.AlertOnStart {
		return
	}
	failedCategories := summarizer.FailedBriefingCategories(runErr)
	cachedCategories := cachedSuccessfulCategories(failed.articles, failedCategories)
	diagnostic := classifyAIFailure(runErr)
	body := fmt.Sprintf(
		"AI summary attempt %q failed and fallback %q is starting.\n\nFailure scope: %s\nCached successful categories: %s\nPrimary input: %d articles (%s)\nFallback input: %d articles (%s)\nFailure stage: %s\nError class: %s",
		failed.name,
		next.name,
		fallbackFailureScope(failedCategories),
		fallbackCachedCategories(cachedCategories),
		len(failed.articles),
		articleCategoryCountsString(failed.articles),
		len(next.articles),
		articleCategoryCountsString(next.articles),
		diagnostic.Stage,
		diagnostic.Code,
	)
	app.sendAIAlert("[news-briefing] AI summary fallback started", body)
}

func fallbackFailureScope(categories []string) string {
	if len(categories) == 0 {
		return "full attempt or editorial synthesis"
	}
	return "categories: " + strings.Join(categories, ", ")
}

func cachedSuccessfulCategories(articles []model.Article, failedCategories []string) []string {
	failed := make(map[string]struct{}, len(failedCategories))
	for _, category := range failedCategories {
		failed[strings.TrimSpace(category)] = struct{}{}
	}
	seen := make(map[string]struct{})
	categories := make([]string, 0)
	for _, article := range articles {
		category := strings.TrimSpace(article.Category)
		if category == "" {
			category = "未分类"
		}
		if _, isFailed := failed[category]; isFailed {
			continue
		}
		if _, ok := seen[category]; ok {
			continue
		}
		seen[category] = struct{}{}
		categories = append(categories, category)
	}
	return categories
}

func fallbackCachedCategories(categories []string) string {
	if len(categories) == 0 {
		return "none identified"
	}
	return strings.Join(categories, ", ")
}

func (app *app) sendAIFinalFailureAlert(enabled bool, runErr error) {
	if !enabled {
		return
	}
	diagnostic := classifyAIFailure(runErr)
	app.sendAIAlert("[news-briefing] AI summary failed", fmt.Sprintf("AI summary failed after all configured attempts.\n\nFailure scope: %s\nFailure stage: %s\nError class: %s", fallbackFailureScope(diagnostic.Categories), diagnostic.Stage, diagnostic.Code))
}

func aiAlertErrorClass(err error) string {
	return classifyAIFailure(err).Code
}

func classifyAIFailure(err error) aiFailureDiagnostic {
	diagnostic := aiFailureDiagnostic{Stage: "unknown", Code: "ai_summary_failed", Categories: summarizer.FailedBriefingCategories(err)}
	switch {
	case err == nil:
		diagnostic.Code = "unknown"
	case errors.Is(err, context.DeadlineExceeded):
		diagnostic.Stage = "ai_cli"
		diagnostic.Code = "timeout"
	case errors.Is(err, context.Canceled):
		diagnostic.Stage = "ai_cli"
		diagnostic.Code = "canceled"
	case summarizer.IsInvalidPromptError(err):
		diagnostic.Stage = "prompt"
		diagnostic.Code = "invalid_prompt"
	default:
		if stage, code, ok := summarizer.BriefingFailureDiagnostic(err); ok {
			diagnostic.Stage = stage
			diagnostic.Code = refineAICLIFailureCode(code, err)
		} else if len(diagnostic.Categories) > 0 {
			diagnostic.Stage = "category_summary"
			diagnostic.Code = "category_validation_failed"
		}
	}
	return diagnostic
}

func refineAICLIFailureCode(code string, err error) string {
	if code != "ai_cli_failed" || err == nil {
		return code
	}
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{"token_revoked", "invalidated oauth token", "refresh token has already been used"} {
		if strings.Contains(lower, marker) {
			return "oauth_revoked"
		}
	}
	for _, marker := range []string{"server_error", "internal_error", "upstream_error", "status: 500", "status: 502", "status: 503", "status: 504"} {
		if strings.Contains(lower, marker) {
			return "upstream_error"
		}
	}
	for _, marker := range []string{"connection reset", "i/o timeout", "eof", "stream error"} {
		if strings.Contains(lower, marker) {
			return "transport_error"
		}
	}
	return code
}

func aiAttemptFieldName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
		} else if builder.Len() > 0 && !strings.HasSuffix(builder.String(), "_") {
			builder.WriteByte('_')
		}
	}
	name := strings.Trim(builder.String(), "_")
	if name == "" {
		return "attempt"
	}
	return name
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
	return app.renderBriefingContextWithReporterAndSourceStats(ctx, commandPath, date, period, articles, filteredArticles, seenArticles, failed, showRaw, sendEmail, nil, nil, watchSiteErrorNotices...)
}

func (app *app) renderBriefingContextWithSourceStats(ctx context.Context, commandPath string, date string, period string, articles []model.Article, filteredArticles []model.Article, seenArticles []model.Article, failed []fetcher.FailedSource, showRaw bool, sendEmail bool, sourceStats *model.SourceStatsReport, watchSiteErrorNotices ...string) error {
	return app.renderBriefingContextWithReporterAndSourceStats(ctx, commandPath, date, period, articles, filteredArticles, seenArticles, failed, showRaw, sendEmail, nil, sourceStats, watchSiteErrorNotices...)
}

func (app *app) renderBriefingContextWithReporter(ctx context.Context, commandPath string, date string, period string, articles []model.Article, filteredArticles []model.Article, seenArticles []model.Article, failed []fetcher.FailedSource, showRaw bool, sendEmail bool, reporter *scheduledRunReporter, watchSiteErrorNotices ...string) error {
	return app.renderBriefingContextWithReporterAndSourceStats(ctx, commandPath, date, period, articles, filteredArticles, seenArticles, failed, showRaw, sendEmail, reporter, nil, watchSiteErrorNotices...)
}

func (app *app) renderBriefingContextWithReporterAndSourceStats(ctx context.Context, commandPath string, date string, period string, articles []model.Article, filteredArticles []model.Article, seenArticles []model.Article, failed []fetcher.FailedSource, showRaw bool, sendEmail bool, reporter *scheduledRunReporter, sourceStats *model.SourceStatsReport, watchSiteErrorNotices ...string) error {
	articles = app.filterArticleImages(articles)
	filteredArticles = app.filterArticleImages(filteredArticles)
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
	var aiArticles []model.Article
	finalSelectedArticles := []model.Article(nil)
	seenArticlesToMark := seenArticles
	if !outputNeedsTranslatedContent(app.cfg.Output.Mode) {
		finalSelectedArticles = append([]model.Article(nil), articles...)
	} else {
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
		aiArticles = articles
		if structuredSummary != nil {
			finalSelectedArticles = briefingSelectedArticles(structuredSummary, aiArticles)
			seenArticlesToMark = eligibleSelectedArticles(finalSelectedArticles, seenArticles)
		} else if len(carryoverIDs(aiArticles)) > 0 {
			return fmt.Errorf("carryover requires structured briefing output")
		}
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

	if sourceStats != nil && sourceStats.SchemaVersion != "" {
		stats := *sourceStats
		stats.Date = date
		stats.Period = period
		stats.SetEnteredAI(aiArticles)
		stats.SetSelectedFinal(finalSelectedArticles)
		statsPath, err := output.WriteSourceStatsSidecar(stats, path)
		if err != nil {
			return err
		}
		logutil.Printf("Source stats saved: %s", statsPath)
	}

	if err := app.runPostMarkdownActions(ctx, briefing, path, sendEmail, failed, reporter); err != nil {
		return err
	}
	if app.fetch.markSeen != nil && len(seenArticlesToMark) > 0 {
		if err := runIfActive(ctx, func() error {
			return app.fetch.markSeen(seenArticlesToMark)
		}); err != nil {
			return fmt.Errorf("mark seen: %w", err)
		}
	}
	if ids := carryoverIDs(finalSelectedArticles); len(ids) > 0 {
		if err := app.carryoverStore().Consume(ctx, ids, path); err != nil {
			return fmt.Errorf("consume carryover: %w", err)
		}
		logutil.Printf("Carryover consumed: entries=%d output=%s", len(ids), path)
	}
	return nil
}

func briefingSelectedArticles(summary *model.BriefingSummary, articles []model.Article) []model.Article {
	if summary == nil || len(articles) == 0 {
		return nil
	}
	seen := make(map[int]struct{})
	selected := make([]model.Article, 0)
	for _, story := range summary.Stories {
		for _, id := range story.SourceArticleIDs {
			index := id - 1
			if index < 0 || index >= len(articles) {
				continue
			}
			if _, ok := seen[index]; ok {
				continue
			}
			seen[index] = struct{}{}
			selected = append(selected, articles[index])
		}
	}
	return selected
}

func eligibleSelectedArticles(selected []model.Article, eligible []model.Article) []model.Article {
	if len(selected) == 0 || len(eligible) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(eligible))
	for _, article := range eligible {
		allowed[briefingArticleIdentity(article)] = struct{}{}
	}
	seen := make(map[string]struct{}, len(selected))
	result := make([]model.Article, 0, len(selected))
	for _, article := range selected {
		key := briefingArticleIdentity(article)
		if _, ok := allowed[key]; !ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, article)
	}
	return result
}

func briefingArticleIdentity(article model.Article) string {
	if link := strings.TrimSpace(article.Link); link != "" {
		return "link:" + link
	}
	return strings.Join([]string{
		"fallback",
		strings.TrimSpace(article.Source),
		strings.TrimSpace(article.Title),
		article.Published.UTC().Format(time.RFC3339Nano),
	}, "\x00")
}

func (app *app) runPostMarkdownActions(ctx context.Context, briefing *model.Briefing, markdownPath string, sendEmail bool, _ []fetcher.FailedSource, reporter *scheduledRunReporter) error {
	hookDone := make(chan error, 1)
	publishDedupeKey := ""
	if reporter != nil {
		publishDedupeKey = "news-briefing:" + scheduledRunID(reporter.window)
		reporter.updateFields("post_actions", map[string]string{"publish_dedupe_key": publishDedupeKey})
	}
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
				PublishDedupeKey: publishDedupeKey,
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
			reporter.markEmailSent()
			logutil.Printf("Email sent to configured recipient(s)")
		}
	}

	hookErr := <-hookDone
	if hookErr != nil {
		logutil.Errorf("publish hook failed: %v", hookErr)
		if app.cfg.PublishHook.EffectiveFailurePolicy() == config.PublishHookFailurePolicyWarn {
			hookErr = nil
		}
	}
	return errors.Join(emailErr, hookErr)
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
			logutil.Printf("Email sent to configured recipient(s)")
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
		publishedAt := item.PublishedAt
		if publishedAt.IsZero() {
			publishedAt = item.DetectedAt
		}
		if !publishedAt.After(from) || publishedAt.After(to) {
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
	publishedAt := item.PublishedAt
	if publishedAt.IsZero() {
		publishedAt = item.DetectedAt
	}
	return model.Article{
		Title:      item.Title,
		Link:       item.URL,
		Summary:    summary,
		Source:     item.Source + " Watch",
		SourceRole: model.SourceRolePrimary,
		Category:   item.BriefingCategory,
		Published:  publishedAt,
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
	watchNotices := make([]string, 0, len(notices))
	recoveries := make([]string, 0, len(notices))
	for _, notice := range notices {
		if strings.HasPrefix(notice, "来源恢复：") {
			recoveries = append(recoveries, notice)
		} else {
			watchNotices = append(watchNotices, notice)
		}
	}
	if len(watchNotices) > 0 {
		b.WriteString("## Watch 站点异常\n\n")
	}
	for _, notice := range watchNotices {
		b.WriteString("- ")
		b.WriteString(notice)
		b.WriteString("\n")
	}
	if len(recoveries) > 0 {
		if len(watchNotices) > 0 {
			b.WriteString("\n")
		}
		b.WriteString("## 来源恢复\n\n")
		for _, notice := range recoveries {
			b.WriteString("- ")
			b.WriteString(strings.TrimPrefix(notice, "来源恢复："))
			b.WriteString(" 已恢复正常\n")
		}
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
