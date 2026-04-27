package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/output"
	"github.com/walker1211/news-briefing/internal/scheduler"
)

func TestNewAppWiresInstanceDependencies(t *testing.T) {
	cfg := executeTestConfig(t, model.OutputModeOriginalOnly)
	app := newApp(cfg)

	funcs := map[string]any{
		"scheduler.startCron":              app.scheduler.startCron,
		"scheduler.startCronContext":       app.scheduler.startCronContext,
		"fetch.fetchAll":                   app.fetch.fetchAll,
		"fetch.fetchAllContext":            app.fetch.fetchAllContext,
		"fetch.fetchAllDetailedContext":    app.fetch.fetchAllDetailedContext,
		"fetch.fetchWindow":                app.fetch.fetchWindow,
		"fetch.fetchWindowContext":         app.fetch.fetchWindowContext,
		"fetch.fetchWindowDetailedContext": app.fetch.fetchWindowDetailedContext,
		"fetch.markSeen":                   app.fetch.markSeen,
		"watch.fetchWatch":                 app.watch.fetchWatch,
		"watch.fetchWatchContext":          app.watch.fetchWatchContext,
		"ai.summarize":                     app.ai.summarize,
		"ai.summarizeContext":              app.ai.summarizeContext,
		"ai.translate":                     app.ai.translate,
		"ai.translateContext":              app.ai.translateContext,
		"ai.deepDive":                      app.ai.deepDive,
		"ai.deepDiveContext":               app.ai.deepDiveContext,
		"output.composeBody":               app.output.composeBody,
		"output.printText":                 app.output.printText,
		"output.printFailed":               app.output.printFailed,
		"output.printArticles":             app.output.printArticles,
		"output.printCLI":                  app.output.printCLI,
		"output.writeMarkdown":             app.output.writeMarkdown,
		"output.writeWatchMarkdown":        app.output.writeWatchMarkdown,
		"output.writeDeepDive":             app.output.writeDeepDive,
		"email.sendEmail":                  app.email.sendEmail,
		"email.sendDeepEmail":              app.email.sendDeepEmail,
		"email.resendMarkdownEmail":        app.email.resendMarkdownEmail,
	}
	for name, fn := range funcs {
		if reflect.ValueOf(fn).IsNil() {
			t.Fatalf("newApp did not wire %s", name)
		}
	}
}

func TestNewAppMarkSeenUsesConfiguredOutputDir(t *testing.T) {
	cfg := executeTestConfig(t, model.OutputModeOriginalOnly)
	app := newApp(cfg)

	if err := app.fetch.markSeen(sampleExecuteArticles()); err != nil {
		t.Fatalf("markSeen() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.Output.Dir, "state", "seen.json"))
	if err != nil {
		t.Fatalf("ReadFile(seen.json) error = %v", err)
	}
	if !strings.Contains(string(data), "https://example.com/news") {
		t.Fatalf("seen.json = %s", data)
	}
}

func TestNewAppWaitContextHonorsCancellation(t *testing.T) {
	app := newApp(executeTestConfig(t, model.OutputModeOriginalOnly))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		app.scheduler.waitForeverContext(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waitForeverContext did not return after context cancellation")
	}
}

func TestExecuteServeUsesScheduler(t *testing.T) {
	t.Setenv("EMAIL_SMTP_AUTH_CODE", "test")
	called := false
	waited := false
	app := &app{
		cfg: executeTestConfigWithEmail(t, model.OutputModeOriginalOnly),
		scheduler: schedulerDeps{
			startCronContext: func(ctx context.Context, cfg *config.Config, run func(scheduler.Window)) error {
				called = true
				return nil
			},
			waitForeverContext: func(ctx context.Context) {
				waited = true
			},
		},
	}

	if err := execute(app, serveCommand{}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if !called {
		t.Fatalf("startCron was not called")
	}
	if !waited {
		t.Fatalf("serve command did not wait after starting scheduler")
	}
}

func TestExecuteServeDoesNotExitProcessOnScheduledRunError(t *testing.T) {
	if os.Getenv("NEWS_SERVE_SCHEDULED_ERROR_SUBPROCESS") == "1" {
		window := scheduler.Window{Period: "0800", From: time.Date(2026, 3, 18, 7, 0, 0, 0, time.UTC), To: time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC)}
		app := &app{
			cfg: &config.Config{},
			scheduler: schedulerDeps{
				startCronContext: func(ctx context.Context, cfg *config.Config, run func(scheduler.Window)) error {
					run(window)
					return nil
				},
				waitForeverContext: func(ctx context.Context) {},
			},
			fetch: fetchDeps{
				fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
					return nil, nil, errors.New("boom")
				},
			},
		}
		_ = execute(app, serveCommand{})
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestExecuteServeDoesNotExitProcessOnScheduledRunError")
	cmd.Env = append(os.Environ(), "NEWS_SERVE_SCHEDULED_ERROR_SUBPROCESS=1")
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			t.Fatalf("scheduled serve error exited process with code 1")
		}
		t.Fatalf("subprocess error = %v", err)
	}
}

func TestExecuteContextFetchPassesContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), contextTestKey{}, "fetch")
	called := false
	app := &app{
		cfg: executeTestConfig(t, model.OutputModeOriginalOnly),
		fetch: fetchDeps{
			fetchAllContext: func(got context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				called = got.Value(contextTestKey{}) == "fetch"
				return nil, nil, nil
			},
		},
		output: silentOutputDeps(),
	}

	if err := executeContext(ctx, app, fetchCommand{}); err != nil {
		t.Fatalf("executeContext() error = %v", err)
	}
	if !called {
		t.Fatal("fetchAllContext() did not receive execute context")
	}
}

func TestExecuteContextServePassesContextToSchedulerAndRun(t *testing.T) {
	t.Setenv("EMAIL_SMTP_AUTH_CODE", "test")
	ctx := context.WithValue(context.Background(), contextTestKey{}, "serve")
	window := scheduler.Window{Period: "0800", From: time.Date(2026, 3, 18, 7, 0, 0, 0, time.UTC), To: time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC)}
	startCtxOK := false
	fetchCtxOK := false
	waitCtxOK := false
	app := &app{
		cfg: executeTestConfigWithEmail(t, model.OutputModeOriginalOnly),
		scheduler: schedulerDeps{
			startCronContext: func(got context.Context, cfg *config.Config, run func(scheduler.Window)) error {
				startCtxOK = got.Value(contextTestKey{}) == "serve"
				run(window)
				return nil
			},
			waitForeverContext: func(got context.Context) {
				waitCtxOK = got.Value(contextTestKey{}) == "serve"
			},
		},
		fetch: fetchDeps{
			fetchWindowContext: func(got context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				fetchCtxOK = got.Value(contextTestKey{}) == "serve"
				return sampleExecuteArticles(), nil, nil
			},
		},
		output: silentBriefingOutputDeps("ORIGINAL ONLY"),
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error { return nil },
		},
	}

	if err := executeContext(ctx, app, serveCommand{}); err != nil {
		t.Fatalf("executeContext() error = %v", err)
	}
	if !startCtxOK || !fetchCtxOK || !waitCtxOK {
		t.Fatalf("context propagation start=%v fetch=%v wait=%v", startCtxOK, fetchCtxOK, waitCtxOK)
	}
}

func TestRenderBriefingContextPassesContextToSummarize(t *testing.T) {
	ctx := context.WithValue(context.Background(), contextTestKey{}, "summarize")
	called := false
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeTranslatedOnly}},
		ai: aiDeps{
			summarizeContext: func(got context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				called = got.Value(contextTestKey{}) == "summarize"
				return "summary", nil
			},
		},
		output: silentBriefingOutputDeps("COMPOSED"),
	}

	if err := app.renderBriefingContext(ctx, "run", "26.03.27", "1400", sampleExecuteArticles(), nil, nil, nil, false, false); err != nil {
		t.Fatalf("renderBriefingContext() error = %v", err)
	}
	if !called {
		t.Fatal("summarizeContext() did not receive render context")
	}
}

func TestRenderBriefingContextStopsBeforeSideEffectsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	printed := false
	wrote := false
	marked := false
	emailed := false
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeTranslatedOnly}},
		fetch: fetchDeps{
			markSeen: func([]model.Article) error { marked = true; return nil },
		},
		ai: aiDeps{
			summarizeContext: func(context.Context, []model.Article, []string, *time.Location) (string, error) {
				cancel()
				return "summary", nil
			},
		},
		output: outputDeps{
			composeBody: func(string, model.OutputMode, model.OutputContent) (string, error) { return "COMPOSED", nil },
			printCLI:    func(*model.Briefing) { printed = true },
			writeMarkdown: func(*model.Briefing, string) (string, error) {
				wrote = true
				return "", nil
			},
			printFailed: func([]fetcher.FailedSource) {},
		},
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error { emailed = true; return nil },
		},
	}

	err := app.renderBriefingContext(ctx, "run", "26.03.27", "1400", sampleExecuteArticles(), nil, sampleExecuteArticles(), nil, false, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("renderBriefingContext() error = %v, want context.Canceled", err)
	}
	if printed || wrote || marked || emailed {
		t.Fatalf("side effects printed=%v wrote=%v marked=%v emailed=%v", printed, wrote, marked, emailed)
	}
}

func TestRenderBriefingContextStopsBeforeMarkSeenAndEmailWhenCancelledAfterWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	marked := false
	emailed := false
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetch: fetchDeps{
			markSeen: func([]model.Article) error { marked = true; return nil },
		},
		output: outputDeps{
			composeBody: func(string, model.OutputMode, model.OutputContent) (string, error) { return "COMPOSED", nil },
			printCLI:    func(*model.Briefing) {},
			writeMarkdown: func(*model.Briefing, string) (string, error) {
				cancel()
				return "output/test.md", nil
			},
			printFailed: func([]fetcher.FailedSource) {},
		},
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error { emailed = true; return nil },
		},
	}

	err := app.renderBriefingContext(ctx, "run", "26.03.27", "1400", sampleExecuteArticles(), nil, sampleExecuteArticles(), nil, false, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("renderBriefingContext() error = %v, want context.Canceled", err)
	}
	if marked || emailed {
		t.Fatalf("side effects marked=%v emailed=%v", marked, emailed)
	}
}

