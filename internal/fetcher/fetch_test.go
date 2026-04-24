package fetcher

import (
	"context"
	"errors"
	"io"
	"net/http"
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
	loc := time.FixedZone("CST", 8*3600)
	from := time.Date(2026, 3, 18, 8, 0, 0, 0, loc)
	to := time.Date(2026, 3, 18, 14, 0, 0, 0, loc)

	fetchAll := func(ctx context.Context, cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
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

	articles, failed, err := fetchWindowContext(context.Background(), &config.Config{}, from, to, false, true, fetchAll)
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
	fetchAll := func(ctx context.Context, cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
		return []sourceFetchResult{{
			Source: config.Source{Name: "RSS", Type: "rss", Category: "AI/科技"},
			Candidates: []fetchedCandidate{{
				Article:         model.Article{Title: "story", Link: "https://example.com/a", Published: from.Add(time.Hour)},
				MatchedKeywords: []string{"AI"},
			}},
		}}, []FailedSource{{Name: "Reddit", Err: errors.New("403")}}, nil
	}

	_, failed, err := fetchWindowContext(context.Background(), &config.Config{}, from, to, true, false, fetchAll)
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

func TestFetchWindowFiltersCandidatesAndPreservesFailures(t *testing.T) {
	from := time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)
	cfg := &config.Config{Output: config.OutputCfg{Dir: "output"}, Sources: []config.Source{
		{Name: "RSS", Type: "rss", Category: "AI/科技"},
		{Name: "HN", Type: "hackernews", Category: "AI/科技"},
	}}

	fetchAll := func(ctx context.Context, cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
		return []sourceFetchResult{{
			Source: config.Source{Name: "RSS", Type: "rss", Category: "AI/科技"},
			Candidates: []fetchedCandidate{
				{Article: model.Article{Title: "miss keyword", Link: "https://example.com/miss", Source: "RSS", Category: "AI/科技", Published: from.Add(time.Hour)}},
				{Article: model.Article{Title: "out window", Link: "https://example.com/window", Source: "RSS", Category: "AI/科技", Published: to.Add(time.Minute)}, MatchedKeywords: []string{"AI"}},
				{Article: model.Article{Title: "include", Link: "https://example.com/include", Source: "RSS", Category: "AI/科技", Published: from.Add(2 * time.Hour)}, MatchedKeywords: []string{"AI"}},
			},
		}}, []FailedSource{{Name: "HN", Err: errors.New("boom")}}, nil
	}

	articles, failed, err := fetchWindowContext(context.Background(), cfg, from, to, false, true, fetchAll)
	if err != nil {
		t.Fatalf("FetchWindow() error = %v", err)
	}
	if len(articles) != 1 || articles[0].Title != "include" {
		t.Fatalf("articles = %#v, want included article only", articles)
	}
	if len(failed) != 1 || failed[0].Name != "HN" {
		t.Fatalf("failed = %#v, want HN failure preserved", failed)
	}
}

func TestFetchWindowDedupsAcceptedCandidates(t *testing.T) {
	from := time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)
	cfg := &config.Config{Output: config.OutputCfg{Dir: "output"}, Sources: []config.Source{{Name: "RSS", Type: "rss", Category: "AI/科技"}}}

	fetchAll := func(ctx context.Context, cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
		return []sourceFetchResult{{
			Source: config.Source{Name: "RSS", Type: "rss", Category: "AI/科技"},
			Candidates: []fetchedCandidate{
				{Article: model.Article{Title: "first", Link: "https://example.com/dup", Source: "RSS", Category: "AI/科技", Published: from.Add(2 * time.Hour)}, MatchedKeywords: []string{"AI"}},
				{Article: model.Article{Title: "second", Link: "https://example.com/dup", Source: "RSS", Category: "AI/科技", Published: from.Add(time.Hour)}, MatchedKeywords: []string{"AI"}},
			},
		}}, nil, nil
	}

	articles, failed, err := fetchWindowContext(context.Background(), cfg, from, to, false, true, fetchAll)
	if err != nil {
		t.Fatalf("FetchWindow() error = %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want no failed sources", failed)
	}
	if len(articles) != 1 || articles[0].Title != "first" {
		t.Fatalf("articles = %#v, want first article only", articles)
	}
}

