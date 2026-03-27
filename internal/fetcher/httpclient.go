package fetcher

import (
	"net/http"
	"net/url"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
)

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var sharedClient *http.Client

// InitHTTPClient 初始化带代理的共享 HTTP 客户端
func InitHTTPClient(proxy config.Proxy) {
	transport := &http.Transport{}

	if proxy.HTTP != "" {
		if u, err := url.Parse(proxy.HTTP); err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
	} else if proxy.Socks5 != "" {
		if u, err := url.Parse(proxy.Socks5); err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
	}

	sharedClient = &http.Client{
		Transport: &uaTransport{inner: transport},
		Timeout:   30 * time.Second,
	}
}

// HTTPClient 返回共享的 HTTP 客户端
func HTTPClient() *http.Client {
	if sharedClient == nil {
		// 兜底：无代理
		sharedClient = &http.Client{
			Transport: &uaTransport{inner: &http.Transport{}},
			Timeout:   30 * time.Second,
		}
	}
	return sharedClient
}

type uaTransport struct {
	inner http.RoundTripper
}

func (t *uaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", userAgent)
	return t.inner.RoundTrip(req)
}
