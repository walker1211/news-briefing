package fetcher

import (
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
)

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var (
	sharedClientMu sync.Mutex
	sharedClient   *http.Client
)

func NewHTTPClient(proxy config.Proxy) *http.Client {
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

	return &http.Client{
		Transport: &uaTransport{inner: transport},
		Timeout:   30 * time.Second,
	}
}

func DefaultHTTPClient() *http.Client {
	return NewHTTPClient(config.Proxy{})
}

// InitHTTPClient 初始化带代理的共享 HTTP 客户端
func InitHTTPClient(proxy config.Proxy) {
	sharedClientMu.Lock()
	defer sharedClientMu.Unlock()
	sharedClient = NewHTTPClient(proxy)
}

// HTTPClient 返回共享的 HTTP 客户端
func HTTPClient() *http.Client {
	sharedClientMu.Lock()
	defer sharedClientMu.Unlock()
	if sharedClient == nil {
		sharedClient = DefaultHTTPClient()
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
