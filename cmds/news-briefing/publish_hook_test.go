package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/walker1211/news-briefing/internal/config"
)

func TestRunPublishHookExpandsCommandHome(t *testing.T) {
	if os.Getenv("NEWS_BRIEFING_PUBLISH_HOOK_HOME_SUBPROCESS") == "1" {
		os.Exit(0)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	commandPath := filepath.Join(home, "hook-test-binary")
	if err := os.Symlink(os.Args[0], commandPath); err != nil {
		t.Skipf("create hook binary symlink: %v", err)
	}
	t.Setenv("NEWS_BRIEFING_PUBLISH_HOOK_HOME_SUBPROCESS", "1")
	err := runPublishHook(context.Background(), config.PublishHookConfig{
		Enabled: true,
		Command: "~/hook-test-binary",
		Args:    []string{"-test.run=TestRunPublishHookExpandsCommandHome"},
	}, publishHookRequest{})
	if err != nil {
		t.Fatalf("runPublishHook() error = %v", err)
	}
}

func TestExpandPublishHookArgsIncludesCardManifestPlaceholders(t *testing.T) {
	got := expandPublishHookArgs([]string{
		"--file", "{markdown_file}",
		"--xhs-card-manifest", "{xhs_card_manifest}",
		"--legacy-card-manifest", "{card_manifest}",
		"--source", "{source_app}",
		"--date", "{date}",
		"--period", "{period}",
	}, publishHookRequest{
		MarkdownFile:     "/tmp/output/26.06.16-晚间-1800.md",
		CardManifestFile: "/tmp/output/26.06.16-晚间-1800.card-manifest.json",
		SourceApp:        "news-briefing",
		Date:             "26.06.16",
		Period:           "1800",
	})
	want := strings.Join([]string{
		"--file", "/tmp/output/26.06.16-晚间-1800.md",
		"--xhs-card-manifest", "/tmp/output/26.06.16-晚间-1800.card-manifest.json",
		"--legacy-card-manifest", "/tmp/output/26.06.16-晚间-1800.card-manifest.json",
		"--source", "news-briefing",
		"--date", "26.06.16",
		"--period", "1800",
	}, "\x00")
	if strings.Join(got, "\x00") != want {
		t.Fatalf("expandPublishHookArgs() = %#v", got)
	}
}

func TestExpandPublishHookArgsLeavesUnknownPlaceholdersStable(t *testing.T) {
	got := expandPublishHookArgs([]string{"--file", "{markdown_file}", "--unknown", "{unknown}"}, publishHookRequest{MarkdownFile: "briefing.md"})
	want := []string{"--file", "briefing.md", "--unknown", "{unknown}"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("expandPublishHookArgs() = %#v, want %#v", got, want)
	}
}

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

func TestRunPublishHookReportsExitCodeWithoutLeakingOutput(t *testing.T) {
	if os.Getenv("NEWS_BRIEFING_PUBLISH_HOOK_EXIT_CODE_SUBPROCESS") == "1" {
		fmt.Fprintln(os.Stdout, "HOOK_STDOUT_MARKER=exit-code")
		fmt.Fprintln(os.Stderr, "HOOK_STDERR_MARKER=exit-code")
		os.Exit(7)
	}

	t.Setenv("NEWS_BRIEFING_PUBLISH_HOOK_EXIT_CODE_SUBPROCESS", "1")
	err := runPublishHook(context.Background(), config.PublishHookConfig{
		Enabled: true,
		Command: os.Args[0],
		Args:    []string{"-test.run=TestRunPublishHookReportsExitCodeWithoutLeakingOutput"},
	}, publishHookRequest{})
	if err == nil {
		t.Fatal("runPublishHook() error = nil, want error")
	}
	message := err.Error()
	if !strings.Contains(message, "exit code 7") {
		t.Fatalf("runPublishHook() error = %q, want exit code", message)
	}
	if strings.Contains(message, "HOOK_STDOUT_MARKER") || strings.Contains(message, "HOOK_STDERR_MARKER") || strings.Contains(message, "exit-code") {
		t.Fatalf("runPublishHook() error leaked command output: %q", message)
	}
}

func TestRunPublishHookReportsUnavailableCommandWithoutLeakingPath(t *testing.T) {
	commandPath := filepath.Join(t.TempDir(), "missing-hook")
	err := runPublishHook(context.Background(), config.PublishHookConfig{
		Enabled: true,
		Command: commandPath,
	}, publishHookRequest{})
	if err == nil {
		t.Fatal("runPublishHook() error = nil, want error")
	}
	message := err.Error()
	if !strings.Contains(message, "command unavailable") {
		t.Fatalf("runPublishHook() error = %q, want command unavailable", message)
	}
	if strings.Contains(message, commandPath) {
		t.Fatalf("runPublishHook() error leaked command path: %q", message)
	}
}

func TestRunPublishHookReportsContextCancellationWithoutLeakingPath(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runPublishHook(ctx, config.PublishHookConfig{
		Enabled: true,
		Command: os.Args[0],
	}, publishHookRequest{})
	if err == nil {
		t.Fatal("runPublishHook() error = nil, want error")
	}
	message := err.Error()
	if !strings.Contains(message, context.Canceled.Error()) {
		t.Fatalf("runPublishHook() error = %q, want context cancellation", message)
	}
	if strings.Contains(message, os.Args[0]) {
		t.Fatalf("runPublishHook() error leaked command path: %q", message)
	}
}
