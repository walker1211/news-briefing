package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/walker1211/news-briefing/internal/model"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Sources          []Source       `yaml:"sources"`
	Keywords         []string       `yaml:"keywords"`
	Email            Email          `yaml:"email"`
	Schedule         Schedule       `yaml:"schedule"`
	ScheduleTimezone string         `yaml:"schedule_timezone"`
	ScheduleLocation *time.Location `yaml:"-"`
	Output           OutputCfg      `yaml:"output"`
	Proxy            Proxy          `yaml:"proxy"`
	AI               AICfg          `yaml:"ai"`
}

type Source struct {
	Name     string   `yaml:"name"`
	URL      string   `yaml:"url"`
	Type     string   `yaml:"type"` // 如 "rss"、"hackernews"、"reddit"、"docs_page"、"repo_page"
	Category string   `yaml:"category"`
	Keywords []string `yaml:"keywords"`
	PageKind string   `yaml:"page_kind"`
	TimeHint string   `yaml:"time_hint"`
}

type Email struct {
	SMTPHost         string        `yaml:"smtp_host"`
	SMTPPort         int           `yaml:"smtp_port"`
	From             string        `yaml:"from"`
	To               string        `yaml:"to"`
	TimeoutRaw       string        `yaml:"timeout"`
	RetryTimesRaw    *int          `yaml:"retry_times"`
	RetryWaitTimeRaw string        `yaml:"retry_wait_time"`
	UseProxy         bool          `yaml:"use_proxy"`
	Timeout          time.Duration `yaml:"-"`
	RetryTimes       int           `yaml:"-"`
	RetryWaitTime    time.Duration `yaml:"-"`
}

// Schedule 定时任务列表，每项为一个 cron 表达式
type Schedule []string

type OutputCfg struct {
	Dir  string           `yaml:"dir"`
	Mode model.OutputMode `yaml:"mode"`
}

type Proxy struct {
	HTTP   string `yaml:"http"`
	Socks5 string `yaml:"socks5"`
}

type AICfg struct {
	Command            string   `yaml:"command"`
	Args               []string `yaml:"args"`
	ExtraFlags         []string `yaml:"extra_flags"`
	AppendSystemPrompt *bool    `yaml:"append_system_prompt"`
}

func (cfg AICfg) ShouldAppendSystemPrompt() bool {
	return cfg.AppendSystemPrompt == nil || *cfg.AppendSystemPrompt
}

func resolveScheduleLocation(name string) (*time.Location, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(trimmed)
	if err != nil {
		return nil, fmt.Errorf("load schedule_timezone %q: %w", trimmed, err)
	}
	return loc, nil
}

func applyEmailDefaults(email *Email) error {
	if strings.TrimSpace(email.TimeoutRaw) == "" {
		email.TimeoutRaw = "3s"
	}
	if email.RetryTimesRaw == nil {
		defaultRetries := 3
		email.RetryTimesRaw = &defaultRetries
	}
	if strings.TrimSpace(email.RetryWaitTimeRaw) == "" {
		email.RetryWaitTimeRaw = "500ms"
	}

	timeout, err := time.ParseDuration(strings.TrimSpace(email.TimeoutRaw))
	if err != nil {
		return fmt.Errorf("parse email.timeout: %w", err)
	}
	if timeout <= 0 {
		return fmt.Errorf("validate email.timeout: must be greater than 0")
	}

	wait, err := time.ParseDuration(strings.TrimSpace(email.RetryWaitTimeRaw))
	if err != nil {
		return fmt.Errorf("parse email.retry_wait_time: %w", err)
	}
	if wait < 0 {
		return fmt.Errorf("validate email.retry_wait_time: must be zero or greater")
	}
	if *email.RetryTimesRaw < 1 {
		return fmt.Errorf("validate email.retry_times: must be at least 1")
	}

	email.Timeout = timeout
	email.RetryTimes = *email.RetryTimesRaw
	email.RetryWaitTime = wait
	return nil
}

func Load(configPath string) (*Config, error) {
	_ = godotenv.Load()

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err = yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Output.Dir == "" {
		cfg.Output.Dir = "output"
	}
	if cfg.Output.Mode == "" {
		cfg.Output.Mode = model.OutputModeTranslatedOnly
	}
	if err := cfg.Output.Mode.Validate(); err != nil {
		return nil, fmt.Errorf("validate output.mode: %w", err)
	}
	if cfg.AI.Command == "" {
		cfg.AI.Command = "ccs"
	}
	if len(cfg.AI.Args) == 0 {
		cfg.AI.Args = []string{"codex"}
	}
	loc, err := resolveScheduleLocation(cfg.ScheduleTimezone)
	if err != nil {
		return nil, err
	}
	cfg.ScheduleLocation = loc
	if err := applyEmailDefaults(&cfg.Email); err != nil {
		return nil, err
	}

	return &cfg, nil
}
