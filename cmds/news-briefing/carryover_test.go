package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	carryoverstate "github.com/walker1211/news-briefing/internal/carryover"
	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
)

func TestInjectCarryoversAddsOutOfWindowArticleOnce(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	target := now.Add(6 * time.Hour)
	app := &app{cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir()}}, now: func() time.Time { return now }}
	article := model.Article{Title: "1M context", Link: "https://x.com/team/status/1", Summary: "details", Source: "X/@team", Category: "AI/科技", Published: now.Add(-8 * time.Hour)}
	entry, _, err := app.carryoverStore().Add(context.Background(), target, article)
	if err != nil {
		t.Fatal(err)
	}

	result, count, err := app.injectCarryovers(target, briefingFetchResult{})
	if err != nil || count != 1 || len(result.articles) != 1 || len(result.seenArticles) != 1 {
		t.Fatalf("injectCarryovers() = %#v, %d, %v", result, count, err)
	}
	if result.articles[0].CarryoverID != entry.ID {
		t.Fatalf("CarryoverID = %q, want %q", result.articles[0].CarryoverID, entry.ID)
	}
}

func TestRenderBriefingConsumesCarryoverOnlyAfterSuccessfulPostActions(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	outputDir := t.TempDir()
	app := &app{
		cfg:   &config.Config{Output: config.OutputCfg{Dir: outputDir, Mode: model.OutputModeTranslatedOnly}},
		now:   func() time.Time { return now },
		fetch: fetchDeps{markSeen: func([]model.Article) error { return nil }},
		ai: aiDeps{summarizeBriefingContext: func(context.Context, []model.Article, []string, *time.Location) (model.BriefingSummary, string, error) {
			return model.BriefingSummary{Stories: []model.BriefingStory{{Title: "selected", SourceArticleIDs: []int{1}}}}, "summary", nil
		}},
		output: outputDeps{
			composeBody:   func(string, model.OutputMode, model.OutputContent) (string, error) { return "body", nil },
			printCLI:      func(*model.Briefing) {},
			printFailed:   func([]fetcher.FailedSource) {},
			writeMarkdown: func(*model.Briefing, string) (string, error) { return filepath.Join(outputDir, "briefing.md"), nil },
		},
		suppressPublishHook: true,
	}
	article := model.Article{Title: "1M context", Link: "https://x.com/team/status/1", Summary: "details", Source: "X/@team", Category: "AI/科技", Published: now.Add(-8 * time.Hour)}
	entry, _, err := app.carryoverStore().Add(context.Background(), now.Add(6*time.Hour), article)
	if err != nil {
		t.Fatal(err)
	}
	article.CarryoverID = entry.ID
	if err := app.renderBriefingContext(context.Background(), "serve", "26.08.17", "1800", []model.Article{article}, nil, []model.Article{article}, nil, false, false); err != nil {
		t.Fatalf("renderBriefingContext() error = %v", err)
	}
	entries, err := app.carryoverStore().List()
	if err != nil || len(entries) != 1 || entries[0].Status != carryoverstate.StatusConsumed {
		t.Fatalf("entries = %#v, %v", entries, err)
	}
}

func TestRenderBriefingKeepsCarryoverPendingWhenPublishFails(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	outputDir := t.TempDir()
	app := &app{
		cfg: &config.Config{
			Output:      config.OutputCfg{Dir: outputDir, Mode: model.OutputModeTranslatedOnly},
			PublishHook: config.PublishHookConfig{Enabled: true, Command: "publisher"},
		},
		now: func() time.Time { return now },
		ai: aiDeps{summarizeBriefingContext: func(context.Context, []model.Article, []string, *time.Location) (model.BriefingSummary, string, error) {
			return model.BriefingSummary{Stories: []model.BriefingStory{{Title: "selected", SourceArticleIDs: []int{1}}}}, "summary", nil
		}},
		output: outputDeps{
			composeBody:   func(string, model.OutputMode, model.OutputContent) (string, error) { return "body", nil },
			printCLI:      func(*model.Briefing) {},
			printFailed:   func([]fetcher.FailedSource) {},
			writeMarkdown: func(*model.Briefing, string) (string, error) { return filepath.Join(outputDir, "briefing.md"), nil },
		},
		publishHook: func(context.Context, config.PublishHookConfig, publishHookRequest) error {
			return errors.New("publish failed")
		},
	}
	article := model.Article{Title: "1M context", Link: "https://x.com/team/status/1", Summary: "details", Source: "X/@team", Category: "AI/科技", Published: now.Add(-8 * time.Hour)}
	entry, _, err := app.carryoverStore().Add(context.Background(), now.Add(6*time.Hour), article)
	if err != nil {
		t.Fatal(err)
	}
	article.CarryoverID = entry.ID
	if err := app.renderBriefingContext(context.Background(), "serve", "26.08.17", "1800", []model.Article{article}, nil, []model.Article{article}, nil, false, false); err == nil {
		t.Fatal("renderBriefingContext() error = nil")
	}
	entries, err := app.carryoverStore().List()
	if err != nil || entries[0].Status != carryoverstate.StatusPending {
		t.Fatalf("entries = %#v, %v", entries, err)
	}
}
