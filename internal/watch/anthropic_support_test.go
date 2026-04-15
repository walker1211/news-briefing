package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAnthropicHomeKeepsAllowedCategories(t *testing.T) {
	html := mustReadFixture(t, "anthropic/home.html")
	items, err := parseAnthropicHome(html, map[string]struct{}{
		"Claude": {},
		"安全保障":   {},
	})
	if err != nil {
		t.Fatalf("parseAnthropicHome() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].Title != "Claude" || !strings.Contains(items[0].URL, "/collections/") {
		t.Fatalf("items[0] = %#v", items[0])
	}
}

func TestParseAnthropicHomeReadsCardTitleWithoutDescriptionNoise(t *testing.T) {
	html := `<html><body>
	<a href="/zh-CN/collections/5370014-claude-api-与控制台">
	  <img src="x.png" />
	  <h3>Claude API 与控制台</h3>
	  <p>关于API访问、定价、计费等的信息。</p>
	  <p>40 篇文章</p>
	</a>
	<a href="/zh-CN/collections/4078535-security">
	  <img src="y.png" />
	  <h3>安全保障</h3>
	  <p>12 篇文章</p>
	</a>
</body></html>`
	items, err := parseAnthropicHome(html, map[string]struct{}{
		"Claude API 与控制台": {},
		"安全保障":            {},
	})
	if err != nil {
		t.Fatalf("parseAnthropicHome() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2; items=%#v", len(items), items)
	}
	if items[0].Title != "Claude API 与控制台" {
		t.Fatalf("items[0].Title = %q", items[0].Title)
	}
	if items[1].Title != "安全保障" {
		t.Fatalf("items[1].Title = %q", items[1].Title)
	}
}

func TestParseAnthropicHomeReadsNextJSCollectionCardTitle(t *testing.T) {
	html := `<html><body>
	<a href="/zh-CN/collections/5370014-claude-api-与控制台" class="collection-card">
	  <div class="wrapper">
	    <div data-testid="collection-name">Claude API 与控制台</div>
	    <span>无关标签</span>
	    <div>关于 API 访问、定价、计费等的信息。</div>
	    <span>40 篇文章</span>
	  </div>
	</a>
	<a href="/zh-CN/collections/4078535-security" class="collection-card">
	  <div class="wrapper">
	    <div data-testid="collection-name">安全保障</div>
	    <span>12 篇文章</span>
	  </div>
	</a>
</body></html>`
	items, err := parseAnthropicHome(html, map[string]struct{}{
		"Claude API 与控制台": {},
		"安全保障":            {},
	})
	if err != nil {
		t.Fatalf("parseAnthropicHome() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2; items=%#v", len(items), items)
	}
	if items[0].Title != "Claude API 与控制台" {
		t.Fatalf("items[0].Title = %q", items[0].Title)
	}
	if items[0].UpdatedText != "40 篇文章" {
		t.Fatalf("items[0].UpdatedText = %q", items[0].UpdatedText)
	}
	if items[1].Title != "安全保障" {
		t.Fatalf("items[1].Title = %q", items[1].Title)
	}
}

func TestParseAnthropicCategoryExtractsArticles(t *testing.T) {
	html := mustReadFixture(t, "anthropic/category_security.html")
	snapshot, err := parseAnthropicCategory("安全保障", "https://support.claude.com/zh-CN/collections/4078535-security", html)
	if err != nil {
		t.Fatalf("parseAnthropicCategory() error = %v", err)
	}
	if snapshot.ItemCount != 2 {
		t.Fatalf("snapshot.ItemCount = %d, want 2", snapshot.ItemCount)
	}
	if snapshot.Items[0].Title == "" || !strings.Contains(snapshot.Items[0].URL, "/articles/") {
		t.Fatalf("snapshot.Items[0] = %#v", snapshot.Items[0])
	}
}

func TestParseAnthropicArticleExtractsSummaryAndBody(t *testing.T) {
	html := mustReadFixture(t, "anthropic/article_identity_verification.html")
	title, summary, body, err := parseAnthropicArticle(html)
	if err != nil {
		t.Fatalf("parseAnthropicArticle() error = %v", err)
	}
	if title != "Claude 上的身份验证" {
		t.Fatalf("title = %q", title)
	}
	if !strings.Contains(summary, "政府颁发的身份证件") {
		t.Fatalf("summary = %q", summary)
	}
	if !strings.Contains(body, "实时自拍") {
		t.Fatalf("body = %q", body)
	}
}

func mustReadFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(data)
}
