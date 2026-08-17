package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/walker1211/news-briefing/internal/carryover"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/logutil"
	"github.com/walker1211/news-briefing/internal/model"
)

func (app *app) carryoverStore() carryover.Store {
	outputDir := "output"
	if app != nil && app.cfg != nil && strings.TrimSpace(app.cfg.Output.Dir) != "" {
		outputDir = app.cfg.Output.Dir
	}
	return carryover.NewStore(filepath.Join(outputDir, "state", "carryover.json"), app.currentTime)
}

func (app *app) runCarryoverAddContext(ctx context.Context, cmd carryoverAddCommand) error {
	targetAt, err := parseRegenTime(cmd.targetRaw, app.displayLocation())
	if err != nil {
		return fmt.Errorf("parse --target: %w", err)
	}
	if !targetAt.After(app.currentTime()) {
		return fmt.Errorf("--target must be in the future")
	}
	article, err := fetcher.FindXVisibleArticleByURL(app.cfg.XAccounts, cmd.url)
	if err != nil {
		return err
	}
	entry, created, err := app.carryoverStore().Add(ctx, targetAt, article)
	if err != nil {
		return err
	}
	app.ensureTextOutputDeps()
	result := "existing"
	if created {
		result = "added"
	}
	app.output.printText(fmt.Sprintf("carryover %s: id=%s target=%s source=%s title=%s", result, entry.ID, entry.TargetAt.In(app.displayLocation()).Format("2006-01-02 15:04"), entry.Article.Source, entry.Article.Title))
	return nil
}

func (app *app) runCarryoverListContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := app.carryoverStore().List()
	if err != nil {
		return err
	}
	app.ensureTextOutputDeps()
	if len(entries) == 0 {
		app.output.printText("No carryover entries.")
		return nil
	}
	var lines []string
	for _, entry := range entries {
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s\t%s", entry.ID, entry.Status, entry.TargetAt.In(app.displayLocation()).Format("2006-01-02 15:04"), entry.Article.Source, entry.Article.Title))
	}
	app.output.printText(strings.Join(lines, "\n"))
	return nil
}

func (app *app) runCarryoverRemoveContext(ctx context.Context, cmd carryoverRemoveCommand) error {
	removed, err := app.carryoverStore().Remove(ctx, cmd.id)
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("carryover entry not found")
	}
	app.ensureTextOutputDeps()
	app.output.printText("carryover removed: " + strings.TrimSpace(cmd.id))
	return nil
}

func (app *app) injectCarryovers(targetAt time.Time, result briefingFetchResult) (briefingFetchResult, int, error) {
	entries, err := app.carryoverStore().PendingFor(targetAt)
	if err != nil {
		return result, 0, err
	}
	for _, entry := range entries {
		article := entry.Article
		article.CarryoverID = entry.ID
		matched := false
		for index := range result.articles {
			if strings.TrimSpace(result.articles[index].Link) != strings.TrimSpace(article.Link) {
				continue
			}
			result.articles[index].CarryoverID = entry.ID
			article = result.articles[index]
			matched = true
			break
		}
		if !matched {
			result.articles = append(result.articles, article)
		}
		eligible := false
		for index := range result.seenArticles {
			if strings.TrimSpace(result.seenArticles[index].Link) == strings.TrimSpace(article.Link) {
				result.seenArticles[index].CarryoverID = entry.ID
				eligible = true
				break
			}
		}
		if !eligible {
			result.seenArticles = append(result.seenArticles, article)
		}
	}
	if len(entries) > 0 {
		logutil.Printf("Carryover injected: target=%s entries=%d", targetAt.In(app.displayLocation()).Format("2006-01-02 15:04"), len(entries))
	}
	return result, len(entries), nil
}

func carryoverIDs(articles []model.Article) []string {
	seen := make(map[string]struct{})
	ids := make([]string, 0)
	for _, article := range articles {
		id := strings.TrimSpace(article.CarryoverID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}
