package main

import (
	"strings"
	"testing"
)

func TestParseArgsRun(t *testing.T) {
	cmd, err := parseArgs([]string{"run", "--raw", "--no-email", "--no-publish"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	run, ok := cmd.(runCommand)
	if !ok {
		t.Fatalf("command type = %T", cmd)
	}
	if !run.raw || !run.noEmail || !run.noPublish {
		t.Fatalf("run command = %#v", run)
	}
}

func TestParseArgsRegen(t *testing.T) {
	cmd, err := parseArgs([]string{"regen", "--from", "2026-03-18 08:00", "--to", "2026-03-18 14:00", "--period", "1400", "--ignore-seen", "--send-email", "--raw", "--no-publish", "--x-visible-history-days", "2", "--x-visible-history-dir", "/tmp/x-visible/history"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	regen, ok := cmd.(regenCommand)
	if !ok {
		t.Fatalf("command type = %T", cmd)
	}
	if regen.fromRaw != "2026-03-18 08:00" || regen.toRaw != "2026-03-18 14:00" {
		t.Fatalf("regen raw window = %#v", regen)
	}
	if regen.period != "1400" || !regen.ignoreSeen || !regen.sendEmail || !regen.raw || !regen.noPublish {
		t.Fatalf("regen command = %#v", regen)
	}
	if regen.xVisibleHistoryDays != 2 || regen.xVisibleHistoryDir != "/tmp/x-visible/history" {
		t.Fatalf("regen history options = %#v", regen)
	}
}

func TestParseArgsFetch(t *testing.T) {
	cmd, err := parseArgs([]string{"fetch", "--zh"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	fetch, ok := cmd.(fetchCommand)
	if !ok {
		t.Fatalf("command type = %T", cmd)
	}
	if !fetch.zh {
		t.Fatalf("fetch command = %#v", fetch)
	}
}

func TestParseArgsAlerts(t *testing.T) {
	cmd, err := parseArgs([]string{"alerts"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if _, ok := cmd.(alertsCommand); !ok {
		t.Fatalf("command type = %T", cmd)
	}
}

func TestParseArgsXRoutes(t *testing.T) {
	cmd, err := parseArgs([]string{"x", "routes"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if _, ok := cmd.(xRoutesCommand); !ok {
		t.Fatalf("command type = %T", cmd)
	}
}

func TestParseArgsXReady(t *testing.T) {
	cmd, err := parseArgs([]string{"x", "ready", "--from", "2026-06-16T08:00:00+08:00", "--to", "2026-06-16T18:00:00+08:00", "--period", "1800", "--no-publish"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	ready, ok := cmd.(xReadyCommand)
	if !ok {
		t.Fatalf("command type = %T", cmd)
	}
	if ready.fromRaw != "2026-06-16T08:00:00+08:00" || ready.toRaw != "2026-06-16T18:00:00+08:00" || ready.period != "1800" || !ready.noPublish {
		t.Fatalf("x ready command = %#v", ready)
	}
}

func TestParseArgsXReadyRejects(t *testing.T) {
	t.Run("missing from", func(t *testing.T) {
		_, err := parseArgs([]string{"x", "ready", "--to", "2026-06-16 18:00"})
		if err == nil || !strings.Contains(err.Error(), "--from") {
			t.Fatalf("parseArgs() error = %v, want missing --from", err)
		}
	})

	t.Run("missing to", func(t *testing.T) {
		_, err := parseArgs([]string{"x", "ready", "--from", "2026-06-16 08:00"})
		if err == nil || !strings.Contains(err.Error(), "--to") {
			t.Fatalf("parseArgs() error = %v, want missing --to", err)
		}
	})

	t.Run("invalid period", func(t *testing.T) {
		_, err := parseArgs([]string{"x", "ready", "--from", "2026-06-16 08:00", "--to", "2026-06-16 18:00", "--period", "2460"})
		if err == nil || !strings.Contains(err.Error(), "HHMM") {
			t.Fatalf("parseArgs() error = %v, want invalid period", err)
		}
	})

	t.Run("unknown flag", func(t *testing.T) {
		_, err := parseArgs([]string{"x", "ready", "--from", "2026-06-16 08:00", "--to", "2026-06-16 18:00", "--bad"})
		if err == nil || !strings.Contains(err.Error(), "unknown flag for x ready") {
			t.Fatalf("parseArgs() error = %v, want unknown flag", err)
		}
	})
}

func TestParseArgsXRoutesRejects(t *testing.T) {
	t.Run("missing subcommand", func(t *testing.T) {
		_, err := parseArgs([]string{"x"})
		if err == nil || !strings.Contains(err.Error(), "x subcommand") {
			t.Fatalf("parseArgs() error = %v, want missing x subcommand", err)
		}
	})

	t.Run("unsupported subcommand", func(t *testing.T) {
		_, err := parseArgs([]string{"x", "foo"})
		if err == nil || !strings.Contains(err.Error(), "unsupported x subcommand") {
			t.Fatalf("parseArgs() error = %v, want unsupported x subcommand", err)
		}
	})

	t.Run("unexpected route args", func(t *testing.T) {
		_, err := parseArgs([]string{"x", "routes", "--bad"})
		if err == nil || !strings.Contains(err.Error(), "unexpected arguments for x routes") {
			t.Fatalf("parseArgs() error = %v, want unexpected arguments for x routes", err)
		}
	})
}

func TestParseArgsServe(t *testing.T) {
	cmd, err := parseArgs([]string{"serve", "--no-publish"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	serve, ok := cmd.(serveCommand)
	if !ok {
		t.Fatalf("command type = %T", cmd)
	}
	if !serve.noPublish {
		t.Fatalf("serve command = %#v", serve)
	}
}

func TestParseArgsDeepRequiresTopic(t *testing.T) {
	_, err := parseArgs([]string{"deep"})
	if err == nil || !strings.Contains(err.Error(), "topic") {
		t.Fatalf("parseArgs() error = %v, want missing topic", err)
	}
}

func TestParseArgsRegenRequiresFrom(t *testing.T) {
	_, err := parseArgs([]string{"regen", "--to", "2026-03-18 14:00"})
	if err == nil || !strings.Contains(err.Error(), "--from") {
		t.Fatalf("parseArgs() error = %v, want missing --from", err)
	}
}

func TestParseArgsRegenRequiresTo(t *testing.T) {
	_, err := parseArgs([]string{"regen", "--from", "2026-03-18 08:00"})
	if err == nil || !strings.Contains(err.Error(), "--to") {
		t.Fatalf("parseArgs() error = %v, want missing --to", err)
	}
}

func TestParseArgsRegenRejectsInvalidXVisibleHistoryDays(t *testing.T) {
	_, err := parseArgs([]string{"regen", "--from", "2026-03-18 08:00", "--to", "2026-03-18 14:00", "--x-visible-history-days", "0"})
	if err == nil || !strings.Contains(err.Error(), "--x-visible-history-days") {
		t.Fatalf("parseArgs() error = %v, want invalid --x-visible-history-days", err)
	}
}

func TestParseArgsRegenDefersToBeforeFromValidation(t *testing.T) {
	cmd, err := parseArgs([]string{"regen", "--from", "2026-03-18 14:00", "--to", "2026-03-18 08:00"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	regen, ok := cmd.(regenCommand)
	if !ok {
		t.Fatalf("command type = %T", cmd)
	}
	if regen.fromRaw != "2026-03-18 14:00" || regen.toRaw != "2026-03-18 08:00" {
		t.Fatalf("regen raw window = %#v", regen)
	}
}

func TestParseArgsRunRejects(t *testing.T) {
	t.Run("unexpected args", func(t *testing.T) {
		_, err := parseArgs([]string{"run", "foo"})
		if err == nil || !strings.Contains(err.Error(), "unexpected arguments for run") {
			t.Fatalf("parseArgs() error = %v, want unexpected arguments for run", err)
		}
	})

	t.Run("unknown flag", func(t *testing.T) {
		_, err := parseArgs([]string{"run", "--bad"})
		if err == nil || !strings.Contains(err.Error(), "unknown flag for run: --bad") {
			t.Fatalf("parseArgs() error = %v, want unknown flag for run: --bad", err)
		}
	})
}

func TestParseArgsFetchRejects(t *testing.T) {
	t.Run("unexpected args", func(t *testing.T) {
		_, err := parseArgs([]string{"fetch", "foo"})
		if err == nil || !strings.Contains(err.Error(), "unexpected arguments for fetch") {
			t.Fatalf("parseArgs() error = %v, want unexpected arguments for fetch", err)
		}
	})

	t.Run("unknown flag", func(t *testing.T) {
		_, err := parseArgs([]string{"fetch", "--bad"})
		if err == nil || !strings.Contains(err.Error(), "unknown flag for fetch: --bad") {
			t.Fatalf("parseArgs() error = %v, want unknown flag for fetch: --bad", err)
		}
	})
}

func TestParseArgsAlertsRejects(t *testing.T) {
	t.Run("unexpected args", func(t *testing.T) {
		_, err := parseArgs([]string{"alerts", "foo"})
		if err == nil || !strings.Contains(err.Error(), "unexpected arguments for alerts") {
			t.Fatalf("parseArgs() error = %v, want unexpected arguments for alerts", err)
		}
	})

	t.Run("unknown flag", func(t *testing.T) {
		_, err := parseArgs([]string{"alerts", "--bad"})
		if err == nil || !strings.Contains(err.Error(), "unknown flag for alerts: --bad") {
			t.Fatalf("parseArgs() error = %v, want unknown flag for alerts: --bad", err)
		}
	})
}

func TestParseArgsServeRejects(t *testing.T) {
	t.Run("unexpected args", func(t *testing.T) {
		_, err := parseArgs([]string{"serve", "foo"})
		if err == nil || !strings.Contains(err.Error(), "unexpected arguments for serve") {
			t.Fatalf("parseArgs() error = %v, want unexpected arguments for serve", err)
		}
	})

	t.Run("unknown flag", func(t *testing.T) {
		_, err := parseArgs([]string{"serve", "--bad"})
		if err == nil || !strings.Contains(err.Error(), "unknown flag for serve: --bad") {
			t.Fatalf("parseArgs() error = %v, want unknown flag for serve: --bad", err)
		}
	})
}

func TestParseArgsHelpRejects(t *testing.T) {
	t.Run("unexpected args", func(t *testing.T) {
		_, err := parseArgs([]string{"help", "foo"})
		if err == nil || !strings.Contains(err.Error(), "unexpected arguments for help") {
			t.Fatalf("parseArgs() error = %v, want unexpected arguments for help", err)
		}
	})

	t.Run("unknown flag", func(t *testing.T) {
		_, err := parseArgs([]string{"help", "--bad"})
		if err == nil || !strings.Contains(err.Error(), "unknown flag for help: --bad") {
			t.Fatalf("parseArgs() error = %v, want unknown flag for help: --bad", err)
		}
	})

	t.Run("help aliases succeed", func(t *testing.T) {
		for _, args := range [][]string{{"-h"}, {"--help"}} {
			cmd, err := parseArgs(args)
			if err != nil {
				t.Fatalf("parseArgs(%v) error = %v", args, err)
			}
			if _, ok := cmd.(helpCommand); !ok {
				t.Fatalf("parseArgs(%v) command type = %T, want helpCommand", args, cmd)
			}
		}
	})

	t.Run("help alias trailing args normalize command name", func(t *testing.T) {
		_, err := parseArgs([]string{"-h", "foo"})
		if err == nil || !strings.Contains(err.Error(), "unexpected arguments for help") {
			t.Fatalf("parseArgs() error = %v, want unexpected arguments for help", err)
		}

		_, err = parseArgs([]string{"--help", "--bad"})
		if err == nil || !strings.Contains(err.Error(), "unknown flag for help: --bad") {
			t.Fatalf("parseArgs() error = %v, want unknown flag for help: --bad", err)
		}
	})
}

func TestParseArgsResendMD(t *testing.T) {
	cmd, err := parseArgs([]string{"resend-md", "--file", "output/26.04.13-晚间-1800.md"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	resend, ok := cmd.(resendMDCommand)
	if !ok {
		t.Fatalf("command type = %T", cmd)
	}
	if resend.file != "output/26.04.13-晚间-1800.md" {
		t.Fatalf("resend file = %q, want %q", resend.file, "output/26.04.13-晚间-1800.md")
	}
}

func TestParseArgsResendMDRejectsMissingFile(t *testing.T) {
	_, err := parseArgs([]string{"resend-md"})
	if err == nil || !strings.Contains(err.Error(), "--file") {
		t.Fatalf("parseArgs() error = %v, want missing --file", err)
	}
}

func TestParseArgsResendMDRejectsUnknownFlag(t *testing.T) {
	_, err := parseArgs([]string{"resend-md", "--bad"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag for resend-md: --bad") {
		t.Fatalf("parseArgs() error = %v", err)
	}
}

func TestParseArgsDeep(t *testing.T) {
	t.Run("single-word topic", func(t *testing.T) {
		cmd, err := parseArgs([]string{"deep", "OpenAI"})
		if err != nil {
			t.Fatalf("parseArgs() error = %v", err)
		}
		deep, ok := cmd.(deepCommand)
		if !ok {
			t.Fatalf("command type = %T", cmd)
		}
		if deep.topic != "OpenAI" {
			t.Fatalf("deep command = %#v", deep)
		}
	})

	t.Run("unknown flag", func(t *testing.T) {
		_, err := parseArgs([]string{"deep", "--bad"})
		if err == nil || !strings.Contains(err.Error(), "unknown flag for deep: --bad") {
			t.Fatalf("parseArgs() error = %v, want unknown flag for deep: --bad", err)
		}
	})

	t.Run("trailing unknown flag", func(t *testing.T) {
		_, err := parseArgs([]string{"deep", "OpenAI", "--bad"})
		if err == nil || !strings.Contains(err.Error(), "unknown flag for deep: --bad") {
			t.Fatalf("parseArgs() error = %v, want unknown flag for deep: --bad", err)
		}
	})

	t.Run("keeps multi-word topic", func(t *testing.T) {
		cmd, err := parseArgs([]string{"deep", "OpenAI", "API"})
		if err != nil {
			t.Fatalf("parseArgs() error = %v", err)
		}
		deep, ok := cmd.(deepCommand)
		if !ok {
			t.Fatalf("command type = %T", cmd)
		}
		if deep.topic != "OpenAI API" {
			t.Fatalf("deep topic = %q, want %q", deep.topic, "OpenAI API")
		}
	})

	t.Run("parses send email flag", func(t *testing.T) {
		cmd, err := parseArgs([]string{"deep", "Claude", "--ignore-seen", "--send-email"})
		if err != nil {
			t.Fatalf("parseArgs() error = %v", err)
		}
		deep, ok := cmd.(deepCommand)
		if !ok {
			t.Fatalf("command type = %T", cmd)
		}
		if deep.topic != "Claude" || !deep.ignoreSeen || !deep.sendEmail {
			t.Fatalf("deep command = %#v", deep)
		}
	})

	t.Run("supports ignore-seen", func(t *testing.T) {
		cmd, err := parseArgs([]string{"deep", "claude", "--ignore-seen"})
		if err != nil {
			t.Fatalf("parseArgs() error = %v", err)
		}
		deep, ok := cmd.(deepCommand)
		if !ok {
			t.Fatalf("command type = %T", cmd)
		}
		if deep.topic != "claude" || !deep.ignoreSeen {
			t.Fatalf("deep command = %#v", deep)
		}
	})

	t.Run("supports explicit window", func(t *testing.T) {
		cmd, err := parseArgs([]string{"deep", "Claude", "--from", "2026-03-28 00:00", "--to", "2026-03-29 23:59"})
		if err != nil {
			t.Fatalf("parseArgs() error = %v", err)
		}
		deep, ok := cmd.(deepCommand)
		if !ok {
			t.Fatalf("command type = %T", cmd)
		}
		if deep.topic != "Claude" || deep.fromRaw != "2026-03-28 00:00" || deep.toRaw != "2026-03-29 23:59" {
			t.Fatalf("deep command = %#v", deep)
		}
	})

	t.Run("supports explicit window with ignore-seen", func(t *testing.T) {
		cmd, err := parseArgs([]string{"deep", "Claude", "API", "--from", "2026-03-28 00:00", "--to", "2026-03-29 23:59", "--ignore-seen"})
		if err != nil {
			t.Fatalf("parseArgs() error = %v", err)
		}
		deep, ok := cmd.(deepCommand)
		if !ok {
			t.Fatalf("command type = %T", cmd)
		}
		if deep.topic != "Claude API" || deep.fromRaw != "2026-03-28 00:00" || deep.toRaw != "2026-03-29 23:59" || !deep.ignoreSeen {
			t.Fatalf("deep command = %#v", deep)
		}
	})

	t.Run("requires paired from and to", func(t *testing.T) {
		for _, args := range [][]string{
			{"deep", "Claude", "--from", "2026-03-28 00:00"},
			{"deep", "Claude", "--to", "2026-03-29 23:59"},
		} {
			_, err := parseArgs(args)
			if err == nil || !strings.Contains(err.Error(), "--from and --to must be provided together") {
				t.Fatalf("parseArgs(%v) error = %v", args, err)
			}
		}
	})
}

func TestParseArgsRegenRejects(t *testing.T) {
	t.Run("unexpected args", func(t *testing.T) {
		_, err := parseArgs([]string{"regen", "--from", "2026-03-18 08:00", "--to", "2026-03-18 14:00", "foo"})
		if err == nil || !strings.Contains(err.Error(), "unexpected arguments for regen") {
			t.Fatalf("parseArgs() error = %v, want unexpected arguments for regen", err)
		}
	})

	t.Run("unknown flag", func(t *testing.T) {
		_, err := parseArgs([]string{"regen", "--from", "2026-03-18 08:00", "--to", "2026-03-18 14:00", "--bad"})
		if err == nil || !strings.Contains(err.Error(), "unknown flag for regen: --bad") {
			t.Fatalf("parseArgs() error = %v, want unknown flag for regen: --bad", err)
		}
	})
}

func TestParseArgsRegenValueLikeUnknownFlagDoesNotReturnUnknownFlag(t *testing.T) {
	t.Run("from value looks like flag", func(t *testing.T) {
		_, err := parseArgs([]string{"regen", "--from", "--bad", "--to", "2026-03-18 14:00"})
		if err == nil {
			t.Fatal("parseArgs() error = nil, want non-nil error")
		}
		if strings.Contains(err.Error(), "unknown flag") {
			t.Fatalf("parseArgs() error = %v, should not contain unknown flag", err)
		}
	})

	t.Run("period value looks like flag", func(t *testing.T) {
		_, err := parseArgs([]string{"regen", "--period", "--bad", "--from", "2026-03-18 08:00", "--to", "2026-03-18 14:00"})
		if err == nil {
			t.Fatal("parseArgs() error = nil, want non-nil error")
		}
		if strings.Contains(err.Error(), "unknown flag") {
			t.Fatalf("parseArgs() error = %v, should not contain unknown flag", err)
		}
	})
}

func TestParseArgsRejectsSingleDashUnknownFlag(t *testing.T) {
	t.Run("run rejects -x", func(t *testing.T) {
		_, err := parseArgs([]string{"run", "-x"})
		if err == nil || !strings.Contains(err.Error(), "unknown flag for run: -x") {
			t.Fatalf("parseArgs() error = %v, want unknown flag for run: -x", err)
		}
	})

	t.Run("deep rejects -x", func(t *testing.T) {
		_, err := parseArgs([]string{"deep", "-x"})
		if err == nil || !strings.Contains(err.Error(), "unknown flag for deep: -x") {
			t.Fatalf("parseArgs() error = %v, want unknown flag for deep: -x", err)
		}
	})
}

func TestParseArgsRejectsUnsupportedDoubleDashSentinel(t *testing.T) {
	_, err := parseArgs([]string{"deep", "--", "OpenAI"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag for deep: --") {
		t.Fatalf("parseArgs() error = %v, want unknown flag for deep: --", err)
	}
}
