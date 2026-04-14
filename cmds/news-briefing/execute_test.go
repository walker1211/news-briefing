package main

import (
	"errors"
	"io"
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
			fetchWindowWithIndex: func(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, model.SourceIndex, error) {
				return nil, nil, model.SourceIndex{}, errors.New("boom")
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
		fetchWindowWithIndex: func(cfg *config.Config, gotFrom, gotTo time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, model.SourceIndex, error) {
			called = gotFrom.Equal(from) && gotTo.Equal(to) && !markSeen && ignoreSeen
			return []model.Article{{Title: "a"}}, nil, model.SourceIndex{}, nil
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
		fetchWindowWithIndex: func(cfg *config.Config, gotFrom, gotTo time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, model.SourceIndex, error) {
			called = gotFrom.Equal(from) && gotTo.Equal(to) && !markSeen && ignoreSeen
			return []model.Article{{Title: "a"}}, nil, model.SourceIndex{}, nil
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

	err := app.renderBriefing("run", "26.03.27", "1400", articles, nil, model.SourceIndex{}, false, false)
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

	err := app.renderBriefing("regen", "26.03.27", "1400", articles, nil, model.SourceIndex{}, false, false)
	if err != nil {
		t.Fatalf("renderBriefing() error = %v", err)
	}
	if gotPath != "regen" {
		t.Fatalf("composeBody() path = %q, want %q", gotPath, "regen")
	}
}

func TestRunBriefingUsesIndexedFetchAndWritesSourceIndex(t *testing.T) {
	articles := sampleExecuteArticles()
	fetchCalled := false
	writeIndexCalled := false
	index := model.SourceIndex{SourceRuns: []model.SourceRun{{Name: "RSS", Status: "success"}}}
	markdownPath := filepath.Join(t.TempDir(), "26.03.27-午间-1400.md")

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetchAllWithIndex: func(cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, model.SourceIndex, error) {
			fetchCalled = true
			if !markSeen {
				t.Fatalf("fetchAllWithIndex() markSeen=%v, want true", markSeen)
			}
			return articles, nil, index, nil
		},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		printCLI:      func(*model.Briefing) {},
		printFailed:   func([]fetcher.FailedSource) {},
		printArticles: func([]model.Article) {},
		writeMarkdown: func(*model.Briefing, string) (string, error) { return markdownPath, nil },
		writeSourceIndex: func(gotMarkdownPath string, gotIndex model.SourceIndex) (string, error) {
			writeIndexCalled = true
			if gotMarkdownPath != markdownPath {
				t.Fatalf("writeSourceIndex() markdownPath = %q, want %q", gotMarkdownPath, markdownPath)
			}
			if len(gotIndex.SourceRuns) != 1 || gotIndex.SourceRuns[0].Name != "RSS" {
				t.Fatalf("writeSourceIndex() index = %#v", gotIndex)
			}
			return filepath.Join(filepath.Dir(markdownPath), "index", "26.03.27-午间-1400.json"), nil
		},
	}

	if err := app.runBriefing("run", "1400", false, false); err != nil {
		t.Fatalf("runBriefing() error = %v", err)
	}
	if !fetchCalled {
		t.Fatal("fetchAllWithIndex() was not called")
	}
	if !writeIndexCalled {
		t.Fatal("writeSourceIndex() was not called")
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
		fetchWindowWithIndex: func(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, model.SourceIndex, error) {
			return sampleExecuteArticles(), nil, model.SourceIndex{}, nil
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
		fetchWindowWithIndex: func(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, model.SourceIndex, error) {
			return sampleExecuteArticles(), nil, model.SourceIndex{}, nil
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

	if err := app.renderBriefing("run", "26.03.27", "1400", articles, nil, model.SourceIndex{}, false, false); err != nil {
		t.Fatalf("renderBriefing() error = %v", err)
	}
	if summarizeCalled {
		t.Fatal("summarize() was called for output.mode=original_only")
	}
	if gotContent.Translated != "" {
		t.Fatalf("composeBody() translated = %q, want empty", gotContent.Translated)
	}
}

func TestRenderBriefingDoesNotFailWhenSourceIndexWriteFails(t *testing.T) {
	articles := sampleExecuteArticles()
	cfg := &config.Config{Sources: []config.Source{{Category: "AI/科技"}}, Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}}

	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	defer stderr.Close()
	oldStderr := os.Stderr
	os.Stderr = stderr
	defer func() { os.Stderr = oldStderr }()

	app := &app{
		cfg:           cfg,
		printCLI:      func(*model.Briefing) {},
		printFailed:   func([]fetcher.FailedSource) {},
		printArticles: func([]model.Article) {},
		composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
			return "ORIGINAL ONLY", nil
		},
		writeMarkdown: func(*model.Briefing, string) (string, error) {
			return filepath.Join(cfg.Output.Dir, "26.03.27-午间-1400.md"), nil
		},
		writeSourceIndex: func(string, model.SourceIndex) (string, error) {
			return "", errors.New("disk full")
		},
	}

	if err := app.renderBriefing("run", "26.03.27", "1400", articles, nil, model.SourceIndex{}, false, false); err != nil {
		t.Fatalf("renderBriefing() error = %v, want nil when source index write fails", err)
	}
	if _, err := stderr.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	data, err := io.ReadAll(stderr)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !strings.Contains(string(data), "Error writing source index") {
		t.Fatalf("stderr = %q, want source index write error", string(data))
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
