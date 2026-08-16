package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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

const sourceShadowSchemaVersion = "news-briefing/source-shadow/v1"

type sourceShadowReport struct {
	SchemaVersion string                  `json:"schema_version"`
	GeneratedAt   time.Time               `json:"generated_at"`
	Window        model.SourceStatsWindow `json:"window"`
	Accepted      []sourceShadowArticle   `json:"accepted"`
	Filtered      []sourceShadowArticle   `json:"filtered"`
	Failures      []sourceShadowFailure   `json:"failures,omitempty"`
	Stats         model.SourceStatsReport `json:"stats"`
}

type sourceShadowArticle struct {
	Title      string    `json:"title"`
	Link       string    `json:"link"`
	Source     string    `json:"source"`
	SourceRole string    `json:"source_role"`
	Category   string    `json:"category"`
	Published  time.Time `json:"published"`
}

type sourceShadowFailure struct {
	Source string `json:"source"`
	Class  string `json:"class"`
}

func (app *app) startScheduledSourceShadow(window scheduler.Window) {
	if app == nil || app.cfg == nil || !app.cfg.SourceShadow.Enabled || len(app.cfg.SourceShadow.Sources) == 0 || app.fetch.fetchWindowOrdinaryDetailedContext == nil {
		return
	}
	shadowCfg := *app.cfg
	shadowCfg.Sources = append([]config.Source(nil), app.cfg.SourceShadow.Sources...)
	shadowCfg.XAccounts.Enabled = false
	timeout := app.cfg.SourceShadow.Timeout
	if timeout <= 0 {
		timeout = config.DefaultSourceShadowTimeout
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		result, err := app.fetch.fetchWindowOrdinaryDetailedContext(ctx, &shadowCfg, window.From, window.To, false, true)
		if writeErr := app.writeSourceShadowReport(window, result, err); writeErr != nil {
			logutil.Warnf("[source-shadow] 保存观察结果失败: %v", writeErr)
			return
		}
		logutil.Printf("[source-shadow] 窗口 %s 已记录：命中 %d，过滤 %d，失败 %d（未进入正式简报）", window.Period, len(result.Articles), len(result.FilteredArticles), len(result.Failed))
	}()
}

func (app *app) writeSourceShadowReport(window scheduler.Window, result fetcher.FetchResult, runErr error) error {
	report := sourceShadowReport{
		SchemaVersion: sourceShadowSchemaVersion,
		GeneratedAt:   app.currentTime(),
		Window:        model.SourceStatsWindow{From: window.From, To: window.To},
		Accepted:      sourceShadowArticles(result.Articles),
		Filtered:      sourceShadowArticles(result.FilteredArticles),
		Failures:      sourceShadowFailures(result.Failed, runErr),
		Stats:         result.SourceStats,
	}
	report.Stats.Failed = make([]model.SourceStatsError, 0, len(report.Failures))
	for _, failure := range report.Failures {
		report.Stats.Failed = append(report.Stats.Failed, model.SourceStatsError{Source: failure.Source, Error: failure.Class})
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal source shadow report: %w", err)
	}
	dir := app.sourceShadowDir()
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.json", window.To.In(app.displayLocation()).Format("2006-01-02"), window.Period))
	if err := statefile.WriteAtomic(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write source shadow report: %w", err)
	}
	app.pruneSourceShadowReports(dir, report.GeneratedAt)
	return nil
}

func (app *app) sourceShadowDir() string {
	return filepath.Join(app.cfg.Output.Dir, "state", "source-shadow")
}

func (app *app) pruneSourceShadowReports(dir string, now time.Time) {
	retention := app.cfg.SourceShadow.Retention
	if retention <= 0 {
		retention = config.DefaultSourceShadowRetention
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || now.Sub(info.ModTime()) <= retention {
			continue
		}
		if removeErr := os.Remove(filepath.Join(dir, entry.Name())); removeErr != nil {
			logutil.Warnf("[source-shadow] 清理过期观察结果失败: %v", removeErr)
		}
	}
}

func sourceShadowArticles(articles []model.Article) []sourceShadowArticle {
	items := make([]sourceShadowArticle, 0, len(articles))
	for _, article := range articles {
		items = append(items, sourceShadowArticle{
			Title:      strings.TrimSpace(article.Title),
			Link:       strings.TrimSpace(article.Link),
			Source:     strings.TrimSpace(article.Source),
			SourceRole: strings.TrimSpace(article.SourceRole),
			Category:   strings.TrimSpace(article.Category),
			Published:  article.Published,
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Published.After(items[j].Published) })
	return items
}

func sourceShadowFailures(failed []fetcher.FailedSource, runErr error) []sourceShadowFailure {
	items := make([]sourceShadowFailure, 0, len(failed)+1)
	for _, failure := range failed {
		items = append(items, sourceShadowFailure{Source: strings.TrimSpace(failure.Name), Class: sourceShadowErrorClass(failure.Err)})
	}
	if runErr != nil {
		items = append(items, sourceShadowFailure{Source: "source-shadow", Class: sourceShadowErrorClass(runErr)})
	}
	return items
}

func sourceShadowErrorClass(err error) string {
	if err == nil {
		return "unknown"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "timeout") || strings.Contains(message, "timed out"):
		return "timeout"
	case strings.Contains(message, "429") || strings.Contains(message, "rate limit"):
		return "rate_limited"
	case strings.Contains(message, "status 5") || strings.Contains(message, "http 5"):
		return "upstream_5xx"
	case strings.Contains(message, "status 4") || strings.Contains(message, "http 4"):
		return "upstream_4xx"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "fetch_failed"
	}
}
