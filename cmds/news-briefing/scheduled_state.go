package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/logutil"
	"github.com/walker1211/news-briefing/internal/scheduler"
	"github.com/walker1211/news-briefing/internal/statefile"
)

const (
	scheduledStateSchemaVersion = 1

	scheduledStatusPending  = "pending"
	scheduledStatusWaitingX = "waiting_x"
	scheduledStatusRunning  = "running"
	scheduledStatusDone     = "done"
	scheduledStatusFailed   = "failed"

	scheduledRunHeartbeatInterval = time.Minute
	scheduledRunHeartbeatStale    = 3 * time.Minute
	scheduledStateLockStale       = 30 * time.Second
	scheduledStateLockWait        = 5 * time.Second
	scheduledTerminalRetention    = 14 * 24 * time.Hour
)

type scheduledStateFile struct {
	SchemaVersion int                             `json:"schemaVersion"`
	UpdatedAt     time.Time                       `json:"updatedAt"`
	Windows       map[string]scheduledWindowState `json:"windows"`
}

type scheduledWindowState struct {
	RunID       string            `json:"runId,omitempty"`
	Expr        string            `json:"expr"`
	Period      string            `json:"period"`
	From        time.Time         `json:"from"`
	To          time.Time         `json:"to"`
	DueAt       time.Time         `json:"dueAt"`
	Status      string            `json:"status"`
	Trigger     string            `json:"trigger,omitempty"`
	StartedAt   time.Time         `json:"startedAt,omitzero"`
	FinishedAt  time.Time         `json:"finishedAt,omitzero"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	HeartbeatAt time.Time         `json:"heartbeatAt,omitzero"`
	Attempts    int               `json:"attempts"`
	LeaseID     string            `json:"leaseId,omitempty"`
	EmailSentAt time.Time         `json:"emailSentAt,omitzero"`
	Error       string            `json:"error,omitempty"`
	Fields      map[string]string `json:"fields,omitempty"`
}

type scheduledWindowWatcher struct {
	cancel context.CancelFunc
}

type scheduledRunReporter struct {
	app     *app
	leaseID string
	window  scheduler.Window
	mu      sync.Mutex
	fields  map[string]string
}

func (r *scheduledRunReporter) updateArticleLimits(report articleLimitReport) {
	if r == nil || !report.applied {
		return
	}
	r.updateFields("article_limit", report.stateFields())
}

func (r *scheduledRunReporter) updateAIStage(attempt aiBriefingAttempt, stage string, elapsed time.Duration) {
	if r == nil {
		return
	}
	fields := map[string]string{
		"ai_attempt":    attempt.name,
		"ai_articles":   fmt.Sprintf("%d", len(attempt.articles)),
		"ai_categories": articleCategoryCountsString(attempt.articles),
	}
	if attempt.fallback {
		fields["ai_fallback"] = "true"
	}
	if elapsed > 0 {
		fields["ai_elapsed"] = elapsed.Round(time.Second).String()
	}
	r.updateFields(stage, fields)
}

func (r *scheduledRunReporter) updateAIFailure(attempt aiBriefingAttempt, diagnostic aiFailureDiagnostic, elapsed time.Duration) {
	if r == nil {
		return
	}
	prefix := "ai_" + aiAttemptFieldName(attempt.name)
	fields := map[string]string{
		prefix + "_status":      "failed",
		prefix + "_error_stage": diagnostic.Stage,
		prefix + "_error_code":  diagnostic.Code,
		prefix + "_elapsed":     elapsed.Round(time.Second).String(),
	}
	if len(diagnostic.Categories) > 0 {
		fields[prefix+"_failed_categories"] = strings.Join(diagnostic.Categories, ",")
	}
	r.updateFields("ai_summary_failed", fields)
}

func (r *scheduledRunReporter) updateAISuccess(attempt aiBriefingAttempt, elapsed time.Duration) {
	if r == nil {
		return
	}
	prefix := "ai_" + aiAttemptFieldName(attempt.name)
	fields := map[string]string{
		prefix + "_status":  "succeeded",
		prefix + "_elapsed": elapsed.Round(time.Second).String(),
	}
	if attempt.fallback {
		fields["ai_recovered"] = "true"
		fields["ai_recovered_by"] = attempt.name
	}
	r.updateFields("ai_summary", fields)
}

func (r *scheduledRunReporter) updateFields(stage string, fields map[string]string) {
	if r == nil || r.app == nil {
		return
	}
	r.mu.Lock()
	if r.fields == nil {
		r.fields = make(map[string]string)
	}
	for key, value := range fields {
		r.fields[key] = value
	}
	r.fields["stage"] = stage
	stateFields := cloneStringMap(r.fields)
	r.mu.Unlock()
	if err := r.app.updateScheduledRun(r.window, r.leaseID, func(record *scheduledWindowState, now time.Time) {
		record.HeartbeatAt = now
		record.Fields = stateFields
	}); err != nil {
		logutil.Errorf("update scheduled run state: %v", err)
	}
}

func (r *scheduledRunReporter) markEmailSent() {
	if r == nil || r.app == nil {
		return
	}
	if err := r.app.updateScheduledRun(r.window, r.leaseID, func(record *scheduledWindowState, now time.Time) {
		record.HeartbeatAt = now
		record.EmailSentAt = now
	}); err != nil {
		logutil.Errorf("update scheduled email state: %v", err)
	}
}

func (r *scheduledRunReporter) stateFields() map[string]string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneStringMap(r.fields)
}

func cloneStringMap(fields map[string]string) map[string]string {
	if len(fields) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(fields))
	for key, value := range fields {
		cloned[key] = value
	}
	return cloned
}

func (app *app) scheduledStatePath() string {
	outputDir := "output"
	if app != nil && app.cfg != nil && strings.TrimSpace(app.cfg.Output.Dir) != "" {
		outputDir = app.cfg.Output.Dir
	}
	return filepath.Join(outputDir, "state", "briefing-scheduler.json")
}

func scheduledRunWindowKey(window scheduler.Window) string {
	return window.From.UTC().Format("20060102T150405Z") + "_" + window.To.UTC().Format("20060102T150405Z")
}

func scheduledRunID(window scheduler.Window) string {
	normalize := func(value time.Time) string {
		return strings.ReplaceAll(value.UTC().Format("20060102T150405.000Z"), ".", "")
	}
	return normalize(window.From) + "_" + normalize(window.To)
}

func scheduledWindowFromState(record scheduledWindowState) (scheduler.Window, error) {
	if record.Period == "" || record.From.IsZero() || !record.To.After(record.From) {
		return scheduler.Window{}, fmt.Errorf("invalid scheduled window state")
	}
	return scheduler.Window{Expr: record.Expr, Period: record.Period, From: record.From, To: record.To}, nil
}

func (app *app) readScheduledState() (scheduledStateFile, error) {
	path := app.scheduledStatePath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return scheduledStateFile{SchemaVersion: scheduledStateSchemaVersion, Windows: make(map[string]scheduledWindowState)}, nil
	}
	if err != nil {
		return scheduledStateFile{}, err
	}
	var state scheduledStateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return scheduledStateFile{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if state.SchemaVersion != scheduledStateSchemaVersion {
		return scheduledStateFile{}, fmt.Errorf("unsupported scheduler state schemaVersion %d", state.SchemaVersion)
	}
	if state.Windows == nil {
		state.Windows = make(map[string]scheduledWindowState)
	}
	return state, nil
}

func (app *app) mutateScheduledState(ctx context.Context, mutate func(*scheduledStateFile, time.Time) (bool, error)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	lockPath := app.scheduledStatePath() + ".lock"
	if err := acquireScheduledStateLock(ctx, lockPath); err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
			logutil.Errorf("remove scheduler state lock: %v", err)
		}
	}()

	state, err := app.readScheduledState()
	if err != nil {
		return err
	}
	now := app.currentTime()
	changed, err := mutate(&state, now)
	if err != nil || !changed {
		return err
	}
	pruneScheduledState(&state, now)
	state.SchemaVersion = scheduledStateSchemaVersion
	state.UpdatedAt = now
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return statefile.WriteAtomic(app.scheduledStatePath(), append(data, '\n'), 0o644)
}

func acquireScheduledStateLock(ctx context.Context, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	deadline := time.Now().Add(scheduledStateLockWait)
	for {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, writeErr := fmt.Fprintf(file, "pid=%d\ncreated_at=%s\n", os.Getpid(), time.Now().Format(time.RFC3339Nano))
			closeErr := file.Close()
			if writeErr != nil {
				_ = os.Remove(path)
				return writeErr
			}
			if closeErr != nil {
				_ = os.Remove(path)
				return closeErr
			}
			return nil
		}
		if !os.IsExist(err) {
			return err
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > scheduledStateLockStale {
			if removeErr := os.Remove(path); removeErr == nil || os.IsNotExist(removeErr) {
				continue
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for scheduler state lock %s", path)
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func pruneScheduledState(state *scheduledStateFile, now time.Time) {
	for key, record := range state.Windows {
		if !scheduledStatusTerminal(record.Status) || record.UpdatedAt.IsZero() {
			continue
		}
		if now.Sub(record.UpdatedAt) > scheduledTerminalRetention {
			delete(state.Windows, key)
		}
	}
}

func scheduledStatusTerminal(status string) bool {
	return status == scheduledStatusDone || status == scheduledStatusFailed
}

func scheduledStatusActive(status string) bool {
	return status == scheduledStatusPending || status == scheduledStatusWaitingX || status == scheduledStatusRunning
}

func (app *app) registerScheduledWindow(window scheduler.Window, dueAt time.Time) error {
	if status, terminal, err := app.legacyScheduledTerminal(window); err != nil {
		return err
	} else if terminal {
		logutil.Printf("[scheduler] 不登记窗口 %s：旧调度状态为 %s", window.Period, status)
		return nil
	}
	if dueAt.IsZero() {
		dueAt = window.To
		if app != nil && app.cfg != nil {
			dueAt = dueAt.Add(app.cfg.ScheduleDelay)
		}
	}
	return app.mutateScheduledState(context.Background(), func(state *scheduledStateFile, now time.Time) (bool, error) {
		key := scheduledRunWindowKey(window)
		if existing, ok := state.Windows[key]; ok {
			if _, err := scheduledWindowFromState(existing); err != nil {
				return false, fmt.Errorf("invalid scheduler state for %s: %w", key, err)
			}
			return false, nil
		}
		state.Windows[key] = scheduledWindowState{
			RunID:     scheduledRunID(window),
			Expr:      window.Expr,
			Period:    window.Period,
			From:      window.From,
			To:        window.To,
			DueAt:     dueAt,
			Status:    scheduledStatusPending,
			Trigger:   "schedule",
			UpdatedAt: now,
		}
		return true, nil
	})
}

func (app *app) markScheduledRunWaitingX(window scheduler.Window, trigger string) (bool, error) {
	if status, terminal, err := app.legacyScheduledTerminal(window); err != nil {
		return false, err
	} else if terminal {
		logutil.Printf("[scheduler] 跳过窗口 %s：旧调度状态为 %s", window.Period, status)
		return false, nil
	}
	active := false
	err := app.mutateScheduledState(context.Background(), func(state *scheduledStateFile, now time.Time) (bool, error) {
		key := scheduledRunWindowKey(window)
		record, ok := state.Windows[key]
		if ok && scheduledStatusTerminal(record.Status) {
			return false, nil
		}
		if ok && record.Status == scheduledStatusRunning {
			active = true
			return false, nil
		}
		if !ok {
			record = scheduledWindowState{RunID: scheduledRunID(window), Expr: window.Expr, Period: window.Period, From: window.From, To: window.To, DueAt: window.To}
		}
		if record.RunID == "" {
			record.RunID = scheduledRunID(window)
		}
		record.Status = scheduledStatusWaitingX
		record.Trigger = trigger
		record.UpdatedAt = now
		record.HeartbeatAt = time.Time{}
		record.LeaseID = ""
		record.Error = ""
		state.Windows[key] = record
		active = true
		return true, nil
	})
	return active, err
}

func (app *app) acquireScheduledRunWindow(window scheduler.Window, trigger string) (string, bool, bool, error) {
	if status, terminal, err := app.legacyScheduledTerminal(window); err != nil {
		return "", false, false, err
	} else if terminal {
		logutil.Printf("[scheduler] 跳过窗口 %s：旧调度状态为 %s", window.Period, status)
		return "", false, false, nil
	}
	leaseID, err := newScheduledLeaseID()
	if err != nil {
		return "", false, false, err
	}
	acquired := false
	emailAlreadySent := false
	err = app.mutateScheduledState(context.Background(), func(state *scheduledStateFile, now time.Time) (bool, error) {
		key := scheduledRunWindowKey(window)
		record, ok := state.Windows[key]
		if ok && scheduledStatusTerminal(record.Status) {
			logutil.Printf("[scheduler] 跳过窗口 %s：状态为 %s", window.Period, record.Status)
			return false, nil
		}
		if ok && record.Status == scheduledStatusRunning && !scheduledRunStateStale(record, now) {
			logutil.Printf("[scheduler] 跳过窗口 %s：已有执行中任务", window.Period)
			return false, nil
		}
		if !ok {
			record = scheduledWindowState{RunID: scheduledRunID(window), Expr: window.Expr, Period: window.Period, From: window.From, To: window.To, DueAt: window.To}
		}
		if record.RunID == "" {
			record.RunID = scheduledRunID(window)
		}
		emailAlreadySent = !record.EmailSentAt.IsZero()
		record.Status = scheduledStatusRunning
		record.Trigger = trigger
		record.StartedAt = now
		record.FinishedAt = time.Time{}
		record.UpdatedAt = now
		record.HeartbeatAt = now
		record.Attempts++
		record.LeaseID = leaseID
		record.Error = ""
		state.Windows[key] = record
		acquired = true
		return true, nil
	})
	return leaseID, acquired, emailAlreadySent, err
}

func (app *app) legacyScheduledTerminal(window scheduler.Window) (string, bool, error) {
	dir := filepath.Join(filepath.Dir(app.scheduledStatePath()), "scheduled-runs")
	key := scheduledRunWindowKey(window)
	for _, status := range []string{scheduledStatusDone, scheduledStatusFailed} {
		_, err := os.Stat(filepath.Join(dir, key+"."+status))
		if err == nil {
			return status, true, nil
		}
		if !os.IsNotExist(err) {
			return "", false, err
		}
	}
	return "", false, nil
}

func newScheduledLeaseID() (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d-%s", os.Getpid(), hex.EncodeToString(random[:])), nil
}

func scheduledRunStateStale(record scheduledWindowState, now time.Time) bool {
	last := record.HeartbeatAt
	if last.IsZero() {
		last = record.UpdatedAt
	}
	return last.IsZero() || now.Sub(last) > scheduledRunHeartbeatStale
}

func (app *app) updateScheduledRun(window scheduler.Window, leaseID string, update func(*scheduledWindowState, time.Time)) error {
	return app.mutateScheduledState(context.Background(), func(state *scheduledStateFile, now time.Time) (bool, error) {
		key := scheduledRunWindowKey(window)
		record, ok := state.Windows[key]
		if !ok || record.Status != scheduledStatusRunning || record.LeaseID != leaseID {
			return false, nil
		}
		update(&record, now)
		record.UpdatedAt = now
		state.Windows[key] = record
		return true, nil
	})
}

func (app *app) finishScheduledRun(window scheduler.Window, leaseID, status string, runErr error, fields map[string]string) error {
	return app.mutateScheduledState(context.Background(), func(state *scheduledStateFile, now time.Time) (bool, error) {
		key := scheduledRunWindowKey(window)
		record, ok := state.Windows[key]
		if !ok || record.Status != scheduledStatusRunning || record.LeaseID != leaseID {
			return false, nil
		}
		record.Status = status
		record.FinishedAt = now
		record.UpdatedAt = now
		record.HeartbeatAt = now
		record.LeaseID = ""
		record.Fields = cloneStringMap(fields)
		if runErr != nil {
			record.Error = runErr.Error()
		} else {
			record.Error = ""
		}
		state.Windows[key] = record
		return true, nil
	})
}

func (app *app) startScheduledRunHeartbeat(ctx context.Context, window scheduler.Window, leaseID string) func() {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(scheduledRunHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				if err := app.updateScheduledRun(window, leaseID, func(record *scheduledWindowState, now time.Time) {
					record.HeartbeatAt = now
				}); err != nil {
					logutil.Errorf("update scheduled heartbeat: %v", err)
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func (app *app) startScheduledWindowReconciler(ctx context.Context) {
	state, err := app.readScheduledState()
	if err != nil {
		logutil.Errorf("[scheduler] 恢复调度状态失败: %v", err)
		return
	}
	for key, record := range state.Windows {
		if !scheduledStatusActive(record.Status) {
			continue
		}
		window, err := scheduledWindowFromState(record)
		if err != nil || scheduledRunWindowKey(window) != key {
			logutil.Errorf("[scheduler] 忽略无效调度窗口 %s: %v", key, err)
			continue
		}
		app.startScheduledWindowWatcher(ctx, window)
	}
}

func (app *app) startScheduledWindowWatcher(parent context.Context, window scheduler.Window) {
	key := scheduledRunWindowKey(window)
	app.scheduledWindowMu.Lock()
	if app.scheduledWindowWatchers == nil {
		app.scheduledWindowWatchers = make(map[string]*scheduledWindowWatcher)
	}
	if _, exists := app.scheduledWindowWatchers[key]; exists {
		app.scheduledWindowMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	watcher := &scheduledWindowWatcher{cancel: cancel}
	app.scheduledWindowWatchers[key] = watcher
	app.scheduledWindowMu.Unlock()

	interval := config.DefaultXRefreshReconcileInterval
	if app.cfg != nil && app.cfg.XAccounts.RefreshReconcile > 0 {
		interval = app.cfg.XAccounts.RefreshReconcile
	}
	go func() {
		defer func() {
			cancel()
			app.scheduledWindowMu.Lock()
			if app.scheduledWindowWatchers[key] == watcher {
				delete(app.scheduledWindowWatchers, key)
			}
			app.scheduledWindowMu.Unlock()
		}()
		if !app.reconcileScheduledWindow(ctx, window) {
			return
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !app.reconcileScheduledWindow(ctx, window) {
					return
				}
			}
		}
	}()
}

func (app *app) stopScheduledWindowWatcher(window scheduler.Window) {
	key := scheduledRunWindowKey(window)
	app.scheduledWindowMu.Lock()
	watcher := app.scheduledWindowWatchers[key]
	app.scheduledWindowMu.Unlock()
	if watcher != nil {
		watcher.cancel()
	}
}

func (app *app) reconcileScheduledWindow(ctx context.Context, window scheduler.Window) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	state, err := app.readScheduledState()
	if err != nil {
		logutil.Errorf("[scheduler] 读取窗口 %s 状态失败: %v", window.Period, err)
		return true
	}
	record, ok := state.Windows[scheduledRunWindowKey(window)]
	if !ok || scheduledStatusTerminal(record.Status) {
		return false
	}
	now := app.currentTime()
	switch record.Status {
	case scheduledStatusPending:
		if !record.DueAt.IsZero() && now.Before(record.DueAt) {
			return true
		}
		if err := app.runScheduledBriefingOnceContext(ctx, window, "cron", true); err != nil {
			logutil.Errorf("[scheduler] pending window run failed: %v", err)
		}
	case scheduledStatusWaitingX:
		xState, stateErr := fetcher.ReadXVisibleRefreshWindowState(app.cfg.XAccounts, window.From, window.To, now)
		if stateErr == nil && xState.Running && !xState.Stale {
			return true
		}
		if stateErr != nil {
			logutil.Warnf("[scheduler] 窗口 %s 的 X 状态不可读，watcher 接管: %v", window.Period, stateErr)
		} else if xState.Running && xState.Stale {
			logutil.Warnf("[scheduler] 窗口 %s 的 X heartbeat 已过期，watcher 接管", window.Period)
		} else {
			logutil.Printf("[scheduler] 窗口 %s 的 X 已进入终态或不再匹配，watcher 接管", window.Period)
		}
		if err := app.runScheduledBriefingOnceContext(ctx, window, "x-wait-reconcile", true); err != nil {
			logutil.Errorf("[scheduler] X waiting reconcile failed: %v", err)
		}
	case scheduledStatusRunning:
		if !scheduledRunStateStale(record, now) {
			return true
		}
		logutil.Warnf("[scheduler] 窗口 %s 的简报 heartbeat 已过期，watcher 接管", window.Period)
		if err := app.runScheduledBriefingOnceContext(ctx, window, "stale-recovery", true); err != nil {
			logutil.Errorf("[scheduler] stale run recovery failed: %v", err)
		}
	default:
		logutil.Errorf("[scheduler] 窗口 %s 状态无效: %q", window.Period, record.Status)
		return false
	}

	state, err = app.readScheduledState()
	if err != nil {
		return true
	}
	record, ok = state.Windows[scheduledRunWindowKey(window)]
	return ok && scheduledStatusActive(record.Status)
}

func (app *app) scheduledWindowState(window scheduler.Window) (scheduledWindowState, bool, error) {
	state, err := app.readScheduledState()
	if err != nil {
		return scheduledWindowState{}, false, err
	}
	record, ok := state.Windows[scheduledRunWindowKey(window)]
	return record, ok, nil
}

func (app *app) runXReadyContext(ctx context.Context, cmd xReadyCommand) error {
	window, err := app.xReadyWindow(cmd)
	if err != nil {
		return err
	}
	loc := app.displayLocation()
	logutil.Printf("[scheduler] X ready callback: [%s -> %s]", window.From.In(loc).Format(time.RFC3339), window.To.In(loc).Format(time.RFC3339))
	return app.runScheduledBriefingOnceContext(ctx, window, "x-ready-callback", true)
}

func (app *app) xReadyWindow(cmd xReadyCommand) (scheduler.Window, error) {
	loc := app.displayLocation()
	from, err := parseXReadyTime(cmd.fromRaw, loc)
	if err != nil {
		return scheduler.Window{}, fmt.Errorf("parse --from: %w", err)
	}
	to, err := parseXReadyTime(cmd.toRaw, loc)
	if err != nil {
		return scheduler.Window{}, fmt.Errorf("parse --to: %w", err)
	}
	if !to.After(from) {
		return scheduler.Window{}, fmt.Errorf("--to must be after --from")
	}
	period := strings.TrimSpace(cmd.period)
	if period == "" {
		period = defaultPeriodFrom(to.In(loc))
	}
	if err := validatePeriod(period); err != nil {
		return scheduler.Window{}, err
	}
	return scheduler.Window{Expr: "x-ready-callback", Period: period, From: from, To: to}, nil
}

func parseXReadyTime(value string, loc *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	return parseRegenTime(value, loc)
}

func (app *app) runScheduledBriefingOnceContext(ctx context.Context, window scheduler.Window, trigger string, sendEmail bool) error {
	if strings.EqualFold(strings.TrimSpace(trigger), "cron") && app != nil && app.cfg != nil {
		xState, statusErr := fetcher.ReadXVisibleRefreshWindowState(app.cfg.XAccounts, window.From, window.To, app.currentTime())
		if statusErr != nil {
			logutil.Warnf("[scheduler] 检查窗口 %s 的 X refresh 状态失败，继续执行 cron 兜底: %v", window.Period, statusErr)
		} else if xState.Running && !xState.Stale {
			active, err := app.markScheduledRunWaitingX(window, trigger)
			if err != nil {
				return fmt.Errorf("mark scheduled run waiting for X: %w", err)
			}
			if active {
				app.startScheduledWindowWatcher(ctx, window)
			}
			logutil.Printf("[scheduler] 窗口 %s 等待 X refresh", window.Period)
			return nil
		} else if xState.Running && xState.Stale {
			logutil.Warnf("[scheduler] 窗口 %s 的 X refresh heartbeat 已过期，cron 接管降级执行", window.Period)
		}
	}

	leaseID, acquired, emailAlreadySent, err := app.acquireScheduledRunWindow(window, trigger)
	if err != nil {
		return err
	}
	if !acquired {
		return nil
	}
	reporter := &scheduledRunReporter{app: app, leaseID: leaseID, window: window}
	if emailAlreadySent && sendEmail {
		logutil.Printf("[scheduler] 窗口 %s 已记录邮件发送成功，接管执行时跳过重复发送", window.Period)
		sendEmail = false
	}
	stopHeartbeat := app.startScheduledRunHeartbeat(ctx, window, leaseID)
	err = app.runScheduledBriefingContextWithReporter(ctx, window, sendEmail, reporter)
	stopHeartbeat()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		if markErr := app.finishScheduledRun(window, leaseID, scheduledStatusFailed, err, reporter.stateFields()); markErr != nil {
			logutil.Errorf("mark scheduled run failed: %v", markErr)
		} else {
			app.stopScheduledWindowWatcher(window)
		}
		return err
	}
	if err := app.finishScheduledRun(window, leaseID, scheduledStatusDone, nil, reporter.stateFields()); err != nil {
		return err
	}
	app.stopScheduledWindowWatcher(window)
	return nil
}
