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
