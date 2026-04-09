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

func TestProjectConfigIncludesDiscoveryEnhancementAISources(t *testing.T) {
	configPaths := map[string]string{
		"project": filepath.Join("..", "..", "configs", "config.yaml"),
		"example": filepath.Join("..", "..", "configs", "config.example.yaml"),
	}
	want := []Source{
		{Name: "AllenAI Blog", URL: "https://allenai.org/rss.xml", Type: "rss", Category: "AI/科技"},
		{Name: "Cognition Blog", URL: "https://cognition.ai/rss.xml", Type: "rss", Category: "AI/科技"},
		{Name: "Bing / Microsoft Search Blog", URL: "https://blogs.bing.com/Home/feed", Type: "rss", Category: "AI/科技"},
	}

	for name, configPath := range configPaths {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			got := make(map[Source]struct{}, len(cfg.Sources))
			for _, source := range cfg.Sources {
				if _, exists := got[source]; exists {
					t.Fatalf("duplicate source found: %+v", source)
				}
				got[source] = struct{}{}
			}
			for _, source := range want {
				if _, exists := got[source]; !exists {
					t.Fatalf("missing discovery enhancement source: %+v", source)
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
	want := []string{"AllenAI", "Ai2", "GLM-5.1"}
	rejected := []string{"GLM", "BigModel", "z.ai", "ACE Studio", "StepFun", "HappyHorse", "Paper Review", "BYOK", "Terafab"}

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
