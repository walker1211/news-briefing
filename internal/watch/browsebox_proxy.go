package watch

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
)

type browseboxProxySession struct {
	proxyURL string
	cancel   context.CancelFunc
	done     chan error
}

var startBrowseboxProxy = startBrowseboxProxyProcess

func watchProxyProviderEnabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Watch.ProxyProvider.Enabled && cfg.Watch.ProxyProvider.Type == config.WatchProxyProviderTypeBrowsebox
}

func browseboxProxyArgs(cfg *config.Config) []string {
	provider := cfg.Watch.ProxyProvider
	mode := strings.TrimSpace(provider.Mode)
	if mode == "" {
		mode = config.WatchProxyProviderModeProxy
	}
	args := []string{
		mode,
		"--select-fastest",
		"--group", provider.Group,
		"--proxy-port", strconv.Itoa(provider.ProxyPort),
		"--controller-port", strconv.Itoa(provider.ControllerPort),
		"--nodes-concurrency", strconv.Itoa(provider.NodesConcurrency),
		"--delay-timeout-ms", strconv.Itoa(provider.DelayTimeoutMS),
	}
	for _, healthURL := range browseboxHealthURLs(cfg) {
		args = append(args, "--health-url", healthURL)
	}
	return args
}

func browseboxHealthURLs(cfg *config.Config) []string {
	if len(cfg.Watch.ProxyProvider.HealthURLs) > 0 {
		return append([]string(nil), cfg.Watch.ProxyProvider.HealthURLs...)
	}
	urls := make([]string, 0, len(cfg.Watch.Sites))
	seen := make(map[string]struct{}, len(cfg.Watch.Sites))
	for _, site := range cfg.Watch.Sites {
		url := strings.TrimSpace(site.HomeURL)
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		urls = append(urls, url)
	}
	return urls
}

func startBrowseboxProxyProcess(ctx context.Context, cfg *config.Config) (*browseboxProxySession, error) {
	provider := cfg.Watch.ProxyProvider
	commandCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(commandCtx, provider.Command, browseboxProxyArgs(cfg)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open browsebox stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open browsebox stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start browsebox proxy: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	go io.Copy(io.Discard, stderr)

	proxyURL, err := waitBrowseboxProxyReady(ctx, stdout, provider.ProxyPort, done)
	if err != nil {
		cancel()
		<-done
		return nil, err
	}
	return &browseboxProxySession{proxyURL: proxyURL, cancel: cancel, done: done}, nil
}

func waitBrowseboxProxyReady(ctx context.Context, stdout io.Reader, port int, done <-chan error) (string, error) {
	ready := fmt.Sprintf("Proxy: http://127.0.0.1:%d", port)
	lines := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
			return "", fmt.Errorf("wait for browsebox proxy ready: timeout")
		case err := <-done:
			return "", fmt.Errorf("browsebox proxy exited before ready: %w", err)
		case line := <-lines:
			if strings.TrimSpace(line) == ready {
				return "http://127.0.0.1:" + strconv.Itoa(port), nil
			}
		}
	}
}

func (s *browseboxProxySession) Close() error {
	if s == nil {
		return nil
	}
	s.cancel()
	return <-s.done
}

func watchHTTPClientForProxy(cfg *config.Config, proxyURL string) *http.Client {
	return fetcher.NewHTTPClient(config.Proxy{HTTP: proxyURL}, cfg.Fetch.Timeout)
}
