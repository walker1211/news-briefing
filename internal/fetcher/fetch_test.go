package fetcher

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

func TestFilterArticlesByWindowUsesHalfOpenInterval(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	from := time.Date(2026, 3, 18, 8, 0, 0, 0, loc)
	to := time.Date(2026, 3, 18, 14, 0, 0, 0, loc)

	articles := []model.Article{
		{Title: "before", Link: "1", Published: from.Add(-time.Minute)},
		{Title: "from", Link: "2", Published: from},
		{Title: "middle", Link: "3", Published: from.Add(3 * time.Hour)},
		{Title: "to", Link: "4", Published: to},
		{Title: "after", Link: "5", Published: to.Add(time.Minute)},
	}

	got := filterArticlesByWindow(articles, from, to)
	if len(got) != 2 {
		t.Fatalf("len(filterArticlesByWindow(...)) = %d, want 2", len(got))
	}
	if got[0].Title != "middle" || got[1].Title != "to" {
		t.Fatalf("filterArticlesByWindow(...) = %#v", got)
	}
}

func TestFilterArticlesByWindowDoesNotDuplicateBoundaryAcrossAdjacentWindows(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	boundary := time.Date(2026, 3, 18, 14, 0, 0, 0, loc)
	articles := []model.Article{{Title: "boundary", Link: "1", Published: boundary}}

	first := filterArticlesByWindow(articles, boundary.Add(-6*time.Hour), boundary)
	second := filterArticlesByWindow(articles, boundary, boundary.Add(6*time.Hour))
	if len(first) != 1 {
		t.Fatalf("len(first) = %d, want 1", len(first))
	}
	if len(second) != 0 {
		t.Fatalf("len(second) = %d, want 0", len(second))
	}
}

func TestApplyDedupUsesBatchModeWhenIgnoreSeen(t *testing.T) {
	articles := []model.Article{
		{Title: "first", Link: "https://example.com/a"},
		{Title: "second", Link: "https://example.com/a"},
	}

	outcome, err := applyDedup(articles, false, true, NewSeenStore("output"))
	if err != nil {
		t.Fatalf("applyDedup() error = %v", err)
	}
	if len(outcome.Articles) != 1 {
		t.Fatalf("len(applyDedup(...).Articles) = %d, want 1", len(outcome.Articles))
	}
	if _, ok := outcome.DuplicateKeys["https://example.com/a"]; !ok {
		t.Fatalf("DuplicateKeys = %#v, want duplicate link recorded", outcome.DuplicateKeys)
	}
}

func TestFetchWindowUsesExplicitBounds(t *testing.T) {
	oldFetch := fetchAllSourcesDetailed
	defer func() { fetchAllSourcesDetailed = oldFetch }()

	loc := time.FixedZone("CST", 8*3600)
	from := time.Date(2026, 3, 18, 8, 0, 0, 0, loc)
	to := time.Date(2026, 3, 18, 14, 0, 0, 0, loc)

	fetchAllSourcesDetailed = func(cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
		return []sourceFetchResult{{
			Source: config.Source{Name: "RSS", Type: "rss", Category: "AI/科技"},
			Candidates: []fetchedCandidate{
				{Article: model.Article{Title: "before", Link: "1", Published: from.Add(-time.Minute)}},
				{Article: model.Article{Title: "from", Link: "2", Published: from}},
				{Article: model.Article{Title: "dup", Link: "2", Published: from.Add(time.Hour)}, MatchedKeywords: []string{"AI"}},
				{Article: model.Article{Title: "to", Link: "3", Published: to}, MatchedKeywords: []string{"AI"}},
				{Article: model.Article{Title: "after", Link: "4", Published: to.Add(time.Minute)}, MatchedKeywords: []string{"AI"}},
			},
		}}, []FailedSource{{Name: "Reddit", Err: errors.New("403")}}, nil
	}

	articles, failed, err := FetchWindow(&config.Config{}, from, to, false, true)
	if err != nil {
		t.Fatalf("FetchWindow() error = %v", err)
	}
	if len(failed) != 1 {
		t.Fatalf("len(failed) = %d, want 1", len(failed))
	}
	if len(articles) != 2 {
		t.Fatalf("len(articles) = %d, want 2", len(articles))
	}
	if articles[0].Title != "to" || articles[1].Title != "dup" {
		t.Fatalf("FetchWindow() articles = %#v", articles)
	}
}

