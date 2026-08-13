package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/logutil"
	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/scheduler"
	"github.com/walker1211/news-briefing/internal/statefile"
)

const (
	scheduledPrefetchSchemaVersion = 1
	scheduledPrefetchStatusRunning = "running"
	scheduledPrefetchStatusSuccess = "succeeded"
	scheduledPrefetchStatusFailed  = "failed"
	scheduledPrefetchWaitTimeout   = 4 * time.Minute
	scheduledPrefetchPollInterval  = 250 * time.Millisecond
	scheduledPrefetchStaleAfter    = 15 * time.Minute
	scheduledPrefetchRetention     = 72 * time.Hour
)

type scheduledPrefetchWindow struct {
	Period string    `json:"period"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
}

type scheduledPrefetchFailure struct {
	Name  string `json:"name"`
	Error string `json:"error,omitempty"`
}

type scheduledPrefetchResult struct {
	FilteredArticles      []model.Article            `json:"filtered_articles,omitempty"`
	SeenArticles          []model.Article            `json:"seen_articles,omitempty"`
	WatchArticles         []model.Article            `json:"watch_articles,omitempty"`
	Failed                []scheduledPrefetchFailure `json:"failed,omitempty"`
	SourceStats           model.SourceStatsReport    `json:"source_stats"`
	WatchSiteErrorNotices []string                   `json:"watch_site_error_notices,omitempty"`
}

type scheduledPrefetchSnapshot struct {
	SchemaVersion     int                      `json:"schema_version"`
	Status            string                   `json:"status"`
	Window            scheduledPrefetchWindow  `json:"window"`
	ConfigFingerprint string                   `json:"config_fingerprint"`
	StartedAt         time.Time                `json:"started_at"`
	FinishedAt        time.Time                `json:"finished_at,omitempty"`
	Result            *scheduledPrefetchResult `json:"result,omitempty"`
	Error             string                   `json:"error,omitempty"`
}

func (app *app) scheduledPrefetchDir() string {
	outputDir := "output"
	if app != nil && app.cfg != nil && strings.TrimSpace(app.cfg.Output.Dir) != "" {
		outputDir = app.cfg.Output.Dir
	}
	return filepath.Join(outputDir, "state", "briefing-prefetch")
}

func (app *app) scheduledPrefetchPath(window scheduler.Window) string {
	return filepath.Join(app.scheduledPrefetchDir(), scheduledRunWindowKey(window)+".json")
}

func (app *app) scheduledPrefetchConfigFingerprint() (string, error) {
	if app == nil || app.cfg == nil {
		return "", fmt.Errorf("prefetch config is nil")
	}
	payload := struct {
		Sources   []config.Source      `json:"sources"`
		Keywords  []string             `json:"keywords"`
		Filters   config.FiltersConfig `json:"filters"`
		Fetch     config.FetchConfig   `json:"fetch"`
		Watch     config.WatchConfig   `json:"watch"`
		Proxy     config.Proxy         `json:"proxy"`
		OutputDir string               `json:"output_dir"`
	}{
		Sources:   app.cfg.Sources,
		Keywords:  app.cfg.Keywords,
		Filters:   app.cfg.Filters,
		Fetch:     app.cfg.Fetch,
		Watch:     app.cfg.Watch,
		Proxy:     app.cfg.Proxy,
		OutputDir: app.cfg.Output.Dir,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode prefetch fingerprint: %w", err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (app *app) startScheduledPrefetch(ctx context.Context, window scheduler.Window) {
	if app == nil || app.fetch.fetchWindowOrdinaryDetailedContext == nil {
		return
	}
	fingerprint, err := app.scheduledPrefetchConfigFingerprint()
	if err != nil {
		logutil.Warnf("[prefetch] 窗口 %s 无法生成配置指纹: %v", window.Period, err)
		return
	}
	if snapshot, readErr := app.readScheduledPrefetch(window); readErr == nil && snapshotMatchesPrefetch(snapshot, window, fingerprint) {
		if snapshot.Status == scheduledPrefetchStatusSuccess {
			logutil.Printf("[prefetch] 窗口 %s 已存在可用结果，跳过重复抓取", window.Period)
			return
		}
		if snapshot.Status == scheduledPrefetchStatusRunning && app.currentTime().Sub(snapshot.StartedAt) < scheduledPrefetchStaleAfter {
			logutil.Printf("[prefetch] 窗口 %s 已有抓取任务运行中", window.Period)
			return
		}
	}
	go func() {
		if err := app.runScheduledPrefetchContext(ctx, window, fingerprint); err != nil {
			logutil.Warnf("[prefetch] 窗口 %s 抓取失败，将由正式流程回退: %v", window.Period, err)
		}
	}()
}

func (app *app) runScheduledPrefetchContext(ctx context.Context, window scheduler.Window, fingerprint string) error {
	startedAt := app.currentTime()
	snapshot := scheduledPrefetchSnapshot{
		SchemaVersion:     scheduledPrefetchSchemaVersion,
		Status:            scheduledPrefetchStatusRunning,
		Window:            scheduledPrefetchWindow{Period: window.Period, From: window.From, To: window.To},
		ConfigFingerprint: fingerprint,
		StartedAt:         startedAt,
	}
	app.cleanupScheduledPrefetches(startedAt)
	if err := app.writeScheduledPrefetch(window, snapshot); err != nil {
		return err
	}

	logutil.Printf("[prefetch] 窗口 %s 开始并行抓取普通 RSS 与 Watch", window.Period)
	date := window.To.In(app.displayLocation()).Format("06.01.02")
	result, err := app.fetchBriefingArticlesWithWatch(ctx, window.To, date, window.Period, func(ctx context.Context) (fetcher.FetchResult, error) {
		return app.fetch.fetchWindowOrdinaryDetailedContext(ctx, app.cfg, window.From, window.To, false, false)
	})
	snapshot.FinishedAt = app.currentTime()
	if err != nil {
		snapshot.Status = scheduledPrefetchStatusFailed
		snapshot.Error = fetcher.SafeErrorMessage(err)
		if writeErr := app.writeScheduledPrefetch(window, snapshot); writeErr != nil {
			return errors.Join(err, writeErr)
		}
		return err
	}
	snapshot.Status = scheduledPrefetchStatusSuccess
	snapshot.Result = persistedScheduledPrefetchResult(result)
	if err := app.writeScheduledPrefetch(window, snapshot); err != nil {
		return err
	}
	logutil.Printf("[prefetch] 窗口 %s 完成：普通源 %d 篇，含 Watch 合并后 %d 篇，耗时 %s", window.Period, len(result.seenArticles), len(result.articles), snapshot.FinishedAt.Sub(startedAt).Round(time.Second))
	return nil
}

func persistedScheduledPrefetchResult(result briefingFetchResult) *scheduledPrefetchResult {
	failed := make([]scheduledPrefetchFailure, 0, len(result.failed))
	for _, item := range result.failed {
		failed = append(failed, scheduledPrefetchFailure{Name: item.Name, Error: item.SafeErrorMessage()})
	}
	return &scheduledPrefetchResult{
		FilteredArticles:      result.filteredArticles,
		SeenArticles:          result.seenArticles,
		WatchArticles:         result.watchArticles,
		Failed:                failed,
		SourceStats:           result.sourceStats,
		WatchSiteErrorNotices: result.watchSiteErrorNotices,
	}
}

func restoredScheduledPrefetchResult(result *scheduledPrefetchResult) briefingFetchResult {
	if result == nil {
		return briefingFetchResult{}
	}
	failed := make([]fetcher.FailedSource, 0, len(result.Failed))
	for _, item := range result.Failed {
		var err error
		if item.Error != "" {
			err = errors.New(item.Error)
		}
		failed = append(failed, fetcher.FailedSource{Name: item.Name, Err: err})
	}
	return briefingFetchResult{
		articles:              append(append([]model.Article(nil), result.SeenArticles...), result.WatchArticles...),
		filteredArticles:      result.FilteredArticles,
		seenArticles:          result.SeenArticles,
		watchArticles:         result.WatchArticles,
		failed:                failed,
		sourceStats:           result.SourceStats,
		watchSiteErrorNotices: result.WatchSiteErrorNotices,
	}
}

func (app *app) writeScheduledPrefetch(window scheduler.Window, snapshot scheduledPrefetchSnapshot) error {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode scheduled prefetch: %w", err)
	}
	if err := statefile.WriteAtomic(app.scheduledPrefetchPath(window), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write scheduled prefetch: %w", err)
	}
	return nil
}

func (app *app) readScheduledPrefetch(window scheduler.Window) (scheduledPrefetchSnapshot, error) {
	path := app.scheduledPrefetchPath(window)
	data, err := os.ReadFile(path)
	if err != nil {
		return scheduledPrefetchSnapshot{}, err
	}
	var snapshot scheduledPrefetchSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return scheduledPrefetchSnapshot{}, fmt.Errorf("decode scheduled prefetch: %w", err)
	}
	return snapshot, nil
}

func snapshotMatchesPrefetch(snapshot scheduledPrefetchSnapshot, window scheduler.Window, fingerprint string) bool {
	return snapshot.SchemaVersion == scheduledPrefetchSchemaVersion &&
		snapshot.Window.Period == window.Period &&
		snapshot.Window.From.Equal(window.From) &&
		snapshot.Window.To.Equal(window.To) &&
		snapshot.ConfigFingerprint == fingerprint
}

func (app *app) waitForScheduledPrefetch(ctx context.Context, window scheduler.Window) (briefingFetchResult, bool, string) {
	if app == nil || app.fetch.fetchWindowXDetailedContext == nil {
		return briefingFetchResult{}, false, "仅 X 抓取器未启用"
	}
	fingerprint, err := app.scheduledPrefetchConfigFingerprint()
	if err != nil {
		return briefingFetchResult{}, false, err.Error()
	}
	deadline := time.Now().Add(scheduledPrefetchWaitTimeout)
	for {
		snapshot, readErr := app.readScheduledPrefetch(window)
		if os.IsNotExist(readErr) {
			return briefingFetchResult{}, false, "预取结果不存在"
		}
		if readErr != nil {
			return briefingFetchResult{}, false, readErr.Error()
		}
		if !snapshotMatchesPrefetch(snapshot, window, fingerprint) {
			return briefingFetchResult{}, false, "预取时间窗或配置指纹不匹配"
		}
		switch snapshot.Status {
		case scheduledPrefetchStatusSuccess:
			if snapshot.Result == nil {
				return briefingFetchResult{}, false, "预取结果为空"
			}
			return restoredScheduledPrefetchResult(snapshot.Result), true, ""
		case scheduledPrefetchStatusFailed:
			return briefingFetchResult{}, false, "预取任务失败"
		case scheduledPrefetchStatusRunning:
			if app.currentTime().Sub(snapshot.StartedAt) >= scheduledPrefetchStaleAfter {
				return briefingFetchResult{}, false, "预取任务已过期"
			}
		default:
			return briefingFetchResult{}, false, "预取状态无效"
		}
		if !time.Now().Before(deadline) {
			return briefingFetchResult{}, false, "等待预取完成超时"
		}
		timer := time.NewTimer(scheduledPrefetchPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return briefingFetchResult{}, false, ctx.Err().Error()
		case <-timer.C:
		}
	}
}

func (app *app) fetchScheduledBriefingArticles(ctx context.Context, window scheduler.Window, date string) (briefingFetchResult, error) {
	prefetched, ok, reason := app.waitForScheduledPrefetch(ctx, window)
	if !ok {
		logutil.Printf("[prefetch] 窗口 %s 未复用预取（%s），执行完整抓取", window.Period, reason)
		return app.fetchBriefingArticlesWithWatch(ctx, window.To, date, window.Period, func(ctx context.Context) (fetcher.FetchResult, error) {
			return app.fetchWindowArticlesDetailed(ctx, window.From, window.To, false, false)
		})
	}

	xResult, err := app.fetch.fetchWindowXDetailedContext(ctx, app.cfg, window.From, window.To, false, false)
	if err != nil {
		return briefingFetchResult{}, err
	}
	combined, err := mergeScheduledPrefetchWithX(ctx, app.cfg.Output.Dir, prefetched, xResult)
	if err != nil {
		return briefingFetchResult{}, err
	}
	logutil.Printf("[prefetch] 窗口 %s 已复用普通 RSS + Watch 结果，仅合并 X %d 篇", window.Period, len(xResult.Articles))
	return combined, nil
}

func mergeScheduledPrefetchWithX(ctx context.Context, outputDir string, prefetched briefingFetchResult, xResult fetcher.FetchResult) (briefingFetchResult, error) {
	seenArticles := append(append([]model.Article(nil), prefetched.seenArticles...), xResult.Articles...)
	sort.SliceStable(seenArticles, func(i, j int) bool {
		return seenArticles[i].Published.After(seenArticles[j].Published)
	})
	deduped, err := fetcher.DedupContext(ctx, seenArticles, false, fetcher.NewSeenStore(outputDir))
	if err != nil {
		return briefingFetchResult{}, fmt.Errorf("deduplicate prefetched articles: %w", err)
	}
	seenArticles = deduped.Articles
	articles := append(append([]model.Article(nil), seenArticles...), prefetched.watchArticles...)
	filteredArticles := append(append([]model.Article(nil), prefetched.filteredArticles...), xResult.FilteredArticles...)
	sort.SliceStable(filteredArticles, func(i, j int) bool {
		return filteredArticles[i].Published.After(filteredArticles[j].Published)
	})
	return briefingFetchResult{
		articles:              articles,
		filteredArticles:      filteredArticles,
		seenArticles:          seenArticles,
		watchArticles:         append([]model.Article(nil), prefetched.watchArticles...),
		failed:                append(append([]fetcher.FailedSource(nil), prefetched.failed...), xResult.Failed...),
		sourceStats:           mergeScheduledSourceStats(prefetched.sourceStats, xResult.SourceStats, seenArticles),
		watchSiteErrorNotices: append([]string(nil), prefetched.watchSiteErrorNotices...),
	}, nil
}

func mergeScheduledSourceStats(ordinary model.SourceStatsReport, x model.SourceStatsReport, accepted []model.Article) model.SourceStatsReport {
	report := ordinary
	if report.SchemaVersion == "" {
		report.SchemaVersion = x.SchemaVersion
	}
	if report.SourceApp == "" {
		report.SourceApp = x.SourceApp
	}
	if report.GeneratedAt.IsZero() || x.GeneratedAt.After(report.GeneratedAt) {
		report.GeneratedAt = x.GeneratedAt
	}
	if report.Window.From.IsZero() || (!x.Window.From.IsZero() && x.Window.From.Before(report.Window.From)) {
		report.Window.From = x.Window.From
	}
	if x.Window.To.After(report.Window.To) {
		report.Window.To = x.Window.To
	}
	report.Sources = append(append([]model.SourceStatsEntry(nil), ordinary.Sources...), x.Sources...)
	report.Failed = append(append([]model.SourceStatsError(nil), ordinary.Failed...), x.Failed...)
	for i := range report.Sources {
		report.Sources[i].AcceptedAfterDedup = 0
	}
	index := make(map[string]int, len(report.Sources))
	for i := range report.Sources {
		index[report.Sources[i].Source] = i
	}
	for _, article := range accepted {
		if i, ok := index[article.Source]; ok {
			report.Sources[i].AcceptedAfterDedup++
		}
	}
	sort.Slice(report.Sources, func(i, j int) bool {
		if report.Sources[i].Category == report.Sources[j].Category {
			return report.Sources[i].Source < report.Sources[j].Source
		}
		return report.Sources[i].Category < report.Sources[j].Category
	})
	report.RecalculateTotals()
	return report
}

func (app *app) cleanupScheduledPrefetches(now time.Time) {
	entries, err := os.ReadDir(app.scheduledPrefetchDir())
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || now.Sub(info.ModTime()) <= scheduledPrefetchRetention {
			continue
		}
		if removeErr := os.Remove(filepath.Join(app.scheduledPrefetchDir(), entry.Name())); removeErr != nil {
			logutil.Warnf("[prefetch] 清理过期结果失败: %v", removeErr)
		}
	}
}
