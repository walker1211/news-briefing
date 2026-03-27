package fetcher

import (
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

func FetchHackerNews(source config.Source, keywords []string, since time.Time) ([]model.Article, error) {
	client := HTTPClient()

	resp, err := client.Get(hnBaseURL + "/topstories.json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var ids []int
	if err := json.NewDecoder(resp.Body).Decode(&ids); err != nil {
		return nil, err
	}

	if len(ids) > 60 {
		ids = ids[:60]
	}

	var (
		mu       sync.Mutex
		articles []model.Article
		wg       sync.WaitGroup
	)

	sem := make(chan struct{}, 10)

	for _, id := range ids {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			item, err := fetchHNItem(client, id)
			if err != nil || item.URL == "" {
				return
			}

			pub := time.Unix(item.Time, 0)
			if pub.Before(since) {
				return
			}

			if !matchKeywords(item.Title, keywords) {
				return
			}

			mu.Lock()
			articles = append(articles, model.Article{
				Title:     item.Title,
				Link:      item.URL,
				Summary:   fmt.Sprintf("HN Score: %d", item.Score),
				Source:    source.Name,
				Category:  source.Category,
				Published: pub,
			})
			mu.Unlock()
		}(id)
	}

	wg.Wait()
	return articles, nil
}

func fetchHNItem(client *http.Client, id int) (*hnItem, error) {
	resp, err := client.Get(fmt.Sprintf("%s/item/%d.json", hnBaseURL, id))
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
