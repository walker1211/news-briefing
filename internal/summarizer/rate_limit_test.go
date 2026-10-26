package summarizer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAICLILimitErrorCode(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"ERROR: exceeded retry limit; last status: 429 Too Many Requests", "rate_limited"},
		{"HTTP 429", "rate_limited"},
		{"HTTP/1.1 429", "rate_limited"},
		{"status code: 429", "rate_limited"},
		{`{"status":429,"error":{"code":"rate_limit_exceeded"}}`, "rate_limited"},
		{"rate_limit_error", "rate_limited"},
		{"slow_down", "rate_limited"},
		{"article 429 is available", ""},
		{"response has 1429 tokens", ""},
		{"HTTP 429: insufficient_quota", "quota_exhausted"},
		{"usage_limit_reached", "quota_exhausted"},
		{"credits_balance_too_low", "quota_exhausted"},
		{"You've hit your usage limit. Try again later.", "quota_exhausted"},
		{"You have reached your Codex usage limit", "quota_exhausted"},
		{"server_error", ""},
	} {
		t.Run(tc.text, func(t *testing.T) {
			err := fmt.Errorf("category failed: %w", errors.New(tc.text))
			if got := AICLILimitErrorCode(err); got != tc.want {
				t.Fatalf("AICLILimitErrorCode() = %q, want %q", got, tc.want)
			}
		})
	}
	if got := AICLILimitErrorCode(nil); got != "" {
		t.Fatalf("nil error code = %q", got)
	}
	joined := errors.Join(errors.New("HTTP 429"), errors.New("insufficient_quota"))
	if got := AICLILimitErrorCode(joined); got != "quota_exhausted" {
		t.Fatalf("joined quota code = %q", got)
	}
}

func TestRateLimitRetryDelayHonorsCooldownAndRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, hint string
		index      int
		configured time.Duration
		want       time.Duration
	}{
		{name: "first", want: 30 * time.Second},
		{name: "second", index: 1, want: time.Minute},
		{name: "third", index: 2, want: 2 * time.Minute},
		{name: "cap", index: 100, want: 2 * time.Minute},
		{name: "configuration", configured: 3 * time.Minute, want: 3 * time.Minute},
		{name: "seconds", hint: "Retry-After: 90", want: 90 * time.Second},
		{name: "quoted", hint: `"Retry-After": "180"`, want: 3 * time.Minute},
		{name: "date", hint: "Retry-After: " + now.Add(4*time.Minute).Format(http.TimeFormat), want: 4 * time.Minute},
		{name: "past", hint: "Retry-After: " + now.Add(-time.Minute).Format(http.TimeFormat), want: 30 * time.Second},
		{name: "invalid", hint: "Retry-After: nope", want: 30 * time.Second},
		{name: "multiple", hint: "Retry-After: 40\nRetry-After: 80", want: 80 * time.Second},
		{name: "large", hint: "Retry-After: 18446744073709551615", want: time.Duration(1<<63 - 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := rateLimitRetryDelay(tc.index, tc.configured, tc.hint, now); got != tc.want {
				t.Fatalf("rateLimitRetryDelay() = %s, want %s", got, tc.want)
			}
		})
	}
}

func rateLimitTestRunner(t *testing.T, script string, delays []time.Duration) *Runner {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test helper requires POSIX shell")
	}
	path := filepath.Join(t.TempDir(), "rate-limit-ai")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	r := NewRunnerWithRetryDelays(path, nil, false, "", "", delays)
	r.failureLogPath = ""
	return r
}

func TestCallClaudeRateLimitRetriesOnlyUntilSuccess(t *testing.T) {
	state := filepath.Join(t.TempDir(), "count")
	r := rateLimitTestRunner(t, fmt.Sprintf(`n=0
if [ -f %q ]; then n=$(cat %q); fi
n=$((n+1))
printf '%%s' "$n" > %q
if [ "$n" -le 2 ]; then
  printf 'ERROR: exceeded retry limit; last status: 429 Too Many Requests\nRetry-After: 45\n' >&2
  exit 1
fi
printf 'successful summary'
`, state, state, state), []time.Duration{time.Second, 2 * time.Second, 5 * time.Second})
	var sleeps []time.Duration
	r.retrySleep = func(_ context.Context, delay time.Duration) error {
		sleeps = append(sleeps, delay)
		return nil
	}
	got, err := r.callClaude("test")
	if err != nil || got != "successful summary" {
		t.Fatalf("callClaude() = %q, %v", got, err)
	}
	if want := []time.Duration{45 * time.Second, time.Minute}; !reflect.DeepEqual(sleeps, want) {
		t.Fatalf("sleeps = %v, want %v", sleeps, want)
	}
	if len(r.rateLimitRetryGate) != 0 {
		t.Fatal("rate-limit gate leaked after success")
	}
}

