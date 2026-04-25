package main

import (
	"context"
	"errors"
	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/output"
	"github.com/walker1211/news-briefing/internal/watch"
	"strings"
	"testing"
	"time"
)

func TestRunDeepDiveContextPassesContextToFetcherAndRunner(t *testing.T) {
	ctx := context.WithValue(context.Background(), contextTestKey{}, "deep")
	fetchCtxOK := false
	deepCtxOK := false
	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeTranslatedOnly}},
		fetch: fetchDeps{
			fetchAllContext: func(got context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				fetchCtxOK = got.Value(contextTestKey{}) == "deep"
				return []model.Article{{Title: "OpenAI release", Summary: "OpenAI ships model", Category: "AI/科技"}}, nil, nil
			},
		},
		ai: aiDeps{
			deepDiveContext: func(got context.Context, topic string, articles []model.Article, loc *time.Location) (string, error) {
				deepCtxOK = got.Value(contextTestKey{}) == "deep"
				return "deep content", nil
			},
		},
		output: silentDeepDiveOutputDeps("COMPOSED DEEP"),
	}

	if err := app.runDeepDiveContext(ctx, deepCommand{topic: "OpenAI"}); err != nil {
		t.Fatalf("runDeepDiveContext() error = %v", err)
	}
	if !fetchCtxOK || !deepCtxOK {
		t.Fatalf("context propagation fetch=%v deep=%v", fetchCtxOK, deepCtxOK)
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
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				if markSeen {
					t.Fatalf("fetchAll() markSeen=%v, want false", markSeen)
				}
				return relevant, nil, nil
			},
		},
		ai: aiDeps{
			deepDiveContext: func(ctx context.Context, topic string, articles []model.Article, loc *time.Location) (string, error) {
				deepDiveCalled = true
				return "DEEP TRANSLATED", nil
			},
		},
		output: outputDeps{
			printFailed: func([]fetcher.FailedSource) {},
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				gotContent = content
				return "ORIGINAL ONLY", nil
			},
			writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
				wroteContent = content
				return "", nil
			},
			printText: func(s string) { printed = s },
		},
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

