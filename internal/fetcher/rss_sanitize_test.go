package fetcher

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/walker1211/news-briefing/internal/config"
)

func rssControlCharsFixture() []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>测试 😀</title>
<item><title>第一条</title><link>https://example.test/one</link><description><![CDATA[中文` + string(rune(0x1e)) + ` 😀` + "\t\n\r" + `保留]]></description><enclosure url="https://example.test/one.jpg" type="image/jpeg" /></item>
<item><title>第二条</title><link>https://example.test/two</link><description><![CDATA[第二条` + string(rune(0x00)) + `摘要]]></description><enclosure url="https://example.test/two.jpg" type="image/jpeg" /></item>
</channel></rss>`)
}

func atomControlCharsFixture() []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"><title>Atom</title>
<entry><title>Atom 条目` + string(rune(0x1f)) + ` 😀</title><id>https://example.test/atom</id><link href="https://example.test/atom" /><content type="html"><![CDATA[内容` + string(rune(0x1e)) + "\t\n\r" + `]]></content></entry>
</feed>`)
}

func TestParseRSSFeedSanitizesControlCharsInRSSCDATA(t *testing.T) {
	feed, sanitized, err := parseRSSFeed(gofeed.NewParser(), rssControlCharsFixture())
	if err != nil {
		t.Fatalf("parseRSSFeed() error = %v", err)
	}
	if sanitized != 2 {
		t.Fatalf("sanitized = %d, want 2", sanitized)
	}
	if len(feed.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(feed.Items))
	}
	if got := feed.Items[0].Description; got != "中文 😀\t\n\r保留" {
		t.Fatalf("first description = %q", got)
	}
	if got := feed.Items[1].Description; got != "第二条摘要" {
		t.Fatalf("second description = %q", got)
	}
}

func TestParseRSSFeedSanitizesControlCharsInAtom(t *testing.T) {
	feed, sanitized, err := parseRSSFeed(gofeed.NewParser(), atomControlCharsFixture())
	if err != nil {
		t.Fatalf("parseRSSFeed() error = %v", err)
	}
	if sanitized != 2 {
		t.Fatalf("sanitized = %d, want 2", sanitized)
	}
	if len(feed.Items) != 1 || feed.Items[0].Title != "Atom 条目 😀" {
		t.Fatalf("items = %#v", feed.Items)
	}
	if got := feed.Items[0].Content; got != "内容\t\n\r" {
		t.Fatalf("content = %q", got)
	}
}

func TestParseRSSFeedSanitizeLeavesCleanXMLAndJSONUntouched(t *testing.T) {
	cleanRSS := []byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>clean</title><item><title>ok</title><link>https://example.test/ok</link></item></channel></rss>`)
	jsonFeed := []byte(`{"version":"https://jsonfeed.org/version/1.1","title":"JSON","items":[{"id":"one","url":"https://example.test/one","title":"ok\u001e"}]}`)
	for name, body := range map[string][]byte{"xml": cleanRSS, "json": jsonFeed} {
		t.Run(name, func(t *testing.T) {
			feed, sanitized, err := parseRSSFeed(gofeed.NewParser(), body)
			if err != nil {
				t.Fatalf("parseRSSFeed() error = %v", err)
			}
			if sanitized != 0 {
				t.Fatalf("sanitized = %d, want 0", sanitized)
			}
			wantTitle := "ok"
			if name == "json" {
				wantTitle += string(rune(0x1e))
			}
			if len(feed.Items) != 1 || feed.Items[0].Title != wantTitle {
				t.Fatalf("feed items = %#v", feed.Items)
			}
		})
	}
}

