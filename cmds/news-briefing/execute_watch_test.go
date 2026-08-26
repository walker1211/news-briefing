package main

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/scheduler"
	"strings"
	"testing"
	"time"
)

func TestRunBriefingMergesWatchArticlesAndWritesSidecar(t *testing.T) {
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC)
	outputDir := t.TempDir()
	var printed *model.Briefing
	watchCalled := false
	watchWritten := false

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: outputDir, Mode: model.OutputModeOriginalOnly}},
		now: func() time.Time { return now },
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return []model.Article{{Title: "news", Category: "AI/科技", Published: now.Add(-time.Hour)}}, nil, nil
			},
		},
		watch: watchDeps{
			fetchWatchContext: func(ctx context.Context, cfg *config.Config, gotNow time.Time) ([]model.Article, *model.WatchReport, error) {
				watchCalled = gotNow.Equal(now)
				return []model.Article{{Title: "watch", Category: "AI/科技", Published: gotNow}}, &model.WatchReport{GeneratedAt: gotNow}, nil
			},
		},
		output: outputDeps{
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
		},
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
		fetch: fetchDeps{
			fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
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
		},
		watch: watchDeps{
			fetchWatchContext: func(ctx context.Context, cfg *config.Config, gotNow time.Time) ([]model.Article, *model.WatchReport, error) {
				watchCalled = gotNow.Equal(window.To)
				return []model.Article{{Title: "watch", Category: "AI/科技", Published: gotNow}}, &model.WatchReport{GeneratedAt: gotNow}, nil
			},
		},
		output: outputDeps{
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
		},
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

