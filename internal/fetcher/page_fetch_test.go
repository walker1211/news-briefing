package fetcher

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

func loadPageFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", "pages", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", path, err)
	}
	return string(data)
}

func TestParseDocsPageExtractsPublishedAnnouncement(t *testing.T) {
	src := config.Source{
		Name:     "GLM Docs",
		URL:      "https://example.com/glm",
		Type:     "docs_page",
		Category: "AI/科技",
		Keywords: []string{"GLM"},
		PageKind: "announcement",
		TimeHint: "published",
	}

	candidate, reason, err := parsePageSource(src, loadPageFixture(t, "glm_docs.html"))
	if err != nil {
		t.Fatalf("parsePageSource() error = %v", err)
	}
	if candidate.Article.Title == "" {
		t.Fatal("candidate.Article.Title is empty")
	}
	if candidate.Article.Summary == "" {
		t.Fatal("candidate.Article.Summary is empty")
	}
	wantPublished := time.Date(2026, 4, 7, 3, 30, 0, 0, time.UTC)
	if !candidate.Article.Published.Equal(wantPublished) {
		t.Fatalf("candidate.Article.Published = %v, want %v", candidate.Article.Published, wantPublished)
	}
	if !reflect.DeepEqual(candidate.MatchedKeywords, []string{"GLM"}) {
		t.Fatalf("candidate.MatchedKeywords = %#v, want %#v", candidate.MatchedKeywords, []string{"GLM"})
	}
	if reason != "" {
		t.Fatalf("reason = %q, want empty", reason)
	}
}

func TestParseRepoPagePrefersReleasePublishedTime(t *testing.T) {
	src := config.Source{
		Name:     "ACE-Step",
		URL:      "https://example.com/ace-step",
		Type:     "repo_page",
		Category: "AI/科技",
		Keywords: []string{"ACE-Step"},
		PageKind: "release",
		TimeHint: "release published",
	}

	candidate, reason, err := parsePageSource(src, loadPageFixture(t, "ace_step_release.html"))
	if err != nil {
		t.Fatalf("parsePageSource() error = %v", err)
	}
	if candidate.Article.Title == "" {
		t.Fatal("candidate.Article.Title is empty")
	}
	if candidate.Article.Summary == "" {
		t.Fatal("candidate.Article.Summary is empty")
	}
	wantPublished := time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC)
	if !candidate.Article.Published.Equal(wantPublished) {
		t.Fatalf("candidate.Article.Published = %v, want %v", candidate.Article.Published, wantPublished)
	}
	if !reflect.DeepEqual(candidate.MatchedKeywords, []string{"ACE-Step"}) {
		t.Fatalf("candidate.MatchedKeywords = %#v, want %#v", candidate.MatchedKeywords, []string{"ACE-Step"})
	}
	if reason != "" {
		t.Fatalf("reason = %q, want empty", reason)
	}
}

func TestParsePageRejectsMissingAcceptableTime(t *testing.T) {
	src := config.Source{
		Name:     "No Time",
		URL:      "https://example.com/no-time",
		Type:     "docs_page",
		Category: "AI/科技",
		Keywords: []string{"GLM"},
		PageKind: "announcement",
		TimeHint: "published",
	}

	_, reason, err := parsePageSource(src, loadPageFixture(t, "no_time.html"))
	if err != nil {
		t.Fatalf("parsePageSource() error = %v", err)
	}
	if reason != model.TraceStatusMissingAcceptableTime {
		t.Fatalf("reason = %q, want %q", reason, model.TraceStatusMissingAcceptableTime)
	}
}

func TestParsePageRejectsNonReleaseStaticPage(t *testing.T) {
	src := config.Source{
		Name:     "EUPE",
		URL:      "https://example.com/eupe",
		Type:     "repo_page",
		Category: "AI/科技",
		Keywords: []string{"EUPE"},
		PageKind: "release",
		TimeHint: "release published",
	}

	_, reason, err := parsePageSource(src, loadPageFixture(t, "non_release.html"))
	if err != nil {
		t.Fatalf("parsePageSource() error = %v", err)
	}
	if reason != model.TraceStatusNonReleasePage {
		t.Fatalf("reason = %q, want %q", reason, model.TraceStatusNonReleasePage)
	}
}
