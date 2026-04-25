package main

import (
	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
	"testing"
	"time"
)

type contextTestKey struct{}

func executeTestConfig(t *testing.T, mode model.OutputMode) *config.Config {
	t.Helper()
	return &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: mode}}
}

func executeTestConfigWithEmail(t *testing.T, mode model.OutputMode) *config.Config {
	t.Helper()
	cfg := executeTestConfig(t, mode)
	cfg.Email = config.Email{
		SMTPHost: "smtp.example.com",
		SMTPPort: 465,
		From:     "from@example.com",
		To:       "to@example.com",
	}
	return cfg
}

func silentOutputDeps() outputDeps {
	return outputDeps{
		printText:     func(string) {},
		printFailed:   func([]fetcher.FailedSource) {},
		printArticles: func([]model.Article) {},
		printCLI:      func(*model.Briefing) {},
	}
}

func silentBriefingOutputDeps(body string) outputDeps {
	deps := silentOutputDeps()
	deps.composeBody = func(string, model.OutputMode, model.OutputContent) (string, error) {
		return body, nil
	}
	deps.writeMarkdown = func(*model.Briefing, string) (string, error) {
		return "", nil
	}
	return deps
}

func silentDeepDiveOutputDeps(body string) outputDeps {
	deps := silentOutputDeps()
	deps.composeBody = func(string, model.OutputMode, model.OutputContent) (string, error) {
		return body, nil
	}
	deps.writeDeepDive = func(string, string, string, string) (string, error) {
		return "", nil
	}
	return deps
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