func TestRenderBriefingContextStopsBeforeEmailWhenCancelledAfterMarkSeen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	emailed := false
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetch: fetchDeps{
			markSeen: func([]model.Article) error {
				cancel()
				return nil
			},
		},
		output: outputDeps{
			composeBody:   func(string, model.OutputMode, model.OutputContent) (string, error) { return "COMPOSED", nil },
			printCLI:      func(*model.Briefing) {},
			writeMarkdown: func(*model.Briefing, string) (string, error) { return "output/test.md", nil },
			printFailed:   func([]fetcher.FailedSource) {},
		},
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error { emailed = true; return nil },
		},
	}

	err := app.renderBriefingContext(ctx, "run", "26.03.27", "1400", sampleExecuteArticles(), nil, sampleExecuteArticles(), nil, false, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("renderBriefingContext() error = %v, want context.Canceled", err)
	}
	if emailed {
		t.Fatal("sendEmail() should not run after context cancellation")
	}
}

func TestExecuteFetchTranslateUsesRunner(t *testing.T) {
	called := false
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Mode: model.OutputModeTranslatedOnly}},
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return []model.Article{{Title: "a"}}, nil, nil
			},
		},
		ai: aiDeps{
			translateContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				called = len(articles) == 1 && articles[0].Title == "a"
				return "ok", nil
			},
		},
		output: outputDeps{
			printArticles: func([]model.Article) {},
			printFailed:   func([]fetcher.FailedSource) {},
		},
	}

	if err := execute(app, fetchCommand{zh: true}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if !called {
		t.Fatalf("translate was not called with fetched articles")
	}
}

func TestExecuteRegenUsesParsedWindowAndFlags(t *testing.T) {
	t.Setenv("EMAIL_SMTP_AUTH_CODE", "test")
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"news-briefing", "regen", "--from", "bad", "--to", "bad"}

	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	from := time.Date(2026, 3, 18, 8, 0, 0, 0, loc)
	to := time.Date(2026, 3, 18, 14, 0, 0, 0, loc)

	called := false
	emailCalled := false
	app := &app{
		cfg: executeTestConfigWithEmail(t, model.OutputModeTranslatedOnly),
		fetch: fetchDeps{
			fetchWindowContext: func(ctx context.Context, cfg *config.Config, gotFrom, gotTo time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				called = gotFrom.Equal(from) && gotTo.Equal(to) && !markSeen && ignoreSeen
				return []model.Article{{Title: "a"}}, nil, nil
			},
		},
		ai: aiDeps{
			summarizeContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				return "summary", nil
			},
		},
		output: silentBriefingOutputDeps(""),
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error {
				emailCalled = true
				return nil
			},
		},
	}

	err = execute(app, regenCommand{fromRaw: "2026-03-18 08:00", toRaw: "2026-03-18 14:00", period: "1400", ignoreSeen: true, sendEmail: true})
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if !called {
		t.Fatalf("fetchWindow was not called with parsed regen arguments")
	}
	if !emailCalled {
		t.Fatalf("sendEmail was not called")
	}
}

func TestExecuteRegenParsesRawWindowInConfiguredTimezone(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	from := time.Date(2026, 3, 18, 8, 0, 0, 0, loc)
	to := time.Date(2026, 3, 18, 14, 0, 0, 0, loc)

	called := false
	app := &app{
		cfg: &config.Config{
			ScheduleLocation: loc,
			Output:           config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly},
		},
		fetch: fetchDeps{
			fetchWindowContext: func(ctx context.Context, cfg *config.Config, gotFrom, gotTo time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				called = gotFrom.Equal(from) && gotTo.Equal(to) && !markSeen && ignoreSeen
				return []model.Article{{Title: "a"}}, nil, nil
			},
		},
		output: silentBriefingOutputDeps(""),
	}

	if err := execute(app, regenCommand{fromRaw: "2026-03-18 08:00", toRaw: "2026-03-18 14:00", ignoreSeen: true}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if !called {
		t.Fatalf("fetchWindow was not called with raw regen window parsed in configured timezone")
	}
}

func TestExecuteRegenRejectsToBeforeFromAfterTimezoneParsing(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	app := &app{
		cfg: &config.Config{ScheduleLocation: loc},
	}
	if err := execute(app, regenCommand{fromRaw: "2026-03-18 14:00", toRaw: "2026-03-18 08:00"}); err == nil || !strings.Contains(err.Error(), "after or equal") {
		t.Fatalf("execute() error = %v, want to>=from validation after timezone parsing", err)
	}
}

