package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
)

func TestBuildWindowDerivesSameDayPreviousPoint(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	to := time.Date(2026, 3, 27, 18, 0, 0, 0, loc)

	got, err := buildWindow(to, "0 18 * * *", []string{"0 7 * * *", "0 18 * * *"}, loc)
	if err != nil {
		t.Fatalf("buildWindow() error = %v", err)
	}

	wantFrom := time.Date(2026, 3, 27, 7, 0, 0, 0, loc)
	assertWindow(t, got, "0 18 * * *", "1800", wantFrom, to)
}

func TestBuildWindowDerivesPreviousDayPoint(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	to := time.Date(2026, 3, 27, 7, 0, 0, 0, loc)

	got, err := buildWindow(to, "0 7 * * *", []string{"0 7 * * *", "0 18 * * *"}, loc)
	if err != nil {
		t.Fatalf("buildWindow() error = %v", err)
	}

	wantFrom := time.Date(2026, 3, 26, 18, 0, 0, 0, loc)
	assertWindow(t, got, "0 7 * * *", "0700", wantFrom, to)
}

func TestBuildWindowUsesPreviousOccurrenceForSingleSchedule(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	to := time.Date(2026, 3, 27, 7, 0, 0, 0, loc)

	got, err := buildWindow(to, "0 7 * * *", []string{"0 7 * * *"}, loc)
	if err != nil {
		t.Fatalf("buildWindow() error = %v", err)
	}

	wantFrom := time.Date(2026, 3, 26, 7, 0, 0, 0, loc)
	assertWindow(t, got, "0 7 * * *", "0700", wantFrom, to)
}

func TestStartReturnsErrorForInvalidCronExpression(t *testing.T) {
	cfg := &config.Config{Schedule: []string{"bad expr"}}

	err := Start(cfg, func(Window) {})
	if err == nil {
		t.Fatal("Start() error = nil, want invalid cron expression error")
	}
	if !strings.Contains(err.Error(), `添加定时任务 "bad expr" 失败`) {
		t.Fatalf("Start() error = %q, want invalid cron expression context", err)
	}
}

func TestStartContextReturnsContextErrorWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := StartContext(ctx, &config.Config{Schedule: []string{"0 7 * * *"}}, func(Window) {})
	if err == nil {
		t.Fatal("StartContext() error = nil, want context error")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("StartContext() error = %q, want context canceled", err.Error())
	}
}

func TestBuildWindowFormatsPeriodInScheduleTimezone(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	to := time.Date(2026, 3, 27, 23, 0, 0, 0, time.UTC)

	got, err := buildWindow(to, "0 16 * * *", []string{"0 16 * * *"}, loc)
	if err != nil {
		t.Fatalf("buildWindow() error = %v", err)
	}

	wantTo := time.Date(2026, 3, 27, 16, 0, 0, 0, loc)
	wantFrom := time.Date(2026, 3, 26, 16, 0, 0, 0, loc)
	assertWindow(t, got, "0 16 * * *", "1600", wantFrom, wantTo)
}

func TestStartRejectsEmptySchedule(t *testing.T) {
	cfg := &config.Config{Schedule: nil}

	err := Start(cfg, func(Window) {})
	if err == nil {
		t.Fatal("Start() error = nil, want empty schedule error")
	}
	if !strings.Contains(err.Error(), "schedule 不能为空") {
		t.Fatalf("Start() error = %q, want empty schedule context", err)
	}
}

func TestBuildWindowAcceptsDuplicateScheduleExpressions(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	to := time.Date(2026, 3, 27, 18, 0, 0, 0, loc)

	got, err := buildWindow(to, "0 18 * * *", []string{"0 7 * * *", "0 18 * * *", "0 18 * * *"}, loc)
	if err != nil {
		t.Fatalf("buildWindow() error = %v", err)
	}

	wantFrom := time.Date(2026, 3, 27, 7, 0, 0, 0, loc)
	assertWindow(t, got, "0 18 * * *", "1800", wantFrom, to)
}

func TestBuildWindowUsesProvidedTriggerTimeInsteadOfGuessingNow(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	to := time.Date(2026, 3, 27, 18, 0, 0, 0, loc)

	got, err := buildWindow(to, "0 18 * * *", []string{"0 7 * * *", "0 18 * * *"}, loc)
	if err != nil {
		t.Fatalf("buildWindow() error = %v", err)
	}
	if !got.To.Equal(to) {
		t.Fatalf("buildWindow().To = %v, want %v", got.To, to)
	}
	if got.Period != "1800" {
		t.Fatalf("buildWindow().Period = %q, want %q", got.Period, "1800")
	}
}

func TestBuildWindowDerivesLatestPointForHighFrequencySchedule(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	to := time.Date(2026, 3, 27, 18, 0, 0, 0, loc)

	got, err := buildWindow(to, "*/15 * * * *", []string{"*/15 * * * *"}, loc)
	if err != nil {
		t.Fatalf("buildWindow() error = %v", err)
	}

	wantFrom := time.Date(2026, 3, 27, 17, 45, 0, 0, loc)
	assertWindow(t, got, "*/15 * * * *", "1800", wantFrom, to)
}

func assertWindow(t *testing.T, got Window, wantExpr, wantPeriod string, wantFrom, wantTo time.Time) {
	t.Helper()
	if got.Expr != wantExpr {
		t.Fatalf("window.Expr = %q, want %q", got.Expr, wantExpr)
	}
	if got.Period != wantPeriod {
		t.Fatalf("window.Period = %q, want %q", got.Period, wantPeriod)
	}
	if !got.From.Equal(wantFrom) {
		t.Fatalf("window.From = %v, want %v", got.From, wantFrom)
	}
	if !got.To.Equal(wantTo) {
		t.Fatalf("window.To = %v, want %v", got.To, wantTo)
	}
}
