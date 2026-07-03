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
		"scheduler.startCron":                     app.scheduler.startCron,
		"scheduler.startCronContext":              app.scheduler.startCronContext,
		"fetch.fetchAll":                          app.fetch.fetchAll,
		"fetch.fetchAllContext":                   app.fetch.fetchAllContext,
		"fetch.fetchAllDetailedContext":           app.fetch.fetchAllDetailedContext,
		"fetch.fetchWindow":                       app.fetch.fetchWindow,
		"fetch.fetchWindowContext":                app.fetch.fetchWindowContext,
		"fetch.fetchWindowDetailedContext":        app.fetch.fetchWindowDetailedContext,
		"fetch.fetchWindowDetailedHistoryContext": app.fetch.fetchWindowDetailedHistoryContext,
		"fetch.fetchXAlertsContext":               app.fetch.fetchXAlertsContext,
		"fetch.markSeen":                          app.fetch.markSeen,
		"watch.fetchWatch":                        app.watch.fetchWatch,
		"watch.fetchWatchContext":                 app.watch.fetchWatchContext,
		"ai.summarize":                            app.ai.summarize,
		"ai.summarizeContext":                     app.ai.summarizeContext,
		"ai.translate":                            app.ai.translate,
		"ai.translateContext":                     app.ai.translateContext,
		"ai.deepDive":                             app.ai.deepDive,
		"ai.deepDiveContext":                      app.ai.deepDiveContext,
		"output.composeBody":                      app.output.composeBody,
		"output.printText":                        app.output.printText,
		"output.printFailed":                      app.output.printFailed,
		"output.printArticles":                    app.output.printArticles,
		"output.printCLI":                         app.output.printCLI,
		"output.writeMarkdown":                    app.output.writeMarkdown,
		"output.writeWatchMarkdown":               app.output.writeWatchMarkdown,
		"output.writeDeepDive":                    app.output.writeDeepDive,
		"email.sendEmail":                         app.email.sendEmail,
		"email.sendDeepEmail":                     app.email.sendDeepEmail,
		"email.resendMarkdownEmail":               app.email.resendMarkdownEmail,
		"publishHook":                             app.publishHook,
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

func TestRenderBriefingRunsPublishHookAfterMarkdownWrite(t *testing.T) {
	cfg := executeTestConfigWithEmail(t, model.OutputModeOriginalOnly)
	cfg.PublishHook = config.PublishHookConfig{
		Enabled: true,
		Command: "content-publisher",
		Args: []string{
			"publish-all",
			"--file", "{markdown_file}",
			"--source", "{source_app}",
			"--date", "{date}",
			"--period", "{period}",
			"--channels", "wechat,xhs",
			"--mode", "submit",
		},
	}
	markdownPath := filepath.Join(cfg.Output.Dir, "briefing.md")
	var hook publishHookRequest
	var emailCalled bool
	app := &app{
		cfg: cfg,
		output: outputDeps{
			printFailed:   func([]fetcher.FailedSource) {},
			printArticles: func([]model.Article) {},
			printCLI:      func(*model.Briefing) {},
			composeBody: func(string, model.OutputMode, model.OutputContent) (string, error) {
				return "body", nil
			},
			writeMarkdown: func(*model.Briefing, string) (string, error) {
				return markdownPath, nil
			},
		},
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error {
				emailCalled = true
				return nil
			},
		},
		publishHook: func(ctx context.Context, cfg config.PublishHookConfig, req publishHookRequest) error {
			hook = req
			return nil
		},
	}

	if err := app.renderBriefingContext(context.Background(), "run", "26.06.04", "0800", sampleExecuteArticles(), nil, nil, nil, false, true); err != nil {
		t.Fatalf("renderBriefingContext() error = %v", err)
	}
	if !emailCalled {
		t.Fatal("sendEmail() was not called")
	}
	if hook.MarkdownFile != markdownPath {
		t.Fatalf("MarkdownFile = %q, want %q", hook.MarkdownFile, markdownPath)
	}
	wantManifestPath := strings.TrimSuffix(markdownPath, ".md") + ".card-manifest.json"
	if hook.CardManifestFile != wantManifestPath {
		t.Fatalf("CardManifestFile = %q, want %q", hook.CardManifestFile, wantManifestPath)
	}
	if hook.SourceApp != "news-briefing" {
		t.Fatalf("SourceApp = %q, want news-briefing", hook.SourceApp)
	}
	if hook.Date != "26.06.04" {
		t.Fatalf("Date = %q, want 26.06.04", hook.Date)
	}
	if hook.Period != "0800" {
		t.Fatalf("Period = %q, want 0800", hook.Period)
	}
}

