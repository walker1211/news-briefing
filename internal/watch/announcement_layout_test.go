package watch

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

const (
	claudeReleaseNotesLayoutTestURL  = "https://platform.claude.com/docs/en/release-notes/overview"
	claudeReleaseNotesLayoutTestDate = "july-24-2026"
)

func releaseNotesLayoutTestHeading(layout, fragment string) string {
	switch layout {
	case "legacy":
		return fmt.Sprintf(`<h3><div id="%s"><div>July 24, 2026</div></div></h3>`, fragment)
	case "current":
		return fmt.Sprintf(`<h3 id="%s">July 24, 2026</h3>`, fragment)
	default:
		panic("unknown release notes test layout: " + layout)
	}
}

func releaseNotesLayoutTestHTML(layout string) string {
	return `<html><body><main>` +
		releaseNotesLayoutTestHeading(layout, claudeReleaseNotesLayoutTestDate) +
		`<ul>` +
		`<li>We've launched <a href="https://www.anthropic.com/news/claude-opus-5">Claude Opus 5</a>, the latest model.</li>` +
		`<li>It improves coding reliability.</li>` +
		`</ul>` +
		`<p>This later content block must not be included.</p>` +
		`<h3 id="june-18-2026">June 18, 2026</h3>` +
		`<ul><li>An older release note.</li></ul>` +
		`</main></body></html>`
}

func TestClaudeReleaseNotesOverviewLayoutsHaveSameSnapshotAndArticle(t *testing.T) {
	const (
		wantTitle   = "We've launched Claude Opus 5"
		wantSummary = "We've launched Claude Opus 5, the latest model."
		wantBody    = "We've launched Claude Opus 5, the latest model. It improves coding reliability."
	)

	tests := []struct {
		name   string
		layout string
	}{
		{name: "legacy nested date id", layout: "legacy"},
		{name: "current heading date id", layout: "current"},
	}

	var snapshots []struct {
		items []model.WatchIndexItem
		hash  string
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html := releaseNotesLayoutTestHTML(tt.layout)
			snapshot, err := parseAnthropicAnnouncementIndex(
				"Claude Platform Release Notes",
				claudeReleaseNotesLayoutTestURL,
				html,
			)
			if err != nil {
				t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
			}
			if len(snapshot.Items) != 2 {
				t.Fatalf("len(snapshot.Items) = %d, want 2; items=%#v", len(snapshot.Items), snapshot.Items)
			}
			item := snapshot.Items[0]
			if item.Title != wantTitle {
				t.Errorf("item.Title = %q, want %q", item.Title, wantTitle)
			}
			wantURL := claudeReleaseNotesLayoutTestURL + "#" + claudeReleaseNotesLayoutTestDate
			if item.URL != wantURL {
				t.Errorf("item.URL = %q, want %q", item.URL, wantURL)
			}
			if item.Position != 1 {
				t.Errorf("item.Position = %d, want 1", item.Position)
			}
			if item.Snippet != wantSummary {
				t.Errorf("item.Snippet = %q, want %q", item.Snippet, wantSummary)
			}
			if item.ItemHash == "" {
				t.Error("item.ItemHash is empty")
			}

			title, summary, body, err := parseAnnouncementArticleFromURL(item.URL, html)
			if err != nil {
				t.Fatalf("parseAnnouncementArticleFromURL() error = %v", err)
			}
			if title != wantTitle {
				t.Errorf("article title = %q, want %q", title, wantTitle)
			}
			if summary != wantSummary {
				t.Errorf("article summary = %q, want %q", summary, wantSummary)
			}
			if body != wantBody {
				t.Errorf("article body = %q, want %q", body, wantBody)
			}

			snapshots = append(snapshots, struct {
				items []model.WatchIndexItem
				hash  string
			}{items: snapshot.Items, hash: snapshot.Hash})
		})
	}

	if len(snapshots) != len(tests) {
		t.Fatalf("got %d snapshots, want %d", len(snapshots), len(tests))
	}
	if !reflect.DeepEqual(snapshots[0].items, snapshots[1].items) {
		t.Fatalf("legacy/current items differ: legacy=%#v current=%#v", snapshots[0].items, snapshots[1].items)
	}
	if snapshots[0].hash != snapshots[1].hash {
		t.Fatalf("legacy/current snapshot hashes differ: legacy=%q current=%q", snapshots[0].hash, snapshots[1].hash)
	}
}

