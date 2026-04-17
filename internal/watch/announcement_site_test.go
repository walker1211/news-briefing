package watch

import (
	"os"
	"path/filepath"
	"testing"
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

	snapshot, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", "https://platform.claude.com/docs/en/release-notes/overview", html)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
	}
	if len(snapshot.Items) == 0 {
		t.Fatal("len(snapshot.Items) = 0, want > 0")
	}
	if snapshot.Items[0].Title != "We've launched Claude Opus 4.7" {
		t.Fatalf("snapshot.Items[0].Title = %q", snapshot.Items[0].Title)
	}
	if snapshot.Items[0].URL != "https://platform.claude.com/docs/en/release-notes/overview#april-16-2026" {
		t.Fatalf("snapshot.Items[0].URL = %q", snapshot.Items[0].URL)
	}
	if snapshot.Items[1].URL != "https://platform.claude.com/docs/en/release-notes/overview#february-17-2026" {
		t.Fatalf("snapshot.Items[1].URL = %q", snapshot.Items[1].URL)
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

	title, summary, body, err := parseAnnouncementArticleFromURL("https://platform.claude.com/docs/en/release-notes/overview#april-16-2026", html)
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

func TestParseClaudeReleaseNotesIndexResolvesRelativeAnnouncementLinks(t *testing.T) {
	html := `<html><body><main><section><ul><li><a href="claude-opus-4-7">We've launched Claude Opus 4.7</a></li></ul></section></main></body></html>`

	snapshot, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", "https://platform.claude.com/docs/en/release-notes/overview", html)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("len(snapshot.Items) = %d, want 1; items=%#v", len(snapshot.Items), snapshot.Items)
	}
	if snapshot.Items[0].URL != "https://platform.claude.com/docs/en/release-notes/overview#claude-opus-4-7" {
		t.Fatalf("snapshot.Items[0].URL = %q", snapshot.Items[0].URL)
	}
}

func TestParseClaudeReleaseNotesIndexIgnoresAnthropicNewsLinks(t *testing.T) {
	html := `<html><body><main><section><ul>
		<li><a href="https://www.anthropic.com/news/claude-opus-4-7">We've launched Claude Opus 4.7</a></li>
		<li><a href="claude-sonnet-4-6">We've launched Claude Sonnet 4.6</a></li>
	</ul></section></main></body></html>`

	snapshot, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", "https://platform.claude.com/docs/en/release-notes/overview", html)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("len(snapshot.Items) = %d, want 1; items=%#v", len(snapshot.Items), snapshot.Items)
	}
	if snapshot.Items[0].URL != "https://platform.claude.com/docs/en/release-notes/overview#claude-sonnet-4-6" {
		t.Fatalf("snapshot.Items[0].URL = %q", snapshot.Items[0].URL)
	}
}

func TestParseClaudeReleaseNotesIndexIgnoresIndexPages(t *testing.T) {
	html := `<html><body><main><section><ul>
		<li><a href="/docs/en/release-notes/api">API Release Notes</a></li>
		<li><a href="/docs/en/release-notes/">Release Notes Home</a></li>
		<li><a href="/docs/en/release-notes/overview">Release Notes Overview</a></li>
		<li><a href="claude-sonnet-4-6">We've launched Claude Sonnet 4.6</a></li>
	</ul></section></main></body></html>`

	snapshot, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", "https://platform.claude.com/docs/en/release-notes/overview", html)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("len(snapshot.Items) = %d, want 1; items=%#v", len(snapshot.Items), snapshot.Items)
	}
	if snapshot.Items[0].URL != "https://platform.claude.com/docs/en/release-notes/overview#claude-sonnet-4-6" {
		t.Fatalf("snapshot.Items[0].URL = %q", snapshot.Items[0].URL)
	}
}

func TestParseClaudeReleaseNotesIndexIgnoresOverviewNoiseLinks(t *testing.T) {
	html := `<html><body><main>
		<nav>
			<a href="claude-opus-4-7">Sidebar Opus Link</a>
		</nav>
		<section id="claude-opus-4-7">
			<h3>April 16, 2026</h3>
			<a href="claude-opus-4-7">We've launched Claude Opus 4.7</a>
			<p>Claude Opus 4.7 is now available in the Anthropic API.</p>
		</section>
	</main></body></html>`

	snapshot, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", "https://platform.claude.com/docs/en/release-notes/overview", html)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("len(snapshot.Items) = %d, want 1; items=%#v", len(snapshot.Items), snapshot.Items)
	}
	if snapshot.Items[0].Title != "We've launched Claude Opus 4.7" {
		t.Fatalf("snapshot.Items[0].Title = %q", snapshot.Items[0].Title)
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