func TestRunScheduledBriefingExcludesWatchArticlePublishedOutsideWindow(t *testing.T) {
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	window := scheduler.Window{Period: "0800", From: now.Add(-14 * time.Hour), To: now}
	oldPublishedAt := time.Date(2026, 7, 24, 17, 0, 0, 0, time.UTC)
	watchURL := "https://www.anthropic.com/news/claude-opus-5"
	var printed *model.Briefing
	var writtenReport *model.WatchReport

	app := &app{
		cfg: &config.Config{ScheduleLocation: time.UTC, Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetch: fetchDeps{
			fetchWindowContext: func(context.Context, *config.Config, time.Time, time.Time, bool, bool) ([]model.Article, []fetcher.FailedSource, error) {
				return []model.Article{{Title: "today", Link: "https://example.com/today", Published: now.Add(-time.Hour)}}, nil, nil
			},
		},
		watch: watchDeps{
			fetchWatchContext: func(context.Context, *config.Config, time.Time) ([]model.Article, *model.WatchReport, error) {
				return []model.Article{{Title: "Anthropic News 文档更新：Introducing Claude Opus 5", Link: watchURL, Published: oldPublishedAt}}, &model.WatchReport{
					GeneratedAt: now,
					Events: []model.WatchEvent{{
						EventType:         "content_changed",
						Source:            "Anthropic News",
						ArticleURL:        watchURL,
						ArticleTitle:      "Introducing Claude Opus 5",
						PublishedAt:       oldPublishedAt,
						DetectedAt:        now,
						IncludeInBriefing: true,
						Reason:            "正文发生变化",
					}},
				}, nil
			},
		},
		output: outputDeps{
			composeBody: func(string, model.OutputMode, model.OutputContent) (string, error) { return "ORIGINAL ONLY", nil },
			writeWatchMarkdown: func(report *model.WatchReport, _, _, _ string) (string, error) {
				writtenReport = report
				return "", nil
			},
			printCLI:      func(briefing *model.Briefing) { printed = briefing },
			printFailed:   func([]fetcher.FailedSource) {},
			printArticles: func([]model.Article) {},
			writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
		},
	}

	if err := app.runScheduledBriefing(window, false); err != nil {
		t.Fatalf("runScheduledBriefing() error = %v", err)
	}
	if printed == nil || len(printed.Articles) != 1 || printed.Articles[0].Title != "today" {
		t.Fatalf("printed briefing articles = %#v, want only current-window article", printed)
	}
	if writtenReport == nil || len(writtenReport.Events) != 1 {
		t.Fatalf("writtenReport = %#v", writtenReport)
	}
	event := writtenReport.Events[0]
	if event.IncludeInBriefing {
		t.Fatalf("event.IncludeInBriefing = true, want false")
	}
	if !strings.Contains(event.Reason, "文章发布日期不在本次简报时间窗") {
		t.Fatalf("event.Reason = %q", event.Reason)
	}
}

func TestFilterWatchFetchResultByWindowFailsClosedOnlyWhenPublishedAtRequired(t *testing.T) {
	now := time.Date(2026, 8, 26, 18, 0, 0, 0, time.UTC)
	missingURL := "https://www.anthropic.com/news/undated-announcement"
	genericURL := "https://example.com/watch/article"
	result := watchFetchResult{
		articles: []model.Article{
			{Title: "undated announcement", Link: missingURL},
			{Title: "generic watch article", Link: genericURL},
		},
		report: &model.WatchReport{Events: []model.WatchEvent{
			{
				EventType:           "new_article",
				Source:              "Anthropic News",
				ArticleURL:          missingURL,
				IncludeInBriefing:   true,
				PublishedAtRequired: true,
				DetectedAt:          now,
			},
			{
				EventType:         "content_changed",
				Source:            "Generic Watch",
				ArticleURL:        genericURL,
				IncludeInBriefing: true,
				DetectedAt:        now,
			},
		}},
	}

	filtered := filterWatchFetchResultByWindow(result, now.Add(-time.Hour), now.Add(time.Hour))
	if len(filtered.articles) != 1 || filtered.articles[0].Link != genericURL {
		t.Fatalf("filtered.articles = %#v, want only generic watch article", filtered.articles)
	}
	missing := filtered.report.Events[0]
	if missing.IncludeInBriefing {
		t.Fatalf("missing-date event IncludeInBriefing = true, want false")
	}
	if !strings.Contains(missing.Reason, "文章发布日期缺失，仅记录在 watch 报告") {
		t.Fatalf("missing-date event Reason = %q", missing.Reason)
	}
	generic := filtered.report.Events[1]
	if !generic.IncludeInBriefing {
		t.Fatalf("generic missing-date event IncludeInBriefing = false, want true")
	}
}

func TestRunBriefingFetchesWatchInParallelWithMainFetch(t *testing.T) {
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC)
	watchStarted := make(chan struct{})
	var printed *model.Briefing
	var marked []model.Article

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		now: func() time.Time { return now },
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				select {
				case <-watchStarted:
				case <-time.After(time.Second):
					t.Fatal("fetchWatchContext did not start before fetchAllContext returned")
				}
				return []model.Article{{Title: "news", Category: "AI/科技", Published: now.Add(-time.Hour)}}, nil, nil
			},
			markSeen: func(got []model.Article) error {
				marked = append([]model.Article(nil), got...)
				return nil
			},
		},
		watch: watchDeps{
			fetchWatchContext: func(ctx context.Context, cfg *config.Config, gotNow time.Time) ([]model.Article, *model.WatchReport, error) {
				close(watchStarted)
				return []model.Article{{Title: "watch", Category: "AI/科技", Published: gotNow}}, &model.WatchReport{GeneratedAt: gotNow}, nil
			},
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				return "ORIGINAL ONLY", nil
			},
			printCLI:      func(b *model.Briefing) { printed = b },
			printFailed:   func([]fetcher.FailedSource) {},
			printArticles: func([]model.Article) {},
			writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
			writeWatchMarkdown: func(*model.WatchReport, string, string, string) (string, error) {
				return "", nil
			},
		},
	}

	if err := app.runBriefingContext(context.Background(), "run", "1600", false, false); err != nil {
		t.Fatalf("runBriefingContext() error = %v", err)
	}
	if printed == nil || len(printed.Articles) != 2 {
		t.Fatalf("printed briefing = %#v, want two articles", printed)
	}
	if printed.Articles[0].Title != "news" || printed.Articles[1].Title != "watch" {
		t.Fatalf("printed.Articles = %#v", printed.Articles)
	}
	if len(marked) != 1 || marked[0].Title != "news" {
		t.Fatalf("markSeen() articles = %#v, want only main news", marked)
	}
}