func TestCallClaudeRateLimitRetryBudgetIsBounded(t *testing.T) {
	r := rateLimitTestRunner(t, "printf 'HTTP 429 Too Many Requests' >&2\nexit 1\n", []time.Duration{time.Second, time.Second, time.Second})
	var sleeps []time.Duration
	r.retrySleep = func(_ context.Context, delay time.Duration) error {
		sleeps = append(sleeps, delay)
		return nil
	}
	_, err := r.callClaude("test")
	if err == nil || !strings.Contains(err.Error(), "failed after 4 attempts") {
		t.Fatalf("exhausted call error = %v", err)
	}
	if want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute}; !reflect.DeepEqual(sleeps, want) {
		t.Fatalf("sleeps = %v, want %v", sleeps, want)
	}
	if len(r.rateLimitRetryGate) != 0 {
		t.Fatal("rate-limit gate leaked after exhaustion")
	}
}

func TestCallClaudeQuotaExhaustionDoesNotRetry(t *testing.T) {
	r := rateLimitTestRunner(t, "printf 'HTTP 429 insufficient_quota server_error' >&2\nexit 1\n", nil)
	r.retrySleep = func(context.Context, time.Duration) error {
		t.Fatal("quota exhaustion must not sleep or retry")
		return nil
	}
	_, err := r.callClaude("test")
	if err == nil || !strings.Contains(err.Error(), "failed after 1 attempts") || AICLILimitErrorCode(err) != "quota_exhausted" {
		t.Fatalf("quota error = %v", err)
	}
}

func TestCallClaudeRateLimitRetriesSerializeAndKeepHealthySibling(t *testing.T) {
	dir := t.TempDir()
	r := rateLimitTestRunner(t, fmt.Sprintf(`prompt="$2"
if [ "$prompt" = healthy ]; then printf 'healthy summary'; exit 0; fi
if [ ! -f %q/"$prompt" ]; then
  touch %q/"$prompt"
  printf 'HTTP 429 Too Many Requests' >&2
  exit 1
fi
if ! mkdir %q/retry-active; then touch %q/overlap; exit 1; fi
sleep 0.1
rmdir %q/retry-active
printf 'retried summary'
`, dir, dir, dir, dir, dir), []time.Duration{time.Second})
	var active, maxActive atomic.Int32
	sleepEntered := make(chan struct{}, 3)
	release := make(chan struct{})
	r.retrySleep = func(ctx context.Context, _ time.Duration) error {
		n := active.Add(1)
		defer active.Add(-1)
		for prev := maxActive.Load(); n > prev && !maxActive.CompareAndSwap(prev, n); prev = maxActive.Load() {
		}
		sleepEntered <- struct{}{}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, prompt := range []string{"one", "two"} {
		wg.Go(func() {
			_, err := r.callClaudeContext(ctx, prompt)
			errs <- err
		})
	}
	select {
	case <-sleepEntered:
	case <-ctx.Done():
		t.Fatal("limited call did not reach retry")
	}
	got, err := r.callClaudeContext(ctx, "healthy")
	if err != nil || got != "healthy summary" {
		t.Fatalf("healthy sibling = %q, %v", got, err)
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("limited sibling failed: %v", err)
		}
	}
	if maxActive.Load() != 1 {
		t.Fatalf("concurrent retry sleeps = %d, want 1", maxActive.Load())
	}
	if _, err := os.Stat(filepath.Join(dir, "overlap")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retry commands overlapped, stat error = %v", err)
	}
}

func TestCallClaudeRateLimitGateCancellationDoesNotLeak(t *testing.T) {
	r := NewRunner("unused-ai", nil, false, "", "")
	r.rateLimitRetryGate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	release, err := r.acquireRateLimitRetryGate(ctx)
	if release != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("gate cancellation error = %v", err)
	}
	<-r.rateLimitRetryGate
	release, err = r.acquireRateLimitRetryGate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
	if len(r.rateLimitRetryGate) != 0 {
		t.Fatal("gate cancellation leaked holder")
	}
}

func TestCallClaudeRateLimitSleepCancellationReleasesGate(t *testing.T) {
	r := rateLimitTestRunner(t, "printf 'HTTP 429' >&2\nexit 1\n", []time.Duration{time.Second})
	entered := make(chan struct{})
	r.retrySleep = func(ctx context.Context, _ time.Duration) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := r.callClaudeContext(ctx, "test")
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("call did not enter rate-limit cooldown")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("sleep cancellation error = %v", err)
	}
	if len(r.rateLimitRetryGate) != 0 {
		t.Fatal("sleep cancellation leaked holder")
	}
}

func TestCallClaudeRateLimitSharesGenericRetryBudget(t *testing.T) {
	state := filepath.Join(t.TempDir(), "count")
	r := rateLimitTestRunner(t, fmt.Sprintf(`n=0
if [ -f %q ]; then n=$(cat %q); fi
n=$((n+1))
printf '%%s' "$n" > %q
if [ "$n" -eq 1 ]; then printf 'server_error' >&2; else printf 'HTTP 429' >&2; fi
exit 1
`, state, state, state), []time.Duration{time.Second, 2 * time.Second})
	var sleeps []time.Duration
	r.retrySleep = func(_ context.Context, delay time.Duration) error {
		sleeps = append(sleeps, delay)
		return nil
	}
	_, err := r.callClaude("test")
	if err == nil || !strings.Contains(err.Error(), "failed after 3 attempts") {
		t.Fatalf("mixed retry error = %v", err)
	}
	if want := []time.Duration{time.Second, time.Minute}; !reflect.DeepEqual(sleeps, want) {
		t.Fatalf("mixed sleeps = %v, want %v", sleeps, want)
	}
}
