package watch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoizeWatchArticleContentFetcherSharesConcurrentURLFetch(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	fetch := memoizeWatchArticleContentFetcher(func(context.Context, string) (watchArticleContent, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return watchArticleContent{title: "shared"}, nil
	})

	const workers = 12
	results := make(chan watchArticleContent, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			content, err := fetch(context.Background(), "https://example.com/article")
			results <- content
			errs <- err
		})
	}
	<-started
	close(release)
	wg.Wait()
	close(results)
	close(errs)

	if got := calls.Load(); got != 1 {
		t.Fatalf("underlying fetch calls = %d, want 1", got)
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("fetch() error = %v", err)
		}
	}
	for content := range results {
		if content.title != "shared" {
			t.Fatalf("content title = %q, want shared", content.title)
		}
	}
}

func TestMemoizeWatchArticleContentFetcherRetriesFailures(t *testing.T) {
	wantErr := errors.New("temporary")
	var calls atomic.Int32
	fetch := memoizeWatchArticleContentFetcher(func(context.Context, string) (watchArticleContent, error) {
		if calls.Add(1) == 1 {
			return watchArticleContent{}, wantErr
		}
		return watchArticleContent{title: "recovered"}, nil
	})

	if _, err := fetch(context.Background(), "https://example.com/article"); !errors.Is(err, wantErr) {
		t.Fatalf("first fetch error = %v, want %v", err, wantErr)
	}
	content, err := fetch(context.Background(), "https://example.com/article")
	if err != nil {
		t.Fatalf("second fetch error = %v", err)
	}
	if content.title != "recovered" || calls.Load() != 2 {
		t.Fatalf("second fetch = %#v, calls=%d", content, calls.Load())
	}
}

func TestMemoizeWatchArticleContentFetcherHonorsWaitingContext(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	fetch := memoizeWatchArticleContentFetcher(func(context.Context, string) (watchArticleContent, error) {
		close(started)
		<-release
		return watchArticleContent{title: "done"}, nil
	})

	go func() {
		_, _ = fetch(context.Background(), "https://example.com/article")
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := fetch(ctx, "https://example.com/article"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting fetch error = %v, want deadline exceeded", err)
	}
	close(release)
}
