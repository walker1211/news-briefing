package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustReadAnnouncementFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", "announcements", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(data)
}

func TestParseAnthropicAnnouncementIndexExtractsNewsArticles(t *testing.T) {
	html := mustReadAnnouncementFixture(t, "anthropic_news_home.html")

	snapshot, err := parseAnthropicAnnouncementIndex("Anthropic News", "https://www.anthropic.com/news", html)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
	}
	if len(snapshot.Items) == 0 {
		t.Fatal("len(snapshot.Items) = 0, want > 0")
	}
	if snapshot.Items[0].Title != "Introducing Claude Opus 4.7" {
		t.Fatalf("snapshot.Items[0].Title = %q", snapshot.Items[0].Title)
	}
	if snapshot.Items[0].URL != "https://www.anthropic.com/news/claude-opus-4-7" {
		t.Fatalf("snapshot.Items[0].URL = %q", snapshot.Items[0].URL)
	}
}

func TestParseClaudeReleaseNotesIndexExtractsEntries(t *testing.T) {
	html := mustReadAnnouncementFixture(t, "claude_release_notes_home.html")

	snapshot, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", "https://docs.claude.com/en/release-notes/overview", html)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
	}
	if len(snapshot.Items) == 0 {
		t.Fatal("len(snapshot.Items) = 0, want > 0")
	}
	if snapshot.Items[0].Title != "We've launched Claude Opus 4.7" {
		t.Fatalf("snapshot.Items[0].Title = %q", snapshot.Items[0].Title)
	}
	if snapshot.Items[0].URL != "https://docs.claude.com/en/release-notes/overview#april-16-2026" {
		t.Fatalf("snapshot.Items[0].URL = %q", snapshot.Items[0].URL)
	}
	if snapshot.Items[1].URL != "https://docs.claude.com/en/release-notes/overview#february-17-2026" {
		t.Fatalf("snapshot.Items[1].URL = %q", snapshot.Items[1].URL)
	}
}

func TestParseClaudeReleaseNotesIndexExtractsEntriesFromDocsAnthropicHost(t *testing.T) {
	html := mustReadAnnouncementFixture(t, "claude_release_notes_home.html")

	snapshot, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", "https://docs.anthropic.com/en/release-notes/overview", html)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
	}
	if len(snapshot.Items) == 0 {
		t.Fatal("len(snapshot.Items) = 0, want > 0")
	}
	if snapshot.Items[0].URL != "https://docs.anthropic.com/en/release-notes/overview#april-16-2026" {
		t.Fatalf("snapshot.Items[0].URL = %q", snapshot.Items[0].URL)
	}
}

func TestParseClaudeReleaseNotesIndexReportsUnavailableRedirectPage(t *testing.T) {
	html := `<html><head><title>App unavailable in region | Claude</title></head><body><main><h1>App unavailable in region</h1></main></body></html>`

	_, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", "https://docs.claude.com/en/release-notes/overview", html)
	if err == nil || !strings.Contains(err.Error(), "release notes page unavailable") {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v, want release notes page unavailable", err)
	}
}

func TestParseClaudeReleaseNotesIndexIgnoresNonReleaseFragments(t *testing.T) {
	html := `<html><body><main>
		<h3><div id="plugins"><div>Plugins</div></div></h3>
		<ul>
			<li>Use plugins to extend Claude Code.</li>
		</ul>
		<h3><div id="april-16-2026"><div>April 16, 2026</div></div></h3>
		<ul>
			<li>We've launched <a href="https://www.anthropic.com/news/claude-opus-4-7">Claude Opus 4.7</a>, our most capable generally available model for complex reasoning and agentic coding.</li>
		</ul>
	</main></body></html>`

	snapshot, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", "https://platform.claude.com/docs/en/release-notes/overview", html)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("len(snapshot.Items) = %d, want 1; items=%#v", len(snapshot.Items), snapshot.Items)
	}
	if snapshot.Items[0].URL != "https://platform.claude.com/docs/en/release-notes/overview#april-16-2026" {
		t.Fatalf("snapshot.Items[0].URL = %q", snapshot.Items[0].URL)
	}
}