func TestRunDeepDiveUsesOutputModeComposedBody(t *testing.T) {
	relevant := sampleExecuteArticles()
	var gotPath string
	var gotContent model.OutputContent
	var wroteContent string
	var printed string

	app := &app{
		cfg: &config.Config{Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeBilingualOriginalFirst}},
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return relevant, nil, nil
			},
		},
		ai: aiDeps{
			deepDiveContext: func(ctx context.Context, topic string, articles []model.Article, loc *time.Location) (string, error) {
				return "DEEP TRANSLATED", nil
			},
		},
		output: outputDeps{
			printFailed: func([]fetcher.FailedSource) {},
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				gotPath, gotContent = path, content
				return "COMPOSED DEEP", nil
			},
			writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
				wroteContent = content
				return "", nil
			},
			printText: func(s string) { printed = s },
		},
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
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return relevant, failed, nil
			},
		},
		ai: aiDeps{
			deepDiveContext: func(ctx context.Context, topic string, articles []model.Article, loc *time.Location) (string, error) {
				return "DEEP TRANSLATED", nil
			},
		},
		output: silentDeepDiveOutputDeps("COMPOSED DEEP"),
		email: emailDeps{
			sendDeepEmail: func(topic string, briefing *model.Briefing, cfg *config.Config, gotFailed []fetcher.FailedSource) error {
				emailedTopic = topic
				emailedBriefing = briefing
				emailedCfg = cfg
				emailedFailed = gotFailed
				return nil
			},
		},
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
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return []model.Article{{Title: "AI bill", Summary: "AI bill summary"}}, nil, nil
			},
		},
		ai: aiDeps{
			deepDiveContext: func(ctx context.Context, topic string, articles []model.Article, loc *time.Location) (string, error) {
				return "你给的 3 条“相关新闻”与“美国 AI 数据中心暂停法案”主题不匹配。你希望我怎么继续？", nil
			},
		},
		output: outputDeps{
			printFailed: func([]fetcher.FailedSource) {},
			writeDeepDive: func(string, string, string, string) (string, error) {
				wrote = true
				return "", nil
			},
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
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return []model.Article{{Title: "Claude ships feature", Summary: "Claude update"}}, nil, nil
			},
		},
		output: silentDeepDiveOutputDeps("ORIGINAL ONLY"),
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error {
				t.Fatal("sendEmail() should not be used for deep")
				return nil
			},
			sendDeepEmail: func(topic string, briefing *model.Briefing, cfg *config.Config, gotFailed []fetcher.FailedSource) error {
				return nil
			},
		},
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
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return []model.Article{{Title: "Claude ships feature", Summary: "Claude update"}}, nil, nil
			},
		},
		output: outputDeps{
			printFailed: func([]fetcher.FailedSource) {},
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				return "ORIGINAL ONLY", nil
			},
			writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
				return "", nil
			},
			printText: func(s string) { printed = s },
		},
		email: emailDeps{
			sendDeepEmail: func(topic string, briefing *model.Briefing, cfg *config.Config, gotFailed []fetcher.FailedSource) error {
				return errors.New("smtp down")
			},
		},
	}

	if err := app.runDeepDive(deepCommand{topic: "Claude", sendEmail: true}); err != nil {
		t.Fatalf("runDeepDive() error = %v, want nil when email send fails", err)
	}
	if printed != "ORIGINAL ONLY" {
		t.Fatalf("printText() got = %q, want %q", printed, "ORIGINAL ONLY")
	}
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
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				fetchAllCalled = true
				return nil, nil, nil
			},
			fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				windowCalled = true
				if markSeen || !ignoreSeen {
					t.Fatalf("fetchWindow() markSeen=%v ignoreSeen=%v, want false true", markSeen, ignoreSeen)
				}
				if !from.Equal(now.Add(-12*time.Hour)) || !to.Equal(now) {
					t.Fatalf("fetchWindow() window = %v ~ %v, want %v ~ %v", from, to, now.Add(-12*time.Hour), now)
				}
				return []model.Article{{Title: "Claude ships feature", Summary: "Claude update"}}, nil, nil
			},
		},
		output: outputDeps{
			printFailed: func([]fetcher.FailedSource) {},
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				return "ORIGINAL ONLY", nil
			},
			writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
				wroteContent = content
				return "", nil
			},
			printText: func(string) {},
		},
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
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				fetchAllCalled = true
				return nil, nil, nil
			},
			fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
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
		},
		output: outputDeps{
			printFailed: func([]fetcher.FailedSource) {},
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				return "ORIGINAL ONLY", nil
			},
			writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
				wroteContent = content
				return "", nil
			},
			printText: func(string) {},
		},
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
		fetch: fetchDeps{
			fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				if markSeen || !ignoreSeen {
					t.Fatalf("fetchWindow() markSeen=%v ignoreSeen=%v, want false true", markSeen, ignoreSeen)
				}
				return []model.Article{{Title: "Claude ships feature", Summary: "Claude update"}}, nil, nil
			},
		},
		output: silentDeepDiveOutputDeps("ORIGINAL ONLY"),
		email: emailDeps{
			sendDeepEmail: func(topic string, briefing *model.Briefing, cfg *config.Config, gotFailed []fetcher.FailedSource) error {
				emailed = true
				return nil
			},
		},
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
		fetch: fetchDeps{
			fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				windowCalled = true
				want := time.Date(2026, 3, 18, 7, 0, 0, 0, loc)
				if !from.Equal(want) || !to.Equal(want) {
					t.Fatalf("fetchWindow() window = %v ~ %v, want %v ~ %v", from, to, want, want)
				}
				return []model.Article{article}, nil, nil
			},
		},
		output: silentDeepDiveOutputDeps("ORIGINAL ONLY"),
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
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return []model.Article{{Title: "Claude ships feature", Summary: "Claude update"}}, nil, nil
			},
		},
		output: outputDeps{
			printFailed: func([]fetcher.FailedSource) {},
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				return "ORIGINAL ONLY", nil
			},
			writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
				wroteDate = date
				return "", nil
			},
			printText: func(string) {},
		},
		email: emailDeps{
			sendDeepEmail: func(topic string, briefing *model.Briefing, cfg *config.Config, gotFailed []fetcher.FailedSource) error {
				emailedDate = briefing.Date
				return nil
			},
		},
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
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return nil, nil, nil
			},
		},
		ai: aiDeps{
			deepDiveContext: func(ctx context.Context, topic string, articles []model.Article, loc *time.Location) (string, error) {
				deepArticles = articles
				return "DEEP TRANSLATED", nil
			},
		},
		output: silentDeepDiveOutputDeps("COMPOSED DEEP"),
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
		fetch: fetchDeps{
			fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return []model.Article{{Title: "Claude ships feature", Summary: "Claude update"}}, nil, nil
			},
		},
		output: silentDeepDiveOutputDeps("ORIGINAL ONLY"),
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
		fetch: fetchDeps{
			fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
				return []model.Article{{Title: "Claude ships feature", Summary: "Claude update"}}, nil, nil
			},
		},
		output: outputDeps{
			printFailed: func([]fetcher.FailedSource) {},
			composeBody: func(path string, mode model.OutputMode, content model.OutputContent) (string, error) {
				return "ORIGINAL ONLY", nil
			},
			writeDeepDive: func(topic, content, outputDir, date string) (string, error) {
				wroteDate = date
				return "", nil
			},
			printText: func(string) {},
		},
		email: emailDeps{
			sendDeepEmail: func(topic string, briefing *model.Briefing, cfg *config.Config, gotFailed []fetcher.FailedSource) error {
				emailedDate = briefing.Date
				return nil
			},
		},
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