func TestRenderBriefingSkipsPublishHookWhenSuppressed(t *testing.T) {
	cfg := executeTestConfig(t, model.OutputModeOriginalOnly)
	cfg.PublishHook = config.PublishHookConfig{
		Enabled: true,
		Command: "content-publisher",
		Args:    []string{"publish-all", "--file", "{markdown_file}"},
	}
	hookCalled := false
	app := &app{
		cfg: cfg,
		output: outputDeps{
			printFailed:   func([]fetcher.FailedSource) {},
			printArticles: func([]model.Article) {},
			printCLI:      func(*model.Briefing) {},
			composeBody: func(string, model.OutputMode, model.OutputContent) (string, error) {
				return "body", nil
			},
			writeMarkdown: func(*model.Briefing, string) (string, error) {
				return filepath.Join(cfg.Output.Dir, "briefing.md"), nil
			},
		},
		publishHook: func(context.Context, config.PublishHookConfig, publishHookRequest) error {
			hookCalled = true
			return nil
		},
		suppressPublishHook: true,
	}

	if err := app.renderBriefingContext(context.Background(), "run", "26.06.04", "0800", sampleExecuteArticles(), nil, nil, nil, false, false); err != nil {
		t.Fatalf("renderBriefingContext() error = %v", err)
	}
	if hookCalled {
		t.Fatal("publish hook was called with suppressPublishHook=true")
	}
}

func TestExecuteRegenNoPublishSuppressesConfiguredPublishHook(t *testing.T) {
	cfg := executeTestConfig(t, model.OutputModeOriginalOnly)
	cfg.PublishHook = config.PublishHookConfig{
		Enabled: true,
		Command: "content-publisher",
		Args:    []string{"publish-all", "--file", "{markdown_file}"},
	}
	hookCalled := false
	app := &app{
		cfg: cfg,
		fetch: fetchDeps{
			fetchWindowDetailedContext: func(context.Context, *config.Config, time.Time, time.Time, bool, bool) (fetcher.FetchResult, error) {
				return fetcher.FetchResult{Articles: sampleExecuteArticles()}, nil
			},
		},
		output: silentBriefingOutputDeps("body"),
		publishHook: func(context.Context, config.PublishHookConfig, publishHookRequest) error {
			hookCalled = true
			return nil
		},
	}

	cmd := regenCommand{fromRaw: "2026-03-18 08:00", toRaw: "2026-03-18 14:00", period: "1400", noPublish: true}
	if err := execute(app, cmd); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if hookCalled {
		t.Fatal("publish hook was called for regen --no-publish")
	}
}

func TestRenderBriefingPassesAbsoluteMarkdownPathToPublishHook(t *testing.T) {
	cfg := executeTestConfig(t, model.OutputModeOriginalOnly)
	cfg.PublishHook = config.PublishHookConfig{
		Enabled: true,
		Command: "content-publisher",
		Args:    []string{"publish-all", "--file", "{markdown_file}"},
	}
	relativeMarkdownPath := filepath.Join("output", "26.06.07-早间-0800.md")
	var hook publishHookRequest
	app := &app{
		cfg: cfg,
		output: outputDeps{
			printFailed:   func([]fetcher.FailedSource) {},
			printArticles: func([]model.Article) {},
			printCLI:      func(*model.Briefing) {},
			composeBody: func(string, model.OutputMode, model.OutputContent) (string, error) {
				return "body", nil
			},
			writeMarkdown: func(*model.Briefing, string) (string, error) {
				return relativeMarkdownPath, nil
			},
		},
		publishHook: func(ctx context.Context, cfg config.PublishHookConfig, req publishHookRequest) error {
			hook = req
			return nil
		},
	}

	if err := app.renderBriefingContext(context.Background(), "run", "26.06.07", "0800", sampleExecuteArticles(), nil, nil, nil, false, false); err != nil {
		t.Fatalf("renderBriefingContext() error = %v", err)
	}
	want, err := filepath.Abs(relativeMarkdownPath)
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}
	if hook.MarkdownFile != want {
		t.Fatalf("MarkdownFile = %q, want absolute path %q", hook.MarkdownFile, want)
	}
	wantManifest := strings.TrimSuffix(want, ".md") + ".card-manifest.json"
	if hook.CardManifestFile != wantManifest {
		t.Fatalf("CardManifestFile = %q, want absolute path %q", hook.CardManifestFile, wantManifest)
	}
}

