package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/scheduler"
)

func TestRegisterScheduledWindowWritesSinglePersistentStateFile(t *testing.T) {
	app := newScheduledOnceTestApp(t, nil)
	window := scheduledStateTestWindow()
	dueAt := window.To.Add(10 * time.Minute)

	if err := app.registerScheduledWindow(window, dueAt); err != nil {
		t.Fatalf("registerScheduledWindow() error = %v", err)
	}
	record, ok, err := app.scheduledWindowState(window)
	if err != nil {
		t.Fatalf("scheduledWindowState() error = %v", err)
	}
	if !ok || record.Status != scheduledStatusPending || !record.DueAt.Equal(dueAt) {
		t.Fatalf("record = %#v, want pending due at %s", record, dueAt)
	}
	entries, err := os.ReadDir(filepath.Join(app.cfg.Output.Dir, "state"))
	if err != nil {
		t.Fatalf("ReadDir(state) error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "briefing-scheduler.json" {
		t.Fatalf("state entries = %v, want only briefing-scheduler.json", entryNames(entries))
	}
}

func TestScheduledBriefingOnceIsIdempotentAfterDone(t *testing.T) {
	app := newScheduledOnceTestApp(t, nil)
	window := scheduledStateTestWindow()
	if err := app.runScheduledBriefingOnceContext(context.Background(), window, "x-ready-callback", false); err != nil {
		t.Fatalf("first run error = %v", err)
	}
	app.fetch.fetchWindowDetailedContext = func(context.Context, *config.Config, time.Time, time.Time, bool, bool) (fetcher.FetchResult, error) {
		return fetcher.FetchResult{}, errors.New("should not run twice")
	}
	if err := app.runScheduledBriefingOnceContext(context.Background(), window, "cron", false); err != nil {
		t.Fatalf("duplicate run error = %v", err)
	}
	record := mustScheduledWindowState(t, app, window)
	if record.Status != scheduledStatusDone || record.Attempts != 1 {
		t.Fatalf("record = %#v, want done with one attempt", record)
	}
}

func TestScheduledBriefingHonorsLegacyTerminalMarkerDuringUpgrade(t *testing.T) {
	app := newScheduledOnceTestApp(t, errors.New("should not run"))
	window := scheduledStateTestWindow()
	legacyDir := filepath.Join(app.cfg.Output.Dir, "state", "scheduled-runs")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	legacyPath := filepath.Join(legacyDir, scheduledRunWindowKey(window)+".done")
	if err := os.WriteFile(legacyPath, []byte("status: done\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(legacy done) error = %v", err)
	}

	if err := app.runScheduledBriefingOnceContext(context.Background(), window, "x-ready-callback", false); err != nil {
		t.Fatalf("runScheduledBriefingOnceContext() error = %v", err)
	}
	if _, err := os.Stat(app.scheduledStatePath()); !os.IsNotExist(err) {
		t.Fatalf("new state unexpectedly written for legacy terminal window; stat error = %v", err)
	}
}

func TestScheduledBriefingCronWaitsWhileXHeartbeatFresh(t *testing.T) {
	window := scheduledStateTestWindow()
	statusPath := filepath.Join(t.TempDir(), "status.json")
	writeTestXStatus(t, statusPath, `{"status":"running","startedAt":"2026-07-27T10:00:00Z","heartbeatAt":"2026-07-27T10:09:00Z","window":{"from":"2026-07-27T00:00:00Z","to":"2026-07-27T10:00:00Z"}}`)
	app := newScheduledOnceTestApp(t, errors.New("should not run"))
	app.now = func() time.Time { return time.Date(2026, 7, 27, 10, 10, 0, 0, time.UTC) }
	app.cfg.XAccounts = config.XAccountsConfig{Enabled: true, RefreshStatusPath: statusPath, HeartbeatStaleAfter: 3 * time.Minute}
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		waitForScheduledWindowWatcherCount(t, app, 0)
	}()

	if err := app.runScheduledBriefingOnceContext(ctx, window, "cron", false); err != nil {
		t.Fatalf("runScheduledBriefingOnceContext() error = %v", err)
	}
	record := mustScheduledWindowState(t, app, window)
	if record.Status != scheduledStatusWaitingX || record.Attempts != 0 {
		t.Fatalf("record = %#v, want waiting_x without an attempt", record)
	}
}

