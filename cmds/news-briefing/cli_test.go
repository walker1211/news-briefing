package main

import (
	"strings"
	"testing"
)

func TestParseArgsRun(t *testing.T) {
	cmd, err := parseArgs([]string{"run", "--raw", "--no-email"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	run, ok := cmd.(runCommand)
	if !ok {
		t.Fatalf("command type = %T", cmd)
	}
	if !run.raw || !run.noEmail {
		t.Fatalf("run command = %#v", run)
	}
}

func TestParseArgsRegen(t *testing.T) {
	cmd, err := parseArgs([]string{"regen", "--from", "2026-03-18 08:00", "--to", "2026-03-18 14:00", "--period", "1400", "--ignore-seen", "--send-email", "--raw"})
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
	if regen.period != "1400" || !regen.ignoreSeen || !regen.sendEmail || !regen.raw {
		t.Fatalf("regen command = %#v", regen)
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

func TestParseArgsServe(t *testing.T) {
	cmd, err := parseArgs([]string{"serve"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if _, ok := cmd.(serveCommand); !ok {
		t.Fatalf("command type = %T", cmd)
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
