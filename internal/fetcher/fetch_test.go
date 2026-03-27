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

	got, err := applyDedup(articles, false, true, NewSeenStore("output"))
	if err != nil {
		t.Fatalf("applyDedup() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(applyDedup(...)) = %d, want 1", len(got))
	}
}

func TestFetchWindowUsesExplicitBounds(t *testing.T) {
	oldFetch := fetchAllSources
	defer func() { fetchAllSources = oldFetch }()

	loc := time.FixedZone("CST", 8*3600)
	from := time.Date(2026, 3, 18, 8, 0, 0, 0, loc)
	to := time.Date(2026, 3, 18, 14, 0, 0, 0, loc)

	fetchAllSources = func(cfg *config.Config, since time.Time) ([]model.Article, []FailedSource, error) {
		return []model.Article{
			{Title: "before", Link: "1", Published: from.Add(-time.Minute)},
			{Title: "from", Link: "2", Published: from},
			{Title: "dup", Link: "2", Published: from.Add(time.Hour)},
			{Title: "to", Link: "3", Published: to},
			{Title: "after", Link: "4", Published: to.Add(time.Minute)},
		}, []FailedSource{{Name: "Reddit", Err: errors.New("403")}}, nil
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
	oldFetch := fetchAllSources
	defer func() { fetchAllSources = oldFetch }()

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
	fetchAllSources = func(cfg *config.Config, since time.Time) ([]model.Article, []FailedSource, error) {
		return []model.Article{{Title: "story", Link: "https://example.com/a", Published: from.Add(time.Hour)}}, []FailedSource{{Name: "Reddit", Err: errors.New("403")}}, nil
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
	fetchRedditSource = func(src config.Source, keywords []string, since time.Time) ([]model.Article, error) {
		order = append(order, src.Name)
		return nil, nil
	}

	sources := []config.Source{{Name: "r1"}, {Name: "r2"}, {Name: "r3"}}
	var failed []FailedSource
	fetchRedditSourcesSerially(sources, nil, time.Time{}, func(item FailedSource) {
		failed = append(failed, item)
	}, func(items []model.Article) {})

	if strings.Join(order, ",") != "r1,r2,r3" {
		t.Fatalf("order = %v", order)
	}
	if len(sleeps) != 2 || sleeps[0] != 2*time.Second || sleeps[1] != 2*time.Second {
		t.Fatalf("sleeps = %v", sleeps)
	}
}

func TestFetchAllSourcesSerializesRedditByType(t *testing.T) {
	oldFetch := fetchWithRetry
	_ = oldFetch
	oldSleep := sleep
	oldFetchReddit := fetchRedditSource
	defer func() {
		sleep = oldSleep
		fetchRedditSource = oldFetchReddit
	}()

	var order []string
	fetchRedditSource = func(src config.Source, keywords []string, since time.Time) ([]model.Article, error) {
		order = append(order, src.Name)
		return nil, nil
	}
	sleep = func(time.Duration) {}

	cfg := &config.Config{Sources: []config.Source{
		{Name: "reddit-1", Type: "reddit", URL: "https://api.example.com/reddit1"},
		{Name: "reddit-2", Type: "reddit", URL: "https://api.example.com/reddit2"},
	}}

	_, _, err := fetchAllSources(cfg, time.Time{})
	if err != nil {
		t.Fatalf("fetchAllSources() error = %v", err)
	}
	if strings.Join(order, ",") != "reddit-1,reddit-2" {
		t.Fatalf("order = %v", order)
	}
}
