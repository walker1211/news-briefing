package fetcher

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

type redditListing struct {
	Data struct {
		Children []struct {
			Data redditPost `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

type redditPost struct {
	Title     string  `json:"title"`
	URL       string  `json:"url"`
	Permalink string  `json:"permalink"`
	Score     int     `json:"score"`
	Created   float64 `json:"created_utc"`
	Selftext  string  `json:"selftext"`
}

func FetchReddit(source config.Source, keywords []string, since time.Time) ([]model.Article, error) {
	client := HTTPClient()

	req, err := http.NewRequest("GET", source.URL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http error: %d %s", resp.StatusCode, resp.Status)
	}

	var listing redditListing
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return nil, err
	}

	var articles []model.Article
	for _, child := range listing.Data.Children {
		post := child.Data
		pub := time.Unix(int64(post.Created), 0)

		if pub.Before(since) {
			continue
		}

		if source.Category != "国际政治" {
			if !matchKeywords(post.Title+" "+post.Selftext, keywords) {
				continue
			}
		}

		link := post.URL
		if link == "" || link == "https://www.reddit.com"+post.Permalink {
			link = "https://www.reddit.com" + post.Permalink
		}

		summary := post.Selftext
		if len(summary) > 300 {
			summary = summary[:300] + "..."
		}
		if summary == "" {
			summary = fmt.Sprintf("Score: %d", post.Score)
		}

		articles = append(articles, model.Article{
			Title:     post.Title,
			Link:      link,
			Summary:   summary,
			Source:    source.Name,
			Category:  source.Category,
			Published: pub,
		})
	}

	return articles, nil
}
