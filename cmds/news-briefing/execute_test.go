package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/output"
	"github.com/walker1211/news-briefing/internal/scheduler"
	"github.com/walker1211/news-briefing/internal/watch"
)

func TestExecuteServeUsesScheduler(t *testing.T) {
	called := false
	waited := false
	app := &app{
		cfg: &config.Config{},
		startCron: func(cfg *config.Config, run func(scheduler.Window)) error {
			called = true
			return nil
		},
		waitForever: func() {
			waited = true
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
			startCron: func(cfg *config.Config, run func(scheduler.Window)) error {
				run(window)
				return nil
			},
			fetchWindow: func(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return nil, nil, errors.New("boom")
			},
			waitForever: func() {},
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

func TestExecuteFetchTranslateUsesRunner(t *testing.T) {
	called := false
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Mode: model.OutputModeTranslatedOnly}},
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return []model.Article{{Title: "a"}}, nil, nil
		},
		translate: func(articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
			called = len(articles) == 1 && articles[0].Title == "a"
			return "ok", nil
		},
		printArticles: func([]model.Article) {},
		printFailed:   func([]fetcher.FailedSource) {},
	}

	if err := execute(app, fetchCommand{zh: true}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if !called {
		t.Fatalf("translate was not called with fetched articles")
	}
}

func TestExecuteRegenUsesParsedWindowAndFlags(t *testing.T) {
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
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeTranslatedOnly}},
		fetchWindow: func(cfg *config.Config, gotFrom, gotTo time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			called = gotFrom.Equal(from) && gotTo.Equal(to) && !markSeen && ignoreSeen
			return []model.Article{{Title: "a"}}, nil, nil
		},
		summarize:     func([]model.Article, []string, *time.Location) (string, error) { return "summary", nil },
		printFailed:   func([]fetcher.FailedSource) {},
		printArticles: func([]model.Article) {},
		printCLI:      func(*model.Briefing) {},
		writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
		sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error {
			emailCalled = true
			return nil
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
		fetchWindow: func(cfg *config.Config, gotFrom, gotTo time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			called = gotFrom.Equal(from) && gotTo.Equal(to) && !markSeen && ignoreSeen
			return []model.Article{{Title: "a"}}, nil, nil
		},
		printFailed:   func([]fetcher.FailedSource) {},
		printArticles: func([]model.Article) {},
		printCLI:      func(*model.Briefing) {},
		writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
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
	app := &app{cfg: &config.Config{ScheduleLocation: loc}}
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
		cfg:       &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeBilingualOriginalFirst}},
		summarize: func([]model.Article, []string, *time.Location) (string, error) { return "TRANSLATED", nil },
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			gotPath, gotMode, gotContent = path, mode, content
			return "COMPOSED", nil
		},
		printCLI:      func(b *model.Briefing) { printed = b },
		writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
		printFailed:   func([]fetcher.FailedSource) {},
		sendEmail:     func(*model.Briefing, *config.Config, []fetcher.FailedSource) error { return nil },
	}

	err := app.renderBriefing("run", "26.03.27", "1400", articles, nil, nil, false, false)
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

func TestRenderBriefingUsesComposedBodyForRegen(t *testing.T) {
	articles := sampleExecuteArticles()
	var gotPath string

	app := &app{
		cfg:       &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeTranslatedOnly}},
		summarize: func([]model.Article, []string, *time.Location) (string, error) { return "TRANSLATED", nil },
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			gotPath = path
			return "COMPOSED", nil
		},
		printCLI:      func(*model.Briefing) {},
		writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
		printFailed:   func([]fetcher.FailedSource) {},
	}

	err := app.renderBriefing("regen", "26.03.27", "1400", articles, nil, nil, false, false)
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
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			fetchCalled = true
			if markSeen {
				t.Fatalf("fetchAll() markSeen=%v, want false", markSeen)
			}
			return articles, nil, nil
		},
		markSeen: func([]model.Article) error { return nil },
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		printCLI:      func(*model.Briefing) {},
		printFailed:   func([]fetcher.FailedSource) {},
		printArticles: func([]model.Article) {},
		writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
	}

	if err := app.runBriefing("run", "1400", false, false); err != nil {
		t.Fatalf("runBriefing() error = %v", err)
	}
	if !fetchCalled {
		t.Fatal("fetchAll() was not called")
	}
}

func TestRunBriefingSkipsMarkSeenWhenSummarizeFails(t *testing.T) {
	outputDir := t.TempDir()
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: outputDir, Mode: model.OutputModeTranslatedOnly}},
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			if markSeen {
				t.Fatalf("fetchAll() markSeen=%v, want false", markSeen)
			}
			return sampleExecuteArticles(), nil, nil
		},
		summarize: func([]model.Article, []string, *time.Location) (string, error) {
			return "", errors.New("ai cli failed")
		},
		markSeen: func(articles []model.Article) error {
			return fetcher.MarkArticlesSeen(outputDir, articles)
		},
		printFailed:   func([]fetcher.FailedSource) {},
		printArticles: func([]model.Article) {},
		printCLI:      func(*model.Briefing) {},
		writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
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
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			if markSeen {
				t.Fatalf("fetchAll() markSeen=%v, want false", markSeen)
			}
			return articles, nil, nil
		},
		summarize: func([]model.Article, []string, *time.Location) (string, error) { return "summary", nil },
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
		markSeen: func(got []model.Article) error {
			marked = true
			if len(got) != len(articles) || got[0].Link != articles[0].Link {
				t.Fatalf("markSeen() articles = %#v, want %#v", got, articles)
			}
			return nil
		},
		printFailed:   func([]fetcher.FailedSource) {},
		printArticles: func([]model.Article) {},
	}

	if err := app.runBriefing("run", "0700", false, false); err != nil {
		t.Fatalf("runBriefing() error = %v", err)
	}
	if !marked {
		t.Fatal("markSeen() was not called after successful briefing")
	}
}

