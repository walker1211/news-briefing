package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

const hnBaseURL = "https://hacker-news.firebaseio.com/v0"

type hnItem struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Score int    `json:"score"`
	Time  int64  `json:"time"`
	Type  string `json:"type"`
}

func FetchHackerNews(source config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return FetchHackerNewsContext(context.Background(), source, keywords, since)
}

func (c *Client) FetchHackerNews(source config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return c.FetchHackerNewsContext(context.Background(), source, keywords, since)
}

func FetchHackerNewsContext(ctx context.Context, source config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return NewClient(HTTPClient()).FetchHackerNewsContext(ctx, source, keywords, since)
}

func (c *Client) FetchHackerNewsContext(ctx context.Context, source config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	client := c.httpClient

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hnBaseURL+"/topstories.json", nil)
	if err != nil {
		return sourceFetchResult{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return sourceFetchResult{}, err
	}
	defer resp.Body.Close()

	var ids []int
	if err := json.NewDecoder(resp.Body).Decode(&ids); err != nil {
		return sourceFetchResult{}, err
	}

	if len(ids) > 60 {
		ids = ids[:60]
	}

	var (
		mu         sync.Mutex
		candidates []fetchedCandidate
		wg         sync.WaitGroup
	)

	sem := make(chan struct{}, 10)

	for _, id := range ids {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			item, err := fetchHNItem(ctx, client, id)
			if err != nil || item.URL == "" {
				return
			}

			pub := time.Unix(item.Time, 0)

			mu.Lock()
			candidates = append(candidates, fetchedCandidate{
				Article: model.Article{
					Title:     item.Title,
					Link:      item.URL,
					Summary:   fmt.Sprintf("HN Score: %d", item.Score),
					Source:    source.Name,
					Category:  source.Category,
					Published: pub,
				},
				MatchedKeywords: matchedKeywords(item.Title, keywords),
			})
			mu.Unlock()
		}(id)
	}

	wg.Wait()
	if err := ctx.Err(); err != nil {
		return sourceFetchResult{}, err
	}
	return sourceFetchResult{Source: source, Candidates: candidates}, nil
}

func fetchHNItem(ctx context.Context, client *http.Client, id int) (*hnItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/item/%d.json", hnBaseURL, id), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var item hnItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, err
	}
	return &item, nil
}