func TestRunScheduledBriefingFetchesWatchInParallelWithMainFetch(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	window := scheduler.Window{
		Period: "1600",
		From:   time.Date(2026, 4, 15, 7, 0, 0, 0, loc),
		To:     time.Date(2026, 4, 15, 16, 0, 0, 0, loc),
	}
	watchStarted := make(chan struct{})
	var printed *model.Briefing

	app := &app{
		cfg: &config.Config{ScheduleLocation: loc, Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetch: fetchDeps{
			fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				select {
				case <-watchStarted:
				case <-time.After(time.Second):
					t.Fatal("fetchWatchContext did not start before fetchWindowContext returned")
				}
				return []model.Article{{Title: "news", Category: "AI/科技", Published: to.Add(-time.Hour)}}, nil, nil
			},
		},
		watch: watchDeps{
			fetchWatchContext: func(ctx context.Context, cfg *config.Config, gotNow time.Time) ([]model.Article, *model.WatchReport, error) {
				close(watchStarted)
				return []model.Article{{Title: "watch", Category: "AI/科技", Published: gotNow}}, &model.WatchReport{GeneratedAt: gotNow}, nil
			},
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				return "ORIGINAL ONLY", nil
			},
			printCLI:      func(b *model.Briefing) { printed = b },
			printFailed:   func([]fetcher.FailedSource) {},
			printArticles: func([]model.Article) {},
			writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
			writeWatchMarkdown: func(*model.WatchReport, string, string, string) (string, error) {
				return "", nil
			},
		},
	}

	if err := app.runScheduledBriefingContext(context.Background(), window, false); err != nil {
		t.Fatalf("runScheduledBriefingContext() error = %v", err)
	}
	if printed == nil || len(printed.Articles) != 2 {
		t.Fatalf("printed briefing = %#v, want two articles", printed)
	}
	if printed.Articles[0].Title != "news" || printed.Articles[1].Title != "watch" {
		t.Fatalf("printed.Articles = %#v", printed.Articles)
	}
}

func TestRunBriefingMainFetchErrorSkipsWatchSidecarAndRender(t *testing.T) {
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC)
	rendered := false
	sidecarWritten := false

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		now: func() time.Time { return now },
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return nil, nil, errors.New("main failed")
			},
		},
		watch: watchDeps{
			fetchWatchContext: func(ctx context.Context, cfg *config.Config, gotNow time.Time) ([]model.Article, *model.WatchReport, error) {
				return nil, &model.WatchReport{GeneratedAt: gotNow}, nil
			},
		},
		output: outputDeps{
			printCLI: func(*model.Briefing) { rendered = true },
			writeWatchMarkdown: func(*model.WatchReport, string, string, string) (string, error) {
				sidecarWritten = true
				return "", nil
			},
		},
	}

	err := app.runBriefingContext(context.Background(), "run", "1600", false, false)
	if err == nil || !strings.Contains(err.Error(), "main failed") {
		t.Fatalf("runBriefingContext() error = %v, want main failed", err)
	}
	if sidecarWritten {
		t.Fatal("writeWatchMarkdown() should not run after main fetch failure")
	}
	if rendered {
		t.Fatal("printCLI() should not run after main fetch failure")
	}
}