func TestRunBriefingMergesWatchArticlesAndWritesSidecar(t *testing.T) {
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC)
	outputDir := t.TempDir()
	var printed *model.Briefing
	watchCalled := false
	watchWritten := false

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: outputDir, Mode: model.OutputModeOriginalOnly}},
		now: func() time.Time { return now },
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return []model.Article{{Title: "news", Category: "AI/科技", Published: now.Add(-time.Hour)}}, nil, nil
		},
		fetchWatch: func(cfg *config.Config, gotNow time.Time) ([]model.Article, *model.WatchReport, error) {
			watchCalled = gotNow.Equal(now)
			return []model.Article{{Title: "watch", Category: "AI/科技", Published: gotNow}}, &model.WatchReport{GeneratedAt: gotNow}, nil
		},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		writeWatchMarkdown: func(report *model.WatchReport, gotOutputDir, date, period string) (string, error) {
			watchWritten = report != nil && gotOutputDir == outputDir && date == "26.04.15" && period == "1600"
			return "", nil
		},
		printCLI:      func(b *model.Briefing) { printed = b },
		printFailed:   func([]fetcher.FailedSource) {},
		printArticles: func([]model.Article) {},
		writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
	}

	if err := app.runBriefing("run", "1600", false, false); err != nil {
		t.Fatalf("runBriefing() error = %v", err)
	}
	if !watchCalled {
		t.Fatal("fetchWatch() was not called with current run time")
	}
	if !watchWritten {
		t.Fatal("writeWatchMarkdown() was not called with expected arguments")
	}
	if printed == nil {
		t.Fatal("printCLI() briefing = nil")
	}
	if len(printed.Articles) != 2 {
		t.Fatalf("len(printed.Articles) = %d, want 2", len(printed.Articles))
	}
	if printed.Articles[0].Title != "news" || printed.Articles[1].Title != "watch" {
		t.Fatalf("printed.Articles = %#v", printed.Articles)
	}
}

func TestRunScheduledBriefingMergesWatchArticlesAndWritesSidecar(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	window := scheduler.Window{
		Period: "1600",
		From:   time.Date(2026, 4, 15, 7, 0, 0, 0, loc),
		To:     time.Date(2026, 4, 15, 16, 0, 0, 0, loc),
	}
	outputDir := t.TempDir()
	var printed *model.Briefing
	watchCalled := false
	watchWritten := false

	app := &app{
		cfg: &config.Config{ScheduleLocation: loc, Output: config.OutputCfg{Dir: outputDir, Mode: model.OutputModeOriginalOnly}},
		fetchWindow: func(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			if markSeen {
				t.Fatalf("fetchWindow() markSeen=%v, want false", markSeen)
			}
			return []model.Article{{Title: "news", Category: "AI/科技", Published: to.Add(-time.Hour)}}, nil, nil
		},
		markSeen: func(got []model.Article) error {
			if len(got) != 1 || got[0].Title != "news" {
				t.Fatalf("markSeen() articles = %#v", got)
			}
			return nil
		},
		fetchWatch: func(cfg *config.Config, gotNow time.Time) ([]model.Article, *model.WatchReport, error) {
			watchCalled = gotNow.Equal(window.To)
			return []model.Article{{Title: "watch", Category: "AI/科技", Published: gotNow}}, &model.WatchReport{GeneratedAt: gotNow}, nil
		},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		writeWatchMarkdown: func(report *model.WatchReport, gotOutputDir, date, period string) (string, error) {
			watchWritten = report != nil && gotOutputDir == outputDir && date == "26.04.15" && period == "1600"
			return "", nil
		},
		printCLI:      func(b *model.Briefing) { printed = b },
		printFailed:   func([]fetcher.FailedSource) {},
		printArticles: func([]model.Article) {},
		writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
	}

	if err := app.runScheduledBriefing(window, false); err != nil {
		t.Fatalf("runScheduledBriefing() error = %v", err)
	}
	if !watchCalled {
		t.Fatal("fetchWatch() was not called with scheduled window end")
	}
	if !watchWritten {
		t.Fatal("writeWatchMarkdown() was not called for scheduled briefing")
	}
	if printed == nil {
		t.Fatal("printCLI() briefing = nil")
	}
	if len(printed.Articles) != 2 {
		t.Fatalf("len(printed.Articles) = %d, want 2", len(printed.Articles))
	}
	if printed.Articles[0].Title != "news" || printed.Articles[1].Title != "watch" {
		t.Fatalf("printed.Articles = %#v", printed.Articles)
	}
}

func TestRunBriefingPrintsWatchSiteErrors(t *testing.T) {
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC)
	var printed []string

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		now: func() time.Time { return now },
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return []model.Article{{Title: "news", Category: "AI/科技", Published: now.Add(-time.Hour)}}, nil, nil
		},
		fetchWatch: func(cfg *config.Config, gotNow time.Time) ([]model.Article, *model.WatchReport, error) {
			return nil, &model.WatchReport{
				GeneratedAt: gotNow,
				Events: []model.WatchEvent{{
					EventType:  "site_error",
					Source:     "Claude Platform Release Notes",
					Category:   "Claude Platform Release Notes",
					DetectedAt: gotNow,
					Reason:     "抓取失败：EOF",
				}},
			}, nil
		},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		printText:          func(s string) { printed = append(printed, s) },
		printCLI:           func(*model.Briefing) {},
		printFailed:        func([]fetcher.FailedSource) {},
		printArticles:      func([]model.Article) {},
		writeMarkdown:      func(*model.Briefing, string) (string, error) { return "", nil },
		writeWatchMarkdown: func(*model.WatchReport, string, string, string) (string, error) { return "", nil },
	}

	if err := app.runBriefing("run", "1600", false, false); err != nil {
		t.Fatalf("runBriefing() error = %v", err)
	}
	if len(printed) != 1 {
		t.Fatalf("len(printed) = %d, want 1; printed=%#v", len(printed), printed)
	}
	want := "Watch 站点异常：Claude Platform Release Notes — 抓取失败：EOF"
	if printed[0] != want {
		t.Fatalf("printed[0] = %q, want %q", printed[0], want)
	}
}

