package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/scheduler"
)

func TestScheduledPrefetchPersistsOrdinaryAndWatchThenMergesX(t *testing.T) {
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	window := scheduler.Window{Period: "0800", From: now.Add(-14 * time.Hour), To: now}
	ordinary := model.Article{Title: "ordinary", Link: "https://example.com/ordinary", Source: "RSS", Category: "AI/科技", Published: now.Add(-time.Hour)}
	watchArticle := model.Article{Title: "watch", Link: "https://example.com/watch", Source: "Watch", Category: "AI/科技", Published: now.Add(-30 * time.Minute)}
	xArticle := model.Article{Title: "x", Link: "https://x.com/example/status/1", Source: "X/example", Category: "AI/科技", Published: now.Add(-time.Minute)}
	ordinaryStats := scheduledPrefetchTestStats(window, ordinary)
	xStats := scheduledPrefetchTestStats(window, xArticle)
	fullFetchCalled := false
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir()}, ScheduleLocation: time.UTC},
		now: func() time.Time { return now },
		fetch: fetchDeps{
			fetchWindowOrdinaryDetailedContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) (fetcher.FetchResult, error) {
				return fetcher.FetchResult{Articles: []model.Article{ordinary}, SourceStats: ordinaryStats}, nil
			},
			fetchWindowXDetailedContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) (fetcher.FetchResult, error) {
				return fetcher.FetchResult{Articles: []model.Article{xArticle}, SourceStats: xStats}, nil
			},
			fetchWindowDetailedContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) (fetcher.FetchResult, error) {
				fullFetchCalled = true
				return fetcher.FetchResult{}, errors.New("full fetch must not run")
			},
		},
		watch: watchDeps{
			fetchWatchContext: func(ctx context.Context, cfg *config.Config, at time.Time) ([]model.Article, *model.WatchReport, error) {
				return []model.Article{watchArticle}, &model.WatchReport{GeneratedAt: now}, nil
			},
		},
	}
	fingerprint, err := app.scheduledPrefetchConfigFingerprint()
	if err != nil {
		t.Fatalf("scheduledPrefetchConfigFingerprint() error = %v", err)
	}
	if err := app.runScheduledPrefetchContext(context.Background(), window, fingerprint); err != nil {
		t.Fatalf("runScheduledPrefetchContext() error = %v", err)
	}

	result, err := app.fetchScheduledBriefingArticles(context.Background(), window, "26.08.13")
	if err != nil {
		t.Fatalf("fetchScheduledBriefingArticles() error = %v", err)
	}
	if fullFetchCalled {
		t.Fatal("full fetch ran despite a valid prefetch snapshot")
	}
	if !reflect.DeepEqual(result.articles, []model.Article{xArticle, ordinary, watchArticle}) {
		t.Fatalf("articles = %#v", result.articles)
	}
	if !reflect.DeepEqual(result.seenArticles, []model.Article{xArticle, ordinary}) {
		t.Fatalf("seenArticles = %#v, Watch must not be marked seen", result.seenArticles)
	}
	if result.sourceStats.Totals.AcceptedAfterDedup != 2 {
		t.Fatalf("AcceptedAfterDedup = %d, want 2 non-Watch articles", result.sourceStats.Totals.AcceptedAfterDedup)
	}
}

func TestScheduledPrefetchConfigChangeFallsBackToFullFetch(t *testing.T) {
	now := time.Date(2026, 8, 13, 18, 0, 0, 0, time.UTC)
	window := scheduler.Window{Period: "1800", From: now.Add(-10 * time.Hour), To: now}
	prefetched := model.Article{Title: "prefetched", Link: "https://example.com/prefetched", Source: "RSS", Category: "AI/科技", Published: now.Add(-time.Hour)}
	fallback := model.Article{Title: "fallback", Link: "https://example.com/fallback", Source: "RSS", Category: "AI/科技", Published: now.Add(-time.Minute)}
	app := &app{
		cfg: &config.Config{Keywords: []string{"before"}, Output: config.OutputCfg{Dir: t.TempDir()}, ScheduleLocation: time.UTC},
		now: func() time.Time { return now },
		fetch: fetchDeps{
			fetchWindowOrdinaryDetailedContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) (fetcher.FetchResult, error) {
				return fetcher.FetchResult{Articles: []model.Article{prefetched}}, nil
			},
			fetchWindowXDetailedContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) (fetcher.FetchResult, error) {
				t.Fatal("X-only fetch must not run for a mismatched snapshot")
				return fetcher.FetchResult{}, nil
			},
			fetchWindowDetailedContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) (fetcher.FetchResult, error) {
				return fetcher.FetchResult{Articles: []model.Article{fallback}}, nil
			},
		},
	}
	fingerprint, err := app.scheduledPrefetchConfigFingerprint()
	if err != nil {
		t.Fatalf("scheduledPrefetchConfigFingerprint() error = %v", err)
	}
	if err := app.runScheduledPrefetchContext(context.Background(), window, fingerprint); err != nil {
		t.Fatalf("runScheduledPrefetchContext() error = %v", err)
	}
	app.cfg.Keywords = []string{"after"}

	result, err := app.fetchScheduledBriefingArticles(context.Background(), window, "26.08.13")
	if err != nil {
		t.Fatalf("fetchScheduledBriefingArticles() error = %v", err)
	}
	if !reflect.DeepEqual(result.articles, []model.Article{fallback}) {
		t.Fatalf("articles = %#v, want fallback result", result.articles)
	}
}