func TestRenderBriefingEmailsSavedMarkdownFile(t *testing.T) {
	cfg := executeTestConfigWithEmail(t, model.OutputModeOriginalOnly)
	markdownPath := filepath.Join(cfg.Output.Dir, "26.06.07-早间-0800.md")
	var sentPath string
	var sendEmailCalled bool
	app := &app{
		cfg: cfg,
		output: outputDeps{
			printFailed:   func([]fetcher.FailedSource) {},
			printArticles: func([]model.Article) {},
			printCLI:      func(*model.Briefing) {},
			composeBody: func(string, model.OutputMode, model.OutputContent) (string, error) {
				return "![NPR](https://npr.brightspotcdn.com/dims3/default/resize/1!/?url=http%3A%2F%2Fnpr-brightspot.s3.amazonaws.com%2Fimage.jpg)", nil
			},
			writeMarkdown: func(*model.Briefing, string) (string, error) {
				if err := os.WriteFile(markdownPath, []byte("# 国际资讯简报 26.06.07 早间 08:00\n\n![NPR](assets/26.06.07-0800/image.jpg)\n"), 0o644); err != nil {
					return "", err
				}
				return markdownPath, nil
			},
		},
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error {
				sendEmailCalled = true
				return nil
			},
			resendMarkdownEmail: func(path string, cfg *config.Config) error {
				sentPath = path
				return nil
			},
		},
	}

	if err := app.renderBriefingContext(context.Background(), "run", "26.06.07", "0800", sampleExecuteArticles(), nil, nil, nil, false, true); err != nil {
		t.Fatalf("renderBriefingContext() error = %v", err)
	}
	if sendEmailCalled {
		t.Fatal("sendEmail() used original briefing content; want saved markdown file email")
	}
	if sentPath != markdownPath {
		t.Fatalf("resendMarkdownEmail() path = %q, want %q", sentPath, markdownPath)
	}
}