func TestRenderBriefingUsesComposedBodyForRun(t *testing.T) {
	articles := sampleExecuteArticles()
	var gotPath string
	var gotMode model.OutputMode
	var gotContent model.OutputContent
	var printed *model.Briefing

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeBilingualOriginalFirst}},
		ai: aiDeps{
			summarizeContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				return "TRANSLATED", nil
			},
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				gotPath, gotMode, gotContent = path, mode, content
				return "COMPOSED", nil
			},
			printCLI:      func(b *model.Briefing) { printed = b },
			writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
			printFailed:   func([]fetcher.FailedSource) {},
		},
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error { return nil },
		},
	}

	err := app.renderBriefing("run", "26.03.27", "1400", articles, nil, nil, nil, false, false)
	if err != nil {
		t.Fatalf("renderBriefing() error = %v", err)
	}
	if gotPath != "run" {
		t.Fatalf("composeBody() path = %q, want %q", gotPath, "run")
	}
	if gotMode != model.OutputModeBilingualOriginalFirst {
		t.Fatalf("composeBody() mode = %q, want %q", gotMode, model.OutputModeBilingualOriginalFirst)
	}
	categoryOrder := []string{"AI/科技"}
	if gotContent.Original != output.GroupedArticleListView(articles, categoryOrder, time.Local) {
		t.Fatalf("composeBody() original = %q, want %q", gotContent.Original, output.GroupedArticleListView(articles, categoryOrder, time.Local))
	}
	if gotContent.Translated != "TRANSLATED" {
		t.Fatalf("composeBody() translated = %q, want %q", gotContent.Translated, "TRANSLATED")
	}
	if printed == nil {
		t.Fatal("printCLI() briefing = nil")
	}
	if printed.RawContent != "COMPOSED" {
		t.Fatalf("printCLI() RawContent = %q, want %q", printed.RawContent, "COMPOSED")
	}
}

func TestRenderBriefingAppendsFilteredArticlesWhenEnabled(t *testing.T) {
	articles := sampleExecuteArticles()
	filtered := []model.Article{{
		Title:     "Market update without keyword",
		Link:      "https://example.com/market",
		Summary:   "Market summary",
		Source:    "Example",
		Category:  "国际政治",
		Published: time.Date(2026, 3, 18, 13, 0, 0, 0, time.UTC),
	}}
	var printed *model.Briefing
	var written *model.Briefing
	var emailed *model.Briefing
	var summarized []model.Article

	app := &app{
		cfg: &config.Config{
			Sources: []config.Source{{Category: "AI/科技"}, {Category: "国际政治"}},
			Output:  config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeTranslatedOnly, IncludeFilteredArticles: true},
		},
		ai: aiDeps{
			summarizeContext: func(ctx context.Context, got []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				summarized = append([]model.Article(nil), got...)
				return "SUMMARY", nil
			},
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				return content.Translated, nil
			},
			printCLI: func(b *model.Briefing) { printed = b },
			writeMarkdown: func(b *model.Briefing, dir string) (string, error) {
				written = b
				return "", nil
			},
			printFailed: func([]fetcher.FailedSource) {},
		},
		email: emailDeps{
			sendEmail: func(b *model.Briefing, cfg *config.Config, failed []fetcher.FailedSource) error {
				emailed = b
				return nil
			},
		},
	}

	err := app.renderBriefing("run", "26.03.27", "1400", articles, filtered, nil, nil, false, true)
	if err != nil {
		t.Fatalf("renderBriefing() error = %v", err)
	}
	if printed == nil {
		t.Fatal("printCLI() briefing = nil")
	}
	if !strings.Contains(printed.RawContent, "SUMMARY") {
		t.Fatalf("RawContent = %q, want composed summary", printed.RawContent)
	}
	if !strings.Contains(printed.RawContent, "## 未命中关键词的候选新闻") {
		t.Fatalf("RawContent = %q, want filtered appendix heading", printed.RawContent)
	}
	if !strings.Contains(printed.RawContent, "Market update without keyword") {
		t.Fatalf("RawContent = %q, want filtered article", printed.RawContent)
	}
	for name, briefing := range map[string]*model.Briefing{
		"writeMarkdown": written,
		"sendEmail":     emailed,
	} {
		if briefing == nil {
			t.Fatalf("%s briefing = nil", name)
		}
		if !strings.Contains(briefing.RawContent, "## 未命中关键词的候选新闻") {
			t.Fatalf("%s RawContent = %q, want filtered appendix heading", name, briefing.RawContent)
		}
		if !strings.Contains(briefing.RawContent, "Market update without keyword") {
			t.Fatalf("%s RawContent = %q, want filtered article", name, briefing.RawContent)
		}
	}
	if len(summarized) != 1 || summarized[0].Title != "OpenAI ships feature" {
		t.Fatalf("summarized = %#v, want accepted articles only", summarized)
	}
}

func TestAppendFilteredArticlesAppendixAllowsNilConfig(t *testing.T) {
	filtered := []model.Article{{Title: "Market update without keyword"}}
	got := (&app{}).appendFilteredArticlesAppendix("BODY", filtered, []string{"国际政治"})
	if got != "BODY" {
		t.Fatalf("appendFilteredArticlesAppendix() = %q, want BODY", got)
	}
}

