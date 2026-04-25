package main

import (
	"context"
	"errors"
	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/scheduler"
	"strings"
	"testing"
	"time"
)

func TestExecuteResendMDSendsMarkdownFile(t *testing.T) {
	t.Setenv("EMAIL_SMTP_AUTH_CODE", "test")
	called := false
	var gotPath string
	app := &app{
		cfg: executeTestConfigWithEmail(t, model.OutputModeOriginalOnly),
		output: outputDeps{
			printText: func(string) {},
		},
		email: emailDeps{
			resendMarkdownEmail: func(path string, cfg *config.Config) error {
				called = true
				gotPath = path
				return nil
			},
		},
	}

	if err := execute(app, resendMDCommand{file: "output/26.04.13-晚间-1800.md"}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if !called {
		t.Fatal("resendMarkdownEmail() was not called")
	}
	if gotPath != "output/26.04.13-晚间-1800.md" {
		t.Fatalf("resendMarkdownEmail() path = %q", gotPath)
	}
}

func TestExecuteResendMDReturnsSendError(t *testing.T) {
	t.Setenv("EMAIL_SMTP_AUTH_CODE", "test")
	app := &app{
		cfg: executeTestConfigWithEmail(t, model.OutputModeOriginalOnly),
		output: outputDeps{
			printText: func(string) {},
		},
		email: emailDeps{
			resendMarkdownEmail: func(path string, cfg *config.Config) error {
				return errors.New("smtp down")
			},
		},
	}

	err := execute(app, resendMDCommand{file: "output/26.04.13-晚间-1800.md"})
	if err == nil || !strings.Contains(err.Error(), "smtp down") {
		t.Fatalf("execute() error = %v, want smtp down", err)
	}
}

func TestExecuteResendMDPrintsSuccessMessage(t *testing.T) {
	t.Setenv("EMAIL_SMTP_AUTH_CODE", "test")
	var printed []string
	app := &app{
		cfg: executeTestConfigWithEmail(t, model.OutputModeOriginalOnly),
		output: outputDeps{
			printText: func(s string) { printed = append(printed, s) },
		},
		email: emailDeps{
			resendMarkdownEmail: func(path string, cfg *config.Config) error {
				return nil
			},
		},
	}

	if err := execute(app, resendMDCommand{file: "output/26.04.13-晚间-1800.md"}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	joined := strings.Join(printed, "\n")
	if !strings.Contains(joined, "Email resent to to@example.com") {
		t.Fatalf("printed = %q", joined)
	}
}

func TestExecuteEmailPreflightBlocksDownstreamWork(t *testing.T) {
	tests := []struct {
		name        string
		cmd         command
		buildApp    func(t *testing.T, called *bool) *app
		calledLabel string
	}{
		{
			name: "run",
			cmd:  runCommand{},
			buildApp: func(t *testing.T, called *bool) *app {
				return &app{
					cfg: executeTestConfig(t, model.OutputModeOriginalOnly),
					fetch: fetchDeps{
						fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
							*called = true
							return nil, nil, nil
						},
					},
				}
			},
			calledLabel: "fetchAll",
		},
		{
			name: "serve",
			cmd:  serveCommand{},
			buildApp: func(t *testing.T, called *bool) *app {
				return &app{
					cfg: executeTestConfig(t, model.OutputModeOriginalOnly),
					scheduler: schedulerDeps{
						startCronContext: func(ctx context.Context, cfg *config.Config, run func(scheduler.Window)) error {
							*called = true
							return nil
						},
					},
				}
			},
			calledLabel: "scheduler",
		},
		{
			name: "regen send email",
			cmd:  regenCommand{fromRaw: "2026-04-15 07:00", toRaw: "2026-04-15 16:00", sendEmail: true},
			buildApp: func(t *testing.T, called *bool) *app {
				return &app{
					cfg: executeTestConfig(t, model.OutputModeOriginalOnly),
					fetch: fetchDeps{
						fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
							*called = true
							return nil, nil, nil
						},
					},
				}
			},
			calledLabel: "fetchWindow",
		},
		{
			name: "deep send email",
			cmd:  deepCommand{topic: "Claude", sendEmail: true},
			buildApp: func(t *testing.T, called *bool) *app {
				return &app{
					cfg: executeTestConfig(t, model.OutputModeOriginalOnly),
					fetch: fetchDeps{
						fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
							*called = true
							return nil, nil, nil
						},
					},
				}
			},
			calledLabel: "fetchAll",
		},
		{
			name: "resend md",
			cmd:  resendMDCommand{file: "output/26.04.13-晚间-1800.md"},
			buildApp: func(t *testing.T, called *bool) *app {
				return &app{
					cfg: executeTestConfig(t, model.OutputModeOriginalOnly),
					email: emailDeps{
						resendMarkdownEmail: func(string, *config.Config) error {
							*called = true
							return nil
						},
					},
				}
			},
			calledLabel: "resendMarkdownEmail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			err := execute(tt.buildApp(t, &called), tt.cmd)
			if err == nil || !strings.Contains(err.Error(), "validate email.smtp_host") {
				t.Fatalf("execute() error = %v, want email preflight error", err)
			}
			if called {
				t.Fatalf("%s should not run after failed email preflight", tt.calledLabel)
			}
		})
	}
}

