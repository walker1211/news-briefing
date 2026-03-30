package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultPeriodFromUsesToTime(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	to := time.Date(2026, 3, 18, 14, 0, 0, 0, loc)
	if got := defaultPeriodFrom(to); got != "1400" {
		t.Fatalf("defaultPeriodFrom() = %q, want %q", got, "1400")
	}
}

func TestParseRegenTimeUsesProvidedLocation(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	got, err := parseRegenTime("2026-03-18 08:00", loc)
	if err != nil {
		t.Fatalf("parseRegenTime() error = %v", err)
	}
	if got.Location().String() != "America/Los_Angeles" {
		t.Fatalf("parseRegenTime() location = %q, want %q", got.Location().String(), "America/Los_Angeles")
	}
	if got.Format("2006-01-02 15:04") != "2026-03-18 08:00" {
		t.Fatalf("parseRegenTime() = %q", got.Format("2006-01-02 15:04"))
	}
}

func TestFormatArticlePublishedAtUsesProvidedLocation(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	published := time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC)
	if got := formatArticlePublishedAt(published, loc); got != "2026-03-18 07:00" {
		t.Fatalf("formatArticlePublishedAt() = %q, want %q", got, "2026-03-18 07:00")
	}
}

func TestReadStringFlagReturnsValue(t *testing.T) {
	args := []string{"regen", "--from", "2026-03-18 08:00", "--to", "2026-03-18 14:00"}
	if got, ok := readStringFlag(args, "--from"); !ok || got != "2026-03-18 08:00" {
		t.Fatalf("readStringFlag() = (%q, %v)", got, ok)
	}
}

func TestReadStringFlagRejectsMissingValue(t *testing.T) {
	args := []string{"regen", "--from", "--to", "2026-03-18 14:00"}
	if _, ok := readStringFlag(args, "--from"); ok {
		t.Fatalf("readStringFlag() ok = true, want false when value is missing")
	}
}

func TestValidatePeriodAcceptsHHMM(t *testing.T) {
	if err := validatePeriod("1400"); err != nil {
		t.Fatalf("validatePeriod() error = %v", err)
	}
}

func TestValidatePeriodRejectsInvalidValues(t *testing.T) {
	cases := []string{"9", "2500", "1260", "ab12"}
	for _, tc := range cases {
		if err := validatePeriod(tc); err == nil {
			t.Fatalf("validatePeriod(%q) error = nil, want error", tc)
		}
	}
}

func TestUsageMentionsRegenDefaults(t *testing.T) {
	usage := usageText()
	if !strings.Contains(usage, "默认不发邮件") {
		t.Fatalf("usageText() missing regen default email note")
	}
	if !strings.Contains(usage, "默认仍会写出 Markdown 文件") {
		t.Fatalf("usageText() missing regen markdown note")
	}
	if !strings.Contains(usage, "schedule_timezone") {
		t.Fatalf("usageText() missing schedule_timezone hint")
	}
	if !strings.Contains(usage, "Flags (for deep)") {
		t.Fatalf("usageText() missing deep flags section")
	}
	if !strings.Contains(usage, "news-briefing deep \"Claude\" --from \"2026-03-28 00:00\" --to \"2026-03-29 23:59\"") {
		t.Fatalf("usageText() missing deep explicit window example")
	}
	if !strings.Contains(usage, "默认读取未读池；若仅传 --ignore-seen，则使用最近 12 小时窗口") {
		t.Fatalf("usageText() missing deep default window behavior note")
	}
	if !strings.Contains(usage, "--send-email                 发送邮件") {
		t.Fatalf("usageText() missing deep send-email flag")
	}
	if !strings.Contains(usage, "news-briefing deep \"Claude\" --send-email") {
		t.Fatalf("usageText() missing deep send-email example")
	}
	if strings.Contains(usage, "Asia/Shanghai") {
		t.Fatalf("usageText() should not hardcode Asia/Shanghai")
	}
}

