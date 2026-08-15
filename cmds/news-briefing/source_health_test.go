package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/scheduler"
)

func TestApplyScheduledSourceHealthPolicySuppressesFirstFailureThenReportsRecovery(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 8, 15, 8, 0, 0, 0, loc)
	app := &app{
		cfg: &config.Config{
			Output:       config.OutputCfg{Dir: t.TempDir()},
			SourceHealth: config.SourceHealthConfig{AlertAfterConsecutiveFailures: 2},
		},
		now: func() time.Time { return now },
	}
	window := scheduler.Window{Period: "0800", From: now.Add(-14 * time.Hour), To: now}
	failing := briefingFetchResult{
		failed:                []fetcher.FailedSource{{Name: "The Diplomat", Err: errors.New("timeout")}},
		watchSiteErrorNotices: []string{"Watch 站点异常：Anthropic Claude Support — 抓取失败：timeout"},
	}

	first := app.applyScheduledSourceHealthPolicy(window, failing)
	if len(first.failed) != 0 || len(first.watchSiteErrorNotices) != 0 {
		t.Fatalf("first = %#v, want first failure suppressed", first)
	}

	now = now.Add(10 * time.Hour)
	window = scheduler.Window{Period: "1800", From: window.To, To: now}
	second := app.applyScheduledSourceHealthPolicy(window, failing)
	if len(second.failed) != 1 || len(second.watchSiteErrorNotices) != 1 {
		t.Fatalf("second = %#v, want failures visible", second)
	}

	now = now.Add(14 * time.Hour)
	window = scheduler.Window{Period: "0800", From: window.To, To: now}
	recovered := app.applyScheduledSourceHealthPolicy(window, briefingFetchResult{})
	if len(recovered.failed) != 0 || len(recovered.watchSiteErrorNotices) != 2 {
		t.Fatalf("recovered = %#v", recovered)
	}
	body := appendWatchSiteErrorNotices("body", recovered.watchSiteErrorNotices)
	for _, want := range []string{"## 来源恢复", "The Diplomat 已恢复正常", "Anthropic Claude Support 已恢复正常"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %q, want %q", body, want)
		}
	}
}

func TestWatchFailureNoticeName(t *testing.T) {
	if got := watchFailureNoticeName("Watch 站点异常：Support — 抓取失败：EOF"); got != "Support" {
		t.Fatalf("site name = %q", got)
	}
	if got := watchFailureNoticeName("Watch 抓取失败：timeout"); got != "Watch" {
		t.Fatalf("watch name = %q", got)
	}
}