func TestRunScheduledBriefingPrintsWatchSiteErrors(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	window := scheduler.Window{
		Period: "1600",
		From:   time.Date(2026, 4, 15, 7, 0, 0, 0, loc),
		To:     time.Date(2026, 4, 15, 16, 0, 0, 0, loc),
	}
	var printed []string

	app := &app{
		cfg: &config.Config{ScheduleLocation: loc, Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetchWindow: func(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return []model.Article{{Title: "news", Category: "AI/科技", Published: to.Add(-time.Hour)}}, nil, nil
		},
		fetchWatch: func(cfg *config.Config, gotNow time.Time) ([]model.Article, *model.WatchReport, error) {
			return nil, &model.WatchReport{
				GeneratedAt: gotNow,
				Events: []model.WatchEvent{{
					EventType:  "site_error",
					Source:     "Claude Platform Release Notes",
					Category:   "Claude Platform Release Notes",
					DetectedAt: gotNow,
					Reason:     "抓取失败：EOF",
				}},
			}, nil
		},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		printText:          func(s string) { printed = append(printed, s) },
		printCLI:           func(*model.Briefing) {},
		printFailed:        func([]fetcher.FailedSource) {},
		printArticles:      func([]model.Article) {},
		writeMarkdown:      func(*model.Briefing, string) (string, error) { return "", nil },
		writeWatchMarkdown: func(*model.WatchReport, string, string, string) (string, error) { return "", nil },
	}

	if err := app.runScheduledBriefing(window, false); err != nil {
		t.Fatalf("runScheduledBriefing() error = %v", err)
	}
	if len(printed) != 1 {
		t.Fatalf("len(printed) = %d, want 1; printed=%#v", len(printed), printed)
	}
	want := "Watch 站点异常：Claude Platform Release Notes — 抓取失败：EOF"
	if printed[0] != want {
		t.Fatalf("printed[0] = %q, want %q", printed[0], want)
	}
}

func TestRunRegenSkipsWatch(t *testing.T) {
	watchCalled := false
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetchWindow: func(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return []model.Article{{Title: "news", Category: "AI/科技", Published: to}}, nil, nil
		},
		fetchWatch: func(cfg *config.Config, now time.Time) ([]model.Article, *model.WatchReport, error) {
			watchCalled = true
			return nil, nil, nil
		},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		printFailed:   func([]fetcher.FailedSource) {},
		printArticles: func([]model.Article) {},
		printCLI:      func(*model.Briefing) {},
		writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
	}

	if err := app.runRegen(regenCommand{fromRaw: "2026-04-15 07:00", toRaw: "2026-04-15 16:00", period: "1600"}); err != nil {
		t.Fatalf("runRegen() error = %v", err)
	}
	if watchCalled {
		t.Fatal("fetchWatch() should not run for regen")
	}
}

func TestExecuteServeScheduledBriefingUsesServePathForOutputMode(t *testing.T) {
	var gotPath string
	window := scheduler.Window{Period: "0800", From: time.Date(2026, 3, 18, 7, 0, 0, 0, time.UTC), To: time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC)}
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeTranslatedOnly}},
		startCron: func(cfg *config.Config, run func(scheduler.Window)) error {
			run(window)
			return nil
		},
		fetchWindow: func(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return sampleExecuteArticles(), nil, nil
		},
		summarize: func([]model.Article, []string, *time.Location) (string, error) { return "TRANSLATED", nil },
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			gotPath = path
			return "TRANSLATED", nil
		},
		printCLI:      func(*model.Briefing) {},
		writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
		sendEmail:     func(*model.Briefing, *config.Config, []fetcher.FailedSource) error { return nil },
		printFailed:   func([]fetcher.FailedSource) {},
		waitForever:   func() {},
	}

	if err := execute(app, serveCommand{}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if gotPath != "serve" {
		t.Fatalf("composeBody() path = %q, want %q", gotPath, "serve")
	}
}

func TestExecuteServeOriginalOnlySkipsSummarize(t *testing.T) {
	summarizeCalled := false
	var gotContent model.OutputContent

	window := scheduler.Window{Period: "0800", From: time.Date(2026, 3, 18, 7, 0, 0, 0, time.UTC), To: time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC)}
	app := &app{
		cfg: &config.Config{Sources: []config.Source{{Category: "AI/科技"}}, Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		startCron: func(cfg *config.Config, run func(scheduler.Window)) error {
			run(window)
			return nil
		},
		fetchWindow: func(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return sampleExecuteArticles(), nil, nil
		},
		summarize: func([]model.Article, []string, *time.Location) (string, error) {
			summarizeCalled = true
			return "TRANSLATED", nil
		},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			gotContent = content
			return "ORIGINAL ONLY", nil
		},
		printCLI:      func(*model.Briefing) {},
		writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
		sendEmail:     func(*model.Briefing, *config.Config, []fetcher.FailedSource) error { return nil },
		printFailed:   func([]fetcher.FailedSource) {},
		waitForever:   func() {},
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
		summarize: func([]model.Article, []string, *time.Location) (string, error) {
			summarizeCalled = true
			return "TRANSLATED", nil
		},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			gotContent = content
			return "ORIGINAL ONLY", nil
		},
		printCLI:      func(*model.Briefing) {},
		writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
		printFailed:   func([]fetcher.FailedSource) {},
		printArticles: func([]model.Article) {},
	}

	if err := app.renderBriefing("run", "26.03.27", "1400", articles, nil, nil, false, false); err != nil {
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
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		printCLI: func(*model.Briefing) {},
		writeMarkdown: func(*model.Briefing, string) (string, error) {
			return "", errors.New("disk full")
		},
		markSeen: func([]model.Article) error {
			marked = true
			return nil
		},
		sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error {
			emailed = true
			return nil
		},
		printFailed:   func([]fetcher.FailedSource) {},
		printArticles: func([]model.Article) {},
	}

	err := app.renderBriefing("run", "26.03.27", "1400", articles, articles, nil, false, true)
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
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return articles, nil, nil
		},
		translate: func([]model.Article, []string, *time.Location) (string, error) {
			translateCalled = true
			return "TRANSLATED", nil
		},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			gotContent = content
			return "ORIGINAL ONLY", nil
		},
		printText:   func(s string) { printed = s },
		printFailed: func([]fetcher.FailedSource) {},
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