func TestScheduledBriefingCronTakesOverStaleXHeartbeat(t *testing.T) {
	window := scheduledStateTestWindow()
	statusPath := filepath.Join(t.TempDir(), "status.json")
	writeTestXStatus(t, statusPath, `{"status":"running","heartbeatAt":"2026-07-27T10:06:00Z","window":{"from":"2026-07-27T00:00:00Z","to":"2026-07-27T10:00:00Z"}}`)
	app := newScheduledOnceTestApp(t, nil)
	app.now = func() time.Time { return time.Date(2026, 7, 27, 10, 10, 0, 0, time.UTC) }
	app.cfg.XAccounts = config.XAccountsConfig{Enabled: true, RefreshStatusPath: statusPath, HeartbeatStaleAfter: 3 * time.Minute}

	if err := app.runScheduledBriefingOnceContext(context.Background(), window, "cron", false); err != nil {
		t.Fatalf("runScheduledBriefingOnceContext() error = %v", err)
	}
	if got := mustScheduledWindowState(t, app, window).Status; got != scheduledStatusDone {
		t.Fatalf("status = %q, want done", got)
	}
}

func TestScheduledWindowReconcilerRecoversWaitingWindowAfterRestart(t *testing.T) {
	window := scheduledStateTestWindow()
	statusPath := filepath.Join(t.TempDir(), "status.json")
	writeTestXStatus(t, statusPath, `{"status":"failed","finishedAt":"2026-07-27T10:11:00Z","window":{"from":"2026-07-27T00:00:00Z","to":"2026-07-27T10:00:00Z"}}`)
	beforeRestart := newScheduledOnceTestApp(t, errors.New("not used"))
	beforeRestart.cfg.XAccounts = config.XAccountsConfig{Enabled: true, RefreshStatusPath: statusPath}
	if _, err := beforeRestart.markScheduledRunWaitingX(window, "cron"); err != nil {
		t.Fatalf("markScheduledRunWaitingX() error = %v", err)
	}

	afterRestart := newScheduledOnceTestApp(t, nil)
	afterRestart.cfg.Output.Dir = beforeRestart.cfg.Output.Dir
	afterRestart.cfg.XAccounts = beforeRestart.cfg.XAccounts
	if keep := afterRestart.reconcileScheduledWindow(context.Background(), window); keep {
		t.Fatal("terminal X state should finish the recovered window")
	}
	record := mustScheduledWindowState(t, afterRestart, window)
	if record.Status != scheduledStatusDone || record.Attempts != 1 {
		t.Fatalf("record = %#v, want one completed recovery attempt", record)
	}
}