func TestRunBriefingMainFetchErrorReturnsWithoutWaitingForWatch(t *testing.T) {
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC)
	watchStarted := make(chan struct{})
	releaseWatch := make(chan struct{})

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		now: func() time.Time { return now },
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				select {
				case <-watchStarted:
				case <-time.After(time.Second):
					t.Fatal("fetchWatchContext did not start before fetchAllContext returned")
				}
				return nil, nil, errors.New("main failed")
			},
		},
		watch: watchDeps{
			fetchWatchContext: func(ctx context.Context, cfg *config.Config, gotNow time.Time) ([]model.Article, *model.WatchReport, error) {
				close(watchStarted)
				<-releaseWatch
				return nil, nil, ctx.Err()
			},
		},
	}
	defer close(releaseWatch)

	done := make(chan error, 1)
	go func() {
		done <- app.runBriefingContext(context.Background(), "run", "1600", false, false)
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "main failed") {
			t.Fatalf("runBriefingContext() error = %v, want main failed", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("runBriefingContext() waited for watch fetch after main fetch failure")
	}
}

func TestRunBriefingWatchErrorAddsNoticeAndStillRenders(t *testing.T) {
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC)
	var rendered *model.Briefing

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		now: func() time.Time { return now },
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return []model.Article{{Title: "news", Category: "AI/科技", Published: now.Add(-time.Hour)}}, nil, nil
			},
		},
		watch: watchDeps{
			fetchWatchContext: func(ctx context.Context, cfg *config.Config, gotNow time.Time) ([]model.Article, *model.WatchReport, error) {
				return nil, nil, errors.New("watch failed")
			},
		},
		output: outputDeps{
			printFailed: func([]fetcher.FailedSource) {},
			printCLI:    func(briefing *model.Briefing) { rendered = briefing },
			composeBody: func(string, model.OutputMode, model.OutputContent) (string, error) {
				return "body", nil
			},
			writeMarkdown: func(*model.Briefing, string) (string, error) {
				return filepath.Join(t.TempDir(), "briefing.md"), nil
			},
		},
	}

	if err := app.runBriefingContext(context.Background(), "run", "1600", false, false); err != nil {
		t.Fatalf("runBriefingContext() error = %v, want nil", err)
	}
	if rendered == nil {
		t.Fatal("printCLI() was not called")
	}
	if !strings.Contains(rendered.RawContent, "Watch 抓取失败") || !strings.Contains(rendered.RawContent, "watch failed") {
		t.Fatalf("RawContent = %q, want watch failure notice", rendered.RawContent)
	}
}

func TestRunBriefingWatchSidecarErrorBlocksRender(t *testing.T) {
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC)
	rendered := false

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		now: func() time.Time { return now },
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return []model.Article{{Title: "news", Category: "AI/科技", Published: now.Add(-time.Hour)}}, nil, nil
			},
		},
		watch: watchDeps{
			fetchWatchContext: func(ctx context.Context, cfg *config.Config, gotNow time.Time) ([]model.Article, *model.WatchReport, error) {
				return nil, &model.WatchReport{GeneratedAt: gotNow}, nil
			},
		},
		output: outputDeps{
			printCLI: func(*model.Briefing) { rendered = true },
			writeWatchMarkdown: func(*model.WatchReport, string, string, string) (string, error) {
				return "", errors.New("sidecar failed")
			},
		},
	}

	err := app.runBriefingContext(context.Background(), "run", "1600", false, false)
	if err == nil || !strings.Contains(err.Error(), "sidecar failed") {
		t.Fatalf("runBriefingContext() error = %v, want sidecar failed", err)
	}
	if rendered {
		t.Fatal("printCLI() should not run after watch sidecar failure")
	}
}