func TestParseClaudeReleaseNotesIndexRequiresDateAnchors(t *testing.T) {
	html := `<html><body><main>
		<nav><a href="claude-for-chrome">Claude for Chrome</a></nav>
		<a href="/docs/en/release-notes/claude-for-chrome">Claude for Chrome</a>
	</main></body></html>`

	_, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", "https://platform.claude.com/docs/en/release-notes/overview", html)
	if err == nil {
		t.Fatal("parseAnthropicAnnouncementIndex() error = nil, want date anchor error")
	}
}

func TestParseAnthropicAnnouncementArticleExtractsSummaryAndBody(t *testing.T) {
	html := mustReadAnnouncementFixture(t, "anthropic_news_opus47.html")

	title, summary, body, err := parseAnthropicAnnouncementArticle(html)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementArticle() error = %v", err)
	}
	if title != "Introducing Claude Opus 4.7" {
		t.Fatalf("title = %q", title)
	}
	if summary == "" {
		t.Fatal("summary = empty")
	}
	if body == "" {
		t.Fatal("body = empty")
	}
}

func TestParseAnthropicAnnouncementArticleIgnoresRelatedContentChanges(t *testing.T) {
	base := `<html><body><main id="main-content">
		<article>
			<div class="page-wrapper hero"><div class="post-header"><h1>Introducing Claude Opus 5</h1><div>Jul 24, 2026</div></div></div>
			<div class="page-wrapper"><article><p>Claude Opus 5 is available today.</p><p>It improves coding reliability.</p></article></div>
			<section class="related"><h2>Related content</h2><article><p>%s</p></article></section>
		</article>
	</main></body></html>`
	firstHTML := fmt.Sprintf(base, "First recommendation")
	secondHTML := fmt.Sprintf(base, "A different recommendation")

	firstTitle, firstSummary, firstBody, err := parseAnthropicAnnouncementArticle(firstHTML)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementArticle(first) error = %v", err)
	}
	secondTitle, secondSummary, secondBody, err := parseAnthropicAnnouncementArticle(secondHTML)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementArticle(second) error = %v", err)
	}
	if firstTitle != secondTitle || firstSummary != secondSummary || firstBody != secondBody {
		t.Fatalf("related content changed parsed result: first=(%q, %q, %q), second=(%q, %q, %q)", firstTitle, firstSummary, firstBody, secondTitle, secondSummary, secondBody)
	}
	if strings.Contains(firstBody, "recommendation") {
		t.Fatalf("body = %q, want related content excluded", firstBody)
	}
}

func TestParseAnnouncementPublishedAtUsesPrimaryAnthropicPost(t *testing.T) {
	html := mustReadAnnouncementFixture(t, "anthropic_news_opus47.html")

	got := parseAnnouncementPublishedAt(html)
	want := time.Date(2026, 4, 16, 17, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("parseAnnouncementPublishedAt() = %v, want %v", got, want)
	}
}

func TestParseAnnouncementPublishedAtPrefersStandardMetadata(t *testing.T) {
	html := `<html><head><meta property="article:published_time" content="2026-07-24T17:00:00Z"></head><body><script>{"publishedOn":"2026-08-24T17:00:00Z"}</script></body></html>`

	got := parseAnnouncementPublishedAt(html)
	want := time.Date(2026, 7, 24, 17, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("parseAnnouncementPublishedAt() = %v, want %v", got, want)
	}
}

func TestParseAnnouncementPublishedAtUsesVisibleAbbreviatedDateNearTitle(t *testing.T) {
	html := `<html><body><main id="main-content"><article>
		<div class="page-wrapper"><div class="post-header"><h1>Introducing Claude Opus 5</h1><div class="body-3">Jul 24, 2026</div></div></div>
		<div class="page-wrapper"><article><p>Article body.</p><p>Changelog: Aug 1, 2026.</p></article></div>
	</article><section><p>Related: Sep 2, 2026.</p></section></main></body></html>`

	got := parseAnnouncementPublishedAt(html)
	want := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("parseAnnouncementPublishedAt() = %v, want %v", got, want)
	}
}