func TestScheduledWindowReconcilerDoesNotInventMissedWindow(t *testing.T) {
	app := newScheduledOnceTestApp(t, errors.New("should not run"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.startScheduledWindowReconciler(ctx)
	if got := scheduledWindowWatcherCount(app); got != 0 {
		t.Fatalf("watcher count = %d, want 0", got)
	}
	if _, err := os.Stat(app.scheduledStatePath()); !os.IsNotExist(err) {
		t.Fatalf("state file unexpectedly created; stat error = %v", err)
	}
}

func TestScheduledWindowWatcherOnlyRunsForPersistedActiveWindow(t *testing.T) {
	app := newScheduledOnceTestApp(t, errors.New("should stay pending"))
	window := scheduledStateTestWindow()
	app.now = func() time.Time { return window.To }
	app.cfg.XAccounts.RefreshReconcile = 10 * time.Millisecond
	if err := app.registerScheduledWindow(window, window.To.Add(time.Hour)); err != nil {
		t.Fatalf("registerScheduledWindow() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.startScheduledWindowReconciler(ctx)
	waitForScheduledWindowWatcherCount(t, app, 1)

	if err := app.mutateScheduledState(context.Background(), func(state *scheduledStateFile, now time.Time) (bool, error) {
		record := state.Windows[scheduledRunWindowKey(window)]
		record.Status = scheduledStatusFailed
		record.UpdatedAt = now
		state.Windows[scheduledRunWindowKey(window)] = record
		return true, nil
	}); err != nil {
		t.Fatalf("mutateScheduledState() error = %v", err)
	}
	waitForScheduledWindowWatcherCount(t, app, 0)
}

func TestConcurrentScheduledTriggersAcquireOneLease(t *testing.T) {
	app := newScheduledOnceTestApp(t, nil)
	window := scheduledStateTestWindow()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	app.fetch.fetchWindowDetailedContext = func(context.Context, *config.Config, time.Time, time.Time, bool, bool) (fetcher.FetchResult, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return fetcher.FetchResult{Articles: sampleExecuteArticles()}, nil
	}

	var group sync.WaitGroup
	errs := make(chan error, 2)
	for _, trigger := range []string{"cron", "x-ready-callback"} {
		group.Add(1)
		go func(trigger string) {
			defer group.Done()
			errs <- app.runScheduledBriefingOnceContext(context.Background(), window, trigger, false)
		}(trigger)
	}
	<-started
	close(release)
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent run error = %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetch calls = %d, want 1", got)
	}
	if got := mustScheduledWindowState(t, app, window).Attempts; got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestStaleRunTakeoverRejectsOldLeaseUpdates(t *testing.T) {
	app := newScheduledOnceTestApp(t, nil)
	window := scheduledStateTestWindow()
	app.now = func() time.Time { return time.Date(2026, 7, 27, 10, 10, 0, 0, time.UTC) }
	if err := app.registerScheduledWindow(window, window.To); err != nil {
		t.Fatalf("registerScheduledWindow() error = %v", err)
	}
	oldLease, acquired, _, err := app.acquireScheduledRunWindow(window, "cron")
	if err != nil || !acquired {
		t.Fatalf("first acquire = %q, %v, %v", oldLease, acquired, err)
	}
	if err := app.mutateScheduledState(context.Background(), func(state *scheduledStateFile, now time.Time) (bool, error) {
		record := state.Windows[scheduledRunWindowKey(window)]
		record.HeartbeatAt = now.Add(-scheduledRunHeartbeatStale - time.Second)
		state.Windows[scheduledRunWindowKey(window)] = record
		return true, nil
	}); err != nil {
		t.Fatalf("make stale error = %v", err)
	}
	newLease, acquired, _, err := app.acquireScheduledRunWindow(window, "stale-recovery")
	if err != nil || !acquired || newLease == oldLease {
		t.Fatalf("takeover lease = %q acquired=%v err=%v", newLease, acquired, err)
	}
	if err := app.finishScheduledRun(window, oldLease, scheduledStatusDone, nil, nil); err != nil {
		t.Fatalf("old lease finish error = %v", err)
	}
	record := mustScheduledWindowState(t, app, window)
	if record.Status != scheduledStatusRunning || record.LeaseID != newLease || record.Attempts != 2 {
		t.Fatalf("record = %#v, want second lease still running", record)
	}
}

func TestStaleRunRecoveryDoesNotResendConfirmedEmail(t *testing.T) {
	app := newScheduledOnceTestApp(t, nil)
	window := scheduledStateTestWindow()
	now := time.Date(2026, 7, 27, 10, 10, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	if err := app.registerScheduledWindow(window, window.To); err != nil {
		t.Fatalf("registerScheduledWindow() error = %v", err)
	}
	_, acquired, _, err := app.acquireScheduledRunWindow(window, "cron")
	if err != nil || !acquired {
		t.Fatalf("first acquire acquired=%v err=%v", acquired, err)
	}
	if err := app.mutateScheduledState(context.Background(), func(state *scheduledStateFile, current time.Time) (bool, error) {
		record := state.Windows[scheduledRunWindowKey(window)]
		record.EmailSentAt = current.Add(-time.Minute)
		record.HeartbeatAt = current.Add(-scheduledRunHeartbeatStale - time.Second)
		state.Windows[scheduledRunWindowKey(window)] = record
		return true, nil
	}); err != nil {
		t.Fatalf("prepare stale state error = %v", err)
	}
	emailCalls := 0
	app.email.sendEmail = func(*model.Briefing, *config.Config, []fetcher.FailedSource) error {
		emailCalls++
		return nil
	}
	if err := app.runScheduledBriefingOnceContext(context.Background(), window, "stale-recovery", true); err != nil {
		t.Fatalf("recovery error = %v", err)
	}
	if emailCalls != 0 {
		t.Fatalf("email calls = %d, want 0", emailCalls)
	}
}

func TestScheduledRunRecordsFieldsAndEmailDelivery(t *testing.T) {
	window := scheduledStateTestWindow()
	app := newScheduledOnceTestApp(t, nil)
	app.cfg.Output.Mode = model.OutputModeTranslatedOnly
	app.cfg.Output.MaxArticlesByCategory = map[string]int{"AI/科技": 1, "国际政治": 1}
	app.fetch.fetchWindowDetailedContext = func(context.Context, *config.Config, time.Time, time.Time, bool, bool) (fetcher.FetchResult, error) {
		return fetcher.FetchResult{Articles: []model.Article{
			{Title: "ai-1", Category: "AI/科技"},
			{Title: "ai-2", Category: "AI/科技"},
			{Title: "politics-1", Category: "国际政治"},
		}}, nil
	}
	app.ai.summarizeContext = func(context.Context, []model.Article, []string, *time.Location) (string, error) {
		record := mustScheduledWindowState(t, app, window)
		if record.Status != scheduledStatusRunning || record.Fields["stage"] != "ai_summary" {
			t.Fatalf("running record = %#v", record)
		}
		return "summary", nil
	}
	app.email.sendEmail = func(*model.Briefing, *config.Config, []fetcher.FailedSource) error { return nil }

	if err := app.runScheduledBriefingOnceContext(context.Background(), window, "x-ready-callback", true); err != nil {
		t.Fatalf("runScheduledBriefingOnceContext() error = %v", err)
	}
	record := mustScheduledWindowState(t, app, window)
	if record.Status != scheduledStatusDone || record.EmailSentAt.IsZero() {
		t.Fatalf("record = %#v, want done with email timestamp", record)
	}
	if record.Fields["article_limit_total_before"] != "3" || record.Fields["article_limit_total_after"] != "2" {
		t.Fatalf("fields = %#v", record.Fields)
	}
}

func TestScheduledRunFailureIsTerminal(t *testing.T) {
	runErr := errors.New("fetch failed")
	app := newScheduledOnceTestApp(t, runErr)
	window := scheduledStateTestWindow()
	if err := app.runScheduledBriefingOnceContext(context.Background(), window, "cron", false); !errors.Is(err, runErr) {
		t.Fatalf("run error = %v, want %v", err, runErr)
	}
	record := mustScheduledWindowState(t, app, window)
	if record.Status != scheduledStatusFailed || record.Error != runErr.Error() {
		t.Fatalf("record = %#v, want terminal failure", record)
	}
}

func newScheduledOnceTestApp(t *testing.T, runErr error) *app {
	t.Helper()
	cfg := executeTestConfig(t, model.OutputModeOriginalOnly)
	return &app{
		cfg: cfg,
		fetch: fetchDeps{
			fetchWindowDetailedContext: func(context.Context, *config.Config, time.Time, time.Time, bool, bool) (fetcher.FetchResult, error) {
				if runErr != nil {
					return fetcher.FetchResult{}, runErr
				}
				return fetcher.FetchResult{Articles: sampleExecuteArticles()}, nil
			},
		},
		output: silentBriefingOutputDeps("body"),
		email: emailDeps{
			sendEmail: func(*model.Briefing, *config.Config, []fetcher.FailedSource) error { return nil },
		},
	}
}

func scheduledStateTestWindow() scheduler.Window {
	return scheduler.Window{
		Expr:   "0 18 * * *",
		Period: "1800",
		From:   time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC),
		To:     time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC),
	}
}

func mustScheduledWindowState(t *testing.T, app *app, window scheduler.Window) scheduledWindowState {
	t.Helper()
	record, ok, err := app.scheduledWindowState(window)
	if err != nil {
		t.Fatalf("scheduledWindowState() error = %v", err)
	}
	if !ok {
		t.Fatal("scheduled window state missing")
	}
	return record
}

func writeTestXStatus(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(status) error = %v", err)
	}
}

func scheduledWindowWatcherCount(app *app) int {
	app.scheduledWindowMu.Lock()
	defer app.scheduledWindowMu.Unlock()
	return len(app.scheduledWindowWatchers)
}

func waitForScheduledWindowWatcherCount(t *testing.T, app *app, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if scheduledWindowWatcherCount(app) == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("watcher count = %d, want %d", scheduledWindowWatcherCount(app), want)
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