func TestFetchWindowReturnsFailedSourcesWhenDedupErrors(t *testing.T) {
	oldFetch := fetchAllSourcesDetailed
	defer func() { fetchAllSourcesDetailed = oldFetch }()

	dir := t.TempDir()
	canonicalDir := filepath.Join(dir, "output", "state")
	if err := os.MkdirAll(canonicalDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(canonicalDir, "seen.json"), []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	from := time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)
	fetchAllSourcesDetailed = func(cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
		return []sourceFetchResult{{
			Source: config.Source{Name: "RSS", Type: "rss", Category: "AI/科技"},
			Candidates: []fetchedCandidate{{
				Article:         model.Article{Title: "story", Link: "https://example.com/a", Published: from.Add(time.Hour)},
				MatchedKeywords: []string{"AI"},
			}},
		}}, []FailedSource{{Name: "Reddit", Err: errors.New("403")}}, nil
	}

	_, failed, err := FetchWindow(&config.Config{}, from, to, true, false)
	if err == nil {
		t.Fatalf("FetchWindow() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "parse seen store") {
		t.Fatalf("FetchWindow() error = %q, want parse seen store error", err.Error())
	}
	if len(failed) != 1 || failed[0].Name != "Reddit" {
		t.Fatalf("failed = %#v, want failed source preserved", failed)
	}
}

func TestFetchWindowWithIndexTracksSourceRunsAndTraces(t *testing.T) {
	oldFetch := fetchAllSourcesDetailed
	defer func() { fetchAllSourcesDetailed = oldFetch }()

	from := time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)
	cfg := &config.Config{Output: config.OutputCfg{Dir: "output"}, Sources: []config.Source{
		{Name: "RSS", Type: "rss", Category: "AI/科技"},
		{Name: "HN", Type: "hackernews", Category: "AI/科技"},
	}}

	fetchAllSourcesDetailed = func(cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
		return []sourceFetchResult{
			{
				Source: config.Source{Name: "RSS", Type: "rss", Category: "AI/科技"},
				Candidates: []fetchedCandidate{
					{Article: model.Article{Title: "miss keyword", Link: "https://example.com/miss", Source: "RSS", Category: "AI/科技", Published: from.Add(time.Hour)}},
					{Article: model.Article{Title: "out window", Link: "https://example.com/window", Source: "RSS", Category: "AI/科技", Published: to.Add(time.Minute)}, MatchedKeywords: []string{"AI"}},
					{Article: model.Article{Title: "include", Link: "https://example.com/include", Source: "RSS", Category: "AI/科技", Published: from.Add(2 * time.Hour)}, MatchedKeywords: []string{"AI"}},
				},
			},
		}, []FailedSource{{Name: "HN", Err: errors.New("boom")}}, nil
	}

	articles, failed, index, err := FetchWindowWithIndex(cfg, from, to, false, true)
	if err != nil {
		t.Fatalf("FetchWindowWithIndex() error = %v", err)
	}
	if len(articles) != 1 || articles[0].Title != "include" {
		t.Fatalf("articles = %#v, want included article only", articles)
	}
	if len(failed) != 1 || failed[0].Name != "HN" {
		t.Fatalf("failed = %#v, want HN failure preserved", failed)
	}
	if len(index.SourceRuns) != 2 {
		t.Fatalf("len(index.SourceRuns) = %d, want 2", len(index.SourceRuns))
	}

	var rssRun, hnRun *model.SourceRun
	for i := range index.SourceRuns {
		run := &index.SourceRuns[i]
		switch run.Name {
		case "RSS":
			rssRun = run
		case "HN":
			hnRun = run
		}
	}
	if rssRun == nil || hnRun == nil {
		t.Fatalf("SourceRuns = %#v, want RSS and HN entries", index.SourceRuns)
	}
	if rssRun.Status != "success" || rssRun.FetchedCount != 3 || rssRun.KeywordMissCount != 1 || rssRun.WindowMissCount != 1 || rssRun.IncludedCount != 1 {
		t.Fatalf("rssRun = %#v", *rssRun)
	}
	if hnRun.Status != string(model.TraceStatusFetchFailed) || !strings.Contains(hnRun.Error, "boom") {
		t.Fatalf("hnRun = %#v", *hnRun)
	}

	statusByLink := map[string]model.ArticleTrace{}
	for _, trace := range index.ArticleTraces {
		statusByLink[trace.Link] = trace
	}
	if got := statusByLink["https://example.com/miss"]; got.Status != model.TraceStatusKeywordMiss || got.RejectionReason != string(model.TraceStatusKeywordMiss) {
		t.Fatalf("keyword miss trace = %#v", got)
	}
	if got := statusByLink["https://example.com/window"]; got.Status != model.TraceStatusOutOfWindow || got.RejectionReason != string(model.TraceStatusOutOfWindow) {
		t.Fatalf("out of window trace = %#v", got)
	}
	if got := statusByLink["https://example.com/include"]; got.Status != model.TraceStatusIncluded || got.RejectionReason != "" || len(got.MatchedKeywords) != 1 {
		t.Fatalf("included trace = %#v", got)
	}
}

