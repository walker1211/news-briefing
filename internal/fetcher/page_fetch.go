package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
)

func FetchDocsPage(src config.Source, _ []string, _ time.Time) (sourceFetchResult, error) {
	return fetchPageSource(src)
}

func FetchRepoPage(src config.Source, _ []string, _ time.Time) (sourceFetchResult, error) {
	return fetchPageSource(src)
}

func fetchPageSource(src config.Source) (sourceFetchResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