func TestAppendFilteredArticlesAppendixHandlesEmptyBody(t *testing.T) {
	filtered := []model.Article{{
		Title:    "Market update without keyword",
		Link:     "https://example.com/market",
		Summary:  "Market summary",
		Source:   "Example",
		Category: "国际政治",
	}}
	app := &app{cfg: &config.Config{Output: config.OutputCfg{IncludeFilteredArticles: true}}}

	got := app.appendFilteredArticlesAppendix("", filtered, []string{"国际政治"})
	if strings.HasPrefix(got, " ") || strings.HasPrefix(got, "\n") || strings.HasPrefix(got, "\x00") {
		t.Fatalf("appendFilteredArticlesAppendix() = %q, want no leading garbage", got)
	}
	if !strings.Contains(got, "## 未命中关键词的候选新闻") {
		t.Fatalf("appendFilteredArticlesAppendix() = %q, want filtered appendix heading", got)
	}
	if !strings.Contains(got, "Market update without keyword") {
		t.Fatalf("appendFilteredArticlesAppendix() = %q, want filtered article", got)
	}
}

func TestRenderBriefingDoesNotAppendFilteredArticlesWhenDisabled(t *testing.T) {
	articles := sampleExecuteArticles()
	filtered := []model.Article{{
		Title:     "Market update without keyword",
		Link:      "https://example.com/market",
		Summary:   "Market summary",
		Source:    "Example",
		Category:  "国际政治",
		Published: time.Date(2026, 3, 18, 13, 0, 0, 0, time.UTC),
	}}
	var printed *model.Briefing

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		output: outputDeps{
			composeBody:   func(string, model.OutputMode, model.OutputContent) (string, error) { return "BODY", nil },
			printCLI:      func(b *model.Briefing) { printed = b },
			writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
			printFailed:   func([]fetcher.FailedSource) {},
		},
	}

	err := app.renderBriefing("run", "26.03.27", "1400", articles, filtered, nil, nil, false, false)
	if err != nil {
		t.Fatalf("renderBriefing() error = %v", err)
	}
	if printed == nil {
		t.Fatal("printCLI() briefing = nil")
	}
	if strings.Contains(printed.RawContent, "未命中关键词的候选新闻") || strings.Contains(printed.RawContent, "Market update without keyword") {
		t.Fatalf("RawContent = %q, filtered appendix should be omitted", printed.RawContent)
	}
}

func TestRenderBriefingUsesComposedBodyForRegen(t *testing.T) {
	articles := sampleExecuteArticles()
	var gotPath string

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeTranslatedOnly}},
		ai: aiDeps{
			summarizeContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				return "TRANSLATED", nil
			},
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				gotPath = path
				return "COMPOSED", nil
			},
			printCLI:      func(*model.Briefing) {},
			writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
			printFailed:   func([]fetcher.FailedSource) {},
		},
	}

	err := app.renderBriefing("regen", "26.03.27", "1400", articles, nil, nil, nil, false, false)
	if err != nil {
		t.Fatalf("renderBriefing() error = %v", err)
	}
	if gotPath != "regen" {
		t.Fatalf("composeBody() path = %q, want %q", gotPath, "regen")
	}
}

func TestRunBriefingUsesFetchAll(t *testing.T) {
	articles := sampleExecuteArticles()
	fetchCalled := false

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				fetchCalled = true
				if markSeen {
					t.Fatalf("fetchAll() markSeen=%v, want false", markSeen)
				}
				return articles, nil, nil
			},
			markSeen: func([]model.Article) error { return nil },
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				return "ORIGINAL ONLY", nil
			},
			printCLI:      func(*model.Briefing) {},
			printFailed:   func([]fetcher.FailedSource) {},
			printArticles: func([]model.Article) {},
			writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
		},
	}

	if err := app.runBriefing("run", "1400", false, false); err != nil {
		t.Fatalf("runBriefing() error = %v", err)
	}
	if !fetchCalled {
		t.Fatal("fetchAll() was not called")
	}
}

func TestRunBriefingPrefersFetchAllDetailedContext(t *testing.T) {
	articles := sampleExecuteArticles()
	detailedCalled := false

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				t.Fatal("fetchAllContext() should not be called when detailed fetch is available")
				return nil, nil, nil
			},
			fetchAllDetailedContext: func(ctx context.Context, cfg *config.Config, markSeen bool) (fetcher.FetchResult, error) {
				detailedCalled = true
				if markSeen {
					t.Fatalf("fetchAllDetailedContext() markSeen=%v, want false", markSeen)
				}
				return fetcher.FetchResult{Articles: articles}, nil
			},
		},
		output: silentBriefingOutputDeps("ORIGINAL ONLY"),
	}

	if err := app.runBriefing("run", "1400", false, false); err != nil {
		t.Fatalf("runBriefing() error = %v", err)
	}
	if !detailedCalled {
		t.Fatal("fetchAllDetailedContext() was not called")
	}
}