func TestRenderBriefingLogsPublishHookFailureWithoutFailing(t *testing.T) {
	cfg := executeTestConfig(t, model.OutputModeOriginalOnly)
	cfg.PublishHook = config.PublishHookConfig{
		Enabled: true,
		Command: "content-publisher",
		Args:    []string{"publish-all", "--file", "{markdown_file}"},
	}
	app := &app{
		cfg: cfg,
		output: outputDeps{
			printFailed:   func([]fetcher.FailedSource) {},
			printArticles: func([]model.Article) {},
			printCLI:      func(*model.Briefing) {},
			composeBody: func(string, model.OutputMode, model.OutputContent) (string, error) {
				return "body", nil
			},
			writeMarkdown: func(*model.Briefing, string) (string, error) {
				return filepath.Join(cfg.Output.Dir, "briefing.md"), nil
			},
		},
		publishHook: func(context.Context, config.PublishHookConfig, publishHookRequest) error {
			return errors.New("hook boom")
		},
	}

	if err := app.renderBriefingContext(context.Background(), "run", "26.06.04", "0800", sampleExecuteArticles(), nil, nil, nil, false, false); err != nil {
		t.Fatalf("renderBriefingContext() error = %v", err)
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

func TestExecuteContextAlertsFetchesXAlertsForCurrentTime(t *testing.T) {
	ctx := context.WithValue(context.Background(), contextTestKey{}, "alerts")
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	called := false
	printed := false
	failedPrinted := false
	app := &app{
		cfg: executeTestConfig(t, model.OutputModeTranslatedOnly),
		now: func() time.Time {
			return now
		},
		fetch: fetchDeps{
			fetchXAlertsContext: func(got context.Context, cfg *config.Config, gotNow time.Time) (fetcher.FetchResult, error) {
				called = got.Value(contextTestKey{}) == "alerts" && gotNow.Equal(now)
				return fetcher.FetchResult{Articles: sampleExecuteArticles()}, nil
			},
		},
		ai: aiDeps{
			summarizeContext: func(context.Context, []model.Article, []string, *time.Location) (string, error) {
				t.Fatal("alerts should not summarize")
				return "", nil
			},
			translateContext: func(context.Context, []model.Article, []string, *time.Location) (string, error) {
				t.Fatal("alerts should not translate")
				return "", nil
			},
		},
		output: outputDeps{
			printArticles: func(articles []model.Article) {
				printed = len(articles) == 1 && articles[0].Title == "OpenAI ships feature"
			},
			printFailed: func([]fetcher.FailedSource) {
				failedPrinted = true
			},
			printText: func(string) {},
			composeBody: func(string, model.OutputMode, model.OutputContent) (string, error) {
				t.Fatal("alerts should not compose output")
				return "", nil
			},
			writeMarkdown: func(*model.Briefing, string) (string, error) {
				t.Fatal("alerts should not write markdown")
				return "", nil
			},
		},
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error {
				t.Fatal("alerts should not send email")
				return nil
			},
		},
	}

	if err := executeContext(ctx, app, alertsCommand{}); err != nil {
		t.Fatalf("executeContext() error = %v", err)
	}
	if !called {
		t.Fatal("fetchXAlertsContext() did not receive context/current time")
	}
	if !printed {
		t.Fatal("printArticles() was not called with alert articles")
	}
	if !failedPrinted {
		t.Fatal("printFailed() was not called")
	}
}

func TestExecuteAlertsPrintsEmptyMessage(t *testing.T) {
	var message string
	app := &app{
		cfg: executeTestConfig(t, model.OutputModeOriginalOnly),
		now: func() time.Time {
			return time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
		},
		fetch: fetchDeps{
			fetchXAlertsContext: func(context.Context, *config.Config, time.Time) (fetcher.FetchResult, error) {
				return fetcher.FetchResult{}, nil
			},
		},
		output: outputDeps{
			printArticles: func([]model.Article) {},
			printFailed:   func([]fetcher.FailedSource) {},
			printText:     func(s string) { message = s },
		},
	}

	if err := execute(app, alertsCommand{}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if !strings.Contains(message, "No X alerts") {
		t.Fatalf("empty message = %q, want No X alerts", message)
	}
}

func TestExecuteContextXRoutesPrintsConfiguredAccounts(t *testing.T) {
	ctx := context.WithValue(context.Background(), contextTestKey{}, "x-routes")
	var printed string
	app := &app{
		cfg: &config.Config{
			XAccounts: config.XAccountsConfig{
				Enabled: false,
				Accounts: []config.XAccountConfig{
					{Handle: " @OpenAI "},
					{Handle: ""},
					{Handle: "AnthropicAI"},
				},
			},
		},
		fetch: fetchDeps{
			fetchAllContext: func(context.Context, *config.Config, bool) ([]model.Article, []fetcher.FailedSource, error) {
				t.Fatal("x routes should not fetch articles")
				return nil, nil, nil
			},
			fetchXAlertsContext: func(context.Context, *config.Config, time.Time) (fetcher.FetchResult, error) {
				t.Fatal("x routes should not fetch alerts")
				return fetcher.FetchResult{}, nil
			},
		},
		ai: aiDeps{
			summarizeContext: func(context.Context, []model.Article, []string, *time.Location) (string, error) {
				t.Fatal("x routes should not summarize")
				return "", nil
			},
		},
		output: outputDeps{
			printText: func(s string) { printed = s },
			writeMarkdown: func(*model.Briefing, string) (string, error) {
				t.Fatal("x routes should not write markdown")
				return "", nil
			},
		},
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error {
				t.Fatal("x routes should not send email")
				return nil
			},
		},
	}

	if err := executeContext(ctx, app, xRoutesCommand{}); err != nil {
		t.Fatalf("executeContext() error = %v", err)
	}
	if printed != "/twitter/user/OpenAI\n/twitter/user/AnthropicAI" {
		t.Fatalf("printed routes = %q", printed)
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

	cfg := executeTestConfigWithEmail(t, model.OutputModeTranslatedOnly)
	cfg.ScheduleLocation = loc

	called := false
	emailCalled := false
	app := &app{
		cfg: cfg,
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

func TestRunScheduledBriefingUsesConfiguredArticleLimits(t *testing.T) {
	cfg := executeTestConfig(t, model.OutputModeTranslatedOnly)
	cfg.Output.MaxArticlesByCategory = map[string]int{"AI/科技": 2, "国际政治": 1}
	var summarized []model.Article
	window := scheduler.Window{Period: "0800", From: time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC)}
	app := &app{
		cfg: cfg,
		fetch: fetchDeps{
			fetchWindowDetailedContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) (fetcher.FetchResult, error) {
				return fetcher.FetchResult{Articles: []model.Article{
					{Title: "first ai", Category: "AI/科技"},
					{Title: "first politics", Category: "国际政治"},
					{Title: "second ai", Category: "AI/科技"},
					{Title: "second politics", Category: "国际政治"},
					{Title: "business", Category: "商业"},
				}}, nil
			},
		},
		ai: aiDeps{
			summarizeContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				summarized = append([]model.Article(nil), articles...)
				return "summary", nil
			},
		},
		output: silentBriefingOutputDeps("body"),
	}

	if err := app.runScheduledBriefingContext(context.Background(), window, false); err != nil {
		t.Fatalf("runScheduledBriefingContext() error = %v", err)
	}
	wantTitles := []string{"first ai", "first politics", "second ai"}
	gotTitles := make([]string, 0, len(summarized))
	for _, article := range summarized {
		gotTitles = append(gotTitles, article.Title)
	}
	if !reflect.DeepEqual(gotTitles, wantTitles) {
		t.Fatalf("summarized titles = %#v, want %#v", gotTitles, wantTitles)
	}
}

func TestExecuteRegenFiltersArticlesBeforeSummary(t *testing.T) {
	cfg := executeTestConfig(t, model.OutputModeTranslatedOnly)
	var summarized []model.Article
	app := &app{
		cfg: cfg,
		fetch: fetchDeps{
			fetchWindowDetailedContext: func(ctx context.Context, cfg *config.Config, gotFrom, gotTo time.Time, markSeen bool, ignoreSeen bool) (fetcher.FetchResult, error) {
				return fetcher.FetchResult{Articles: []model.Article{
					{Title: "first ai", Category: "AI/科技"},
					{Title: "first politics", Category: "国际政治"},
					{Title: "second ai", Category: "AI/科技"},
					{Title: "second politics", Category: "国际政治"},
					{Title: "business", Category: "商业"},
				}}, nil
			},
		},
		ai: aiDeps{
			summarizeContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				summarized = append([]model.Article(nil), articles...)
				return "summary", nil
			},
		},
		output: silentBriefingOutputDeps("body"),
	}

	cmd := regenCommand{fromRaw: "2026-03-18 08:00", toRaw: "2026-03-18 14:00", maxArticlesByCategory: map[string]int{"AI/科技": 2, "国际政治": 1}}
	if err := execute(app, cmd); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	wantTitles := []string{"first ai", "first politics", "second ai"}
	gotTitles := make([]string, 0, len(summarized))
	for _, article := range summarized {
		gotTitles = append(gotTitles, article.Title)
	}
	if !reflect.DeepEqual(gotTitles, wantTitles) {
		t.Fatalf("summarized titles = %#v, want %#v", gotTitles, wantTitles)
	}
}