func TestExecuteEmailPreflightSkippedWhenCommandDoesNotSendEmail(t *testing.T) {
	tests := []struct {
		name        string
		cmd         command
		buildApp    func(t *testing.T, called *bool) *app
		calledLabel string
	}{
		{
			name: "run no email",
			cmd:  runCommand{noEmail: true},
			buildApp: func(t *testing.T, called *bool) *app {
				return &app{
					cfg: executeTestConfig(t, model.OutputModeOriginalOnly),
					fetch: fetchDeps{
						fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
							*called = true
							return sampleExecuteArticles(), nil, nil
						},
					},
					output: silentBriefingOutputDeps("ORIGINAL ONLY"),
				}
			},
			calledLabel: "fetchAll",
		},
		{
			name: "regen without send email",
			cmd:  regenCommand{fromRaw: "2026-04-15 07:00", toRaw: "2026-04-15 16:00"},
			buildApp: func(t *testing.T, called *bool) *app {
				return &app{
					cfg: executeTestConfig(t, model.OutputModeOriginalOnly),
					fetch: fetchDeps{
						fetchWindowContext: func(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
							*called = true
							return sampleExecuteArticles(), nil, nil
						},
					},
					output: silentBriefingOutputDeps("ORIGINAL ONLY"),
				}
			},
			calledLabel: "fetchWindow",
		},
		{
			name: "deep without send email",
			cmd:  deepCommand{topic: "Claude"},
			buildApp: func(t *testing.T, called *bool) *app {
				return &app{
					cfg: executeTestConfig(t, model.OutputModeOriginalOnly),
					fetch: fetchDeps{
						fetchAllContext: func(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []fetcher.FailedSource, error) {
							*called = true
							return []model.Article{{Title: "Claude ships feature", Summary: "Claude update"}}, nil, nil
						},
					},
					output: silentDeepDiveOutputDeps("ORIGINAL ONLY"),
				}
			},
			calledLabel: "fetchAll",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			if err := execute(tt.buildApp(t, &called), tt.cmd); err != nil {
				t.Fatalf("execute() error = %v", err)
			}
			if !called {
				t.Fatalf("%s was not called", tt.calledLabel)
			}
		})
	}
}

func TestExecuteEmailPreflightRequiresSMTPAuthCode(t *testing.T) {
	t.Setenv("EMAIL_SMTP_AUTH_CODE", "")
	app := &app{cfg: executeTestConfigWithEmail(t, model.OutputModeOriginalOnly)}

	err := execute(app, runCommand{})
	if err == nil || !strings.Contains(err.Error(), "EMAIL_SMTP_AUTH_CODE") {
		t.Fatalf("execute() error = %v, want missing SMTP auth code", err)
	}
}

func TestExecuteEmailPreflightRequiresSocks5WhenEmailProxyEnabled(t *testing.T) {
	t.Setenv("EMAIL_SMTP_AUTH_CODE", "test")
	cfg := executeTestConfigWithEmail(t, model.OutputModeOriginalOnly)
	cfg.Email.UseProxy = true
	app := &app{cfg: cfg}

	err := execute(app, runCommand{})
	if err == nil || !strings.Contains(err.Error(), "email.use_proxy requires proxy.socks5") {
		t.Fatalf("execute() error = %v, want proxy.socks5 preflight error", err)
	}
}