func TestParseClaudeReleaseNotesIndexDeduplicatesSameDateIDs(t *testing.T) {
	html := `<html><body><main>
  <h3 id="july-24-2026"><div id="july-24-2026">July 24, 2026</div></h3>
  <ul><li>We've launched <a href="https://www.anthropic.com/news/claude-opus-5">Claude Opus 5</a>, first copy.</li></ul>
  <h3><div id="july-24-2026">July 24, 2026</div></h3>
  <ul><li>Duplicate copy that must not become another item.</li></ul>
  <h3 id="june-18-2026">June 18, 2026</h3>
  <ul><li>An older release note.</li></ul>
</main></body></html>`

	snapshot, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", claudeReleaseNotesLayoutTestURL, html)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
	}
	if len(snapshot.Items) != 2 {
		t.Fatalf("len(snapshot.Items) = %d, want 2; items=%#v", len(snapshot.Items), snapshot.Items)
	}
	if snapshot.Items[0].Title != "We've launched Claude Opus 5" {
		t.Fatalf("first duplicate won with title = %q, want first item's title", snapshot.Items[0].Title)
	}
	if snapshot.Items[0].URL != claudeReleaseNotesLayoutTestURL+"#july-24-2026" {
		t.Fatalf("first item URL = %q", snapshot.Items[0].URL)
	}
	if snapshot.Items[1].URL != claudeReleaseNotesLayoutTestURL+"#june-18-2026" {
		t.Fatalf("second item URL = %q, want next date only", snapshot.Items[1].URL)
	}
	if snapshot.Items[0].Position != 1 || snapshot.Items[1].Position != 2 {
		t.Fatalf("item positions = %d, %d; want 1, 2", snapshot.Items[0].Position, snapshot.Items[1].Position)
	}
}

func TestParseClaudeReleaseNotesIndexFiltersNonDateHeadings(t *testing.T) {
	html := `<html><body><main>
  <h1 id="july-24-2026">A page title, not an entry.</h1>
  <h3 id="plugins">Plugins</h3>
  <ul><li>Not a dated release note.</li></ul>
  <h3 id="2026-07-24">Wrong date format.</h3>
  <ul><li>Another non-entry.</li></ul>
  <h3><div id="july-24-2026-extra">Date with suffix.</div></h3>
  <ul><li>Still not an entry.</li></ul>
  <h3 id="july-24-2026">July 24, 2026</h3>
  <ul><li>A valid dated release note.</li></ul>
</main></body></html>`

	snapshot, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", claudeReleaseNotesLayoutTestURL, html)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("len(snapshot.Items) = %d, want 1; items=%#v", len(snapshot.Items), snapshot.Items)
	}
	if snapshot.Items[0].URL != claudeReleaseNotesLayoutTestURL+"#july-24-2026" {
		t.Fatalf("item URL = %q, want only exact date fragment", snapshot.Items[0].URL)
	}
	if snapshot.Items[0].Title != "A valid dated release note." {
		t.Fatalf("item title = %q", snapshot.Items[0].Title)
	}
}

func TestParseClaudeReleaseNotesOverviewArticleStopsAtNextHeading(t *testing.T) {
	stops := []string{"h1", "h2", "h3"}
	layouts := []string{"legacy", "current"}
	for _, layout := range layouts {
		for _, stop := range stops {
			t.Run(layout+" stops at "+stop, func(t *testing.T) {
				html := `<html><body><main>` +
					releaseNotesLayoutTestHeading(layout, claudeReleaseNotesLayoutTestDate) +
					`<div class="separator"></div>` +
					fmt.Sprintf(`<%s>Next section</%s><ul><li>Content from the next section.</li></ul>`, stop, stop) +
					`</main></body></html>`
				_, summary, body, err := parseAnnouncementArticleFromURL(
					claudeReleaseNotesLayoutTestURL+"#"+claudeReleaseNotesLayoutTestDate,
					html,
				)
				if err == nil || !strings.Contains(err.Error(), "release notes entry content not found") {
					t.Fatalf("parseAnnouncementArticleFromURL() error = %v, want empty-content error", err)
				}
				if summary != "" || body != "" {
					t.Fatalf("summary/body = %q/%q, want empty when next heading blocks content", summary, body)
				}
			})
		}
	}
}

func TestParseClaudeReleaseNotesOverviewArticleRequiresContent(t *testing.T) {
	for _, layout := range []string{"legacy", "current"} {
		t.Run(layout, func(t *testing.T) {
			html := `<html><body><main>` +
				releaseNotesLayoutTestHeading(layout, claudeReleaseNotesLayoutTestDate) +
				`</main></body></html>`
			_, summary, body, err := parseAnnouncementArticleFromURL(
				claudeReleaseNotesLayoutTestURL+"#"+claudeReleaseNotesLayoutTestDate,
				html,
			)
			if err == nil || !strings.Contains(err.Error(), "release notes entry content not found") {
				t.Fatalf("parseAnnouncementArticleFromURL() error = %v, want empty-content error", err)
			}
			if summary != "" || body != "" {
				t.Fatalf("summary/body = %q/%q, want empty", summary, body)
			}
		})
	}
}