func TestExecuteRegenUsesXVisibleHistoryOptions(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	from := time.Date(2026, 5, 19, 8, 0, 0, 0, loc)
	to := time.Date(2026, 5, 21, 8, 0, 0, 0, loc)

	called := false
	app := &app{
		cfg: &config.Config{
			ScheduleLocation: loc,
			Output:           config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly},
			XAccounts:        config.XAccountsConfig{HistoryDir: "/config/history"},
		},
		fetch: fetchDeps{
			fetchWindowDetailedHistoryContext: func(ctx context.Context, cfg *config.Config, gotFrom, gotTo time.Time, markSeen bool, ignoreSeen bool, historyDir string) (fetcher.FetchResult, error) {
				called = gotFrom.Equal(from) && gotTo.Equal(to) && !markSeen && ignoreSeen && historyDir == "/override/history"
				return fetcher.FetchResult{Articles: sampleExecuteArticles()}, nil
			},
		},
		output: silentBriefingOutputDeps(""),
	}

	cmd := regenCommand{
		fromRaw:             "2026-05-19 08:00",
		toRaw:               "2026-05-21 08:00",
		ignoreSeen:          true,
		xVisibleHistoryDays: 2,
		xVisibleHistoryDir:  "/override/history",
	}
	if err := execute(app, cmd); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if !called {
		t.Fatalf("history fetch was not called with parsed regen arguments")
	}
}

func TestExecuteRegenHistoryRejectsMissingDir(t *testing.T) {
	app := &app{cfg: &config.Config{}}
	err := execute(app, regenCommand{fromRaw: "2026-05-19 08:00", toRaw: "2026-05-20 08:00", xVisibleHistoryDays: 1})
	if err == nil || !strings.Contains(err.Error(), "x visible history dir") {
		t.Fatalf("execute() error = %v, want missing x visible history dir", err)
	}
}

