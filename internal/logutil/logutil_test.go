package logutil

import (
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

var outputCaptureMu sync.Mutex

func TestStamp(t *testing.T) {
	now := time.Date(2026, 4, 24, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	got := Stamp(now, "Starting news aggregator in scheduled mode...")
	want := "[2026-04-24T09:30:00+08:00] Starting news aggregator in scheduled mode..."
	if got != want {
		t.Fatalf("Stamp() = %q, want %q", got, want)
	}
}

func TestPrintlnWritesTimestampedMessageToStdout(t *testing.T) {
	got := captureOutput(t, &os.Stdout, func() {
		Println("scheduler started")
	})

	assertLogLine(t, got, "scheduler started")
}

func TestPrintfFormatsAndWritesTimestampedMessageToStdout(t *testing.T) {
	got := captureOutput(t, &os.Stdout, func() {
		Printf("runs = %d", 2)
	})

	assertLogLine(t, got, "runs = 2")
}

func TestWarnfFormatsAndWritesTimestampedMessageToStderr(t *testing.T) {
	got := captureOutput(t, &os.Stderr, func() {
		Warnf("retry succeeded: %d", 2)
	})

	assertLogLine(t, got, "retry succeeded: 2")
}

func TestErrorfFormatsAndWritesTimestampedMessageToStderr(t *testing.T) {
	got := captureOutput(t, &os.Stderr, func() {
		Errorf("send email: %s", "timeout")
	})

	assertLogLine(t, got, "send email: timeout")
}

func captureOutput(t *testing.T, target **os.File, write func()) string {
	t.Helper()

	outputCaptureMu.Lock()
	defer outputCaptureMu.Unlock()

	original := *target
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	defer reader.Close()
	*target = writer
	defer func() { *target = original }()

	write()
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	return string(data)
}

func assertLogLine(t *testing.T, got string, wantMessage string) {
	t.Helper()

	line := strings.TrimSpace(got)
	pattern := regexp.MustCompile(`^\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}.*\] ` + regexp.QuoteMeta(wantMessage) + `$`)
	if !pattern.MatchString(line) {
		t.Fatalf("log line = %q, want timestamped message %q", got, wantMessage)
	}
}