func TestFetchWindowWithIndexTracksDuplicateCandidatesPerTrace(t *testing.T) {
	oldFetch := fetchAllSourcesDetailed
	defer func() { fetchAllSourcesDetailed = oldFetch }()

	from := time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)
	cfg := &config.Config{Output: config.OutputCfg{Dir: "output"}, Sources: []config.Source{{Name: "RSS", Type: "rss", Category: "AI/科技"}}}

	fetchAllSourcesDetailed = func(cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
		return []sourceFetchResult{{
			Source: config.Source{Name: "RSS", Type: "rss", Category: "AI/科技"},
			Candidates: []fetchedCandidate{
				{Article: model.Article{Title: "first", Link: "https://example.com/dup", Source: "RSS", Category: "AI/科技", Published: from.Add(2 * time.Hour)}, MatchedKeywords: []string{"AI"}},
				{Article: model.Article{Title: "second", Link: "https://example.com/dup", Source: "RSS", Category: "AI/科技", Published: from.Add(time.Hour)}, MatchedKeywords: []string{"AI"}},
			},
		}}, nil, nil
	}

	articles, failed, index, err := FetchWindowWithIndex(cfg, from, to, false, true)
	if err != nil {
		t.Fatalf("FetchWindowWithIndex() error = %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want no failed sources", failed)
	}
	if len(articles) != 1 || articles[0].Title != "first" {
		t.Fatalf("articles = %#v, want first article only", articles)
	}
	if len(index.ArticleTraces) != 2 {
		t.Fatalf("len(index.ArticleTraces) = %d, want 2", len(index.ArticleTraces))
	}

	statuses := map[string]model.TraceStatus{}
	for _, trace := range index.ArticleTraces {
		statuses[trace.Title] = trace.Status
	}
	if statuses["first"] != model.TraceStatusIncluded {
		t.Fatalf("first trace status = %q, want %q", statuses["first"], model.TraceStatusIncluded)
	}
	if statuses["second"] != model.TraceStatusDuplicateInBatch {
		t.Fatalf("second trace status = %q, want %q", statuses["second"], model.TraceStatusDuplicateInBatch)
	}
}

func TestFetchWindowWithIndexKeepsNewestTraceWhenDuplicateOrderDiffers(t *testing.T) {
	oldFetch := fetchAllSourcesDetailed
	defer func() { fetchAllSourcesDetailed = oldFetch }()

	from := time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)
	cfg := &config.Config{Output: config.OutputCfg{Dir: "output"}, Sources: []config.Source{{Name: "RSS", Type: "rss", Category: "AI/科技"}}}

	fetchAllSourcesDetailed = func(cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
		return []sourceFetchResult{{
			Source: config.Source{Name: "RSS", Type: "rss", Category: "AI/科技"},
			Candidates: []fetchedCandidate{
				{Article: model.Article{Title: "older", Link: "https://example.com/dup-order", Source: "RSS", Category: "AI/科技", Published: from.Add(time.Hour)}, MatchedKeywords: []string{"AI"}},
				{Article: model.Article{Title: "newer", Link: "https://example.com/dup-order", Source: "RSS", Category: "AI/科技", Published: from.Add(2 * time.Hour)}, MatchedKeywords: []string{"AI"}},
			},
		}}, nil, nil
	}

	articles, _, index, err := FetchWindowWithIndex(cfg, from, to, false, true)
	if err != nil {
		t.Fatalf("FetchWindowWithIndex() error = %v", err)
	}
	if len(articles) != 1 || articles[0].Title != "newer" {
		t.Fatalf("articles = %#v, want newer article only", articles)
	}

	statuses := map[string]model.TraceStatus{}
	for _, trace := range index.ArticleTraces {
		statuses[trace.Title] = trace.Status
	}
	if statuses["newer"] != model.TraceStatusIncluded {
		t.Fatalf("newer trace status = %q, want %q", statuses["newer"], model.TraceStatusIncluded)
	}
	if statuses["older"] != model.TraceStatusDuplicateInBatch {
		t.Fatalf("older trace status = %q, want %q", statuses["older"], model.TraceStatusDuplicateInBatch)
	}
}

