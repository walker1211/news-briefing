package main

import (
	"context"
	"fmt"
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
	cfg       *config.Config
	now       func() time.Time
	scheduler schedulerDeps
	fetch     fetchDeps
	watch     watchDeps
	ai        aiDeps
	output    outputDeps
	email     emailDeps
}

type schedulerDeps struct {
	startCron          func(*config.Config, func(scheduler.Window)) error
	startCronContext   func(context.Context, *config.Config, func(scheduler.Window)) error
	waitForever        func()
	waitForeverContext func(context.Context)
}

type fetchDeps struct {
	fetchAll           func(*config.Config, bool) ([]model.Article, []fetcher.FailedSource, error)
	fetchAllContext    func(context.Context, *config.Config, bool) ([]model.Article, []fetcher.FailedSource, error)
	fetchWindow        func(*config.Config, time.Time, time.Time, bool, bool) ([]model.Article, []fetcher.FailedSource, error)
	fetchWindowContext func(context.Context, *config.Config, time.Time, time.Time, bool, bool) ([]model.Article, []fetcher.FailedSource, error)
	markSeen           func([]model.Article) error
}

type watchDeps struct {
	fetchWatch        func(*config.Config, time.Time) ([]model.Article, *model.WatchReport, error)
	fetchWatchContext func(context.Context, *config.Config, time.Time) ([]model.Article, *model.WatchReport, error)
}

type aiDeps struct {
	summarize        func([]model.Article, []string, *time.Location) (string, error)
	summarizeContext func(context.Context, []model.Article, []string, *time.Location) (string, error)
	translate        func([]model.Article, []string, *time.Location) (string, error)
	translateContext func(context.Context, []model.Article, []string, *time.Location) (string, error)
	deepDive         func(string, []model.Article, *time.Location) (string, error)
	deepDiveContext  func(context.Context, string, []model.Article, *time.Location) (string, error)
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
}

