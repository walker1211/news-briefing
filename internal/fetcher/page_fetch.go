package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
)

func FetchDocsPage(src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return FetchDocsPageContext(context.Background(), src, keywords, since)
}

func FetchDocsPageContext(ctx context.Context, src config.Source, _ []string, _ time.Time) (sourceFetchResult, error) {
	return fetchPageSource(ctx, src)
}

func FetchRepoPage(src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return FetchRepoPageContext(context.Background(), src, keywords, since)
}

func FetchRepoPageContext(ctx context.Context, src config.Source, _ []string, _ time.Time) (sourceFetchResult, error) {
	return fetchPageSource(ctx, src)
}

func fetchPageSource(ctx context.Context, src config.Source) (sourceFetchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return sourceFetchResult{}, fmt.Errorf("build page request: %w", err)
	}

	resp, err := HTTPClient().Do(req)
	if err != nil {
		return sourceFetchResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return sourceFetchResult{}, fmt.Errorf("fetch page: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return sourceFetchResult{}, fmt.Errorf("read page body: %w", err)
	}

	candidate, accepted, err := parsePageSource(src, string(body))
	if err != nil {
		return sourceFetchResult{}, err
	}
	if !accepted {
		return sourceFetchResult{Source: src}, nil
	}

	return sourceFetchResult{Source: src, Candidates: []fetchedCandidate{candidate}}, nil
}