func TestExecuteRegenHistoryRejectsWindowWiderThanDays(t *testing.T) {
	app := &app{cfg: &config.Config{XAccounts: config.XAccountsConfig{HistoryDir: "/config/history"}}}
	err := execute(app, regenCommand{fromRaw: "2026-05-19 08:00", toRaw: "2026-05-21 08:01", xVisibleHistoryDays: 2})
	if err == nil || !strings.Contains(err.Error(), "--x-visible-history-days") {
		t.Fatalf("execute() error = %v, want history days window validation", err)
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

func TestXReadyWindowParsesRFC3339AndDerivesPeriodInConfiguredTimezone(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	app := &app{cfg: &config.Config{ScheduleLocation: loc}}

	window, err := app.xReadyWindow(xReadyCommand{fromRaw: "2026-06-16T08:00:00+08:00", toRaw: "2026-06-16T18:00:00+08:00"})
	if err != nil {
		t.Fatalf("xReadyWindow() error = %v", err)
	}
	wantFrom := time.Date(2026, 6, 16, 8, 0, 0, 0, loc)
	wantTo := time.Date(2026, 6, 16, 18, 0, 0, 0, loc)
	if window.Expr != "x-ready-callback" || window.Period != "1800" || !window.From.Equal(wantFrom) || !window.To.Equal(wantTo) {
		t.Fatalf("window = %#v, want expr x-ready-callback period 1800 from %s to %s", window, wantFrom, wantTo)
	}
}

func TestXReadyWindowParsesLocalTimeAndRejectsEmptyWindow(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	app := &app{cfg: &config.Config{ScheduleLocation: loc}}

	window, err := app.xReadyWindow(xReadyCommand{fromRaw: "2026-06-16 08:00", toRaw: "2026-06-16 18:00", period: "1800"})
	if err != nil {
		t.Fatalf("xReadyWindow() error = %v", err)
	}
	if window.From.Location() != loc || window.To.Location() != loc || window.Period != "1800" {
		t.Fatalf("window = %#v, want configured location and explicit period", window)
	}

	_, err = app.xReadyWindow(xReadyCommand{fromRaw: "2026-06-16 18:00", toRaw: "2026-06-16 18:00"})
	if err == nil || !strings.Contains(err.Error(), "after --from") {
		t.Fatalf("xReadyWindow() error = %v, want empty window rejection", err)
	}
}

func TestExecuteXReadyRunsScheduledWindowOnce(t *testing.T) {
	t.Setenv("EMAIL_SMTP_AUTH_CODE", "test")
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	from := time.Date(2026, 6, 16, 8, 0, 0, 0, loc)
	to := time.Date(2026, 6, 16, 18, 0, 0, 0, loc)
	cfg := executeTestConfigWithEmail(t, model.OutputModeOriginalOnly)
	cfg.ScheduleLocation = loc
	calls := 0
	emailCalls := 0
	app := &app{
		cfg: cfg,
		fetch: fetchDeps{
			fetchWindowDetailedContext: func(ctx context.Context, cfg *config.Config, gotFrom, gotTo time.Time, markSeen bool, ignoreSeen bool) (fetcher.FetchResult, error) {
				calls++
				if !gotFrom.Equal(from) || !gotTo.Equal(to) || markSeen || ignoreSeen {
					t.Fatalf("fetch window = %s -> %s markSeen=%v ignoreSeen=%v", gotFrom, gotTo, markSeen, ignoreSeen)
				}
				return fetcher.FetchResult{Articles: sampleExecuteArticles()}, nil
			},
		},
		output: silentBriefingOutputDeps("body"),
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error {
				emailCalls++
				return nil
			},
		},
	}

	cmd := xReadyCommand{fromRaw: "2026-06-16T08:00:00+08:00", toRaw: "2026-06-16T18:00:00+08:00"}
	if err := execute(app, cmd); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if err := execute(app, cmd); err != nil {
		t.Fatalf("execute() second error = %v", err)
	}
	if calls != 1 || emailCalls != 1 {
		t.Fatalf("calls=%d emailCalls=%d, want one run due to done state", calls, emailCalls)
	}
}

func TestScheduledBriefingOnceSkipsDoneAndFreshRunning(t *testing.T) {
	window := scheduler.Window{Expr: "0 18 * * *", Period: "1800", From: time.Date(2026, 6, 16, 8, 0, 0, 0, time.UTC), To: time.Date(2026, 6, 16, 18, 0, 0, 0, time.UTC)}
	app := newScheduledOnceTestApp(t, errors.New("should not run"))
	paths := app.scheduledRunPaths(window)
	if err := os.MkdirAll(filepath.Dir(paths.done), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(paths.done, []byte("done"), 0o644); err != nil {
		t.Fatalf("WriteFile(done) error = %v", err)
	}
	if err := app.runScheduledBriefingOnceContext(context.Background(), window, "x-ready-callback", false); err != nil {
		t.Fatalf("runScheduledBriefingOnceContext(done) error = %v", err)
	}

	app = newScheduledOnceTestApp(t, errors.New("should not run"))
	paths = app.scheduledRunPaths(window)
	if err := os.MkdirAll(filepath.Dir(paths.running), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(paths.running, []byte("running"), 0o644); err != nil {
		t.Fatalf("WriteFile(running) error = %v", err)
	}
	if err := app.runScheduledBriefingOnceContext(context.Background(), window, "cron", false); err != nil {
		t.Fatalf("runScheduledBriefingOnceContext(running) error = %v", err)
	}
}

func TestScheduledBriefingOnceReplacesStaleRunningAndWritesFailed(t *testing.T) {
	window := scheduler.Window{Expr: "0 18 * * *", Period: "1800", From: time.Date(2026, 6, 16, 8, 0, 0, 0, time.UTC), To: time.Date(2026, 6, 16, 18, 0, 0, 0, time.UTC)}
	runErr := errors.New("fetch failed")
	app := newScheduledOnceTestApp(t, runErr)
	paths := app.scheduledRunPaths(window)
	if err := os.MkdirAll(filepath.Dir(paths.running), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(paths.running, []byte("running"), 0o644); err != nil {
		t.Fatalf("WriteFile(running) error = %v", err)
	}
	stale := time.Now().Add(-scheduledRunRunningTTL - time.Minute)
	if err := os.Chtimes(paths.running, stale, stale); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	err := app.runScheduledBriefingOnceContext(context.Background(), window, "cron", false)
	if !errors.Is(err, runErr) {
		t.Fatalf("runScheduledBriefingOnceContext() error = %v, want %v", err, runErr)
	}
	if _, err := os.Stat(paths.failed); err != nil {
		t.Fatalf("failed marker missing: %v", err)
	}
}

func newScheduledOnceTestApp(t *testing.T, runErr error) *app {
	t.Helper()
	calls := 0
	cfg := executeTestConfig(t, model.OutputModeOriginalOnly)
	return &app{
		cfg: cfg,
		fetch: fetchDeps{
			fetchWindowDetailedContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) (fetcher.FetchResult, error) {
				calls++
				if runErr != nil {
					return fetcher.FetchResult{}, runErr
				}
				return fetcher.FetchResult{Articles: sampleExecuteArticles()}, nil
			},
		},
		output: silentBriefingOutputDeps("body"),
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

func TestRenderBriefingAppendsTranslatedFilteredArticlesWhenEnabled(t *testing.T) {
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
	var translated []model.Article

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
			translateContext: func(ctx context.Context, got []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				if strings.Join(categoryOrder, ",") != "AI/科技,国际政治" {
					t.Fatalf("translate() categoryOrder = %v", categoryOrder)
				}
				translated = append([]model.Article(nil), got...)
				return "== 国际政治 ==\n1. 市场无关键词更新\n摘要：市场摘要\n来源: Example | 2026-03-18 13:00\nhttps://example.com/market", nil
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
	if !strings.Contains(printed.RawContent, "市场无关键词更新") {
		t.Fatalf("RawContent = %q, want translated filtered article", printed.RawContent)
	}
	if strings.Contains(printed.RawContent, "Market update without keyword") {
		t.Fatalf("RawContent = %q, want translated-only filtered appendix", printed.RawContent)
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
		if !strings.Contains(briefing.RawContent, "市场无关键词更新") {
			t.Fatalf("%s RawContent = %q, want translated filtered article", name, briefing.RawContent)
		}
	}
	if len(summarized) != 1 || summarized[0].Title != "OpenAI ships feature" {
		t.Fatalf("summarized = %#v, want accepted articles only", summarized)
	}
	if len(translated) != 1 || translated[0].Title != "Market update without keyword" {
		t.Fatalf("translated = %#v, want filtered articles only", translated)
	}
}

func TestRenderBriefingIncludesFailedSourcesInBody(t *testing.T) {
	var printed *model.Briefing
	var written *model.Briefing
	var emailed *model.Briefing
	var emailedFailed []fetcher.FailedSource
	failed := []fetcher.FailedSource{{
		Name: "X history",
		Err:  errors.New("no succeeded X history archive overlaps requested window 2026-05-20T00:00:00Z ~ 2026-05-21T00:00:00Z"),
	}}

	app := &app{
		cfg: &config.Config{
			Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly},
		},
		output: outputDeps{
			composeBody: func(string, model.OutputMode, model.OutputContent) (string, error) { return "正文", nil },
			printCLI: func(b *model.Briefing) {
				printed = b
			},
			writeMarkdown: func(b *model.Briefing, dir string) (string, error) {
				written = b
				return "", nil
			},
			printFailed: func([]fetcher.FailedSource) {},
		},
		email: emailDeps{
			sendEmail: func(b *model.Briefing, cfg *config.Config, failed []fetcher.FailedSource) error {
				emailedFailed = failed
				emailed = b
				return nil
			},
		},
	}

	if err := app.renderBriefing("run", "26.03.27", "1400", nil, nil, nil, failed, false, true); err != nil {
		t.Fatalf("renderBriefing() error = %v", err)
	}
	for name, briefing := range map[string]*model.Briefing{"printCLI": printed, "writeMarkdown": written, "sendEmail": emailed} {
		if briefing == nil {
			t.Fatalf("%s briefing = nil", name)
		}
		if !strings.Contains(briefing.RawContent, "抓取异常") || !strings.Contains(briefing.RawContent, "X history") {
			t.Fatalf("%s RawContent = %q, want failed section", name, briefing.RawContent)
		}
	}
	if len(emailedFailed) != 0 {
		t.Fatalf("emailed failed = %#v, want empty to avoid duplicate failed section", emailedFailed)
	}
}

func TestRenderBriefingReturnsFilteredAppendixTranslateErrorBeforeSideEffects(t *testing.T) {
	articles := sampleExecuteArticles()
	filtered := []model.Article{{Title: "Market update without keyword", Category: "国际政治"}}
	printed := false
	wrote := false
	emailed := false

	app := &app{
		cfg: &config.Config{
			Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeTranslatedOnly, IncludeFilteredArticles: true},
		},
		ai: aiDeps{
			summarizeContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				return "SUMMARY", nil
			},
			translateContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				return "", errors.New("translate failed")
			},
		},
		output: outputDeps{
			composeBody:   func(string, model.OutputMode, model.OutputContent) (string, error) { return "SUMMARY", nil },
			printCLI:      func(*model.Briefing) { printed = true },
			writeMarkdown: func(*model.Briefing, string) (string, error) { wrote = true; return "", nil },
			printFailed:   func([]fetcher.FailedSource) {},
		},
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error {
				emailed = true
				return nil
			},
		},
	}

	err := app.renderBriefing("run", "26.03.27", "1400", articles, filtered, nil, nil, false, true)
	if err == nil || !strings.Contains(err.Error(), "translate filtered articles") {
		t.Fatalf("renderBriefing() error = %v, want filtered appendix translation error", err)
	}
	if printed || wrote || emailed {
		t.Fatalf("side effects ran after translation failure: printed=%v wrote=%v emailed=%v", printed, wrote, emailed)
	}
}

func TestRenderBriefingSkipsFilteredAppendixTranslateWhenNoFilteredArticles(t *testing.T) {
	translateCalled := false
	var printed *model.Briefing

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly, IncludeFilteredArticles: true}},
		ai: aiDeps{
			translateContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				translateCalled = true
				return "TRANSLATED", nil
			},
		},
		output: outputDeps{
			composeBody:   func(string, model.OutputMode, model.OutputContent) (string, error) { return "BODY", nil },
			printCLI:      func(b *model.Briefing) { printed = b },
			writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
			printFailed:   func([]fetcher.FailedSource) {},
		},
	}

	if err := app.renderBriefing("run", "26.03.27", "1400", sampleExecuteArticles(), nil, nil, nil, false, false); err != nil {
		t.Fatalf("renderBriefing() error = %v", err)
	}
	if translateCalled {
		t.Fatal("translate() was called without filtered articles")
	}
	if printed == nil || printed.RawContent != "BODY" {
		t.Fatalf("RawContent = %q, want BODY", printed.RawContent)
	}
}

