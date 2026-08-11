package fetcher

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mmcdole/gofeed"
	"github.com/walker1211/news-briefing/internal/statefile"
)

const maxRSSFeedBytes = 32 * 1024 * 1024

type rssFeedCache struct {
	dir   string
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

type rssFeedCacheMetadata struct {
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
}

func newRSSFeedCache(dir string) *rssFeedCache {
	return &rssFeedCache{dir: dir, locks: make(map[string]*sync.Mutex)}
}

func (c *Client) fetchRSSFeedHTTP(ctx context.Context, feedURL, cacheKeyURL string, parser *gofeed.Parser) (*gofeed.Feed, http.Header, int64, string, error) {
	if c.rssCache == nil {
		return c.fetchRSSFeedNetwork(ctx, feedURL, parser, nil, "network")
	}
	return c.rssCache.fetch(ctx, c.httpClient, feedURL, cacheKeyURL, parser)
}

func (c *Client) fetchRSSFeedNetwork(ctx context.Context, feedURL string, parser *gofeed.Parser, metadata *rssFeedCacheMetadata, status string) (*gofeed.Feed, http.Header, int64, string, error) {
	req, err := newRSSFeedRequest(ctx, feedURL, metadata)
	if err != nil {
		return nil, nil, 0, status, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, 0, status, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, resp.Header, 0, status, fmt.Errorf("http error: %d %s", resp.StatusCode, resp.Status)
	}
	body, err := readRSSFeedBody(resp.Body)
	if err != nil {
		return nil, resp.Header, 0, status, err
	}
	feed, err := parser.Parse(bytes.NewReader(body))
	return feed, resp.Header, int64(len(body)), status, err
}

func (cache *rssFeedCache) fetch(ctx context.Context, client *http.Client, feedURL, cacheKeyURL string, parser *gofeed.Parser) (*gofeed.Feed, http.Header, int64, string, error) {
	key := rssCacheKey(cacheKeyURL)
	keyLock := cache.lockFor(key)
	keyLock.Lock()
	defer keyLock.Unlock()
	metadata, cachedBody, _ := cache.load(key)
	req, err := newRSSFeedRequest(ctx, feedURL, metadata)
	if err != nil {
		return nil, nil, 0, "network", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, 0, "network", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && len(cachedBody) > 0 {
		feed, parseErr := parser.Parse(bytes.NewReader(cachedBody))
		return feed, resp.Header, 0, "not_modified", parseErr
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, resp.Header, 0, "network", fmt.Errorf("http error: %d %s", resp.StatusCode, resp.Status)
	}
	body, err := readRSSFeedBody(resp.Body)
	if err != nil {
		return nil, resp.Header, 0, "network", err
	}
	feed, err := parser.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, resp.Header, int64(len(body)), "network", err
	}
	newMetadata := rssFeedCacheMetadata{ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}
	if saveErr := cache.save(key, newMetadata, body); saveErr != nil {
		return feed, resp.Header, int64(len(body)), "cache_write_failed", nil
	}
	return feed, resp.Header, int64(len(body)), "updated", nil
}

func (cache *rssFeedCache) lockFor(key string) *sync.Mutex {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if existing := cache.locks[key]; existing != nil {
		return existing
	}
	lock := &sync.Mutex{}
	cache.locks[key] = lock
	return lock
}

func newRSSFeedRequest(ctx context.Context, feedURL string, metadata *rssFeedCacheMetadata) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml;q=0.9, */*;q=0.8")
	req.Header.Set("User-Agent", userAgent)
	if metadata != nil {
		if metadata.ETag != "" {
			req.Header.Set("If-None-Match", metadata.ETag)
		}
		if metadata.LastModified != "" {
			req.Header.Set("If-Modified-Since", metadata.LastModified)
		}
	}
	return req, nil
}

func readRSSFeedBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxRSSFeedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxRSSFeedBytes {
		return nil, fmt.Errorf("RSS feed exceeds %d bytes", maxRSSFeedBytes)
	}
	return body, nil
}

func rssCacheKey(rawURL string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(rawURL)))
	return hex.EncodeToString(sum[:])
}

func (cache *rssFeedCache) load(key string) (*rssFeedCacheMetadata, []byte, error) {
	metadataBytes, err := os.ReadFile(filepath.Join(cache.dir, key+".json"))
	if err != nil {
		return nil, nil, err
	}
	var metadata rssFeedCacheMetadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return nil, nil, err
	}
	file, err := os.Open(filepath.Join(cache.dir, key+".xml.gz"))
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, nil, err
	}
	defer reader.Close()
	body, err := readRSSFeedBody(reader)
	return &metadata, body, err
}

func (cache *rssFeedCache) save(key string, metadata rssFeedCacheMetadata, body []byte) error {
	if err := os.MkdirAll(cache.dir, 0o755); err != nil {
		return err
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(body); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := statefile.WriteAtomic(filepath.Join(cache.dir, key+".xml.gz"), compressed.Bytes(), 0o644); err != nil {
		return err
	}
	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return statefile.WriteAtomic(filepath.Join(cache.dir, key+".json"), append(metadataBytes, '\n'), 0o644)
}
