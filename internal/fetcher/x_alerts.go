package fetcher

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

const xAlertLookback = 24 * time.Hour

var xAlertSignalTermGroups = [][]string{
	{"launch", "launched", "上线", "推出"},
	{"release", "released", "发布"},
	{"shipping", "ships", "rollout", "available", "introducing"},
	{"announce", "announced", "宣布"},
	{"open source", "open-source", "open sourced", "开源"},
	{"acquire", "acquired", "acquisition", "收购"},
	{"partnership", "融资", "合作"},
	{"outage", "incident", "事故", "中断", "宕机"},
	{"vulnerability", "breach", "漏洞"},
}

func FetchXAlertsContext(ctx context.Context, cfg *config.Config, now time.Time) (FetchResult, error) {
	if cfg == nil {
		return FetchResult{}, nil
	}
	from := now.Add(-xAlertLookback)
	filters := newFilterContext(cfg)
	results, failed, err := fetchXVisibleNDJSON(ctx, cfg.XAccounts, filters.includeKeywords(model.Article{Category: cfg.XAccounts.Category}, config.Source{Name: xVisibleSourceName, Category: cfg.XAccounts.Category}), from, now)
	if err != nil {
		return FetchResult{}, err
	}

	alerts := make([]model.Article, 0)
	for _, result := range results {
		for _, candidate := range result.Candidates {
			matched, excluded := filterCandidate(candidate.Article, result.Source, filters)
			if len(matched) == 0 && len(candidate.MatchedKeywords) > 0 && len(filters.includeKeywords(candidate.Article, result.Source)) == 0 {
				matched = candidate.MatchedKeywords
			}
			if len(matched) == 0 || len(excluded) > 0 {
				continue
			}
			if !isXAlertCandidate(candidate.Article) {
				continue
			}
			alerts = append(alerts, candidate.Article)
		}
	}

	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].Published.After(alerts[j].Published)
	})

	return FetchResult{Articles: alerts, Failed: failed}, nil
}

func isXAlertCandidate(article model.Article) bool {
	score := xAlertSignalScore(article.Title + " " + article.Summary)
	if strings.HasPrefix(article.Source, "X/@") {
		return score >= 1
	}
	if strings.HasPrefix(article.Source, "X Search/") {
		return score >= 2
	}
	return false
}

func xAlertSignalScore(text string) int {
	normalized := strings.ToLower(text)
	score := 0
	for _, group := range xAlertSignalTermGroups {
		for _, term := range group {
			if strings.Contains(normalized, term) {
				score++
				break
			}
		}
	}
	return score
}
