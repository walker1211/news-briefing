package fetcher

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/walker1211/news-briefing/internal/config"
)

func TestFetchRedditTruncatesSummaryOnUTF8Boundary(t *testing.T) {
	selftext := strings.Repeat("a", 299) + "中tail"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		payload := map[string]any{
			"data": map[string]any{
				"children": []any{map[string]any{
					"data": redditPost{
						Title: "UTF-8 summary", URL: "https://example.com/post", Score: 42,
						Created:  float64(time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC).Unix()),
						Selftext: selftext,
					},
				}},
			},
		}
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	result, err := NewClient(server.Client()).FetchReddit(config.Source{
		Name: "Test Reddit", URL: server.URL, Type: config.SourceTypeReddit, Category: "AI/科技",
	}, nil, time.Time{})
	if err != nil {
		t.Fatalf("FetchReddit() error = %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("len(result.Candidates) = %d, want 1", len(result.Candidates))
	}
	got := result.Candidates[0].Article.Summary
	if want := strings.Repeat("a", 299) + "..."; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("summary is not valid UTF-8: %q", got)
	}
}
