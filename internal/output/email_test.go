package output

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
)

func TestBuildEmailBodyPreservesBodyOrderAndSingleTitle(t *testing.T) {
	briefing := &model.Briefing{
		Date:       "26.03.27",
		Period:     "1400",
		RawContent: "TRANSLATED\n\n---\n\nORIGINAL",
	}

	got := buildEmailBody(briefing, nil)
	title := "国际资讯简报 26.03.27 午间 14:00"
	if strings.Count(got, title) != 1 {
		t.Fatalf("buildEmailBody() title count = %d, want 1 in %q", strings.Count(got, title), got)
	}
	if !strings.Contains(got, "TRANSLATED\n\n---\n\nORIGINAL") {
		t.Fatalf("buildEmailBody() body = %q", got)
	}
}

func TestBuildEmailBodyOmitsFailedSectionWhenNoFailures(t *testing.T) {
	briefing := &model.Briefing{
		Date:       "26.03.18",
		Period:     "1400",
		RawContent: "## AI/科技\n\n正文",
	}

	got := buildEmailBody(briefing, nil)
	if strings.Contains(got, "抓取异常") {
		t.Fatalf("buildEmailBody() = %q, want no failure section", got)
	}
}

func TestBuildEmailBodyAppendsFailedSection(t *testing.T) {
	briefing := &model.Briefing{
		Date:       "26.03.18",
		Period:     "1400",
		RawContent: "## AI/科技\n\n正文",
	}
	failed := []fetcher.FailedSource{{
		Name: "Reddit Singularity",
		Err:  errors.New("http error: 403 Forbidden"),
	}}

	got := buildEmailBody(briefing, failed)
	wantParts := []string{
		"国际资讯简报 26.03.18 午间 14:00",
		"## AI/科技",
		"---\n抓取异常",
		"- Reddit Singularity: http error: 403 Forbidden",
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Fatalf("buildEmailBody() = %q, want substring %q", got, want)
		}
	}
}

func TestBuildDeepEmailBodyUsesDeepTitle(t *testing.T) {
	briefing := &model.Briefing{RawContent: "正文"}
	got := buildDeepEmailBody("Claude", briefing, nil)
	if !strings.Contains(got, "国际资讯话题深挖 | Claude") {
		t.Fatalf("buildDeepEmailBody() = %q", got)
	}
}

func TestBuildDeepEmailBodyAppendsFailedSection(t *testing.T) {
	briefing := &model.Briefing{RawContent: "正文"}
	failed := []fetcher.FailedSource{{
		Name: "HN",
		Err:  errors.New("timeout"),
	}}

	got := buildDeepEmailBody("Claude", briefing, failed)
	wantParts := []string{
		"国际资讯话题深挖 | Claude",
		"正文",
		"---\n抓取异常",
		"- HN: timeout",
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Fatalf("buildDeepEmailBody() = %q, want substring %q", got, want)
		}
	}
}