func TestRunDeepDiveOriginalOnlySkipsDeepDiveAndKeepsOriginalBlock(t *testing.T) {
	relevant := sampleExecuteArticles()
	deepDiveCalled := false
	var gotContent model.OutputContent
	var wroteContent string
	var printed string

	app := &app{
		cfg: &config.Config{Sources: []config.Source{{Category: "AI/科技"}}, Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			if markSeen {
				t.Fatalf("fetchAll() markSeen=%v, want false", markSeen)
			}
			return relevant, nil, nil
		},
		printFailed: func([]fetcher.FailedSource) {},
		deepDive: func(string, []model.Article, *time.Location) (string, error) {
			deepDiveCalled = true
			return "DEEP TRANSLATED", nil
		},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			gotContent = content
			return "ORIGINAL ONLY", nil
		},
		writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
			wroteContent = content
			return "", nil
		},
		printText: func(s string) { printed = s },
	}

	if err := app.runDeepDive(deepCommand{topic: "OpenAI"}); err != nil {
		t.Fatalf("runDeepDive() error = %v", err)
	}
	if deepDiveCalled {
		t.Fatal("deepDive() was called for output.mode=original_only")
	}
	if gotContent.Translated != "" {
		t.Fatalf("composeBody() translated = %q, want empty", gotContent.Translated)
	}
	if wroteContent != "ORIGINAL ONLY" {
		t.Fatalf("writeDeepDive() content = %q, want %q", wroteContent, "ORIGINAL ONLY")
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
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return articles, nil, nil
		},
		translate: func(articles []model.Article, categoryOrder []string, loc *time.Location) (string, error) {
			if strings.Join(categoryOrder, ",") != "国际政治,AI/科技" {
				t.Fatalf("translate() categoryOrder = %v", categoryOrder)
			}
			return "TRANSLATED", nil
		},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			gotPath, gotContent = path, content
			return "COMPOSED", nil
		},
		printText:   func(s string) { printed = s },
		printFailed: func([]fetcher.FailedSource) {},
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
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return sampleExecuteArticles(), nil, nil
		},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			called = true
			return "", nil
		},
		printArticles: func([]model.Article) { printedArticles = true },
		printFailed:   func([]fetcher.FailedSource) {},
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

func TestRunDeepDiveUsesOutputModeComposedBody(t *testing.T) {
	relevant := sampleExecuteArticles()
	var gotPath string
	var gotContent model.OutputContent
	var wroteContent string
	var printed string

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeBilingualOriginalFirst}},
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return relevant, nil, nil
		},
		printFailed: func([]fetcher.FailedSource) {},
		deepDive: func(string, []model.Article, *time.Location) (string, error) {
			return "DEEP TRANSLATED", nil
		},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			gotPath, gotContent = path, content
			return "COMPOSED DEEP", nil
		},
		writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
			wroteContent = content
			return "", nil
		},
		printText: func(s string) { printed = s },
	}

	if err := app.runDeepDive(deepCommand{topic: "OpenAI"}); err != nil {
		t.Fatalf("runDeepDive() error = %v", err)
	}
	if gotPath != "deep" {
		t.Fatalf("composeBody() path = %q, want %q", gotPath, "deep")
	}
	if gotContent.Original != output.ArticleListView(relevant, time.Local) {
		t.Fatalf("composeBody() original = %q, want %q", gotContent.Original, output.ArticleListView(relevant, time.Local))
	}
	if gotContent.Translated != "DEEP TRANSLATED" {
		t.Fatalf("composeBody() translated = %q, want %q", gotContent.Translated, "DEEP TRANSLATED")
	}
	if wroteContent != "COMPOSED DEEP" {
		t.Fatalf("writeDeepDive() content = %q, want %q", wroteContent, "COMPOSED DEEP")
	}
	if printed != "COMPOSED DEEP" {
		t.Fatalf("printText() got = %q, want %q", printed, "COMPOSED DEEP")
	}
}

func TestRunDeepDiveSendEmailUsesDeepSender(t *testing.T) {
	relevant := sampleExecuteArticles()
	failed := []fetcher.FailedSource{{Name: "HN", Err: errors.New("timeout")}}
	var emailedTopic string
	var emailedBriefing *model.Briefing
	var emailedCfg *config.Config
	var emailedFailed []fetcher.FailedSource

	app := &app{
		cfg: &config.Config{
			Email:  config.Email{To: "test@example.com"},
			Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeTranslatedOnly},
		},
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return relevant, failed, nil
		},
		printFailed: func([]fetcher.FailedSource) {},
		deepDive: func(string, []model.Article, *time.Location) (string, error) {
			return "DEEP TRANSLATED", nil
		},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "COMPOSED DEEP", nil
		},
		writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
			return "", nil
		},
		sendDeepEmail: func(topic string, briefing *model.Briefing, cfg *config.Config, gotFailed []fetcher.FailedSource) error {
			emailedTopic = topic
			emailedBriefing = briefing
			emailedCfg = cfg
			emailedFailed = gotFailed
			return nil
		},
		printText: func(string) {},
	}

	if err := app.runDeepDive(deepCommand{topic: "OpenAI", sendEmail: true}); err != nil {
		t.Fatalf("runDeepDive() error = %v", err)
	}
	if emailedTopic != "OpenAI" {
		t.Fatalf("sendDeepEmail() topic = %q, want %q", emailedTopic, "OpenAI")
	}
	if emailedBriefing == nil {
		t.Fatal("sendDeepEmail() briefing = nil")
	}
	if emailedBriefing.RawContent != "COMPOSED DEEP" {
		t.Fatalf("sendDeepEmail() RawContent = %q, want %q", emailedBriefing.RawContent, "COMPOSED DEEP")
	}
	if emailedCfg != app.cfg {
		t.Fatal("sendDeepEmail() cfg mismatch")
	}
	if len(emailedFailed) != 1 || emailedFailed[0].Name != "HN" {
		t.Fatalf("sendDeepEmail() failed = %#v", emailedFailed)
	}
	if emailedBriefing.Date == "" {
		t.Fatal("sendDeepEmail() briefing date is empty")
	}
}