func newApp(cfg *config.Config) *app {
	httpClient := fetcher.NewHTTPClient(cfg.Proxy)
	fetchClient := fetcher.NewClient(httpClient)
	watchRunner := watch.NewRunner(httpClient)
	aiRunner := summarizer.NewRunner(cfg.AI.Command, cfg.AI.Args, cfg.AI.ExtraFlags, cfg.AI.ShouldAppendSystemPrompt(), cfg.Proxy.HTTP, cfg.Proxy.Socks5)
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
			fetchAll:           fetchClient.FetchAll,
			fetchAllContext:    fetchClient.FetchAllContext,
			fetchWindow:        fetchClient.FetchWindow,
			fetchWindowContext: fetchClient.FetchWindowContext,
			markSeen: func(articles []model.Article) error {
				return fetcher.MarkArticlesSeen(cfg.Output.Dir, articles)
			},
		},
		watch: watchDeps{
			fetchWatch:        watchRunner.Run,
			fetchWatchContext: watchRunner.RunContext,
		},
		ai: aiDeps{
			summarize:        aiRunner.Summarize,
			summarizeContext: aiRunner.SummarizeContext,
			translate:        aiRunner.Translate,
			translateContext: aiRunner.TranslateContext,
			deepDive:         aiRunner.DeepDive,
			deepDiveContext:  aiRunner.DeepDiveContext,
		},
		output: outputDeps{
			composeBody: output.FormatBody,
			printText:   func(s string) { fmt.Println(s) },
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
		},
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
	switch c := cmd.(type) {
	case runCommand:
		return app.runBriefingContext(ctx, "run", currentPeriod(), c.raw, !c.noEmail)
	case regenCommand:
		return app.runRegenContext(ctx, c)
	case fetchCommand:
		return app.runFetchContext(ctx, c)
	case serveCommand:
		logutil.Println("Starting news aggregator in scheduled mode...")
		if err := app.startScheduler(ctx, app.cfg, func(window scheduler.Window) {
			if err := app.runScheduledBriefingContext(ctx, window, true); err != nil {
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
		if app.email.resendMarkdownEmail == nil {
			app.email.resendMarkdownEmail = output.SendMarkdownFile
		}
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

func (app *app) fetchWatchArticles(ctx context.Context, now time.Time) ([]model.Article, *model.WatchReport, error) {
	if app.watch.fetchWatchContext != nil {
		return app.watch.fetchWatchContext(ctx, app.cfg, now)
	}
	if app.watch.fetchWatch != nil {
		return app.watch.fetchWatch(app.cfg, now)
	}
	return nil, nil, nil
}

func (app *app) summarizeArticles(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
	if app.ai.summarizeContext != nil {
		return app.ai.summarizeContext(ctx, articles, categoryOrder, loc)
	}
	if app.ai.summarize != nil {
		return app.ai.summarize(articles, categoryOrder, loc)
	}
	return summarizer.SummarizeContext(ctx, articles, categoryOrder, loc)
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

func (app *app) runFetchContext(ctx context.Context, cmd fetchCommand) error {
	logutil.Println("Fetching news...")
	articles, failed, err := app.fetchAllArticles(ctx, false)
	if err != nil {
		return err
	}
	logutil.Printf("Found %d articles after filtering.", len(articles))
	if app.output.composeBody == nil {
		app.output.composeBody = output.FormatBody
	}
	if app.output.printText == nil {
		app.output.printText = func(s string) { fmt.Println(s) }
	}

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
	articles, failed, err := app.fetchAllArticles(ctx, false)
	if err != nil {
		return err
	}
	seenArticles := append([]model.Article(nil), articles...)
	if app.watch.fetchWatchContext != nil || app.watch.fetchWatch != nil {
		watchArticles, report, err := app.fetchWatchArticles(ctx, now)
		if err != nil {
			return err
		}
		articles = append(articles, watchArticles...)
		app.printWatchSiteErrors(report)
		if app.output.writeWatchMarkdown != nil && report != nil {
			if _, err := app.output.writeWatchMarkdown(report, app.cfg.Output.Dir, now.Format("06.01.02"), period); err != nil {
				return err
			}
		}
	}
	return app.renderBriefingContext(ctx, commandPath, now.Format("06.01.02"), period, articles, seenArticles, failed, showRaw, sendEmail)
}

func (app *app) runScheduledBriefing(window scheduler.Window, sendEmail bool) error {
	return app.runScheduledBriefingContext(context.Background(), window, sendEmail)
}

func (app *app) runScheduledBriefingContext(ctx context.Context, window scheduler.Window, sendEmail bool) error {
	logutil.Println("Fetching news...")
	articles, failed, err := app.fetchWindowArticles(ctx, window.From, window.To, false, false)
	if err != nil {
		return err
	}
	seenArticles := append([]model.Article(nil), articles...)
	loc := app.displayLocation()
	if app.watch.fetchWatchContext != nil || app.watch.fetchWatch != nil {
		watchArticles, report, err := app.fetchWatchArticles(ctx, window.To)
		if err != nil {
			return err
		}
		articles = append(articles, watchArticles...)
		app.printWatchSiteErrors(report)
		if app.output.writeWatchMarkdown != nil && report != nil {
			if _, err := app.output.writeWatchMarkdown(report, app.cfg.Output.Dir, window.To.In(loc).Format("06.01.02"), window.Period); err != nil {
				return err
			}
		}
	}
	return app.renderBriefingContext(ctx, "serve", window.To.In(loc).Format("06.01.02"), window.Period, articles, seenArticles, failed, false, sendEmail)
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

	logutil.Printf("Fetching news for window %s ~ %s...", from.Format("2006-01-02 15:04"), to.Format("2006-01-02 15:04"))
	articles, failed, err := app.fetchWindowArticles(ctx, from, to, false, cmd.ignoreSeen)
	if err != nil {
		return err
	}
	return app.renderBriefingContext(ctx, "regen", to.Format("06.01.02"), period, articles, nil, failed, cmd.raw, cmd.sendEmail)
}

func (app *app) renderBriefing(commandPath string, date string, period string, articles []model.Article, seenArticles []model.Article, failed []fetcher.FailedSource, showRaw bool, sendEmail bool) error {
	return app.renderBriefingContext(context.Background(), commandPath, date, period, articles, seenArticles, failed, showRaw, sendEmail)
}

func (app *app) renderBriefingContext(ctx context.Context, commandPath string, date string, period string, articles []model.Article, seenArticles []model.Article, failed []fetcher.FailedSource, showRaw bool, sendEmail bool) error {
	logutil.Printf("Found %d articles after filtering.", len(articles))
	app.output.printFailed(failed)
	if app.output.composeBody == nil {
		app.output.composeBody = output.FormatBody
	}

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
	if outputNeedsTranslatedContent(app.cfg.Output.Mode) {
		logutil.Println("Generating summary with AI CLI...")
		var err error
		summary, err = app.summarizeArticles(ctx, articles, categoryOrder, app.displayLocation())
		if err != nil {
			return err
		}
		content.Translated = summary
	}
	body, err := app.output.composeBody(commandPath, app.cfg.Output.Mode, content)
	if err != nil {
		return err
	}

	briefing := &model.Briefing{
		Date:       date,
		Period:     period,
		Articles:   articles,
		Summary:    summary,
		RawContent: body,
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

	if !sendEmail {
		logutil.Println("Skipping email")
		return nil
	}
	if err := runIfActive(ctx, func() error {
		return app.email.sendEmail(briefing, app.cfg, failed)
	}); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		logutil.Errorf("Error sending email: %v", err)
	} else {
		logutil.Printf("Email sent to %s", app.cfg.Email.To)
	}
	return nil
}

func (app *app) runDeepDive(cmd deepCommand) error {
	return app.runDeepDiveContext(context.Background(), cmd)
}

func (app *app) runDeepDiveContext(ctx context.Context, cmd deepCommand) error {
	logutil.Printf("Deep diving into: %s", cmd.topic)

	var (
		articles     []model.Article
		failed       []fetcher.FailedSource
		err          error
		briefingDate = app.currentTime().In(app.displayLocation()).Format("06.01.02")
	)
	windowTo := app.currentTime()
	windowFrom := windowTo.Add(-12 * time.Hour)
	if cmd.fromRaw != "" || cmd.toRaw != "" {
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
		windowFrom = from
		windowTo = to
		briefingDate = to.In(app.displayLocation()).Format("06.01.02")
		articles, failed, err = app.fetchWindowArticles(ctx, from, to, false, cmd.ignoreSeen)
	} else if cmd.ignoreSeen {
		to := app.currentTime()
		from := to.Add(-12 * time.Hour)
		windowFrom = from
		windowTo = to
		articles, failed, err = app.fetchWindowArticles(ctx, from, to, false, true)
	} else {
		articles, failed, err = app.fetchAllArticles(ctx, false)
	}
	if err != nil {
		return err
	}
	watchArticles, err := loadWatchSeenArticles(app.cfg.Output.Dir, windowFrom, windowTo)
	if err != nil {
		return err
	}
	articles = append(articles, watchArticles...)
	app.output.printFailed(failed)
	if app.output.composeBody == nil {
		app.output.composeBody = output.FormatBody
	}
	if app.output.printText == nil {
		app.output.printText = func(s string) { fmt.Println(s) }
	}

	relevant, err := selectDeepDiveArticles(cmd.topic, articles)
	if err != nil {
		return err
	}

	formattedContent := model.OutputContent{
		Original: output.ArticleListView(relevant, app.displayLocation()),
	}
	if outputNeedsTranslatedContent(app.cfg.Output.Mode) {
		logutil.Printf("Found %d relevant articles. Generating deep dive...", len(relevant))
		content, err := app.deepDiveArticles(ctx, cmd.topic, relevant, app.displayLocation())
		if err != nil {
			return err
		}
		if looksLikeInteractiveFollowUp(content) {
			return fmt.Errorf("deep dive returned interactive follow-up instead of final content")
		}
		formattedContent.Translated = content
	} else {
		logutil.Printf("Found %d relevant articles.", len(relevant))
	}

	body, err := app.output.composeBody("deep", app.cfg.Output.Mode, formattedContent)
	if err != nil {
		return err
	}

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
		if app.email.sendDeepEmail == nil {
			app.email.sendDeepEmail = output.SendDeepEmail
		}
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

func (app *app) printWatchSiteErrors(report *model.WatchReport) {
	if report == nil || app.output.printText == nil {
		return
	}
	for _, event := range report.Events {
		if event.EventType != "site_error" {
			continue
		}
		app.output.printText(fmt.Sprintf("Watch 站点异常：%s — %s", event.Source, event.Reason))
	}
}
