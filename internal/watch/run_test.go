package watch

import (
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

func TestRunBootstrapsCategoryBaselineWithoutBriefingArticles(t *testing.T) {
	oldFetch := fetchWatchHTML
	defer func() { fetchWatchHTML = oldFetch }()

	responses := map[string]string{
		"https://support.claude.com/zh-CN":                                  mustReadFixture(t, "anthropic/home.html"),
		"https://support.claude.com/zh-CN/collections/4078535-security":     mustReadFixture(t, "anthropic/category_security.html"),
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": mustReadFixture(t, "anthropic/article_identity_verification.html"),
	}
	fetchWatchHTML = func(url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              "anthropic_support",
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"身份验证", "电话验证"},
		}}},
	}

	articles, report, err := Run(cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(report.Events) != 0 {
		t.Fatalf("report.Events = %#v, want bootstrap to stay silent", report.Events)
	}
	if len(articles) != 0 {
		t.Fatalf("articles = %#v, want bootstrap to avoid briefing output", articles)
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	indexState, err := indexStore.Load()
	if err != nil {
		t.Fatalf("indexStore.Load() error = %v", err)
	}
	snapshot, ok := indexState.Categories["Anthropic Claude Support::安全保障"]
	if !ok || snapshot.ItemCount == 0 {
		t.Fatalf("bootstrap category snapshot missing: %#v", indexState.Categories)
	}
}

func TestRunBackfillsMissingArticleStateForExistingCategoryBaseline(t *testing.T) {
	oldFetch := fetchWatchHTML
	defer func() { fetchWatchHTML = oldFetch }()

	categoryHTML := mustReadFixture(t, "anthropic/category_security.html")
	snapshot, err := parseAnthropicCategory("安全保障", "https://support.claude.com/zh-CN/collections/4078535-security", categoryHTML)
	if err != nil {
		t.Fatalf("parseAnthropicCategory() error = %v", err)
	}

	responses := map[string]string{
		"https://support.claude.com/zh-CN":                                  mustReadFixture(t, "anthropic/home.html"),
		"https://support.claude.com/zh-CN/collections/4078535-security":     categoryHTML,
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": mustReadFixture(t, "anthropic/article_identity_verification.html"),
	}
	fetchWatchHTML = func(url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              "anthropic_support",
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"身份验证", "电话验证"},
		}}},
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	articleStore := NewArticleStore(cfg.Output.Dir)
	if err := indexStore.Save(IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Anthropic Claude Support::安全保障": snapshot,
	}}); err != nil {
		t.Fatalf("indexStore.Save() error = %v", err)
	}

	articles, report, err := Run(cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(report.Events) != 0 {
		t.Fatalf("report.Events = %#v, want silent article baseline backfill", report.Events)
	}
	if len(articles) != 0 {
		t.Fatalf("articles = %#v, want no briefing output during article baseline backfill", articles)
	}
	state, err := articleStore.Load()
	if err != nil {
		t.Fatalf("articleStore.Load() error = %v", err)
	}
	seeded, ok := state["https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证"]
	if !ok || seeded.BodyHash == "" {
		t.Fatalf("article state not backfilled: %#v", state)
	}

	responses["https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证"] = `<html><head><title>Claude 上的身份验证</title><meta name="description" content="某些使用场景需要提供政府颁发的身份证件与实时自拍。" /></head><body><article><h1>Claude 上的身份验证</h1><p>某些使用场景需要提供政府颁发的身份证件与实时自拍。</p><p>新增了实时自拍与手机号码交叉校验。</p></article></body></html>`

	articles, report, err = Run(cfg, time.Date(2026, 4, 15, 17, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Run() second error = %v", err)
	}
	found := false
	for _, event := range report.Events {
		if event.EventType == "content_changed" && event.ArticleURL == "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证" {
			found = true
		}
	}
	if !found {
		t.Fatalf("second report.Events = %#v", report.Events)
	}
	if len(articles) != 1 {
		t.Fatalf("second articles = %#v", articles)
	}
}

func TestRunDetectsContentChangedForExistingArticle(t *testing.T) {
	oldFetch := fetchWatchHTML
	defer func() { fetchWatchHTML = oldFetch }()

	responses := map[string]string{
		"https://support.claude.com/zh-CN":                                  mustReadFixture(t, "anthropic/home.html"),
		"https://support.claude.com/zh-CN/collections/4078535-security":     mustReadFixture(t, "anthropic/category_security.html"),
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": `<html><head><title>Claude 上的身份验证</title><meta name="description" content="某些使用场景需要提供政府颁发的身份证件与实时自拍。" /></head><body><article><h1>Claude 上的身份验证</h1><p>某些使用场景需要提供政府颁发的身份证件与实时自拍。</p><p>新增了实时自拍与手机号码交叉校验。</p></article></body></html>`,
	}
	fetchWatchHTML = func(url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              "anthropic_support",
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"身份验证", "电话验证"},
		}}},
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	articleStore := NewArticleStore(cfg.Output.Dir)
	if err := indexStore.Save(IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Anthropic Claude Support::安全保障": {
			Scope:     "category",
			Source:    "Anthropic Claude Support",
			Category:  "安全保障",
			URL:       "https://support.claude.com/zh-CN/collections/4078535-security",
			ItemCount: 1,
			Items: []model.WatchIndexItem{{
				Title:    "Claude 上的身份验证",
				URL:      "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
				Position: 1,
				ItemHash: "same-item",
			}},
			Hash: "old-snapshot",
		},
	}}); err != nil {
		t.Fatalf("indexStore.Save() error = %v", err)
	}
	if err := articleStore.Save(ArticleState{
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": {
			URL:         "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
			Title:       "Claude 上的身份验证",
			SummaryHash: "summary-old",
			BodyHash:    "body-old",
		},
	}); err != nil {
		t.Fatalf("articleStore.Save() error = %v", err)
	}

	articles, report, err := Run(cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	found := false
	for _, event := range report.Events {
		if event.EventType == "content_changed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("report.Events = %#v", report.Events)
	}
	if len(articles) != 1 {
		t.Fatalf("articles = %#v", articles)
	}
}

