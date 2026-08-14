package imageutil

import "testing"

func TestIsUsableRemoteImageURLRejectsReadhubGenericSitePreview(t *testing.T) {
	for _, rawURL := range []string{
		"https://readhub.cn/social-image.webp",
		"https://www.readhub.cn/social-image.webp?version=1",
	} {
		if IsUsableRemoteImageURL(rawURL) {
			t.Fatalf("IsUsableRemoteImageURL(%q) = true, want false for generic site preview", rawURL)
		}
	}
}

func TestIsUsableRemoteImageURLKeepsReadhubArticleImage(t *testing.T) {
	rawURL := "https://cdn.readhub.cn/topic/deepseek-unitree.webp"
	if !IsUsableRemoteImageURL(rawURL) {
		t.Fatalf("IsUsableRemoteImageURL(%q) = false, want true for article image", rawURL)
	}
}

func TestIsUsableRemoteImageURLRejectsCaixinGenericShareImage(t *testing.T) {
	for _, rawURL := range []string{
		"https://file.caixin.com/images/common/images/shareimg.jpg",
		"https://file.caixin.com/images/common/images/shareimg.jpg?from=rss",
	} {
		if IsUsableRemoteImageURL(rawURL) {
			t.Fatalf("IsUsableRemoteImageURL(%q) = true, want false for Caixin generic share image", rawURL)
		}
	}
}

func TestIsUsableRemoteImageURLKeepsCaixinArticleImage(t *testing.T) {
	rawURL := "https://file.caixin.com/images/2026/08/story.jpg"
	if !IsUsableRemoteImageURL(rawURL) {
		t.Fatalf("IsUsableRemoteImageURL(%q) = false, want true for Caixin article image", rawURL)
	}
}

func TestFilterRejectsConfiguredExactURLAndIgnoresQuery(t *testing.T) {
	filter := NewFilter([]ExactURLRule{{Host: "media.example.com", Path: "/promo/download.jpg"}})
	for _, rawURL := range []string{
		"https://media.example.com/promo/download.jpg",
		"https://MEDIA.EXAMPLE.COM/promo/download.jpg?campaign=app",
	} {
		if filter.IsUsableRemoteImageURL(rawURL) {
			t.Fatalf("Filter.IsUsableRemoteImageURL(%q) = true, want false", rawURL)
		}
	}
	if !filter.IsUsableRemoteImageURL("https://media.example.com/news/download.jpg") {
		t.Fatal("Filter rejected a different path on the configured host")
	}
	if !filter.IsUsableRemoteImageURL("https://cdn.example.com/promo/download.jpg") {
		t.Fatal("Filter rejected the configured path on a different host")
	}
}
