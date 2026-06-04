package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/walker1211/news-briefing/internal/config"
)

func TestRunPublishHookDoesNotLeakCommandOutput(t *testing.T) {
	if os.Getenv("NEWS_BRIEFING_PUBLISH_HOOK_LEAK_SUBPROCESS") == "1" {
		fmt.Fprintln(os.Stdout, "HOOK_STDOUT_MARKER=abc123")
		fmt.Fprintln(os.Stderr, "HOOK_STDERR_MARKER=abc123")
		os.Exit(1)
	}

	t.Setenv("NEWS_BRIEFING_PUBLISH_HOOK_LEAK_SUBPROCESS", "1")
	err := runPublishHook(context.Background(), config.PublishHookConfig{
		Enabled: true,
		Command: os.Args[0],
		Args:    []string{"-test.run=TestRunPublishHookDoesNotLeakCommandOutput"},
	}, publishHookRequest{})
	if err == nil {
		t.Fatal("runPublishHook() error = nil, want error")
	}
	message := err.Error()
	if strings.Contains(message, "SECRET_TOKEN") || strings.Contains(message, "abc123") {
		t.Fatalf("runPublishHook() error leaked command output: %q", message)
	}
}