func TestRunDeepDiveRejectsInteractiveFollowUpOutput(t *testing.T) {
	wrote := false
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeTranslatedOnly}},
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return []model.Article{{Title: "AI bill", Summary: "AI bill summary"}}, nil, nil
		},
		printFailed: func([]fetcher.FailedSource) {},
		deepDive: func(string, []model.Article, *time.Location) (string, error) {
			return "你给的 3 条“相关新闻”与“美国 AI 数据中心暂停法案”主题不匹配。你希望我怎么继续？", nil
		},
		writeDeepDive: func(string, string, string, string) (string, error) {
			wrote = true
			return "", nil
		},
	}

	err := app.runDeepDive(deepCommand{topic: "AI bill"})
	if err == nil {
		t.Fatalf("runDeepDive() error = nil, want rejection for interactive follow-up output")
	}
	if !strings.Contains(err.Error(), "deep dive returned interactive follow-up") {
		t.Fatalf("runDeepDive() error = %v", err)
	}
	if wrote {
		t.Fatalf("runDeepDive() unexpectedly wrote interactive output to file")
	}
}

func TestRunDeepDiveSendEmailDoesNotUseRegularSender(t *testing.T) {
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return []model.Article{{Title: "Claude ships feature", Summary: "Claude update"}}, nil, nil
		},
		printFailed: func([]fetcher.FailedSource) {},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
			return "", nil
		},
		sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error {
			t.Fatal("sendEmail() should not be used for deep")
			return nil
		},
		sendDeepEmail: func(topic string, briefing *model.Briefing, cfg *config.Config, gotFailed []fetcher.FailedSource) error {
			return nil
		},
		printText: func(string) {},
	}

	if err := app.runDeepDive(deepCommand{topic: "Claude", sendEmail: true}); err != nil {
		t.Fatalf("runDeepDive() error = %v", err)
	}
}

func TestRunDeepDiveSendEmailFailureDoesNotFailCommand(t *testing.T) {
	var printed string
	app := &app{
		cfg: &config.Config{
			Email:  config.Email{To: "test@example.com"},
			Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly},
		},
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return []model.Article{{Title: "Claude ships feature", Summary: "Claude update"}}, nil, nil
		},
		printFailed: func([]fetcher.FailedSource) {},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
			return "", nil
		},
		sendDeepEmail: func(topic string, briefing *model.Briefing, cfg *config.Config, gotFailed []fetcher.FailedSource) error {
			return errors.New("smtp down")
		},
		printText: func(s string) { printed = s },
	}

	if err := app.runDeepDive(deepCommand{topic: "Claude", sendEmail: true}); err != nil {
		t.Fatalf("runDeepDive() error = %v, want nil when email send fails", err)
	}
	if printed != "ORIGINAL ONLY" {
		t.Fatalf("printText() got = %q, want %q", printed, "ORIGINAL ONLY")
	}
}