func TestFetchWindowWithIndexDoesNotOverwriteRejectedTraceWithDedupResult(t *testing.T) {
	oldFetch := fetchAllSourcesDetailed
	defer func() { fetchAllSourcesDetailed = oldFetch }()

	from := time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)
	cfg := &config.Config{Output: config.OutputCfg{Dir: "output"}, Sources: []config.Source{{Name: "RSS", Type: "rss", Category: "AI/科技"}}}

	fetchAllSourcesDetailed = func(cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
		return []sourceFetchResult{{
			Source: config.Source{Name: "RSS", Type: "rss", Category: "AI/科技"},
			Candidates: []fetchedCandidate{
				{Article: model.Article{Title: "miss", Link: "https://example.com/mixed", Source: "RSS", Category: "AI/科技", Published: from.Add(time.Hour)}},
				{Article: model.Article{Title: "include", Link: "https://example.com/mixed", Source: "RSS", Category: "AI/科技", Published: from.Add(2 * time.Hour)}, MatchedKeywords: []string{"AI"}},
			},
		}}, nil, nil
	}

	articles, _, index, err := FetchWindowWithIndex(cfg, from, to, false, true)
	if err != nil {
		t.Fatalf("FetchWindowWithIndex() error = %v", err)
	}
	if len(articles) != 1 || articles[0].Title != "include" {
		t.Fatalf("articles = %#v, want include article only", articles)
	}

	statusByTitle := map[string]model.TraceStatus{}
	reasonByTitle := map[string]string{}
	for _, trace := range index.ArticleTraces {
		statusByTitle[trace.Title] = trace.Status
		reasonByTitle[trace.Title] = trace.RejectionReason
	}
	if statusByTitle["miss"] != model.TraceStatusKeywordMiss || reasonByTitle["miss"] != string(model.TraceStatusKeywordMiss) {
		t.Fatalf("miss trace = status %q reason %q, want keyword_miss", statusByTitle["miss"], reasonByTitle["miss"])
	}
	if statusByTitle["include"] != model.TraceStatusIncluded || reasonByTitle["include"] != "" {
		t.Fatalf("include trace = status %q reason %q, want included", statusByTitle["include"], reasonByTitle["include"])
	}
}