func TestParseAnnouncementPublishedAtUsesVisibleFullMonthNearTitle(t *testing.T) {
	html := `<html><body><main id="main-content"><article>
		<div class="page-wrapper"><div class="post-header"><h1>Introducing Claude Sonnet 5</h1><div class="body-3">June 30, 2026</div></div></div>
		<div class="page-wrapper"><article><p>Article body.</p></article></div>
	</article></main></body></html>`

	got := parseAnnouncementPublishedAt(html)
	want := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("parseAnnouncementPublishedAt() = %v, want %v", got, want)
	}
}

func TestParseClaudeReleaseNotesOverviewArticleExtractsEntryByFragment(t *testing.T) {
	html := `<html><body><main>
		<h3><div id="april-16-2026"><div>April 16, 2026</div></div></h3>
		<ul>
			<li>We've launched <a href="https://www.anthropic.com/news/claude-opus-4-7">Claude Opus 4.7</a>, our most capable generally available model for complex reasoning and agentic coding.</li>
		</ul>
		<h3><div id="february-17-2026"><div>February 17, 2026</div></div></h3>
		<ul>
			<li>We've launched <a href="https://www.anthropic.com/news/claude-sonnet-4-6">Claude Sonnet 4.6</a>, our latest balanced model combining speed and intelligence for everyday tasks.</li>
		</ul>
	</main></body></html>`

	title, summary, body, err := parseAnnouncementArticleFromURL("https://docs.claude.com/en/release-notes/overview#april-16-2026", html)
	if err != nil {
		t.Fatalf("parseAnnouncementArticleFromURL() error = %v", err)
	}
	if title != "We've launched Claude Opus 4.7" {
		t.Fatalf("title = %q", title)
	}
	if summary != "We've launched Claude Opus 4.7, our most capable generally available model for complex reasoning and agentic coding." {
		t.Fatalf("summary = %q", summary)
	}
	if body != "We've launched Claude Opus 4.7, our most capable generally available model for complex reasoning and agentic coding." {
		t.Fatalf("body = %q", body)
	}
}

func TestParseAnthropicAnnouncementIndexIgnoresOutsideAnnouncementLinks(t *testing.T) {
	html := `<html><body><main>
		<a href="/news/claude-opus-4-7"><h2>Introducing Claude Opus 4.7</h2></a>
		<a href="https://example.com/promo"><h2>External Promo</h2></a>
		<a href="/legal/privacy"><h2>Privacy Policy</h2></a>
	</main></body></html>`

	snapshot, err := parseAnthropicAnnouncementIndex("Anthropic News", "https://www.anthropic.com/news", html)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("len(snapshot.Items) = %d, want 1; items=%#v", len(snapshot.Items), snapshot.Items)
	}
	if snapshot.Items[0].URL != "https://www.anthropic.com/news/claude-opus-4-7" {
		t.Fatalf("snapshot.Items[0].URL = %q", snapshot.Items[0].URL)
	}
}

func TestParseClaudeReleaseNotesOverviewArticleIncludesListItems(t *testing.T) {
	html := `<html><body><main>
		<section id="claude-opus-4-7">
			<h3>April 16, 2026</h3>
			<a href="claude-opus-4-7">We've launched Claude Opus 4.7</a>
			<p>Claude Opus 4.7 is now available in the Anthropic API.</p>
			<ul>
				<li>Improved coding reliability</li>
				<li>Better tool use stability</li>
			</ul>
		</section>
	</main></body></html>`

	title, summary, body, err := parseAnnouncementArticleFromURL("https://platform.claude.com/docs/en/release-notes/overview#claude-opus-4-7", html)
	if err != nil {
		t.Fatalf("parseAnnouncementArticleFromURL() error = %v", err)
	}
	if title != "We've launched Claude Opus 4.7" {
		t.Fatalf("title = %q", title)
	}
	if summary != "Claude Opus 4.7 is now available in the Anthropic API." {
		t.Fatalf("summary = %q", summary)
	}
	if body != "Claude Opus 4.7 is now available in the Anthropic API. Improved coding reliability Better tool use stability" {
		t.Fatalf("body = %q", body)
	}
}