func TestRunScheduledBriefingPrefersFetchWindowDetailedContext(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	window := scheduler.Window{
		Period: "0800",
		From:   time.Date(2026, 3, 18, 7, 0, 0, 0, loc),
		To:     time.Date(2026, 3, 18, 8, 0, 0, 0, loc),
	}
	detailedCalled := false

	app := &app{
		cfg: &config.Config{ScheduleLocation: loc, Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetch: fetchDeps{
			fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				t.Fatal("fetchWindowContext() should not be called when detailed fetch is available")
				return nil, nil, nil
			},
			fetchWindowDetailedContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) (fetcher.FetchResult, error) {
				detailedCalled = true
				if !from.Equal(window.From) || !to.Equal(window.To) || markSeen || ignoreSeen {
					t.Fatalf("fetchWindowDetailedContext() args from=%v to=%v markSeen=%v ignoreSeen=%v", from, to, markSeen, ignoreSeen)
				}
				return fetcher.FetchResult{Articles: sampleExecuteArticles()}, nil
			},
		},
		output: silentBriefingOutputDeps("ORIGINAL ONLY"),
	}

	if err := app.runScheduledBriefing(window, false); err != nil {
		t.Fatalf("runScheduledBriefing() error = %v", err)
	}
	if !detailedCalled {
		t.Fatal("fetchWindowDetailedContext() was not called")
	}
}

func TestRunRegenPrefersFetchWindowDetailedContextAndPreservesIgnoreSeen(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	from := time.Date(2026, 3, 18, 8, 0, 0, 0, loc)
	to := time.Date(2026, 3, 18, 14, 0, 0, 0, loc)
	detailedCalled := false

	app := &app{
		cfg: &config.Config{ScheduleLocation: loc, Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetch: fetchDeps{
			fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				t.Fatal("fetchWindowContext() should not be called when detailed fetch is available")
				return nil, nil, nil
			},
			fetchWindowDetailedContext: func(ctx context.Context, cfg *config.Config, gotFrom, gotTo time.Time, markSeen bool, ignoreSeen bool) (fetcher.FetchResult, error) {
				detailedCalled = true
				if !gotFrom.Equal(from) || !gotTo.Equal(to) || markSeen || !ignoreSeen {
					t.Fatalf("fetchWindowDetailedContext() args from=%v to=%v markSeen=%v ignoreSeen=%v", gotFrom, gotTo, markSeen, ignoreSeen)
				}
				return fetcher.FetchResult{Articles: sampleExecuteArticles()}, nil
			},
		},
		output: silentBriefingOutputDeps("ORIGINAL ONLY"),
	}

	if err := app.runRegen(regenCommand{fromRaw: "2026-03-18 08:00", toRaw: "2026-03-18 14:00", ignoreSeen: true}); err != nil {
		t.Fatalf("runRegen() error = %v", err)
	}
	if !detailedCalled {
		t.Fatal("fetchWindowDetailedContext() was not called")
	}
}

func TestFetchBriefingArticlesWithWatchCarriesFilteredArticlesAndMarksOnlyAcceptedMain(t *testing.T) {
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC)
	accepted := model.Article{Title: "accepted", Category: "AI/科技", Published: now.Add(-time.Hour)}
	filtered := model.Article{Title: "filtered", Category: "AI/科技", Published: now.Add(-2 * time.Hour)}
	watchArticle := model.Article{Title: "watch", Category: "AI/科技", Published: now}

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		watch: watchDeps{
			fetchWatchContext: func(ctx context.Context, cfg *config.Config, gotNow time.Time) ([]model.Article, *model.WatchReport, error) {
				if !gotNow.Equal(now) {
					t.Fatalf("fetchWatchContext() now=%v, want %v", gotNow, now)
				}
				return []model.Article{watchArticle}, nil, nil
			},
		},
	}

	result, err := app.fetchBriefingArticlesWithWatch(context.Background(), now, "26.04.15", "1600", func(ctx context.Context) (fetcher.FetchResult, error) {
		return fetcher.FetchResult{
			Articles:         []model.Article{accepted},
			FilteredArticles: []model.Article{filtered},
		}, nil
	})
	if err != nil {
		t.Fatalf("fetchBriefingArticlesWithWatch() error = %v", err)
	}
	if !reflect.DeepEqual(result.filteredArticles, []model.Article{filtered}) {
		t.Fatalf("filteredArticles = %#v, want %#v", result.filteredArticles, []model.Article{filtered})
	}
	if !reflect.DeepEqual(result.seenArticles, []model.Article{accepted}) {
		t.Fatalf("seenArticles = %#v, want only accepted main article", result.seenArticles)
	}
	if !reflect.DeepEqual(result.articles, []model.Article{accepted, watchArticle}) {
		t.Fatalf("articles = %#v, want accepted plus watch article", result.articles)
	}
}

func TestRunBriefingSkipsMarkSeenWhenSummarizeFails(t *testing.T) {
	outputDir := t.TempDir()
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: outputDir, Mode: model.OutputModeTranslatedOnly}},
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				if markSeen {
					t.Fatalf("fetchAll() markSeen=%v, want false", markSeen)
				}
				return sampleExecuteArticles(), nil, nil
			},
			markSeen: func(articles []model.Article) error {
				return fetcher.MarkArticlesSeen(outputDir, articles)
			},
		},
		ai: aiDeps{
			summarizeContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				return "", errors.New("ai cli failed")
			},
		},
		output: silentBriefingOutputDeps(""),
	}

	err := app.runBriefing("run", "0700", false, false)
	if err == nil || !strings.Contains(err.Error(), "ai cli failed") {
		t.Fatalf("runBriefing() error = %v, want ai cli failed", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "state", "seen.json")); !os.IsNotExist(err) {
		t.Fatalf("seen.json exists after failed summarize, err=%v", err)
	}
}

