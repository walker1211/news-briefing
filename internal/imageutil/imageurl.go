package imageutil

import (
	"net/url"
	"path/filepath"
	"strings"
)

type ExactURLRule struct {
	Host string
	Path string
}

type Filter struct {
	blockedExactURLs map[string]struct{}
}

func NewFilter(rules []ExactURLRule) Filter {
	blocked := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		path := strings.TrimSpace(rule.Path)
		if parsed, err := url.Parse(path); err == nil {
			path = parsed.EscapedPath()
		}
		blocked[exactURLKey(rule.Host, path)] = struct{}{}
	}
	return Filter{blockedExactURLs: blocked}
}

func (f Filter) IsUsableRemoteImageURL(raw string) bool {
	if !IsUsableRemoteImageURL(raw) {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	_, blocked := f.blockedExactURLs[exactURLKey(parsed.Hostname(), parsed.EscapedPath())]
	return !blocked
}

func exactURLKey(host string, path string) string {
	return strings.ToLower(strings.TrimSpace(host)) + "\x00" + strings.TrimSuffix(strings.TrimSpace(path), "/")
}

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
	if (host == "readhub.cn" || host == "www.readhub.cn") && path == "/social-image.webp" {
		return true
	}
	return host == "file.caixin.com" && path == "/images/common/images/shareimg.jpg"
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
