package logutil

import (
	"testing"
	"time"
)

func TestStamp(t *testing.T) {
	now := time.Date(2026, 4, 24, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	got := Stamp(now, "Starting news aggregator in scheduled mode...")
	want := "[2026-04-24T09:30:00+08:00] Starting news aggregator in scheduled mode..."
	if got != want {
		t.Fatalf("Stamp() = %q, want %q", got, want)
	}
}
