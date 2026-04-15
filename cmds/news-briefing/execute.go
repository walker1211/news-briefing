package main

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/output"
	"github.com/walker1211/news-briefing/internal/scheduler"
	"github.com/walker1211/news-briefing/internal/summarizer"
	"github.com/walker1211/news-briefing/internal/watch"
)

type app struct {
	cfg                 *config.Config
	now                 func() time.Time
	startCron           func(*config.Config, func(scheduler.Window)) error
	waitForever         func()
	fetchAll            func(*config.Config, bool) ([]model.Article, []fetcher.FailedSource, error)
	fetchWindow         func(*config.Config, time.Time, time.Time, bool, bool) ([]model.Article, []fetcher.FailedSource, error)
	fetchWatch          func(*config.Config, time.Time) ([]model.Article, *model.WatchReport, error)
	summarize           func([]model.Article, []string, *time.Location) (string, error)
	translate           func([]model.Article, []string, *time.Location) (string, error)
	deepDive            func(string, []model.Article, *time.Location) (string, error)
	composeBody         func(string, model.OutputMode, model.OutputContent) (string, error)
	printText           func(string)
	printFailed         func([]fetcher.FailedSource)
	printArticles       func([]model.Article)
	printCLI            func(*model.Briefing)
	writeMarkdown       func(*model.Briefing, string) (string, error)
	writeWatchMarkdown  func(*model.WatchReport, string, string, string) (string, error)
	sendEmail           func(*model.Briefing, *config.Config, []fetcher.FailedSource) error
	sendDeepEmail       func(string, *model.Briefing, *config.Config, []fetcher.FailedSource) error
	resendMarkdownEmail func(string, *config.Config) error
	writeDeepDive       func(string, string, string, string) (string, error)
}