func TestExecuteResendMDSendsMarkdownFile(t *testing.T) {
	called := false
	var gotPath string
	app := &app{
		cfg: &config.Config{Email: config.Email{To: "test@example.com"}},
		resendMarkdownEmail: func(path string, cfg *config.Config) error {
			called = true
			gotPath = path
			return nil
		},
		printText: func(string) {},
	}

	if err := execute(app, resendMDCommand{file: "output/26.04.13-晚间-1800.md"}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if !called {
		t.Fatal("resendMarkdownEmail() was not called")
	}
	if gotPath != "output/26.04.13-晚间-1800.md" {
		t.Fatalf("resendMarkdownEmail() path = %q", gotPath)
	}
}

func TestExecuteResendMDReturnsSendError(t *testing.T) {
	app := &app{
		cfg: &config.Config{},
		resendMarkdownEmail: func(path string, cfg *config.Config) error {
			return errors.New("smtp down")
		},
		printText: func(string) {},
	}

	err := execute(app, resendMDCommand{file: "output/26.04.13-晚间-1800.md"})
	if err == nil || !strings.Contains(err.Error(), "smtp down") {
		t.Fatalf("execute() error = %v, want smtp down", err)
	}
}

func TestExecuteResendMDPrintsSuccessMessage(t *testing.T) {
	var printed []string
	app := &app{
		cfg:       &config.Config{Email: config.Email{To: "test@example.com"}},
		printText: func(s string) { printed = append(printed, s) },
		resendMarkdownEmail: func(path string, cfg *config.Config) error {
			return nil
		},
	}

	if err := execute(app, resendMDCommand{file: "output/26.04.13-晚间-1800.md"}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	joined := strings.Join(printed, "\n")
	if !strings.Contains(joined, "Email resent to test@example.com") {
		t.Fatalf("printed = %q", joined)
	}
}

func sampleExecuteArticles() []model.Article {
	return []model.Article{{
		Title:     "OpenAI ships feature",
		Link:      "https://example.com/news",
		Summary:   "Feature summary",
		Source:    "Example",
		Category:  "AI/科技",
		Published: time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC),
	}}
}

func TestSelectDeepDiveArticlesPrefersExactMatch(t *testing.T) {
	articles := []model.Article{
		{Title: "AI data center bill advances", Summary: "committee update"},
		{Title: "General AI news", Summary: "other summary"},
	}

	relevant, err := selectDeepDiveArticles("AI data center bill", articles)
	if err != nil {
		t.Fatalf("selectDeepDiveArticles() error = %v", err)
	}
	if len(relevant) != 1 {
		t.Fatalf("selectDeepDiveArticles() len = %d, want 1", len(relevant))
	}
	if relevant[0].Title != "AI data center bill advances" {
		t.Fatalf("selectDeepDiveArticles() picked %q", relevant[0].Title)
	}
}

func TestSelectDeepDiveArticlesFallsBackToKeywordMatches(t *testing.T) {
	articles := []model.Article{
		{Title: "US proposes AI data center restrictions", Summary: "new bill pauses expansion"},
		{Title: "AI startup funding rises", Summary: "venture update"},
		{Title: "Data center cooling costs grow", Summary: "industry note"},
	}

	relevant, err := selectDeepDiveArticles("美国 AI 数据中心 暂停法案", articles)
	if err != nil {
		t.Fatalf("selectDeepDiveArticles() error = %v", err)
	}
	if len(relevant) != 1 {
		t.Fatalf("selectDeepDiveArticles() len = %d, want 1", len(relevant))
	}
	if relevant[0].Title != "US proposes AI data center restrictions" {
		t.Fatalf("selectDeepDiveArticles() picked %q", relevant[0].Title)
	}
}

func TestSelectDeepDiveArticlesRejectsWeakMatches(t *testing.T) {
	articles := []model.Article{
		{Title: "Relay for OpenClaw", Summary: "open source tooling"},
		{Title: "Another devtools launch", Summary: "productivity news"},
	}

	_, err := selectDeepDiveArticles("美国 AI 数据中心 暂停法案", articles)
	if err == nil {
		t.Fatalf("selectDeepDiveArticles() error = nil, want weak-match rejection")
	}
	if !strings.Contains(err.Error(), "no sufficiently relevant articles") {
		t.Fatalf("selectDeepDiveArticles() error = %v", err)
	}
}

func TestSelectDeepDiveArticlesNormalizesPunctuationForExactMatch(t *testing.T) {
	articles := []model.Article{
		{Title: "Anthropic’s Claude", Summary: "subscription growth continues"},
		{Title: "General AI market update", Summary: "other summary"},
	}

	relevant, err := selectDeepDiveArticles("Anthropic's Claude", articles)
	if err != nil {
		t.Fatalf("selectDeepDiveArticles() error = %v", err)
	}
	if len(relevant) != 1 {
		t.Fatalf("selectDeepDiveArticles() len = %d, want 1", len(relevant))
	}
	if relevant[0].Title != "Anthropic’s Claude" {
		t.Fatalf("selectDeepDiveArticles() picked %q", relevant[0].Title)
	}
}

func TestSelectDeepDiveArticlesIgnoresEnglishStopwordOnlyMatches(t *testing.T) {
	articles := []model.Article{
		{Title: "Ceasefire deal with allies", Summary: "situation is tense"},
	}

	_, err := selectDeepDiveArticles("Claude popularity with paying consumers is skyrocketing", articles)
	if err == nil {
		t.Fatalf("selectDeepDiveArticles() error = nil, want weak-match rejection")
	}
	if !strings.Contains(err.Error(), "no sufficiently relevant articles") {
		t.Fatalf("selectDeepDiveArticles() error = %v", err)
	}
}

func TestSelectDeepDiveArticlesRejectsStopwordOnlyTopic(t *testing.T) {
	articles := []model.Article{
		{Title: "The market reacts", Summary: "investors wait"},
	}

	_, err := selectDeepDiveArticles("the", articles)
	if err == nil {
		t.Fatalf("selectDeepDiveArticles() error = nil, want weak-match rejection")
	}
	if !strings.Contains(err.Error(), "no sufficiently relevant articles") {
		t.Fatalf("selectDeepDiveArticles() error = %v", err)
	}
}

func TestSelectDeepDiveArticlesRejectsPunctuationOnlyTopic(t *testing.T) {
	articles := []model.Article{
		{Title: "Claude ships feature", Summary: "product update"},
	}

	_, err := selectDeepDiveArticles("...", articles)
	if err == nil {
		t.Fatalf("selectDeepDiveArticles() error = nil, want empty-topic rejection")
	}
	if !strings.Contains(err.Error(), "no sufficiently relevant articles") {
		t.Fatalf("selectDeepDiveArticles() error = %v", err)
	}
}

func TestRunDeepDiveIgnoreSeenUsesFetchWindow(t *testing.T) {
	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	windowCalled := false
	fetchAllCalled := false
	var wroteContent string

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		now: func() time.Time { return now },
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			fetchAllCalled = true
			return nil, nil, nil
		},
		fetchWindow: func(cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			windowCalled = true
			if markSeen || !ignoreSeen {
				t.Fatalf("fetchWindow() markSeen=%v ignoreSeen=%v, want false true", markSeen, ignoreSeen)
			}
			if !from.Equal(now.Add(-12*time.Hour)) || !to.Equal(now) {
				t.Fatalf("fetchWindow() window = %v ~ %v, want %v ~ %v", from, to, now.Add(-12*time.Hour), now)
			}
			return []model.Article{{Title: "Claude ships feature", Summary: "Claude update"}}, nil, nil
		},
		printFailed: func([]fetcher.FailedSource) {},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
			wroteContent = content
			return "", nil
		},
		printText: func(string) {},
	}

	if err := app.runDeepDive(deepCommand{topic: "Claude", ignoreSeen: true}); err != nil {
		t.Fatalf("runDeepDive() error = %v", err)
	}
	if fetchAllCalled {
		t.Fatal("fetchAll() was called when ignoreSeen=true")
	}
	if !windowCalled {
		t.Fatal("fetchWindow() was not called when ignoreSeen=true")
	}
	if wroteContent != "ORIGINAL ONLY" {
		t.Fatalf("writeDeepDive() content = %q, want %q", wroteContent, "ORIGINAL ONLY")
	}
}