func TestAppendFilteredArticlesAppendixAllowsNilConfig(t *testing.T) {
	filtered := []model.Article{{Title: "Market update without keyword"}}
	got, err := (&app{}).appendFilteredArticlesAppendix(context.Background(), "BODY", filtered, []string{"国际政治"})
	if err != nil {
		t.Fatalf("appendFilteredArticlesAppendix() error = %v", err)
	}
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
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{IncludeFilteredArticles: true}},
		ai: aiDeps{
			translateContext: func(ctx context.Context, articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
				return "市场无关键词更新", nil
			},
		},
	}

	got, err := app.appendFilteredArticlesAppendix(context.Background(), "", filtered, []string{"国际政治"})
	if err != nil {
		t.Fatalf("appendFilteredArticlesAppendix() error = %v", err)
	}
	if strings.HasPrefix(got, " ") || strings.HasPrefix(got, "\n") || strings.HasPrefix(got, "\x00") {
		t.Fatalf("appendFilteredArticlesAppendix() = %q, want no leading garbage", got)
	}
	if !strings.Contains(got, "## 未命中关键词的候选新闻") {
		t.Fatalf("appendFilteredArticlesAppendix() = %q, want filtered appendix heading", got)
	}
	if !strings.Contains(got, "市场无关键词更新") {
		t.Fatalf("appendFilteredArticlesAppendix() = %q, want translated filtered article", got)
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
