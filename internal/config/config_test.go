package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestLoadAppliesDefaultAIConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.AI.Command != "ccs" {
		t.Fatalf("AI.Command = %q, want %q", cfg.AI.Command, "ccs")
	}
	if len(cfg.AI.Args) != 1 || cfg.AI.Args[0] != "codex" {
		t.Fatalf("AI.Args = %v, want %v", cfg.AI.Args, []string{"codex"})
	}
	if len(cfg.AI.ExtraFlags) != 0 {
		t.Fatalf("AI.ExtraFlags = %v, want empty", cfg.AI.ExtraFlags)
	}
	if !cfg.AI.ShouldAppendSystemPrompt() {
		t.Fatalf("AI.ShouldAppendSystemPrompt() = false, want true")
	}
}

func TestLoadPreservesConfiguredAIFlags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
ai:
  command: claude
  args:
    - --model
    - claude-opus-4-6
  extra_flags:
    - --bare
    - --disable-slash-commands
  append_system_prompt: false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.AI.Command != "claude" {
		t.Fatalf("AI.Command = %q, want %q", cfg.AI.Command, "claude")
	}
	if !reflect.DeepEqual(cfg.AI.Args, []string{"--model", "claude-opus-4-6"}) {
		t.Fatalf("AI.Args = %v", cfg.AI.Args)
	}
	if !reflect.DeepEqual(cfg.AI.ExtraFlags, []string{"--bare", "--disable-slash-commands"}) {
		t.Fatalf("AI.ExtraFlags = %v", cfg.AI.ExtraFlags)
	}
	if cfg.AI.ShouldAppendSystemPrompt() {
		t.Fatalf("AI.ShouldAppendSystemPrompt() = true, want false")
	}
}

func TestLoadAppliesDefaultOutputMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Output.Mode != model.OutputModeTranslatedOnly {
		t.Fatalf("Output.Mode = %q, want %q", cfg.Output.Mode, model.OutputModeTranslatedOnly)
	}
}

