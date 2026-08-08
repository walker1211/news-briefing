package imageutil

import (
	"net/url"
	"path/filepath"
	"strings"
)

func IsUsableRemoteImageURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return !IsTrackingImageURL(raw) && !isGenericSitePreviewImageURL(parsed)
}

func isGenericSitePreviewImageURL(parsed *url.URL) bool {
	host := strings.ToLower(parsed.Hostname())
	path := strings.ToLower(strings.TrimSuffix(parsed.EscapedPath(), "/"))
	return (host == "readhub.cn" || host == "www.readhub.cn") && path == "/social-image.webp"
}

func IsTrackingImageURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	value := strings.ToLower(strings.Join([]string{
		parsed.Hostname(),
		parsed.EscapedPath(),
		parsed.RawQuery,
	}, " "))
	base := strings.ToLower(filepath.Base(parsed.EscapedPath()))
	if strings.Contains(value, "rss-pixel") || strings.Contains(value, "tracking-pixel") {
		return true
	}
	if strings.Contains(value, "/tracking/") && strings.Contains(value, "pixel") {
		return true
	}
	if base == "pixel.png" || base == "pixel.gif" || base == "transparent.png" || base == "spacer.gif" {
		return true
	}
	return strings.Contains(value, "1x1") && strings.Contains(value, "pixel")
}