func TestClaudeReleaseNotesOverviewRootParagraphIsSnippetAndBody(t *testing.T) {
	const (
		wantTitle   = "We've launched Claude Sonnet 5"
		wantContent = "We've launched Claude Sonnet 5, now available."
	)
	for _, layout := range []string{"legacy", "current"} {
		t.Run(layout, func(t *testing.T) {
			html := `<html><body><main>` +
				releaseNotesLayoutTestHeading(layout, claudeReleaseNotesLayoutTestDate) +
				`<p>We've launched <a href="https://www.anthropic.com/news/claude-sonnet-5">Claude Sonnet 5</a>, now available.</p>` +
				`<p>This second paragraph is a later content block.</p>` +
				`</main></body></html>`

			snapshot, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", claudeReleaseNotesLayoutTestURL, html)
			if err != nil {
				t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
			}
			if len(snapshot.Items) != 1 {
				t.Fatalf("len(snapshot.Items) = %d, want 1; items=%#v", len(snapshot.Items), snapshot.Items)
			}
			if snapshot.Items[0].Title != wantTitle {
				t.Errorf("item title = %q, want %q", snapshot.Items[0].Title, wantTitle)
			}
			if snapshot.Items[0].Snippet != wantContent {
				t.Errorf("item snippet = %q, want %q", snapshot.Items[0].Snippet, wantContent)
			}

			title, summary, body, err := parseAnnouncementArticleFromURL(snapshot.Items[0].URL, html)
			if err != nil {
				t.Fatalf("parseAnnouncementArticleFromURL() error = %v", err)
			}
			if title != wantTitle || summary != wantContent || body != wantContent {
				t.Fatalf("article = (%q, %q, %q), want (%q, %q, %q)", title, summary, body, wantTitle, wantContent, wantContent)
			}
		})
	}
}

func TestRunAnnouncementSiteMigratesReleaseNotesURLAcrossDOMLayouts(t *testing.T) {
	const (
		oldHomeURL = "https://docs.claude.com/en/release-notes/overview"
		oldDateURL = oldHomeURL + "#" + claudeReleaseNotesLayoutTestDate
		newDateURL = claudeReleaseNotesLayoutTestURL + "#" + claudeReleaseNotesLayoutTestDate
	)
	oldHTML := releaseNotesLayoutTestHTML("legacy")
	newHTML := releaseNotesLayoutTestHTML("current")
	oldSnapshot, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", oldHomeURL, oldHTML)
	if err != nil {
		t.Fatalf("parse old index error = %v", err)
	}
	if len(oldSnapshot.Items) == 0 {
		t.Fatal("old snapshot has no items")
	}

	now := time.Date(2026, 9, 2, 18, 0, 0, 0, time.UTC)
	articleState := ArticleState{}
	for _, item := range oldSnapshot.Items {
		articleState[item.URL] = model.WatchArticleState{
			URL:           item.URL,
			Title:         item.Title,
			SummaryHash:   hashWatchContent("legacy summary"),
			BodyHash:      hashWatchContent("legacy body"),
			LastCheckedAt: now,
			LastChangedAt: now,
		}
	}
	indexState := IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Claude Platform Release Notes::Claude Platform Release Notes": oldSnapshot,
	}}
	site := config.WatchSite{
		Name:    "Claude Platform Release Notes",
		Type:    config.WatchTypeAnnouncementPage,
		HomeURL: claudeReleaseNotesLayoutTestURL,
	}
	fetchHTML := func(ctx context.Context, url string) (string, error) {
		if url != claudeReleaseNotesLayoutTestURL {
			t.Fatalf("unexpected fetch URL = %q", url)
		}
		return newHTML, nil
	}

	articles, seenItems, events, err := runAnnouncementSite(
		context.Background(), site, now, indexState, articleState,
		fetchHTML, 1, time.Hour, 1,
	)
	if err != nil {
		t.Fatalf("runAnnouncementSite() error = %v", err)
	}
	if len(articles) != 0 || len(seenItems) != 0 || len(events) != 0 {
		t.Fatalf("runAnnouncementSite() = articles=%#v seen=%#v events=%#v, want no pseudo-new event", articles, seenItems, events)
	}
	currentSnapshot := indexState.Categories["Claude Platform Release Notes::Claude Platform Release Notes"]
	if len(currentSnapshot.Items) == 0 || currentSnapshot.Items[0].URL != newDateURL {
		t.Fatalf("current snapshot = %#v, want canonical platform URL", currentSnapshot)
	}
	if _, ok := articleState[oldDateURL]; ok {
		t.Fatalf("legacy article state key %q remains after migration", oldDateURL)
	}
	if migrated, ok := articleState[newDateURL]; !ok || migrated.URL != newDateURL {
		t.Fatalf("canonical article state missing or stale: %#v", articleState)
	}
}