func TestLoadAcceptsValidOutputModes(t *testing.T) {
	modes := []model.OutputMode{
		model.OutputModeOriginalOnly,
		model.OutputModeTranslatedOnly,
		model.OutputModeBilingualTranslatedFirst,
		model.OutputModeBilingualOriginalFirst,
	}

	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output:
  mode: ` + string(mode) + `
proxy: {}
`
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			if cfg.Output.Mode != mode {
				t.Fatalf("Output.Mode = %q, want %q", cfg.Output.Mode, mode)
			}
		})
	}
}

func TestLoadRejectsInvalidOutputMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output:
  mode: invalid_mode
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "output.mode") {
		t.Fatalf("Load() error = %q, want mention output.mode", err)
	}
	if !strings.Contains(err.Error(), "invalid_mode") {
		t.Fatalf("Load() error = %q, want mention invalid value", err)
	}
}

func TestLoadDefaultsScheduleTimezoneToLocal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ScheduleLocation == nil {
		t.Fatal("ScheduleLocation = nil")
	}
	if cfg.ScheduleLocation.String() != time.Local.String() {
		t.Fatalf("ScheduleLocation = %q, want %q", cfg.ScheduleLocation.String(), time.Local.String())
	}
}

func TestLoadAppliesConfiguredScheduleTimezone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
schedule_timezone: Asia/Shanghai
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ScheduleLocation == nil {
		t.Fatal("ScheduleLocation = nil")
	}
	if got := cfg.ScheduleLocation.String(); got != "Asia/Shanghai" {
		t.Fatalf("ScheduleLocation = %q, want %q", got, "Asia/Shanghai")
	}
}

func TestLoadRejectsInvalidScheduleTimezone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
schedule_timezone: Mars/Base
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "schedule_timezone") {
		t.Fatalf("Load() error = %q, want mention schedule_timezone", err)
	}
}

func TestLoadTrimsScheduleTimezoneWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
schedule_timezone: " Asia/Shanghai "
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.ScheduleLocation.String(); got != "Asia/Shanghai" {
		t.Fatalf("ScheduleLocation = %q, want %q", got, "Asia/Shanghai")
	}
}

func TestLoadAppliesDefaultEmailDeliveryConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Email.Timeout != 3*time.Second {
		t.Fatalf("Email.Timeout = %v, want %v", cfg.Email.Timeout, 3*time.Second)
	}
	if cfg.Email.RetryTimes != 3 {
		t.Fatalf("Email.RetryTimes = %d, want %d", cfg.Email.RetryTimes, 3)
	}
	if cfg.Email.RetryWaitTime != 500*time.Millisecond {
		t.Fatalf("Email.RetryWaitTime = %v, want %v", cfg.Email.RetryWaitTime, 500*time.Millisecond)
	}
	if cfg.Email.UseProxy {
		t.Fatalf("Email.UseProxy = true, want false")
	}
}

func TestLoadParsesConfiguredEmailDeliveryConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
  timeout: 4s
  retry_times: 2
  retry_wait_time: 250ms
  use_proxy: true
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Email.Timeout != 4*time.Second {
		t.Fatalf("Email.Timeout = %v, want %v", cfg.Email.Timeout, 4*time.Second)
	}
	if cfg.Email.RetryTimes != 2 {
		t.Fatalf("Email.RetryTimes = %d, want %d", cfg.Email.RetryTimes, 2)
	}
	if cfg.Email.RetryWaitTime != 250*time.Millisecond {
		t.Fatalf("Email.RetryWaitTime = %v, want %v", cfg.Email.RetryWaitTime, 250*time.Millisecond)
	}
	if !cfg.Email.UseProxy {
		t.Fatalf("Email.UseProxy = false, want true")
	}
}

func TestLoadRejectsInvalidEmailDeliveryConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name: "bad timeout",
			config: `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
  timeout: nope
schedule: []
output: {}
proxy: {}
`,
			wantErr: "email.timeout",
		},
		{
			name: "retry times below one",
			config: `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
  retry_times: 0
schedule: []
output: {}
proxy: {}
`,
			wantErr: "email.retry_times",
		},
		{
			name: "negative retry wait",
			config: `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
  retry_wait_time: -1ms
schedule: []
output: {}
proxy: {}
`,
			wantErr: "email.retry_wait_time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(tt.config), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadAllowsEmptySourcesScheduleAndEmail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadAllowsHackerNewsSourceWithoutURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources:
  - name: Hacker News
    type: hackernews
    category: AI/科技
keywords: []
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsInvalidSourceConfig(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		wantErr string
	}{
		{name: "missing name", source: "url: https://example.com/feed.xml\ntype: rss\ncategory: AI/科技", wantErr: "sources[0].name"},
		{name: "invalid type", source: "name: Example\nurl: https://example.com/feed.xml\ntype: nope\ncategory: AI/科技", wantErr: "sources[0].type"},
		{name: "invalid url", source: "name: Example\nurl: notaurl\ntype: rss\ncategory: AI/科技", wantErr: "sources[0].url"},
		{name: "missing category", source: "name: Example\nurl: https://example.com/feed.xml\ntype: rss", wantErr: "sources[0].category"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := "sources:\n  - " + strings.ReplaceAll(tt.source, "\n", "\n    ") + "\n" +
				"keywords: []\n" +
				"email:\n" +
				"  smtp_host: smtp.example.com\n" +
				"  smtp_port: 465\n" +
				"  from: from@example.com\n" +
				"  to: to@example.com\n" +
				"schedule: []\n" +
				"output: {}\n" +
				"proxy: {}\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadRejectsInvalidScheduleExpression(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule:
  - nope
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "schedule[0]") {
		t.Fatalf("Load() error = %v, want schedule[0]", err)
	}
}

func TestValidateEmailForSendingRequiresCompleteIdentity(t *testing.T) {
	tests := []struct {
		name    string
		email   Email
		wantErr string
	}{
		{name: "missing smtp host", email: Email{SMTPPort: 465, From: "from@example.com", To: "to@example.com"}, wantErr: "email.smtp_host"},
		{name: "bad port", email: Email{SMTPHost: "smtp.example.com", SMTPPort: 70000, From: "from@example.com", To: "to@example.com"}, wantErr: "email.smtp_port"},
		{name: "bad from", email: Email{SMTPHost: "smtp.example.com", SMTPPort: 465, From: "not-an-address", To: "to@example.com"}, wantErr: "email.from"},
		{name: "bad to", email: Email{SMTPHost: "smtp.example.com", SMTPPort: 465, From: "from@example.com", To: "not-an-address"}, wantErr: "email.to"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEmailForSending(tt.email)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateEmailForSending() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateEmailForSendingAcceptsCompleteIdentity(t *testing.T) {
	err := ValidateEmailForSending(Email{SMTPHost: "smtp.example.com", SMTPPort: 465, From: "from@example.com", To: "to@example.com"})
	if err != nil {
		t.Fatalf("ValidateEmailForSending() error = %v", err)
	}
}

func TestLoadRejectsInvalidEmailIdentityConfig(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr string
	}{
		{name: "missing smtp host", email: "smtp_port: 465\nfrom: from@example.com\nto: to@example.com", wantErr: "email.smtp_host"},
		{name: "bad port", email: "smtp_host: smtp.example.com\nsmtp_port: 70000\nfrom: from@example.com\nto: to@example.com", wantErr: "email.smtp_port"},
		{name: "bad from", email: "smtp_host: smtp.example.com\nsmtp_port: 465\nfrom: not-an-address\nto: to@example.com", wantErr: "email.from"},
		{name: "bad to", email: "smtp_host: smtp.example.com\nsmtp_port: 465\nfrom: from@example.com\nto: not-an-address", wantErr: "email.to"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := "sources: []\n" +
				"keywords: []\n" +
				"email:\n  " + strings.ReplaceAll(tt.email, "\n", "\n  ") + "\n" +
				"schedule: []\n" +
				"output: {}\n" +
				"proxy: {}\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadRejectsInvalidProxyConfig(t *testing.T) {
	tests := []struct {
		name    string
		proxy   string
		wantErr string
	}{
		{name: "bad http scheme", proxy: "http: ftp://example.com", wantErr: "proxy.http"},
		{name: "bad socks scheme", proxy: "socks5: http://127.0.0.1:1080", wantErr: "proxy.socks5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := "sources: []\n" +
				"keywords: []\n" +
				"email:\n" +
				"  smtp_host: smtp.example.com\n" +
				"  smtp_port: 465\n" +
				"  from: from@example.com\n" +
				"  to: to@example.com\n" +
				"schedule: []\n" +
				"output: {}\n" +
				"proxy:\n  " + strings.ReplaceAll(tt.proxy, "\n", "\n  ") + "\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadRejectsInvalidAIConfig(t *testing.T) {
	tests := []struct {
		name    string
		ai      string
		wantErr string
	}{
		{name: "blank command", ai: "command: \" \"", wantErr: "ai.command"},
		{name: "blank arg", ai: "command: ccs\nargs:\n  - \" \"", wantErr: "ai.args[0]"},
		{name: "blank extra flag", ai: "command: ccs\nextra_flags:\n  - \" \"", wantErr: "ai.extra_flags[0]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := "sources: []\n" +
				"keywords: []\n" +
				"email:\n" +
				"  smtp_host: smtp.example.com\n" +
				"  smtp_port: 465\n" +
				"  from: from@example.com\n" +
				"  to: to@example.com\n" +
				"schedule: []\n" +
				"output: {}\n" +
				"proxy: {}\n" +
				"ai:\n  " + strings.ReplaceAll(tt.ai, "\n", "\n  ") + "\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadSupportsPageSourcesAndScopedKeywords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources:
  - name: GLM Docs
    url: https://example.com/glm
    type: docs_page
    category: AI/科技
    keywords:
      - GLM
    page_kind: announcement
    time_hint: published
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Sources) != 1 {
		t.Fatalf("len(Sources) = %d, want %d", len(cfg.Sources), 1)
	}

	source := cfg.Sources[0]
	if !reflect.DeepEqual(source.Keywords, []string{"GLM"}) {
		t.Fatalf("Source.Keywords = %v, want %v", source.Keywords, []string{"GLM"})
	}
	if source.PageKind != "announcement" {
		t.Fatalf("Source.PageKind = %q, want %q", source.PageKind, "announcement")
	}
	if source.TimeHint != "published" {
		t.Fatalf("Source.TimeHint = %q, want %q", source.TimeHint, "published")
	}
}

func TestLoadSupportsWatchSites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
watch:
  sites:
    - name: Anthropic Claude Support
      type: anthropic_support
      home_url: https://support.claude.com/zh-CN
      briefing_category: AI/科技
      category_allowlist:
        - Claude
        - 安全保障
      high_value_keywords:
        - 身份验证
        - 电话验证
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Watch.Sites) != 1 {
		t.Fatalf("len(cfg.Watch.Sites) = %d, want 1", len(cfg.Watch.Sites))
	}
	site := cfg.Watch.Sites[0]
	if site.Type != WatchTypeAnthropicSupport {
		t.Fatalf("site.Type = %q", site.Type)
	}
	if !reflect.DeepEqual(site.CategoryAllowlist, []string{"Claude", "安全保障"}) {
		t.Fatalf("site.CategoryAllowlist = %#v", site.CategoryAllowlist)
	}
	if !reflect.DeepEqual(site.HighValueKeywords, []string{"身份验证", "电话验证"}) {
		t.Fatalf("site.HighValueKeywords = %#v", site.HighValueKeywords)
	}
}

func TestLoadRejectsInvalidWatchSiteConfig(t *testing.T) {
	tests := []struct {
		name    string
		site    string
		wantErr string
	}{
		{name: "missing name", site: "type: anthropic_support\nhome_url: https://support.claude.com/zh-CN\nbriefing_category: AI/科技", wantErr: "watch.sites[0].name"},
		{name: "invalid type", site: "name: Anthropic\ntype: nope\nhome_url: https://support.claude.com/zh-CN\nbriefing_category: AI/科技", wantErr: "watch.sites[0].type"},
		{name: "invalid home url", site: "name: Anthropic\ntype: anthropic_support\nhome_url: notaurl\nbriefing_category: AI/科技", wantErr: "watch.sites[0].home_url"},
		{name: "missing briefing category", site: "name: Anthropic\ntype: anthropic_support\nhome_url: https://support.claude.com/zh-CN", wantErr: "watch.sites[0].briefing_category"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := "sources: []\n" +
				"keywords: []\n" +
				"watch:\n" +
				"  sites:\n" +
				"    - " + strings.ReplaceAll(tt.site, "\n", "\n      ") + "\n" +
				"email:\n" +
				"  smtp_host: smtp.example.com\n" +
				"  smtp_port: 465\n" +
				"  from: from@example.com\n" +
				"  to: to@example.com\n" +
				"schedule: []\n" +
				"output: {}\n" +
				"proxy: {}\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestProjectConfigIncludesAnthropicSupportWatch(t *testing.T) {
	configPaths := map[string]string{
		"project": filepath.Join("..", "..", "configs", "config.yaml"),
		"example": filepath.Join("..", "..", "configs", "config.example.yaml"),
	}

	for name, configPath := range configPaths {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			found := false
			for _, site := range cfg.Watch.Sites {
				if site.Name != "Anthropic Claude Support" || site.Type != WatchTypeAnthropicSupport {
					continue
				}
				found = true
				if site.BriefingCategory != "AI/科技" {
					t.Fatalf("site.BriefingCategory = %q", site.BriefingCategory)
				}
				if len(site.CategoryAllowlist) == 0 || len(site.HighValueKeywords) == 0 {
					t.Fatalf("site = %#v", site)
				}
			}
			if !found {
				t.Fatalf("Anthropic Claude Support site not found in %#v", cfg.Watch.Sites)
			}
		})
	}
}

func TestProjectConfigIncludesAnthropicOfficialAnnouncementWatchSites(t *testing.T) {
	configPaths := map[string]string{
		"project": filepath.Join("..", "..", "configs", "config.yaml"),
		"example": filepath.Join("..", "..", "configs", "config.example.yaml"),
	}
	want := []WatchSite{
		{
			Name:             "Anthropic News",
			Type:             WatchTypeAnnouncementPage,
			HomeURL:          "https://www.anthropic.com/news",
			BriefingCategory: "AI/科技",
		},
		{
			Name:             "Claude Platform Release Notes",
			Type:             WatchTypeAnnouncementPage,
			HomeURL:          "https://platform.claude.com/docs/en/release-notes/overview",
			BriefingCategory: "AI/科技",
		},
	}

	for name, configPath := range configPaths {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			got := make(map[string]WatchSite, len(cfg.Watch.Sites))
			for _, site := range cfg.Watch.Sites {
				key := site.Name + "|" + site.Type + "|" + site.HomeURL + "|" + site.BriefingCategory
				got[key] = site
			}

			for _, site := range want {
				key := site.Name + "|" + site.Type + "|" + site.HomeURL + "|" + site.BriefingCategory
				gotSite, exists := got[key]
				if !exists {
					t.Fatalf("missing Anthropic official watch site: %+v", site)
				}
				if len(gotSite.HighValueKeywords) == 0 {
					t.Fatalf("site.HighValueKeywords for %q = empty", gotSite.Name)
				}
			}
		})
	}
}

func TestProjectConfigIncludesDiscoveryEnhancementAISources(t *testing.T) {
	configPaths := map[string]string{
		"project": filepath.Join("..", "..", "configs", "config.yaml"),
		"example": filepath.Join("..", "..", "configs", "config.example.yaml"),
	}
	want := []Source{
		{Name: "AllenAI Blog", URL: "https://allenai.org/rss.xml", Type: SourceTypeRSS, Category: "AI/科技"},
		{Name: "Cognition Blog", URL: "https://cognition.ai/rss.xml", Type: SourceTypeRSS, Category: "AI/科技"},
		{Name: "Bing / Microsoft Search Blog", URL: "https://blogs.bing.com/Home/feed", Type: SourceTypeRSS, Category: "AI/科技"},
	}

	for name, configPath := range configPaths {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			got := make(map[string]Source, len(cfg.Sources))
			for _, source := range cfg.Sources {
				key := source.Name + "|" + source.URL + "|" + source.Type + "|" + source.Category
				if _, exists := got[key]; exists {
					t.Fatalf("duplicate source found: %+v", source)
				}
				got[key] = source
			}
			for _, source := range want {
				key := source.Name + "|" + source.URL + "|" + source.Type + "|" + source.Category
				gotSource, exists := got[key]
				if !exists {
					t.Fatalf("missing discovery enhancement source: %+v", source)
				}
				if len(gotSource.Keywords) != 0 {
					t.Fatalf("Source.Keywords for %q = %v, want empty", gotSource.Name, gotSource.Keywords)
				}
				if gotSource.PageKind != "" {
					t.Fatalf("Source.PageKind for %q = %q, want empty", gotSource.Name, gotSource.PageKind)
				}
				if gotSource.TimeHint != "" {
					t.Fatalf("Source.TimeHint for %q = %q, want empty", gotSource.Name, gotSource.TimeHint)
				}
			}
		})
	}
}

func TestProjectConfigIncludesDiscoveryEnhancementAIKeywords(t *testing.T) {
	configPaths := map[string]string{
		"project": filepath.Join("..", "..", "configs", "config.yaml"),
		"example": filepath.Join("..", "..", "configs", "config.example.yaml"),
	}
	want := []string{"AllenAI", "Ai2", "GLM"}
	rejected := []string{"BigModel", "z.ai", "ACE Studio", "StepFun", "HappyHorse", "Paper Review", "BYOK", "Terafab"}

	for name, configPath := range configPaths {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			got := make(map[string]struct{}, len(cfg.Keywords))
			for _, keyword := range cfg.Keywords {
				if _, exists := got[keyword]; exists {
					t.Fatalf("duplicate keyword found: %q", keyword)
				}
				got[keyword] = struct{}{}
			}
			for _, keyword := range want {
				if _, exists := got[keyword]; !exists {
					t.Fatalf("missing discovery enhancement keyword: %q", keyword)
				}
			}
			for _, keyword := range rejected {
				if _, exists := got[keyword]; exists {
					t.Fatalf("unexpected noisy keyword included: %q", keyword)
				}
			}
		})
	}
}