func TestFetchWindowIncludesDocsPageCandidate(t *testing.T) {
	from := time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	cfg := &config.Config{Output: config.OutputCfg{Dir: "output"}, Sources: []config.Source{{Name: "GLM Docs", Type: "docs_page", Category: "AI/科技", URL: "https://example.com/glm"}}}
	fetchers := stubSourceFetchers()
	fetchers.docsPage = func(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
		return sourceFetchResult{
			Source: src,
			Candidates: []fetchedCandidate{{
				Article:         model.Article{Title: "GLM-4.5 发布", Link: "https://example.com/glm", Source: "GLM Docs", Category: "AI/科技", Published: from.Add(3 * time.Hour)},
				MatchedKeywords: []string{"GLM"},
			}},
		}, nil
	}

	fetchAll := func(ctx context.Context, cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
		return fetchAllSourcesDetailedWith(ctx, cfg, since, fetchers, sleepContext)
	}
	articles, failed, err := fetchWindowContext(context.Background(), cfg, from, to, false, true, fetchAll)
	if err != nil {
		t.Fatalf("FetchWindow() error = %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want no failed sources", failed)
	}
	if len(articles) != 1 || articles[0].Title != "GLM-4.5 发布" {
		t.Fatalf("articles = %#v, want included docs page article", articles)
	}
}

func TestFetchWindowRejectsUnacceptedDocsPageCandidate(t *testing.T) {
	from := time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	cfg := &config.Config{Output: config.OutputCfg{Dir: "output"}, Sources: []config.Source{{Name: "No Time", Type: "docs_page", Category: "AI/科技", URL: "https://example.com/no-time"}}}
	fetchers := stubSourceFetchers()
	fetchers.docsPage = func(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
		return sourceFetchResult{Source: src}, nil
	}

	fetchAll := func(ctx context.Context, cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
		return fetchAllSourcesDetailedWith(ctx, cfg, since, fetchers, sleepContext)
	}
	articles, failed, err := fetchWindowContext(context.Background(), cfg, from, to, false, true, fetchAll)
	if err != nil {
		t.Fatalf("FetchWindow() error = %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want no failed sources", failed)
	}
	if len(articles) != 0 {
		t.Fatalf("articles = %#v, want no included articles", articles)
	}
}

func stubSourceFetchers() sourceFetchers {
	stub := func(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
		return sourceFetchResult{Source: src}, nil
	}
	return sourceFetchers{
		rss:        stub,
		hackernews: stub,
		reddit:     stub,
		docsPage:   stub,
		repoPage:   stub,
	}
}

