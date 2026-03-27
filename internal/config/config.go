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
	Name     string `yaml:"name"`
	URL      string `yaml:"url"`
	Type     string `yaml:"type"` // "rss"、"hackernews" 或 "reddit"
	Category string `yaml:"category"`
}

type Email struct {
	SMTPHost string `yaml:"smtp_host"`
	SMTPPort int    `yaml:"smtp_port"`
	From     string `yaml:"from"`
	To       string `yaml:"to"`
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

	return &cfg, nil
}