func TestRunDeepDiveExplicitWindowUsesFetchWindow(t *testing.T) {
	loc := time.FixedZone("PDT", -7*3600)
	windowCalled := false
	fetchAllCalled := false
	var wroteContent string

	app := &app{
		cfg: &config.Config{
			ScheduleLocation: loc,
			Output:           config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly},
		},
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			fetchAllCalled = true
			return nil, nil, nil
		},
		fetchWindow: func(cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			windowCalled = true
			wantFrom := time.Date(2026, 3, 28, 0, 0, 0, 0, loc)
			wantTo := time.Date(2026, 3, 29, 23, 59, 0, 0, loc)
			if !from.Equal(wantFrom) || !to.Equal(wantTo) {
				t.Fatalf("fetchWindow() window = %v ~ %v, want %v ~ %v", from, to, wantFrom, wantTo)
			}
			if markSeen || ignoreSeen {
				t.Fatalf("fetchWindow() markSeen=%v ignoreSeen=%v, want false false", markSeen, ignoreSeen)
			}
			return []model.Article{{Title: "Claude ships feature", Summary: "Claude update"}}, nil, nil
		},
		printFailed: func([]fetcher.FailedSource) {},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
			wroteContent = content
			return "", nil
		},
		printText: func(string) {},
	}

	if err := app.runDeepDive(deepCommand{topic: "Claude", fromRaw: "2026-03-28 00:00", toRaw: "2026-03-29 23:59"}); err != nil {
		t.Fatalf("runDeepDive() error = %v", err)
	}
	if fetchAllCalled {
		t.Fatal("fetchAll() was called when explicit window is set")
	}
	if !windowCalled {
		t.Fatal("fetchWindow() was not called for explicit window")
	}
	if wroteContent != "ORIGINAL ONLY" {
		t.Fatalf("writeDeepDive() content = %q, want %q", wroteContent, "ORIGINAL ONLY")
	}
}

func TestRunDeepDiveExplicitWindowWithIgnoreSeenPassesIgnoreSeen(t *testing.T) {
	loc := time.FixedZone("PDT", -7*3600)
	emailed := false
	app := &app{
		cfg: &config.Config{
			ScheduleLocation: loc,
			Output:           config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly},
		},
		fetchWindow: func(cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			if markSeen || !ignoreSeen {
				t.Fatalf("fetchWindow() markSeen=%v ignoreSeen=%v, want false true", markSeen, ignoreSeen)
			}
			return []model.Article{{Title: "Claude ships feature", Summary: "Claude update"}}, nil, nil
		},
		printFailed: func([]fetcher.FailedSource) {},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
			return "", nil
		},
		sendDeepEmail: func(topic string, briefing *model.Briefing, cfg *config.Config, gotFailed []fetcher.FailedSource) error {
			emailed = true
			return nil
		},
		printText: func(string) {},
	}

	if err := app.runDeepDive(deepCommand{topic: "Claude", fromRaw: "2026-03-28 00:00", toRaw: "2026-03-29 23:59", ignoreSeen: true, sendEmail: true}); err != nil {
		t.Fatalf("runDeepDive() error = %v", err)
	}
	if !emailed {
		t.Fatal("sendDeepEmail() was not called with explicit window + ignoreSeen")
	}
}

func TestRunDeepDiveExplicitWindowRejectsToBeforeFrom(t *testing.T) {
	loc := time.FixedZone("PDT", -7*3600)
	app := &app{
		cfg: &config.Config{ScheduleLocation: loc},
	}

	err := app.runDeepDive(deepCommand{topic: "Claude", fromRaw: "2026-03-29 23:59", toRaw: "2026-03-28 00:00"})
	if err == nil {
		t.Fatal("runDeepDive() error = nil, want invalid window error")
	}
	if !strings.Contains(err.Error(), "--to must be after or equal to --from") {
		t.Fatalf("runDeepDive() error = %v", err)
	}
}

func TestRunDeepDiveDisplayedTimeMatchesConfiguredWindowTimezone(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}

	article := model.Article{
		Title:     "Claude ships feature",
		Summary:   "Claude update",
		Source:    "Example",
		Link:      "https://example.com/claude",
		Category:  "AI/科技",
		Published: time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC),
	}
	shown := output.ArticleListView([]model.Article{article}, loc)
	if !strings.Contains(shown, "2026-03-18 07:00") {
		t.Fatalf("ArticleListView() = %q, want displayed Los Angeles time", shown)
	}

	windowCalled := false
	app := &app{
		cfg: &config.Config{
			ScheduleLocation: loc,
			Output:           config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly},
		},
		fetchWindow: func(cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			windowCalled = true
			want := time.Date(2026, 3, 18, 7, 0, 0, 0, loc)
			if !from.Equal(want) || !to.Equal(want) {
				t.Fatalf("fetchWindow() window = %v ~ %v, want %v ~ %v", from, to, want, want)
			}
			return []model.Article{article}, nil, nil
		},
		printFailed: func([]fetcher.FailedSource) {},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
			return "", nil
		},
		printText: func(string) {},
	}

	if err := app.runDeepDive(deepCommand{topic: "Claude", fromRaw: "2026-03-18 07:00", toRaw: "2026-03-18 07:00"}); err != nil {
		t.Fatalf("runDeepDive() error = %v", err)
	}
	if !windowCalled {
		t.Fatal("fetchWindow() was not called for explicit window")
	}
}

func TestRunDeepDiveUsesConfiguredTimezoneForBriefingDate(t *testing.T) {
	loc := time.FixedZone("PDT", -7*3600)
	now := time.Date(2026, 3, 19, 6, 30, 0, 0, time.UTC)
	var wroteDate string
	var emailedDate string

	app := &app{
		cfg: &config.Config{
			ScheduleLocation: loc,
			Output:           config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly},
		},
		now: func() time.Time { return now },
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return []model.Article{{Title: "Claude ships feature", Summary: "Claude update"}}, nil, nil
		},
		printFailed: func([]fetcher.FailedSource) {},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
			wroteDate = date
			return "", nil
		},
		sendDeepEmail: func(topic string, briefing *model.Briefing, cfg *config.Config, gotFailed []fetcher.FailedSource) error {
			emailedDate = briefing.Date
			return nil
		},
		printText: func(string) {},
	}

	if err := app.runDeepDive(deepCommand{topic: "Claude", sendEmail: true}); err != nil {
		t.Fatalf("runDeepDive() error = %v", err)
	}
	if wroteDate != "26.03.18" {
		t.Fatalf("writeDeepDive() date = %q, want %q", wroteDate, "26.03.18")
	}
	if emailedDate != "26.03.18" {
		t.Fatalf("sendDeepEmail() briefing.Date = %q, want %q", emailedDate, "26.03.18")
	}
}

