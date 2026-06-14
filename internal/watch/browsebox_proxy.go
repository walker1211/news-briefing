package watch

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
)

type browseboxProxySession struct {
	proxyURL string
	cancel   context.CancelFunc
	done     chan error
}

type browseboxProcessOutput struct {
	mu     sync.Mutex
	stdout []string
	stderr []string
}

const browseboxProcessOutputLimit = 20

var startBrowseboxProxy = startBrowseboxProxyProcess

func (o *browseboxProcessOutput) addStdout(line string) {
	o.add(&o.stdout, line)
}

func (o *browseboxProcessOutput) addStderr(line string) {
	o.add(&o.stderr, line)
}

func (o *browseboxProcessOutput) add(lines *[]string, line string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	*lines = append(*lines, line)
	if len(*lines) > browseboxProcessOutputLimit {
		*lines = append([]string(nil), (*lines)[len(*lines)-browseboxProcessOutputLimit:]...)
	}
}

func (o *browseboxProcessOutput) summary() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	parts := make([]string, 0, 2)
	if len(o.stdout) > 0 {
		parts = append(parts, "stdout: "+strings.Join(o.stdout, "\nstdout: "))
	}
	if len(o.stderr) > 0 {
		parts = append(parts, "stderr: "+strings.Join(o.stderr, "\nstderr: "))
	}
	return strings.Join(parts, "\n")
}

func appendBrowseboxProcessOutput(err error, output *browseboxProcessOutput) error {
	if err == nil || output == nil {
		return err
	}
	if summary := output.summary(); summary != "" {
		return fmt.Errorf("%w\n%s", err, summary)
	}
	return err
}

func recordBrowseboxLines(reader io.Reader, record func(string)) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		record(scanner.Text())
	}
}

func scanBrowseboxStdout(reader io.Reader, output *browseboxProcessOutput, lines chan<- string) {
	defer close(lines)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if output != nil {
			output.addStdout(line)
		}
		select {
		case lines <- line:
		default:
		}
	}
}

func waitBrowseboxRecorders(done ...<-chan struct{}) {
	for _, ch := range done {
		<-ch
	}
}

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

func browseboxCommandWithDir(command string) (string, string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return command, ""
	}
	if filepath.IsAbs(command) {
		return command, filepath.Dir(command)
	}
	if strings.ContainsRune(command, os.PathSeparator) {
		absCommand, err := filepath.Abs(command)
		if err == nil {
			return absCommand, filepath.Dir(absCommand)
		}
	}
	return command, ""
}

func startBrowseboxProxyProcess(ctx context.Context, cfg *config.Config) (*browseboxProxySession, error) {
	provider := cfg.Watch.ProxyProvider
	command, commandDir := browseboxCommandWithDir(provider.Command)
	commandCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(commandCtx, command, browseboxProxyArgs(cfg)...)
	if commandDir != "" {
		cmd.Dir = commandDir
	}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := cmd.Process.Signal(os.Interrupt)
		if err == nil || errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}
	cmd.WaitDelay = 5 * time.Second
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
	go func() {
		done <- cmd.Wait()
		close(done)
	}()
	processOutput := &browseboxProcessOutput{}
	stdoutLines := make(chan string, 64)
	stdoutDone := make(chan struct{})
	stderrDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		scanBrowseboxStdout(stdout, processOutput, stdoutLines)
	}()
	go func() {
		defer close(stderrDone)
		recordBrowseboxLines(stderr, processOutput.addStderr)
	}()

	startupTimeout := provider.StartupTimeout
	if startupTimeout <= 0 {
		startupTimeout = config.DefaultWatchBrowseboxStartupTimeout
	}
	proxyURL, err := waitBrowseboxProxyReady(ctx, stdoutLines, provider.ProxyPort, done, startupTimeout)
	if err != nil {
		cancel()
		<-done
		waitBrowseboxRecorders(stdoutDone, stderrDone)
		return nil, appendBrowseboxProcessOutput(err, processOutput)
	}
	return &browseboxProxySession{proxyURL: proxyURL, cancel: cancel, done: done}, nil
}

func waitBrowseboxProxyReady(ctx context.Context, lines <-chan string, port int, done <-chan error, timeout time.Duration) (string, error) {
	ready := fmt.Sprintf("Proxy: http://127.0.0.1:%d", port)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
			return "", fmt.Errorf("wait for browsebox proxy ready: timeout")
		case err := <-done:
			return "", fmt.Errorf("browsebox proxy exited before ready: %w", err)
		case line, ok := <-lines:
			if !ok {
				lines = nil
				continue
			}
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

func shouldRetryWatchProxyWithDirectFetch(rawURL string, html string) bool {
	return isClaudeReleaseNotesOverviewURL(rawURL) && isClaudeAppUnavailableHTML(html)
}