func TestFetchWindowWithIndexMarksAllAcceptedDuplicatesAsSeenBefore(t *testing.T) {
	oldFetch := fetchAllSourcesDetailed
	defer func() { fetchAllSourcesDetailed = oldFetch }()

	dir := t.TempDir()
	canonicalDir := filepath.Join(dir, "output", "state")
	if err := os.MkdirAll(canonicalDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	seenContent := `[
	  {"url":"https://example.com/seen-dup","time":"2026-03-17T10:00:00Z"}
	]`
	if err := os.WriteFile(filepath.Join(canonicalDir, "seen.json"), []byte(seenContent), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	from := time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)
	cfg := &config.Config{Output: config.OutputCfg{Dir: "output"}, Sources: []config.Source{{Name: "RSS", Type: "rss", Category: "AI/科技"}}}

	fetchAllSourcesDetailed = func(cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
		return []sourceFetchResult{{
			Source: config.Source{Name: "RSS", Type: "rss", Category: "AI/科技"},
			Candidates: []fetchedCandidate{
				{Article: model.Article{Title: "older", Link: "https://example.com/seen-dup", Source: "RSS", Category: "AI/科技", Published: from.Add(time.Hour)}, MatchedKeywords: []string{"AI"}},
				{Article: model.Article{Title: "newer", Link: "https://example.com/seen-dup", Source: "RSS", Category: "AI/科技", Published: from.Add(2 * time.Hour)}, MatchedKeywords: []string{"AI"}},
			},
		}}, nil, nil
	}

	articles, _, index, err := FetchWindowWithIndex(cfg, from, to, false, false)
	if err != nil {
		t.Fatalf("FetchWindowWithIndex() error = %v", err)
	}
	if len(articles) != 0 {
		t.Fatalf("articles = %#v, want no included articles", articles)
	}

	statusByTitle := map[string]model.TraceStatus{}
	reasonByTitle := map[string]string{}
	for _, trace := range index.ArticleTraces {
		statusByTitle[trace.Title] = trace.Status
		reasonByTitle[trace.Title] = trace.RejectionReason
	}
	if statusByTitle["older"] != model.TraceStatusSeenBefore || reasonByTitle["older"] != string(model.TraceStatusSeenBefore) {
		t.Fatalf("older trace = status %q reason %q, want seen_before", statusByTitle["older"], reasonByTitle["older"])
	}
	if statusByTitle["newer"] != model.TraceStatusSeenBefore || reasonByTitle["newer"] != string(model.TraceStatusSeenBefore) {
		t.Fatalf("newer trace = status %q reason %q, want seen_before", statusByTitle["newer"], reasonByTitle["newer"])
	}
}

func TestFetchWithRetryRejectsUnknownSourceType(t *testing.T) {
	src := config.Source{Name: "mystery", Type: "unknown"}
	_, err := fetchWithRetry(src, nil, time.Time{})
	if err == nil {
		t.Fatalf("fetchWithRetry() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "mystery") || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("error = %q, want source name/type", err.Error())
	}
}

func TestFetchRedditSourcesKeepsTwoSecondGapAndOrder(t *testing.T) {
	oldSleep := sleep
	oldFetchReddit := fetchRedditSource
	defer func() {
		sleep = oldSleep
		fetchRedditSource = oldFetchReddit
	}()

	var sleeps []time.Duration
	sleep = func(d time.Duration) {
		sleeps = append(sleeps, d)
	}

	var order []string
	fetchRedditSource = func(src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
		order = append(order, src.Name)
		return sourceFetchResult{Source: src}, nil
	}

	sources := []config.Source{{Name: "r1"}, {Name: "r2"}, {Name: "r3"}}
	var failed []FailedSource
	fetchRedditSourcesSerially(sources, nil, time.Time{}, func(item FailedSource) {
		failed = append(failed, item)
	}, func(items sourceFetchResult) {})

	if strings.Join(order, ",") != "r1,r2,r3" {
		t.Fatalf("order = %v", order)
	}
	if len(sleeps) != 2 || sleeps[0] != 2*time.Second || sleeps[1] != 2*time.Second {
		t.Fatalf("sleeps = %v", sleeps)
	}
}

func TestFetchAllSourcesSerializesRedditByType(t *testing.T) {
	oldSleep := sleep
	oldFetchReddit := fetchRedditSource
	oldFetchRSS := fetchRSSSource
	defer func() {
		sleep = oldSleep
		fetchRedditSource = oldFetchReddit
		fetchRSSSource = oldFetchRSS
	}()

	var order []string
	fetchRedditSource = func(src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
		order = append(order, src.Name)
		return sourceFetchResult{Source: src}, nil
	}
	fetchRSSSource = func(src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
		return sourceFetchResult{Source: src}, nil
	}
	sleep = func(time.Duration) {}

	cfg := &config.Config{Sources: []config.Source{
		{Name: "reddit-1", Type: "reddit", URL: "https://api.example.com/reddit1"},
		{Name: "reddit-2", Type: "reddit", URL: "https://api.example.com/reddit2"},
	}}

	_, _, err := fetchAllSourcesDetailed(cfg, time.Time{})
	if err != nil {
		t.Fatalf("fetchAllSourcesDetailed() error = %v", err)
	}
	if strings.Join(order, ",") != "reddit-1,reddit-2" {
		t.Fatalf("order = %v", order)
	}
}