func TestEmailSenderRejectsNilInputs(t *testing.T) {
	sender := NewEmailSender()
	cfg := &config.Config{}
	briefing := &model.Briefing{}
	tests := []struct {
		name    string
		send    func() error
		wantErr string
	}{
		{
			name:    "send email nil config",
			send:    func() error { return sender.SendEmail(briefing, nil, nil) },
			wantErr: "config is nil",
		},
		{
			name:    "send email nil briefing",
			send:    func() error { return sender.SendEmail(nil, cfg, nil) },
			wantErr: "briefing is nil",
		},
		{
			name:    "send deep email nil config",
			send:    func() error { return sender.SendDeepEmail("Claude", briefing, nil, nil) },
			wantErr: "config is nil",
		},
		{
			name:    "send deep email nil briefing",
			send:    func() error { return sender.SendDeepEmail("Claude", nil, cfg, nil) },
			wantErr: "briefing is nil",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.send()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("send() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestBriefingSubjectFromMarkdownFilename(t *testing.T) {
	subject, err := briefingSubjectFromMarkdownFilename("output/26.04.13-晚间-1800.md")
	if err != nil {
		t.Fatalf("briefingSubjectFromMarkdownFilename() error = %v", err)
	}
	if subject != "国际资讯简报 26.04.13 晚间 18:00" {
		t.Fatalf("subject = %q", subject)
	}
}

func TestBriefingSubjectFromMarkdownFilenameRejectsInvalidName(t *testing.T) {
	_, err := briefingSubjectFromMarkdownFilename("output/bad-name.md")
	if err == nil || !strings.Contains(err.Error(), "markdown filename") {
		t.Fatalf("briefingSubjectFromMarkdownFilename() error = %v", err)
	}
}

func TestSendMarkdownFileRejectsFileOutsideOutputDir(t *testing.T) {
	cfg := &config.Config{Output: config.OutputCfg{Dir: "output"}}
	err := SendMarkdownFile("/tmp/26.04.13-晚间-1800.md", cfg)
	if err == nil || !strings.Contains(err.Error(), "outside output dir") {
		t.Fatalf("SendMarkdownFile() error = %v", err)
	}
}

func TestSendMarkdownFileRejectsSymlinkEscapingOutputDir(t *testing.T) {
	dir := t.TempDir()
	outsidePath := filepath.Join(dir, "secret.md")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	linkPath := filepath.Join(outputDir, "26.04.13-晚间-1800.md")
	if err := os.Symlink(outsidePath, linkPath); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	cfg := &config.Config{Output: config.OutputCfg{Dir: outputDir}}
	err := SendMarkdownFile(linkPath, cfg)
	if err == nil || !strings.Contains(err.Error(), "outside output dir") {
		t.Fatalf("SendMarkdownFile() error = %v", err)
	}
}

func TestSendMarkdownFileRejectsSymlinkPathOutsideOutputDirEvenWhenTargetInside(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	realPath := filepath.Join(outputDir, "26.04.13-晚间-1800.md")
	if err := os.WriteFile(realPath, []byte("real body"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	linkDir := filepath.Join(dir, "links")
	if err := os.Mkdir(linkDir, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	linkPath := filepath.Join(linkDir, "26.04.13-晚间-1800.md")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	cfg := &config.Config{Output: config.OutputCfg{Dir: outputDir}, Email: config.Email{RetryTimes: 1}}
	sender := NewEmailSender()
	sender.smtpSend = func(cfg *config.Config, subject, body, password string) error { return nil }
	if err := os.Setenv("EMAIL_SMTP_AUTH_CODE", "secret"); err != nil {
		t.Fatalf("Setenv() error = %v", err)
	}
	defer os.Unsetenv("EMAIL_SMTP_AUTH_CODE")

	err := sender.SendMarkdownFile(linkPath, cfg)
	if err == nil || !strings.Contains(err.Error(), "outside output dir") {
		t.Fatalf("SendMarkdownFile() error = %v", err)
	}
}

func TestEmailDialContextUsesConfiguredSocks5Proxy(t *testing.T) {
	capturedAddr := ""
	capturedTimeout := time.Duration(0)
	sender := NewEmailSender()
	sender.newSocks5EmailDialContext = func(proxyAddr string, timeout time.Duration) (func(context.Context, string, string) (net.Conn, error), error) {
		capturedAddr = proxyAddr
		capturedTimeout = timeout
		return func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, errors.New("stop")
		}, nil
	}

	cfg := &config.Config{
		Email: config.Email{Timeout: 2 * time.Second, UseProxy: true},
		Proxy: config.Proxy{Socks5: "socks5://127.0.0.1:1080"},
	}
	_, err := sender.newEmailDialContext(cfg)
	if err != nil {
		t.Fatalf("newEmailDialContext() error = %v", err)
	}
	if capturedAddr != "socks5://127.0.0.1:1080" {
		t.Fatalf("proxy addr = %q, want socks5://127.0.0.1:1080", capturedAddr)
	}
	if capturedTimeout != 2*time.Second {
		t.Fatalf("timeout = %v, want %v", capturedTimeout, 2*time.Second)
	}
}

func TestEmailDialContextRejectsMissingSocks5ProxyWhenEnabled(t *testing.T) {
	cfg := &config.Config{Email: config.Email{Timeout: time.Second, UseProxy: true}}
	_, err := newEmailDialContext(cfg)
	if err == nil || !strings.Contains(err.Error(), "proxy.socks5") {
		t.Fatalf("newEmailDialContext() error = %v", err)
	}
}

func TestEmailDialerDirectIgnoresProxyEnvWhenDisabled(t *testing.T) {
	capturedTimeout := time.Duration(0)
	sender := NewEmailSender()
	sender.newDirectEmailDialContext = func(timeout time.Duration) func(ctx context.Context, network, address string) (net.Conn, error) {
		capturedTimeout = timeout
		return func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, errors.New("stop")
		}
	}

	cfg := &config.Config{Email: config.Email{SMTPHost: "smtp.example.com", SMTPPort: 465, Timeout: time.Second, UseProxy: false}}
	err := sender.deliverSMTPMessage(cfg, "subject", "body", "secret")
	if err == nil || !strings.Contains(err.Error(), "stop") {
		t.Fatalf("deliverSMTPMessage() error = %v", err)
	}
	if capturedTimeout != time.Second {
		t.Fatalf("capturedTimeout = %v, want %v", capturedTimeout, time.Second)
	}
}

func TestSendEmailWithRetryStopsAfterSuccess(t *testing.T) {
	cfg := &config.Config{Email: config.Email{RetryTimes: 3, RetryWaitTime: 0, UseProxy: false}}
	attempts := 0
	sender := NewEmailSender()
	sender.smtpSend = func(cfg *config.Config, subject, body, password string) error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary timeout")
		}
		return nil
	}
	sender.sleep = func(time.Duration) {}
	if err := os.Setenv("EMAIL_SMTP_AUTH_CODE", "secret"); err != nil {
		t.Fatalf("Setenv() error = %v", err)
	}
	defer os.Unsetenv("EMAIL_SMTP_AUTH_CODE")

	if err := sender.sendEmailWithContent(cfg, "subject", "body"); err != nil {
		t.Fatalf("sendEmailWithContent() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestSendEmailWithRetryReturnsLastError(t *testing.T) {
	cfg := &config.Config{Email: config.Email{RetryTimes: 3, RetryWaitTime: 0, UseProxy: false}}
	sender := NewEmailSender()
	sender.smtpSend = func(cfg *config.Config, subject, body, password string) error {
		return errors.New("temporary timeout")
	}
	sender.sleep = func(time.Duration) {}
	if err := os.Setenv("EMAIL_SMTP_AUTH_CODE", "secret"); err != nil {
		t.Fatalf("Setenv() error = %v", err)
	}
	defer os.Unsetenv("EMAIL_SMTP_AUTH_CODE")

	err := sender.sendEmailWithContent(cfg, "subject", "body")
	if err == nil || !strings.Contains(err.Error(), "after 3 attempts") {
		t.Fatalf("sendEmailWithContent() error = %v", err)
	}
}