func TestFetchWithRetryRejectsUnknownSourceType(t *testing.T) {
	src := config.Source{Name: "mystery", Type: "unknown"}
	_, err := fetchWithRetry(context.Background(), src, nil, time.Time{})
	if err == nil {
		t.Fatalf("fetchWithRetry() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "mystery") || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("error = %q, want source name/type", err.Error())
	}
}

func TestClientFetchAllSourcesRetriesRedditSerially(t *testing.T) {
	attempts := 0
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts < maxRetries {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Status:     "500 Internal Server Error",
				Body:       io.NopCloser(strings.NewReader("boom")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(`{"data":{"children":[]}}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	sleep := func(context.Context, time.Duration) error { return nil }
	_, failed, err := fetchAllSourcesDetailedWith(context.Background(), &config.Config{Sources: []config.Source{{Name: "reddit", Type: "reddit", URL: "https://example.com/reddit.json"}}}, time.Time{}, client.serialSourceFetchers(sleep), sleep)
	if err != nil {
		t.Fatalf("fetchAllSourcesDetailed() error = %v", err)
	}
	if attempts != maxRetries {
		t.Fatalf("attempts = %d, want %d", attempts, maxRetries)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want no failed sources", failed)
	}
}

func TestFetchRedditSourcesKeepsTwoSecondGapAndOrder(t *testing.T) {
	var sleeps []time.Duration
	sleep := func(ctx context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}

	var order []string
	fetchReddit := func(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
		order = append(order, src.Name)
		return sourceFetchResult{Source: src}, nil
	}

	sources := []config.Source{{Name: "r1"}, {Name: "r2"}, {Name: "r3"}}
	var failed []FailedSource
	fetchRedditSourcesSeriallyWith(context.Background(), sources, nil, time.Time{}, fetchReddit, sleep, func(item FailedSource) {
		failed = append(failed, item)
	}, func(items sourceFetchResult) {})

	if strings.Join(order, ",") != "r1,r2,r3" {
		t.Fatalf("order = %v", order)
	}
	if len(sleeps) != 2 || sleeps[0] != 2*time.Second || sleeps[1] != 2*time.Second {
		t.Fatalf("sleeps = %v", sleeps)
	}
}

func TestFetchWithRetryReturnsContextErrorWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetchWithRetry(ctx, config.Source{Name: "RSS", Type: "rss"}, nil, time.Time{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("fetchWithRetry() error = %v, want context.Canceled", err)
	}
}

func TestFetchWithRetryStopsDuringRetrySleepWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fetchers := stubSourceFetchers()
	fetchers.rss = func(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
		return sourceFetchResult{}, errors.New("temporary")
	}
	sleep := func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}

	_, err := fetchWithRetryUsing(ctx, config.Source{Name: "RSS", Type: "rss"}, nil, time.Time{}, fetchers, sleep)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("fetchWithRetry() error = %v, want context.Canceled", err)
	}
}

func TestFetchWindowContextReturnsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := FetchWindowContext(ctx, &config.Config{}, time.Now().Add(-time.Hour), time.Now(), false, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FetchWindowContext() error = %v, want context.Canceled", err)
	}
}

func TestFetchWindowContextReturnsContextErrorAfterFetchCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	from := time.Now().Add(-time.Hour)
	to := time.Now()
	cfg := &config.Config{Output: config.OutputCfg{Dir: t.TempDir()}}
	fetchAll := func(ctx context.Context, cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
		cancel()
		return []sourceFetchResult{{
			Source: config.Source{Name: "RSS", Type: "rss"},
			Candidates: []fetchedCandidate{{
				Article:         model.Article{Title: "story", Link: "https://example.com/a", Published: to},
				MatchedKeywords: []string{"AI"},
			}},
		}}, nil, nil
	}

	_, _, err := fetchWindowContext(ctx, cfg, from, to, true, false, fetchAll)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FetchWindowContext() error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.Output.Dir, "state", "seen.json")); !os.IsNotExist(err) {
		t.Fatalf("seen.json exists after cancelled fetch, err=%v", err)
	}
}

func TestFetchAllSourcesDetailedReturnsContextErrorAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fetchers := stubSourceFetchers()
	fetchers.rss = func(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
		cancel()
		return sourceFetchResult{
			Source: src,
			Candidates: []fetchedCandidate{{
				Article:         model.Article{Title: "story", Link: "https://example.com/a", Published: time.Now()},
				MatchedKeywords: []string{"AI"},
			}},
		}, nil
	}

	_, _, err := fetchAllSourcesDetailedWith(ctx, &config.Config{Sources: []config.Source{{Name: "RSS", Type: "rss"}}}, time.Now().Add(-time.Hour), fetchers, sleepContext)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("fetchAllSourcesDetailed() error = %v, want context.Canceled", err)
	}
}

func TestFetchRedditSourcesStopsDuringGapWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var order []string
	fetchReddit := func(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
		order = append(order, src.Name)
		return sourceFetchResult{Source: src}, nil
	}
	sleep := func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}

	var failed []FailedSource
	fetchRedditSourcesSeriallyWith(ctx, []config.Source{{Name: "r1"}, {Name: "r2"}}, nil, time.Time{}, fetchReddit, sleep, func(item FailedSource) {
		failed = append(failed, item)
	}, func(sourceFetchResult) {})

	if strings.Join(order, ",") != "r1" {
		t.Fatalf("order = %v, want only first source fetched", order)
	}
	if len(failed) != 1 || !errors.Is(failed[0].Err, context.Canceled) {
		t.Fatalf("failed = %#v, want context.Canceled failure", failed)
	}
}

func TestFetchAllSourcesSerializesRedditByType(t *testing.T) {
	var order []string
	fetchers := stubSourceFetchers()
	fetchers.reddit = func(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
		order = append(order, src.Name)
		return sourceFetchResult{Source: src}, nil
	}
	fetchers.rss = func(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
		return sourceFetchResult{Source: src}, nil
	}
	sleep := func(context.Context, time.Duration) error { return nil }

	cfg := &config.Config{Sources: []config.Source{
		{Name: "reddit-1", Type: "reddit", URL: "https://api.example.com/reddit1"},
		{Name: "reddit-2", Type: "reddit", URL: "https://api.example.com/reddit2"},
	}}

	_, _, err := fetchAllSourcesDetailedWith(context.Background(), cfg, time.Time{}, fetchers, sleep)
	if err != nil {
		t.Fatalf("fetchAllSourcesDetailed() error = %v", err)
	}
	if strings.Join(order, ",") != "reddit-1,reddit-2" {
		t.Fatalf("order = %v", order)
	}
}