func TestMergeScheduledPrefetchRechecksSeenState(t *testing.T) {
	outputDir := t.TempDir()
	now := time.Date(2026, 8, 13, 18, 0, 0, 0, time.UTC)
	ordinary := model.Article{Title: "ordinary", Link: "https://example.com/ordinary", Source: "RSS", Published: now.Add(-time.Hour)}
	watchArticle := model.Article{Title: "watch", Link: "https://example.com/watch", Source: "Watch", Published: now.Add(-30 * time.Minute)}
	xArticle := model.Article{Title: "x", Link: "https://x.com/example/status/2", Source: "X/example", Published: now.Add(-time.Minute)}
	if err := fetcher.MarkArticlesSeen(outputDir, []model.Article{ordinary}); err != nil {
		t.Fatalf("MarkArticlesSeen() error = %v", err)
	}

	result, err := mergeScheduledPrefetchWithX(context.Background(), outputDir, briefingFetchResult{
		articles:      []model.Article{ordinary, watchArticle},
		seenArticles:  []model.Article{ordinary},
		watchArticles: []model.Article{watchArticle},
	}, fetcher.FetchResult{Articles: []model.Article{xArticle}})
	if err != nil {
		t.Fatalf("mergeScheduledPrefetchWithX() error = %v", err)
	}
	if !reflect.DeepEqual(result.articles, []model.Article{xArticle, watchArticle}) {
		t.Fatalf("articles = %#v, prefetched article marked seen after prefetch must be removed", result.articles)
	}
}

func TestScheduledPrefetchPersistsRedactedFailure(t *testing.T) {
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	window := scheduler.Window{Period: "0800", From: now.Add(-14 * time.Hour), To: now}
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir()}, ScheduleLocation: time.UTC},
		now: func() time.Time { return now },
		fetch: fetchDeps{
			fetchWindowOrdinaryDetailedContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) (fetcher.FetchResult, error) {
				return fetcher.FetchResult{Failed: []fetcher.FailedSource{{Name: "feed", Err: errors.New("GET https://example.com/rss?access_key=secret-value failed")}}}, nil
			},
			fetchWindowXDetailedContext: func(context.Context, *config.Config, time.Time, time.Time, bool, bool) (fetcher.FetchResult, error) {
				return fetcher.FetchResult{}, nil
			},
		},
	}
	fingerprint, err := app.scheduledPrefetchConfigFingerprint()
	if err != nil {
		t.Fatalf("scheduledPrefetchConfigFingerprint() error = %v", err)
	}
	if err := app.runScheduledPrefetchContext(context.Background(), window, fingerprint); err != nil {
		t.Fatalf("runScheduledPrefetchContext() error = %v", err)
	}
	snapshot, err := app.readScheduledPrefetch(window)
	if err != nil {
		t.Fatalf("readScheduledPrefetch() error = %v", err)
	}
	if got := snapshot.Result.Failed[0].Error; got != "GET https://example.com/rss?access_key=[REDACTED] failed" {
		t.Fatalf("persisted failure = %q", got)
	}
}

func scheduledPrefetchTestStats(window scheduler.Window, article model.Article) model.SourceStatsReport {
	report := model.SourceStatsReport{
		SchemaVersion: "source-stats/v1",
		SourceApp:     "news-briefing",
		GeneratedAt:   window.To,
		Window:        model.SourceStatsWindow{From: window.From, To: window.To},
		Sources: []model.SourceStatsEntry{{
			Source:             article.Source,
			Category:           article.Category,
			AcceptedAfterDedup: 1,
		}},
	}
	report.RecalculateTotals()
	return report
}