func TestRunKeepsArticleCountChangedInSidecarOnly(t *testing.T) {
	oldFetch := fetchWatchHTML
	defer func() { fetchWatchHTML = oldFetch }()

	responses := map[string]string{
		"https://support.claude.com/zh-CN":                                  mustReadFixture(t, "anthropic/home.html"),
		"https://support.claude.com/zh-CN/collections/4078535-security":     mustReadFixture(t, "anthropic/category_security.html"),
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": mustReadFixture(t, "anthropic/article_identity_verification.html"),
		"https://support.claude.com/zh-CN/articles/14330000-电话验证":           `<html><head><title>电话验证</title><meta name="description" content="电话验证帮助内容。" /></head><body><article><h1>电话验证</h1><p>电话验证帮助内容。</p></article></body></html>`,
	}
	fetchWatchHTML = func(url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              "anthropic_support",
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"仅匹配不存在的词"},
		}}},
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	if err := indexStore.Save(IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Anthropic Claude Support::安全保障": {
			Scope:     "category",
			Source:    "Anthropic Claude Support",
			Category:  "安全保障",
			URL:       "https://support.claude.com/zh-CN/collections/4078535-security",
			ItemCount: 1,
			Items: []model.WatchIndexItem{{
				Title:    "Claude 上的身份验证",
				URL:      "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
				Position: 1,
				ItemHash: "same-item",
			}},
			Hash: "old-snapshot",
		},
	}}); err != nil {
		t.Fatalf("indexStore.Save() error = %v", err)
	}

	articles, report, err := Run(cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	found := false
	for _, event := range report.Events {
		if event.EventType == "article_count_changed" {
			found = true
			if event.IncludeInBriefing {
				t.Fatalf("article_count_changed should stay sidecar only: %#v", event)
			}
		}
	}
	if !found {
		t.Fatalf("report.Events = %#v", report.Events)
	}
	if len(articles) != 0 {
		t.Fatalf("articles = %#v, want sidecar only", articles)
	}
}