func TestParseRSSFeedSanitizeDoesNotRepairMalformedOrNonUTF8Input(t *testing.T) {
	truncatedWithControl := append([]byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><item><title>cut`), 0x1e)
	invalidUTF8 := append([]byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><item><title>`), 0xff)
	invalidUTF8 = append(invalidUTF8, 0x1e)
	invalidUTF8 = append(invalidUTF8, []byte(`</title></item></channel></rss>`)...)
	utf16 := []byte{0xff, 0xfe, '<', 0, 'r', 0, 's', 0, 's', 0, '>', 0, 0x1e, 0, '<', 0, '/', 0, 'r', 0, 's', 0, 's', 0, '>', 0}
	utf16NoBOM := []byte{'<', 0, 'r', 0, 's', 0, 's', 0, '>', 0, 0x1e, 0, '<', 0, '/', 0, 'r', 0, 's', 0, 's', 0, '>', 0}
	utf32NoBOM := []byte{'<', 0, 0, 0, 'r', 0, 0, 0, 's', 0, 0, 0, 's', 0, 0, 0, '>', 0, 0, 0, 0x1e, 0, 0, 0}
	numericReference := []byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><item><title>&#x1e;</title></item></channel></rss>`)
	_, sanitized, err := parseRSSFeed(gofeed.NewParser(), truncatedWithControl)
	if err == nil {
		t.Fatal("truncated XML with a control character unexpectedly succeeded")
	}
	if sanitized != 1 {
		t.Fatalf("truncated XML sanitized = %d, want 1", sanitized)
	}

	for name, body := range map[string][]byte{
		"invalid_utf8":      invalidUTF8,
		"utf16_bom":         utf16,
		"utf16_without_bom": utf16NoBOM,
		"utf32_without_bom": utf32NoBOM,
		"numeric_reference": numericReference,
	} {
		t.Run(name, func(t *testing.T) {
			_, sanitized, err := parseRSSFeed(gofeed.NewParser(), body)
			if err == nil {
				t.Fatal("parseRSSFeed() unexpectedly succeeded")
			}
			if sanitized != 0 {
				t.Fatalf("sanitized = %d, want 0", sanitized)
			}
		})
	}
}

func TestFetchRSSSanitizesNetworkAndNotModifiedCacheBodies(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write(rssControlCharsFixture())
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.SetRSSCacheDir(t.TempDir())
	source := config.Source{Name: "cached control chars", Type: config.SourceTypeRSS, URL: server.URL, Category: "AI"}
	first, err := client.FetchRSS(source, nil, time.Time{})
	if err != nil {
		t.Fatalf("first FetchRSS() error = %v", err)
	}
	second, err := client.FetchRSS(source, nil, time.Time{})
	if err != nil {
		t.Fatalf("second FetchRSS() error = %v", err)
	}
	for name, result := range map[string]sourceFetchResult{"first": first, "second": second} {
		if result.SanitizedControlChars != 2 || result.FetchedCount != 2 || len(result.Candidates) != 2 {
			t.Fatalf("%s result = %#v", name, result)
		}
	}
	if first.CacheStatus != "updated" || first.ResponseBytes != int64(len(rssControlCharsFixture())) || second.CacheStatus != "not_modified" || second.ResponseBytes != 0 {
		t.Fatalf("cache metrics = first:%#v second:%#v", first, second)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
}

func TestFetchRedditRSSSanitizesHTTPAndCurlFallbackWithoutExtraRequests(t *testing.T) {
	for name, status := range map[string]int{"http": http.StatusOK, "curl": http.StatusForbidden} {
		t.Run(name, func(t *testing.T) {
			var requests atomic.Int32
			client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests.Add(1)
				return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(rssControlCharsFixture()))), Request: req}, nil
			})})
			var curlCalls atomic.Int32
			client.fetchCurl = func(context.Context, string) ([]byte, error) {
				curlCalls.Add(1)
				return rssControlCharsFixture(), nil
			}
			result, err := client.FetchRSS(config.Source{Name: "reddit", Type: config.SourceTypeRSS, URL: "https://www.reddit.com/r/test/.rss", Category: "AI"}, nil, time.Time{})
			if err != nil {
				t.Fatalf("FetchRSS() error = %v", err)
			}
			if result.SanitizedControlChars != 2 || len(result.Candidates) != 2 || requests.Load() != 1 {
				t.Fatalf("result = %#v, requests = %d", result, requests.Load())
			}
			wantCurlCalls := int32(0)
			if status == http.StatusForbidden {
				wantCurlCalls = 1
			}
			if curlCalls.Load() != wantCurlCalls {
				t.Fatalf("curl calls = %d, want %d", curlCalls.Load(), wantCurlCalls)
			}
		})
	}
}

func TestSourceStatsAccumulatesSanitizedControlCharsAndMarshalsKey(t *testing.T) {
	source := config.Source{Name: "sanitized", Type: config.SourceTypeRSS, Category: "AI"}
	acc := newSourceStatsAccumulator(&config.Config{}, time.Time{}, time.Now())
	acc.countFetched(sourceFetchResult{Source: source, FetchedCount: 2, SanitizedControlChars: 3})
	acc.countFetched(sourceFetchResult{Source: source, FetchedCount: 1, SanitizedControlChars: 4})
	report := acc.build(nil)
	if len(report.Sources) != 1 || report.Sources[0].SanitizedControlChars != 7 || report.Totals.SanitizedControlChars != 7 {
		t.Fatalf("report = %#v", report)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	totals, ok := decoded["totals"].(map[string]any)
	if !ok || totals["sanitized_control_chars"] != float64(7) {
		t.Fatalf("JSON totals = %#v", decoded["totals"])
	}
}