func TestRunDeepDiveIncludesWatchSeenArticles(t *testing.T) {
	outputDir := t.TempDir()
	seenStore := watch.NewSeenStore(outputDir)
	if err := seenStore.Save(model.WatchSeenState{Items: []model.WatchSeenArticle{{
		ID:               "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
		URL:              "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
		Title:            "Claude 上的身份验证",
		Source:           "Anthropic Claude Support",
		BriefingCategory: "AI/科技",
		WatchCategory:    "安全保障",
		Summary:          "支持文档新增身份验证说明",
		Body:             "某些使用场景需要提供政府颁发的身份证件与实时自拍。",
		EventType:        "content_changed",
		DetectedAt:       time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC),
	}}}); err != nil {
		t.Fatalf("seenStore.Save() error = %v", err)
	}

	var deepArticles []model.Article
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: outputDir, Mode: model.OutputModeTranslatedOnly}},
		now: func() time.Time { return time.Date(2026, 4, 15, 18, 0, 0, 0, time.UTC) },
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return nil, nil, nil
		},
		printFailed: func([]fetcher.FailedSource) {},
		deepDive: func(topic string, articles []model.Article, loc *time.Location) (string, error) {
			deepArticles = articles
			return "DEEP TRANSLATED", nil
		},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "COMPOSED DEEP", nil
		},
		writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
			return "", nil
		},
		printText: func(string) {},
	}

	if err := app.runDeepDive(deepCommand{topic: "身份验证"}); err != nil {
		t.Fatalf("runDeepDive() error = %v", err)
	}
	if len(deepArticles) != 1 {
		t.Fatalf("len(deepArticles) = %d, want 1", len(deepArticles))
	}
	if deepArticles[0].Title != "Claude 上的身份验证" {
		t.Fatalf("deepArticles[0] = %#v", deepArticles[0])
	}
	if !strings.Contains(deepArticles[0].Summary, "[Watch][安全保障]") {
		t.Fatalf("deepArticles[0].Summary = %q", deepArticles[0].Summary)
	}
}

func TestRunDeepDiveWithoutWatchSeenFileStaysCompatible(t *testing.T) {
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetchAll: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return []model.Article{{Title: "Claude ships feature", Summary: "Claude update"}}, nil, nil
		},
		printFailed: func([]fetcher.FailedSource) {},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
			return "", nil
		},
		printText: func(string) {},
	}

	if err := app.runDeepDive(deepCommand{topic: "Claude"}); err != nil {
		t.Fatalf("runDeepDive() error = %v", err)
	}
}

func TestLoadWatchSeenArticlesFiltersOldItems(t *testing.T) {
	outputDir := t.TempDir()
	seenStore := watch.NewSeenStore(outputDir)
	if err := seenStore.Save(model.WatchSeenState{Items: []model.WatchSeenArticle{
		{
			ID:               "old",
			URL:              "https://support.claude.com/old",
			Title:            "旧监听",
			Source:           "Anthropic Claude Support",
			BriefingCategory: "AI/科技",
			WatchCategory:    "安全保障",
			Summary:          "旧摘要",
			Body:             "旧正文",
			EventType:        "content_changed",
			DetectedAt:       time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:               "boundary",
			URL:              "https://support.claude.com/boundary",
			Title:            "边界监听",
			Source:           "Anthropic Claude Support",
			BriefingCategory: "AI/科技",
			WatchCategory:    "安全保障",
			Summary:          "边界摘要",
			Body:             "边界正文",
			EventType:        "new_article",
			DetectedAt:       time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			ID:               "new",
			URL:              "https://support.claude.com/new",
			Title:            "新监听",
			Source:           "Anthropic Claude Support",
			BriefingCategory: "AI/科技",
			WatchCategory:    "安全保障",
			Summary:          "新摘要",
			Body:             "新正文",
			EventType:        "new_article",
			DetectedAt:       time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC),
		},
	}}); err != nil {
		t.Fatalf("seenStore.Save() error = %v", err)
	}

	articles, err := loadWatchSeenArticles(outputDir, time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 15, 23, 59, 59, 0, time.UTC))
	if err != nil {
		t.Fatalf("loadWatchSeenArticles() error = %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("len(articles) = %d, want 1", len(articles))
	}
	if articles[0].Title != "新监听" {
		t.Fatalf("articles[0] = %#v", articles[0])
	}
}

func TestRunDeepDiveExplicitWindowUsesWindowDateForOutput(t *testing.T) {
	loc := time.FixedZone("PDT", -7*3600)
	now := time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC)
	var wroteDate string
	var emailedDate string

	app := &app{
		cfg: &config.Config{
			ScheduleLocation: loc,
			Output:           config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly},
		},
		now: func() time.Time { return now },
		fetchWindow: func(cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
			return []model.Article{{Title: "Claude ships feature", Summary: "Claude update"}}, nil, nil
		},
		printFailed: func([]fetcher.FailedSource) {},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
			wroteDate = date
			return "", nil
		},
		sendDeepEmail: func(topic string, briefing *model.Briefing, cfg *config.Config, gotFailed []fetcher.FailedSource) error {
			emailedDate = briefing.Date
			return nil
		},
		printText: func(string) {},
	}

	if err := app.runDeepDive(deepCommand{topic: "Claude", fromRaw: "2026-03-28 00:00", toRaw: "2026-03-29 23:59", sendEmail: true}); err != nil {
		t.Fatalf("runDeepDive() error = %v", err)
	}
	if wroteDate != "26.03.29" {
		t.Fatalf("writeDeepDive() date = %q, want %q", wroteDate, "26.03.29")
	}
	if emailedDate != "26.03.29" {
		t.Fatalf("sendDeepEmail() briefing.Date = %q, want %q", emailedDate, "26.03.29")
	}
}