func TestRunDeletesArticleStateForRemovedArticle(t *testing.T) {
	oldFetch := fetchWatchHTML
	defer func() { fetchWatchHTML = oldFetch }()

	responses := map[string]string{
		"https://support.claude.com/zh-CN":                              `<html><body><a href="/zh-CN/collections/4078535-security">安全保障 <span>0 articles</span></a></body></html>`,
		"https://support.claude.com/zh-CN/collections/4078535-security": `<html><body><h1>安全保障</h1></body></html>`,
	}
	fetchWatchHTML = func(url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              "anthropic_support",
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"身份验证"},
		}}},
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	articleStore := NewArticleStore(cfg.Output.Dir)
	removedURL := "https://support.claude.com/zh-CN/articles/old-identity-check"
	if err := indexStore.Save(IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Anthropic Claude Support::安全保障": {
			Scope:     "category",
			Source:    "Anthropic Claude Support",
			Category:  "安全保障",
			URL:       "https://support.claude.com/zh-CN/collections/4078535-security",
			ItemCount: 1,
			Items: []model.WatchIndexItem{{
				Title:    "旧文章",
				URL:      removedURL,
				Position: 1,
				ItemHash: "old-item",
			}},
			Hash: "old-snapshot",
		},
	}}); err != nil {
		t.Fatalf("indexStore.Save() error = %v", err)
	}
	if err := articleStore.Save(ArticleState{
		removedURL: {
			URL:         removedURL,
			Title:       "旧文章",
			SummaryHash: "old-summary",
			BodyHash:    "old-body",
		},
	}); err != nil {
		t.Fatalf("articleStore.Save() error = %v", err)
	}

	_, report, err := Run(cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	found := false
	for _, event := range report.Events {
		if event.EventType == "removed_article" && event.ArticleURL == removedURL {
			found = true
		}
	}
	if !found {
		t.Fatalf("report.Events = %#v", report.Events)
	}

	state, err := articleStore.Load()
	if err != nil {
		t.Fatalf("articleStore.Load() error = %v", err)
	}
	if _, ok := state[removedURL]; ok {
		t.Fatalf("removed article state still exists: %#v", state[removedURL])
	}
}

func TestRunWritesSeenStateForContentChangedBriefingArticle(t *testing.T) {
	oldFetch := fetchWatchHTML
	defer func() { fetchWatchHTML = oldFetch }()

	responses := map[string]string{
		"https://support.claude.com/zh-CN":                                  mustReadFixture(t, "anthropic/home.html"),
		"https://support.claude.com/zh-CN/collections/4078535-security":     mustReadFixture(t, "anthropic/category_security.html"),
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": `<html><head><title>Claude 上的身份验证</title><meta name="description" content="某些使用场景需要提供政府颁发的身份证件与实时自拍。" /></head><body><article><h1>Claude 上的身份验证</h1><p>某些使用场景需要提供政府颁发的身份证件与实时自拍。</p><p>新增了实时自拍与手机号码交叉校验。</p></article></body></html>`,
	}
	fetchWatchHTML = func(url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              "anthropic_support",
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"身份验证", "电话验证"},
		}}},
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	articleStore := NewArticleStore(cfg.Output.Dir)
	seenStore := NewSeenStore(cfg.Output.Dir)
	if err := indexStore.Save(IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Anthropic Claude Support::安全保障": {
			Scope:     "category",
			Source:    "Anthropic Claude Support",
			Category:  "安全保障",
			URL:       "https://support.claude.com/zh-CN/collections/4078535-security",
			ItemCount: 1,
			Items: []model.WatchIndexItem{{
				Title:    "Claude 上的身份验证",
				URL:      "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
				Position: 1,
				ItemHash: "same-item",
			}},
			Hash: "old-snapshot",
		},
	}}); err != nil {
		t.Fatalf("indexStore.Save() error = %v", err)
	}
	if err := articleStore.Save(ArticleState{
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": {
			URL:         "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
			Title:       "Claude 上的身份验证",
			SummaryHash: "summary-old",
			BodyHash:    "body-old",
		},
	}); err != nil {
		t.Fatalf("articleStore.Save() error = %v", err)
	}

	articles, _, err := Run(cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("len(articles) = %d, want 1", len(articles))
	}
	seen, err := seenStore.Load()
	if err != nil {
		t.Fatalf("seenStore.Load() error = %v", err)
	}
	if len(seen.Items) != 1 {
		t.Fatalf("len(seen.Items) = %d, want 1; items=%#v", len(seen.Items), seen.Items)
	}
	item := seen.Items[0]
	if item.URL != "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证" {
		t.Fatalf("seen item url = %q", item.URL)
	}
	if item.WatchCategory != "安全保障" || item.EventType != "content_changed" {
		t.Fatalf("seen item = %#v", item)
	}
	if item.Body == "" || item.Summary == "" {
		t.Fatalf("seen item body/summary missing: %#v", item)
	}
}