func TestRunBriefingPrintsWatchSiteErrors(t *testing.T) {
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC)
	var printed []string

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		now: func() time.Time { return now },
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return []model.Article{{Title: "news", Category: "AI/科技", Published: now.Add(-time.Hour)}}, nil, nil
			},
		},
		watch: watchDeps{
			fetchWatchContext: func(ctx context.Context, cfg *config.Config, gotNow time.Time) ([]model.Article, *model.WatchReport, error) {
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
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				return "ORIGINAL ONLY", nil
			},
			printText:          func(s string) { printed = append(printed, s) },
			printCLI:           func(*model.Briefing) {},
			printFailed:        func([]fetcher.FailedSource) {},
			printArticles:      func([]model.Article) {},
			writeMarkdown:      func(*model.Briefing, string) (string, error) { return "", nil },
			writeWatchMarkdown: func(*model.WatchReport, string, string, string) (string, error) { return "", nil },
		},
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
		fetch: fetchDeps{
			fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return []model.Article{{Title: "news", Category: "AI/科技", Published: to.Add(-time.Hour)}}, nil, nil
			},
		},
		watch: watchDeps{
			fetchWatchContext: func(ctx context.Context, cfg *config.Config, gotNow time.Time) ([]model.Article, *model.WatchReport, error) {
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
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				return "ORIGINAL ONLY", nil
			},
			printText:          func(s string) { printed = append(printed, s) },
			printCLI:           func(*model.Briefing) {},
			printFailed:        func([]fetcher.FailedSource) {},
			printArticles:      func([]model.Article) {},
			writeMarkdown:      func(*model.Briefing, string) (string, error) { return "", nil },
			writeWatchMarkdown: func(*model.WatchReport, string, string, string) (string, error) { return "", nil },
		},
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

func TestRunScheduledBriefingIncludesWatchSiteErrorsInBody(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	window := scheduler.Window{
		Period: "1600",
		From:   time.Date(2026, 4, 15, 7, 0, 0, 0, loc),
		To:     time.Date(2026, 4, 15, 16, 0, 0, 0, loc),
	}
	var written string

	app := &app{
		cfg: &config.Config{ScheduleLocation: loc, Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetch: fetchDeps{
			fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return []model.Article{{Title: "news", Category: "AI/科技", Published: to.Add(-time.Hour)}}, nil, nil
			},
		},
		watch: watchDeps{
			fetchWatchContext: func(ctx context.Context, cfg *config.Config, gotNow time.Time) ([]model.Article, *model.WatchReport, error) {
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
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				return "ORIGINAL ONLY", nil
			},
			printText:     func(string) {},
			printCLI:      func(*model.Briefing) {},
			printFailed:   func([]fetcher.FailedSource) {},
			printArticles: func([]model.Article) {},
			writeMarkdown: func(briefing *model.Briefing, outputDir string) (string, error) {
				written = briefing.RawContent
				return "", nil
			},
			writeWatchMarkdown: func(*model.WatchReport, string, string, string) (string, error) { return "", nil },
		},
	}

	if err := app.runScheduledBriefing(window, false); err != nil {
		t.Fatalf("runScheduledBriefing() error = %v", err)
	}
	want := "## Watch 站点异常\n\n- Watch 站点异常：Claude Platform Release Notes — 抓取失败：EOF"
	if !strings.Contains(written, want) {
		t.Fatalf("written body = %q, want to contain %q", written, want)
	}
}

func TestRunRegenSkipsWatch(t *testing.T) {
	watchCalled := false
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly}},
		fetch: fetchDeps{
			fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return []model.Article{{Title: "news", Category: "AI/科技", Published: to}}, nil, nil
			},
		},
		watch: watchDeps{
			fetchWatchContext: func(ctx context.Context, cfg *config.Config, now time.Time) ([]model.Article, *model.WatchReport, error) {
				watchCalled = true
				return nil, nil, nil
			},
		},
		output: outputDeps{
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				return "ORIGINAL ONLY", nil
			},
			printFailed:   func([]fetcher.FailedSource) {},
			printArticles: func([]model.Article) {},
			printCLI:      func(*model.Briefing) {},
			writeMarkdown: func(*model.Briefing, string) (string, error) { return "", nil },
		},
	}

	if err := app.runRegen(regenCommand{fromRaw: "2026-04-15 07:00", toRaw: "2026-04-15 16:00", period: "1600"}); err != nil {
		t.Fatalf("runRegen() error = %v", err)
	}
	if watchCalled {
		t.Fatal("fetchWatch() should not run for regen")
	}
}