func TestRunBriefingMarksSeenAfterWriteMarkdownSucceeds(t *testing.T) {
	articles := sampleExecuteArticles()
	marked := false
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeTranslatedOnly}},
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				if markSeen {
					t.Fatalf("fetchAll() markSeen=%v, want false", markSeen)
				}
				return articles, nil, nil
			},
			markSeen: func(got []model.Article) error {
				marked = true
				if len(got) != len(articles) || got[0].Link != articles[0].Link {
					t.Fatalf("markSeen() articles = %#v, want %#v", got, articles)
				}
				return nil
			},
		},
		ai: aiDeps{
			summarizeContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				return "summary", nil
			},
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				return "COMPOSED", nil
			},
			printCLI: func(*model.Briefing) {},
			writeMarkdown: func(*model.Briefing, string) (string, error) {
				if marked {
					t.Fatal("markSeen() ran before writeMarkdown() finished")
				}
				return "output/26.04.21-早间-0700.md", nil
			},
			printFailed:   func([]fetcher.FailedSource) {},
			printArticles: func([]model.Article) {},
		},
	}

	if err := app.runBriefing("run", "0700", false, false); err != nil {
		t.Fatalf("runBriefing() error = %v", err)
	}
	if !marked {
		t.Fatal("markSeen() was not called after successful briefing")
	}
}

func TestExecuteServeScheduledBriefingUsesServePathForOutputMode(t *testing.T) {
	t.Setenv("EMAIL_SMTP_AUTH_CODE", "test")
	var gotPath string
	window := scheduler.Window{Period: "0800", From: time.Date(2026, 3, 18, 7, 0, 0, 0, time.UTC), To: time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC)}
	app := &app{
		cfg: executeTestConfigWithEmail(t, model.OutputModeTranslatedOnly),
		scheduler: schedulerDeps{
			startCronContext: func(ctx context.Context, cfg *config.Config, run func(scheduler.Window)) error {
				run(window)
				return nil
			},
			waitForeverContext: func(ctx context.Context) {},
		},
		fetch: fetchDeps{
			fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return sampleExecuteArticles(), nil, nil
			},
		},
		ai: aiDeps{
			summarizeContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				return "TRANSLATED", nil
			},
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				gotPath = path
				return "TRANSLATED", nil
			},
			printCLI:      func(*model.Briefing) {},
			writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
			printFailed:   func([]fetcher.FailedSource) {},
		},
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error { return nil },
		},
	}

	if err := execute(app, serveCommand{}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if gotPath != "serve" {
		t.Fatalf("composeBody() path = %q, want %q", gotPath, "serve")
	}
}

func TestExecuteServeOriginalOnlySkipsSummarize(t *testing.T) {
	t.Setenv("EMAIL_SMTP_AUTH_CODE", "test")
	summarizeCalled := false
	var gotContent model.OutputContent

	window := scheduler.Window{Period: "0800", From: time.Date(2026, 3, 18, 7, 0, 0, 0, time.UTC), To: time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC)}
	app := &app{
		cfg: executeTestConfigWithEmail(t, model.OutputModeOriginalOnly),
		scheduler: schedulerDeps{
			startCronContext: func(ctx context.Context, cfg *config.Config, run func(scheduler.Window)) error {
				run(window)
				return nil
			},
			waitForeverContext: func(ctx context.Context) {},
		},
		fetch: fetchDeps{
			fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return sampleExecuteArticles(), nil, nil
			},
		},
		ai: aiDeps{
			summarizeContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				summarizeCalled = true
				return "TRANSLATED", nil
			},
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				gotContent = content
				return "ORIGINAL ONLY", nil
			},
			printCLI:      func(*model.Briefing) {},
			writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
			printFailed:   func([]fetcher.FailedSource) {},
		},
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error { return nil },
		},
	}

	if err := execute(app, serveCommand{}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if summarizeCalled {
		t.Fatal("summarize() was called for serve with output.mode=original_only")
	}
	if gotContent.Translated != "" {
		t.Fatalf("composeBody() translated = %q, want empty", gotContent.Translated)
	}
}

func TestRenderBriefingOriginalOnlySkipsSummarize(t *testing.T) {
	articles := sampleExecuteArticles()
	summarizeCalled := false
	var gotContent model.OutputContent

	app := &app{
		cfg: &config.Config{Sources: []config.Source{{Category: "AI/科技"}}, Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		ai: aiDeps{
			summarizeContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				summarizeCalled = true
				return "TRANSLATED", nil
			},
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				gotContent = content
				return "ORIGINAL ONLY", nil
			},
			printCLI:      func(*model.Briefing) {},
			writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
			printFailed:   func([]fetcher.FailedSource) {},
			printArticles: func([]model.Article) {},
		},
	}

	if err := app.renderBriefing("run", "26.03.27", "1400", articles, nil, nil, nil, false, false); err != nil {
		t.Fatalf("renderBriefing() error = %v", err)
	}
	if summarizeCalled {
		t.Fatal("summarize() was called for output.mode=original_only")
	}
	if gotContent.Translated != "" {
		t.Fatalf("composeBody() translated = %q, want empty", gotContent.Translated)
	}
}