func TestUsageMentionsMigratedConfigAndSeenPaths(t *testing.T) {
	usage := usageText()
	if !strings.Contains(usage, "configs/config.yaml") {
		t.Fatalf("usageText() missing configs/config.yaml")
	}
	if !strings.Contains(usage, "<output.dir>/state/seen.json") {
		t.Fatalf("usageText() missing <output.dir>/state/seen.json")
	}
}

func TestParseArgsErrorIncludesDetails(t *testing.T) {
	_, err := parseArgs([]string{"regen", "--to", "2026-03-18 14:00"})
	if err == nil {
		t.Fatalf("parseArgs() error = nil, want detail error")
	}
	if !strings.Contains(err.Error(), "--from is required") {
		t.Fatalf("parseArgs() error = %v, want missing --from detail", err)
	}
}

func TestUsageErrorTextIncludesDetailsAndUsage(t *testing.T) {
	text := usageErrorText(fmt.Errorf("--from is required"))
	if !strings.Contains(text, "--from is required") {
		t.Fatalf("usageErrorText() = %q, want parse error detail", text)
	}
	if !strings.Contains(text, "国际资讯聚合器") {
		t.Fatalf("usageErrorText() = %q, want usage text", text)
	}
}

func TestLoadDefaultConfigUsesConfigsPath(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	configPath := filepath.Join(configDir, "config.yaml")
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
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() {
		_ = os.Chdir(cwd)
	}()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	cfg, err := loadDefaultConfig()
	if err != nil {
		t.Fatalf("loadDefaultConfig() error = %v", err)
	}
	if cfg.Output.Dir != "output" {
		t.Fatalf("Output.Dir = %q, want %q", cfg.Output.Dir, "output")
	}
}

func TestLoadDefaultConfigIgnoresLegacyRootConfigPath(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "config.yaml")
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
	if err := os.WriteFile(legacyPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() {
		_ = os.Chdir(cwd)
	}()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	_, err = loadDefaultConfig()
	if err == nil {
		t.Fatalf("loadDefaultConfig() error = nil, want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "configs/config.yaml") {
		t.Fatalf("error = %q, want mention configs/config.yaml", msg)
	}
	if strings.Contains(msg, "mkdir -p configs") || strings.Contains(msg, "mv config.yaml configs/config.yaml") {
		t.Fatalf("error = %q, should not mention legacy migration steps", msg)
	}
}

func TestLoadDefaultConfigFailsWhenNoConfigExists(t *testing.T) {
	dir := t.TempDir()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() {
		_ = os.Chdir(cwd)
	}()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	_, err = loadDefaultConfig()
	if err == nil {
		t.Fatalf("loadDefaultConfig() error = nil, want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "configs/config.yaml") {
		t.Fatalf("error = %q, want mention configs/config.yaml", msg)
	}
	if !strings.Contains(msg, "configs/config.example.yaml") {
		t.Fatalf("error = %q, want mention configs/config.example.yaml", msg)
	}
}

func TestMainPrintsHelpWithoutConfig(t *testing.T) {
	if os.Getenv("NEWS_MAIN_HELP_SUBPROCESS") == "1" {
		if err := os.Chdir(os.Getenv("NEWS_MAIN_WORKDIR")); err != nil {
			fmt.Fprintf(os.Stderr, "chdir: %v\n", err)
			os.Exit(2)
		}
		oldArgs := os.Args
		defer func() { os.Args = oldArgs }()
		os.Args = []string{"news-briefing", "help"}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMainPrintsHelpWithoutConfig")
	cmd.Env = append(os.Environ(),
		"NEWS_MAIN_HELP_SUBPROCESS=1",
		"NEWS_MAIN_WORKDIR="+t.TempDir(),
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess error = %v\noutput=%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "国际资讯聚合器") {
		t.Fatalf("output = %q, want usage text", text)
	}
	if strings.Contains(text, "Error loading config") {
		t.Fatalf("output = %q, should not require config for help", text)
	}
}