func TestRunBootstrapDoesNotWriteSeenState(t *testing.T) {
	oldFetch := fetchWatchHTML
	defer func() { fetchWatchHTML = oldFetch }()

	responses := map[string]string{
		"https://support.claude.com/zh-CN":                                  mustReadFixture(t, "anthropic/home.html"),
		"https://support.claude.com/zh-CN/collections/4078535-security":     mustReadFixture(t, "anthropic/category_security.html"),
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": mustReadFixture(t, "anthropic/article_identity_verification.html"),
	}
	fetchWatchHTML = func(url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              "anthropic_support",
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"身份验证", "电话验证"},
		}}},
	}

	_, _, err := Run(cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	seenStore := NewSeenStore(cfg.Output.Dir)
	seen, err := seenStore.Load()
	if err != nil {
		t.Fatalf("seenStore.Load() error = %v", err)
	}
	if len(seen.Items) != 0 {
		t.Fatalf("len(seen.Items) = %d, want 0", len(seen.Items))
	}
}

func TestRunContentChangedUpdatesSeenState(t *testing.T) {
	oldFetch := fetchWatchHTML
	defer func() { fetchWatchHTML = oldFetch }()

	responses := map[string]string{
		"https://support.claude.com/zh-CN":                                  mustReadFixture(t, "anthropic/home.html"),
		"https://support.claude.com/zh-CN/collections/4078535-security":     mustReadFixture(t, "anthropic/category_security.html"),
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": mustReadFixture(t, "anthropic/article_identity_verification.html"),
	}
	fetchWatchHTML = func(url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              "anthropic_support",
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"身份验证", "电话验证"},
		}}},
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	articleStore := NewArticleStore(cfg.Output.Dir)
	seenStore := NewSeenStore(cfg.Output.Dir)
	if err := indexStore.Save(IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Anthropic Claude Support::安全保障": {
			Scope:     "category",
			Source:    "Anthropic Claude Support",
			Category:  "安全保障",
			URL:       "https://support.claude.com/zh-CN/collections/4078535-security",
			ItemCount: 1,
			Items: []model.WatchIndexItem{{
				Title:    "Claude 上的身份验证",
				URL:      "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
				Position: 1,
				ItemHash: "same-item",
			}},
			Hash: "old-snapshot",
		},
	}}); err != nil {
		t.Fatalf("indexStore.Save() error = %v", err)
	}
	if err := articleStore.Save(ArticleState{
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": {
			URL:         "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
			Title:       "Claude 上的身份验证",
			SummaryHash: "summary-old",
			BodyHash:    "body-old",
		},
	}); err != nil {
		t.Fatalf("articleStore.Save() error = %v", err)
	}
	if err := seenStore.Save(model.WatchSeenState{Items: []model.WatchSeenArticle{{
		ID:               "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
		URL:              "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
		Title:            "Claude 上的身份验证",
		Source:           "Anthropic Claude Support",
		BriefingCategory: "AI/科技",
		WatchCategory:    "安全保障",
		Summary:          "旧摘要",
		Body:             "旧正文",
		EventType:        "new_article",
		DetectedAt:       time.Date(2026, 4, 15, 15, 0, 0, 0, time.UTC),
	}}}); err != nil {
		t.Fatalf("seenStore.Save() error = %v", err)
	}

	responses["https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证"] = `<html><head><title>Claude 上的身份验证</title><meta name="description" content="某些使用场景需要提供政府颁发的身份证件与实时自拍。" /></head><body><article><h1>Claude 上的身份验证</h1><p>某些使用场景需要提供政府颁发的身份证件与实时自拍。</p><p>新增了实时自拍与手机号码交叉校验。</p></article></body></html>`

	_, _, err := Run(cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	seen, err := seenStore.Load()
	if err != nil {
		t.Fatalf("seenStore.Load() error = %v", err)
	}
	if len(seen.Items) != 2 {
		t.Fatalf("len(seen.Items) = %d, want 2; items=%#v", len(seen.Items), seen.Items)
	}
	item := seen.Items[1]
	if item.EventType != "content_changed" {
		t.Fatalf("item.EventType = %q, want content_changed", item.EventType)
	}
	if item.Body == "旧正文" || item.DetectedAt.IsZero() {
		t.Fatalf("item = %#v", item)
	}
}