func newApp(cfg *config.Config) *app {
	runner := summarizer.NewRunner(cfg.AI.Command, cfg.AI.Args, cfg.AI.ExtraFlags, cfg.AI.ShouldAppendSystemPrompt(), cfg.Proxy.HTTP, cfg.Proxy.Socks5)
	return &app{
		cfg:       cfg,
		now:       time.Now,
		startCron: scheduler.Start,
		waitForever: func() {
			select {}
		},
		fetchAll:    fetcher.FetchAll,
		fetchWindow: fetcher.FetchWindow,
		fetchWatch:  watch.Run,
		summarize:   runner.Summarize,
		translate:   runner.Translate,
		deepDive:    runner.DeepDive,
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
		printCLI:            output.PrintCLI,
		writeMarkdown:       output.WriteMarkdown,
		writeWatchMarkdown:  output.WriteWatchMarkdown,
		sendEmail:           output.SendEmail,
		sendDeepEmail:       output.SendDeepEmail,
		resendMarkdownEmail: output.SendMarkdownFile,
		writeDeepDive:       output.WriteDeepDive,
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
	switch c := cmd.(type) {
	case runCommand:
		return app.runBriefing("run", currentPeriod(), c.raw, !c.noEmail)
	case regenCommand:
		return app.runRegen(c)
	case fetchCommand:
		return app.runFetch(c)
	case serveCommand:
		fmt.Println("Starting news aggregator in scheduled mode...")
		if err := app.startCron(app.cfg, func(window scheduler.Window) {
			if err := app.runScheduledBriefing(window, true); err != nil {
				fmt.Fprintf(os.Stderr, "scheduled run failed: %v\n", err)
			}
		}); err != nil {
			return err
		}
		if app.waitForever != nil {
			app.waitForever()
		}
		return nil
	case deepCommand:
		return app.runDeepDive(c)
	case resendMDCommand:
		if app.resendMarkdownEmail == nil {
			app.resendMarkdownEmail = output.SendMarkdownFile
		}
		if err := app.resendMarkdownEmail(c.file, app.cfg); err != nil {
			return err
		}
		app.printText(fmt.Sprintf("Email resent to %s", app.cfg.Email.To))
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

func (app *app) runFetch(cmd fetchCommand) error {
	fmt.Println("Fetching news...")
	articles, failed, err := app.fetchAll(app.cfg, false)
	if err != nil {
		return err
	}
	fmt.Printf("Found %d articles after filtering.\n\n", len(articles))
	if app.composeBody == nil {
		app.composeBody = output.FormatBody
	}
	if app.printText == nil {
		app.printText = func(s string) { fmt.Println(s) }
	}

	if !cmd.zh {
		app.printArticles(articles)
		app.printFailed(failed)
		return nil
	}
	if len(articles) == 0 {
		app.printFailed(failed)
		return nil
	}

	categoryOrder := categoryOrderFromSources(app.cfg.Sources)
	content := model.OutputContent{
		Original: output.GroupedArticleListView(articles, categoryOrder, app.displayLocation()),
	}
	if outputNeedsTranslatedContent(app.cfg.Output.Mode) {
		fmt.Println("Translating with AI CLI...")
		translated, err := app.translate(articles, categoryOrder, app.displayLocation())
		if err != nil {
			return err
		}
		content.Translated = translated
	}
	body, err := app.composeBody("fetch --zh", app.cfg.Output.Mode, content)
	if err != nil {
		return err
	}
	fmt.Println()
	app.printText(body)
	app.printFailed(failed)
	return nil
}

func (app *app) runBriefing(commandPath string, period string, showRaw bool, sendEmail bool) error {
	fmt.Println("Fetching news...")
	if app.fetchAll == nil {
		app.fetchAll = fetcher.FetchAll
	}
	now := app.currentTime()
	articles, failed, err := app.fetchAll(app.cfg, true)
	if err != nil {
		return err
	}
	if app.fetchWatch != nil {
		watchArticles, report, err := app.fetchWatch(app.cfg, now)
		if err != nil {
			return err
		}
		articles = append(articles, watchArticles...)
		if app.writeWatchMarkdown != nil && report != nil {
			if _, err := app.writeWatchMarkdown(report, app.cfg.Output.Dir, now.Format("06.01.02"), period); err != nil {
				return err
			}
		}
	}
	return app.renderBriefing(commandPath, now.Format("06.01.02"), period, articles, failed, showRaw, sendEmail)
}

func (app *app) runScheduledBriefing(window scheduler.Window, sendEmail bool) error {
	fmt.Println("Fetching news...")
	if app.fetchWindow == nil {
		app.fetchWindow = fetcher.FetchWindow
	}
	articles, failed, err := app.fetchWindow(app.cfg, window.From, window.To, true, false)
	if err != nil {
		return err
	}
	loc := app.cfg.ScheduleLocation
	if loc == nil {
		loc = time.Local
	}
	if app.fetchWatch != nil {
		watchArticles, report, err := app.fetchWatch(app.cfg, window.To)
		if err != nil {
			return err
		}
		articles = append(articles, watchArticles...)
		if app.writeWatchMarkdown != nil && report != nil {
			if _, err := app.writeWatchMarkdown(report, app.cfg.Output.Dir, window.To.In(loc).Format("06.01.02"), window.Period); err != nil {
				return err
			}
		}
	}
	return app.renderBriefing("serve", window.To.In(loc).Format("06.01.02"), window.Period, articles, failed, false, sendEmail)
}

func (app *app) runRegen(cmd regenCommand) error {
	loc := time.Local
	if app.cfg != nil && app.cfg.ScheduleLocation != nil {
		loc = app.cfg.ScheduleLocation
	}
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

	fmt.Printf("Fetching news for window %s ~ %s...\n", from.Format("2006-01-02 15:04"), to.Format("2006-01-02 15:04"))
	if app.fetchWindow == nil {
		app.fetchWindow = fetcher.FetchWindow
	}
	articles, failed, err := app.fetchWindow(app.cfg, from, to, false, cmd.ignoreSeen)
	if err != nil {
		return err
	}
	return app.renderBriefing("regen", to.Format("06.01.02"), period, articles, failed, cmd.raw, cmd.sendEmail)
}

func (app *app) renderBriefing(commandPath string, date string, period string, articles []model.Article, failed []fetcher.FailedSource, showRaw bool, sendEmail bool) error {
	fmt.Printf("Found %d articles after filtering.\n", len(articles))
	app.printFailed(failed)
	if app.composeBody == nil {
		app.composeBody = output.FormatBody
	}

	if showRaw {
		fmt.Println("\n--- Raw Articles ---")
		app.printArticles(articles)
		fmt.Println("--- End Raw Articles ---")
		fmt.Println()
	}

	categoryOrder := categoryOrderFromSources(app.cfg.Sources)
	content := model.OutputContent{
		Original: output.GroupedArticleListView(articles, categoryOrder, app.displayLocation()),
	}
	summary := ""
	if outputNeedsTranslatedContent(app.cfg.Output.Mode) {
		fmt.Println("Generating summary with AI CLI...")
		var err error
		summary, err = app.summarize(articles, categoryOrder, app.displayLocation())
		if err != nil {
			return err
		}
		content.Translated = summary
	}
	body, err := app.composeBody(commandPath, app.cfg.Output.Mode, content)
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

	app.printCLI(briefing)

	path, err := app.writeMarkdown(briefing, app.cfg.Output.Dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing markdown: %v\n", err)
	} else {
		fmt.Printf("Markdown saved: %s\n", path)
	}

	if !sendEmail {
		fmt.Println("Skipping email")
		return nil
	}
	if err := app.sendEmail(briefing, app.cfg, failed); err != nil {
		fmt.Fprintf(os.Stderr, "Error sending email: %v\n", err)
	} else {
		fmt.Printf("Email sent to %s\n", app.cfg.Email.To)
	}
	return nil
}

func (app *app) runDeepDive(cmd deepCommand) error {
	fmt.Printf("Deep diving into: %s\n", cmd.topic)

	var (
		articles     []model.Article
		failed       []fetcher.FailedSource
		err          error
		briefingDate = app.currentTime().In(app.displayLocation()).Format("06.01.02")
	)
	if cmd.fromRaw != "" || cmd.toRaw != "" {
		loc := time.Local
		if app.cfg != nil && app.cfg.ScheduleLocation != nil {
			loc = app.cfg.ScheduleLocation
		}
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
		briefingDate = to.In(app.displayLocation()).Format("06.01.02")
		articles, failed, err = app.fetchWindow(app.cfg, from, to, false, cmd.ignoreSeen)
	} else if cmd.ignoreSeen {
		to := app.currentTime()
		from := to.Add(-12 * time.Hour)
		articles, failed, err = app.fetchWindow(app.cfg, from, to, false, true)
	} else {
		articles, failed, err = app.fetchAll(app.cfg, false)
	}
	if err != nil {
		return err
	}
	app.printFailed(failed)
	if app.composeBody == nil {
		app.composeBody = output.FormatBody
	}
	if app.printText == nil {
		app.printText = func(s string) { fmt.Println(s) }
	}

	relevant, err := selectDeepDiveArticles(cmd.topic, articles)
	if err != nil {
		return err
	}

	formattedContent := model.OutputContent{
		Original: output.ArticleListView(relevant, app.displayLocation()),
	}
	if outputNeedsTranslatedContent(app.cfg.Output.Mode) {
		fmt.Printf("Found %d relevant articles. Generating deep dive...\n", len(relevant))
		content, err := app.deepDive(cmd.topic, relevant, app.displayLocation())
		if err != nil {
			return err
		}
		if looksLikeInteractiveFollowUp(content) {
			return fmt.Errorf("deep dive returned interactive follow-up instead of final content")
		}
		formattedContent.Translated = content
	} else {
		fmt.Printf("Found %d relevant articles.\n", len(relevant))
	}

	body, err := app.composeBody("deep", app.cfg.Output.Mode, formattedContent)
	if err != nil {
		return err
	}

	briefing := &model.Briefing{
		Date:       briefingDate,
		Articles:   relevant,
		RawContent: body,
	}

	path, err := app.writeDeepDive(cmd.topic, body, app.cfg.Output.Dir, briefing.Date)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing deep dive: %v\n", err)
	} else {
		fmt.Printf("Deep dive saved: %s\n", path)
	}

	if cmd.sendEmail {
		if app.sendDeepEmail == nil {
			app.sendDeepEmail = output.SendDeepEmail
		}
		if err := app.sendDeepEmail(cmd.topic, briefing, app.cfg, failed); err != nil {
			fmt.Fprintf(os.Stderr, "Error sending email: %v\n", err)
		} else {
			fmt.Printf("Email sent to %s\n", app.cfg.Email.To)
		}
	}

	fmt.Println()
	app.printText(body)
	return nil
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