func TestRenderBriefingReturnsWriteMarkdownErrorBeforeMarkSeenAndEmail(t *testing.T) {
	articles := sampleExecuteArticles()
	marked := false
	emailed := false
	app := &app{
		cfg: &config.Config{Email: config.Email{To: "test@example.com"}, Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetch: fetchDeps{
			markSeen: func([]model.Article) error {
				marked = true
				return nil
			},
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				return "ORIGINAL ONLY", nil
			},
			printCLI: func(*model.Briefing) {},
			writeMarkdown: func(*model.Briefing, string) (string, error) {
				return "", errors.New("disk full")
			},
			printFailed:   func([]fetcher.FailedSource) {},
			printArticles: func([]model.Article) {},
		},
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error {
				emailed = true
				return nil
			},
		},
	}

	err := app.renderBriefing("run", "26.03.27", "1400", articles, nil, articles, nil, false, true)
	if err == nil || !strings.Contains(err.Error(), "write markdown: disk full") {
		t.Fatalf("renderBriefing() error = %v, want wrapped write markdown error", err)
	}
	if marked {
		t.Fatal("markSeen() should not be called when writeMarkdown fails")
	}
	if emailed {
		t.Fatal("sendEmail() should not be called when writeMarkdown fails")
	}
}

func TestExecuteFetchTranslateOriginalOnlySkipsTranslate(t *testing.T) {
	articles := sampleExecuteArticles()
	translateCalled := false
	var gotContent model.OutputContent
	var printed string

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Mode: model.OutputModeOriginalOnly}},
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return articles, nil, nil
			},
		},
		ai: aiDeps{
			translateContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				translateCalled = true
				return "TRANSLATED", nil
			},
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				gotContent = content
				return "ORIGINAL ONLY", nil
			},
			printText:   func(s string) { printed = s },
			printFailed: func([]fetcher.FailedSource) {},
		},
	}

	if err := execute(app, fetchCommand{zh: true}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if translateCalled {
		t.Fatal("translate() was called for output.mode=original_only")
	}
	if gotContent.Translated != "" {
		t.Fatalf("composeBody() translated = %q, want empty", gotContent.Translated)
	}
	if printed != "ORIGINAL ONLY" {
		t.Fatalf("printText() got = %q, want %q", printed, "ORIGINAL ONLY")
	}
}

func TestExecuteFetchTranslateUsesOutputModeComposedBody(t *testing.T) {
	articles := sampleExecuteArticles()
	var gotPath string
	var gotContent model.OutputContent
	var printed string

	app := &app{
		cfg: &config.Config{Sources: []config.Source{{Category: "国际政治"}, {Category: "AI/科技"}}, Output: config.OutputCfg{Mode: model.OutputModeBilingualTranslatedFirst}},
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return articles, nil, nil
			},
		},
		ai: aiDeps{
			translateContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				if strings.Join(categoryOrder, ",") != "国际政治,AI/科技" {
					t.Fatalf("translate() categoryOrder = %v", categoryOrder)
				}
				return "TRANSLATED", nil
			},
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				gotPath, gotContent = path, content
				return "COMPOSED", nil
			},
			printText:   func(s string) { printed = s },
			printFailed: func([]fetcher.FailedSource) {},
		},
	}

	if err := execute(app, fetchCommand{zh: true}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if gotPath != "fetch --zh" {
		t.Fatalf("composeBody() path = %q, want %q", gotPath, "fetch --zh")
	}
	categoryOrder := []string{"国际政治", "AI/科技"}
	if gotContent.Original != output.GroupedArticleListView(articles, categoryOrder, time.Local) {
		t.Fatalf("composeBody() original = %q, want %q", gotContent.Original, output.GroupedArticleListView(articles, categoryOrder, time.Local))
	}
	if gotContent.Translated != "TRANSLATED" {
		t.Fatalf("composeBody() translated = %q, want %q", gotContent.Translated, "TRANSLATED")
	}
	if printed != "COMPOSED" {
		t.Fatalf("printText() got = %q, want %q", printed, "COMPOSED")
	}
}

func TestExecuteFetchWithoutZhBypassesOutputModeComposer(t *testing.T) {
	called := false
	printedArticles := false
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Mode: model.OutputModeTranslatedOnly}},
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return sampleExecuteArticles(), nil, nil
			},
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				called = true
				return "", nil
			},
			printArticles: func([]model.Article) { printedArticles = true },
			printFailed:   func([]fetcher.FailedSource) {},
		},
	}

	if err := execute(app, fetchCommand{zh: false}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if called {
		t.Fatal("composeBody() was called for plain fetch")
	}
	if !printedArticles {
		t.Fatal("printArticles() was not called for plain fetch")
	}
}
