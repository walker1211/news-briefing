package config

import (
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
	"github.com/walker1211/news-briefing/internal/model"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Sources          []Source        `yaml:"sources"`
	Keywords         []string        `yaml:"keywords"`
	Fetch            FetchConfig     `yaml:"fetch"`
	Watch            WatchConfig     `yaml:"watch"`
	XAccounts        XAccountsConfig `yaml:"x_accounts"`
	Email            Email           `yaml:"email"`
	Schedule         Schedule        `yaml:"schedule"`
	ScheduleDelayRaw string          `yaml:"schedule_delay"`
	ScheduleDelay    time.Duration   `yaml:"-"`
	ScheduleTimezone string          `yaml:"schedule_timezone"`
	ScheduleLocation *time.Location  `yaml:"-"`
	Output           OutputCfg       `yaml:"output"`
	Proxy            Proxy           `yaml:"proxy"`
	AI               AICfg           `yaml:"ai"`
}

const (
	SourceTypeRSS        = "rss"
	SourceTypeHackerNews = "hackernews"
	SourceTypeReddit     = "reddit"
	SourceTypeDocsPage   = "docs_page"
	SourceTypeRepoPage   = "repo_page"

	WatchTypeAnthropicSupport = "anthropic_support"
	WatchTypeAnnouncementPage = "announcement_page"

	DefaultFetchTimeout         = 30 * time.Second
	DefaultFetchRetryTimes      = 3
	DefaultFetchRetryWaitTime   = 200 * time.Millisecond
	DefaultXRefreshWaitTimeout  = 10 * time.Minute
	DefaultXRefreshWaitInterval = 5 * time.Second
)

type Source struct {
	Name     string   `yaml:"name"`
	URL      string   `yaml:"url"`
	Type     string   `yaml:"type"`
	Category string   `yaml:"category"`
	Keywords []string `yaml:"keywords"`
	PageKind string   `yaml:"page_kind"`
	TimeHint string   `yaml:"time_hint"`
}

type FetchConfig struct {
	TimeoutRaw       string        `yaml:"timeout"`
	RetryTimesRaw    *int          `yaml:"retry_times"`
	RetryWaitTimeRaw string        `yaml:"retry_wait_time"`
	Timeout          time.Duration `yaml:"-"`
	RetryTimes       int           `yaml:"-"`
	RetryWaitTime    time.Duration `yaml:"-"`
}

type WatchConfig struct {
	Sites []WatchSite `yaml:"sites"`
}

type WatchSite struct {
	Name              string   `yaml:"name"`
	Type              string   `yaml:"type"`
	HomeURL           string   `yaml:"home_url"`
	BriefingCategory  string   `yaml:"briefing_category"`
	CategoryAllowlist []string `yaml:"category_allowlist"`
	HighValueKeywords []string `yaml:"high_value_keywords"`
}

type XAccountsConfig struct {
	Enabled                bool             `yaml:"enabled"`
	AccountsPath           string           `yaml:"accounts_path"`
	SearchesPath           string           `yaml:"searches_path"`
	HistoryDir             string           `yaml:"history_dir"`
	RefreshStatusPath      string           `yaml:"refresh_status_path"`
	LookbackRaw            string           `yaml:"lookback"`
	RefreshWaitTimeoutRaw  string           `yaml:"refresh_wait_timeout"`
	RefreshWaitIntervalRaw string           `yaml:"refresh_wait_interval"`
	MaxPostsPerAccount     int              `yaml:"max_posts_per_account"`
	Concurrency            int              `yaml:"concurrency"`
	Category               string           `yaml:"category"`
	Accounts               []XAccountConfig `yaml:"accounts"`
	Lookback               time.Duration    `yaml:"-"`
	RefreshWaitTimeout     time.Duration    `yaml:"-"`
	RefreshWaitInterval    time.Duration    `yaml:"-"`
}

type XAccountConfig struct {
	Handle string `yaml:"handle"`
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
	Dir                     string           `yaml:"dir"`
	Mode                    model.OutputMode `yaml:"mode"`
	IncludeFilteredArticles bool             `yaml:"include_filtered_articles"`
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

func applyScheduleDelay(cfg *Config) error {
	raw := strings.TrimSpace(cfg.ScheduleDelayRaw)
	if raw == "" {
		cfg.ScheduleDelay = 0
		return nil
	}
	delay, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("parse schedule_delay: %w", err)
	}
	if delay < 0 {
		return fmt.Errorf("validate schedule_delay: must be zero or greater")
	}
	cfg.ScheduleDelay = delay
	return nil
}

var supportedSourceTypes = map[string]struct{}{
	SourceTypeRSS:        {},
	SourceTypeHackerNews: {},
	SourceTypeReddit:     {},
	SourceTypeDocsPage:   {},
	SourceTypeRepoPage:   {},
}

var supportedWatchTypes = map[string]struct{}{
	WatchTypeAnthropicSupport: {},
	WatchTypeAnnouncementPage: {},
}

func applyFetchDefaults(fetch *FetchConfig) error {
	if strings.TrimSpace(fetch.TimeoutRaw) == "" {
		fetch.TimeoutRaw = DefaultFetchTimeout.String()
	}
	if fetch.RetryTimesRaw == nil {
		defaultRetries := DefaultFetchRetryTimes
		fetch.RetryTimesRaw = &defaultRetries
	}
	if strings.TrimSpace(fetch.RetryWaitTimeRaw) == "" {
		fetch.RetryWaitTimeRaw = DefaultFetchRetryWaitTime.String()
	}

	timeout, err := time.ParseDuration(strings.TrimSpace(fetch.TimeoutRaw))
	if err != nil {
		return fmt.Errorf("parse fetch.timeout: %w", err)
	}
	if timeout <= 0 {
		return fmt.Errorf("validate fetch.timeout: must be greater than 0")
	}

	wait, err := time.ParseDuration(strings.TrimSpace(fetch.RetryWaitTimeRaw))
	if err != nil {
		return fmt.Errorf("parse fetch.retry_wait_time: %w", err)
	}
	if wait < 0 {
		return fmt.Errorf("validate fetch.retry_wait_time: must be zero or greater")
	}
	if *fetch.RetryTimesRaw < 1 {
		return fmt.Errorf("validate fetch.retry_times: must be at least 1")
	}

	fetch.Timeout = timeout
	fetch.RetryTimes = *fetch.RetryTimesRaw
	fetch.RetryWaitTime = wait
	return nil
}

func applyXAccountsDefaults(cfg *XAccountsConfig) error {
	if strings.TrimSpace(cfg.LookbackRaw) == "" {
		cfg.LookbackRaw = "24h"
	}
	lookback, err := time.ParseDuration(strings.TrimSpace(cfg.LookbackRaw))
	if err != nil {
		return fmt.Errorf("parse x_accounts.lookback: %w", err)
	}
	if lookback <= 0 {
		return fmt.Errorf("validate x_accounts.lookback: must be greater than 0")
	}
	cfg.Lookback = lookback
	if strings.TrimSpace(cfg.RefreshWaitTimeoutRaw) == "" {
		cfg.RefreshWaitTimeoutRaw = DefaultXRefreshWaitTimeout.String()
	}
	refreshWaitTimeout, err := time.ParseDuration(strings.TrimSpace(cfg.RefreshWaitTimeoutRaw))
	if err != nil {
		return fmt.Errorf("parse x_accounts.refresh_wait_timeout: %w", err)
	}
	if refreshWaitTimeout <= 0 {
		return fmt.Errorf("validate x_accounts.refresh_wait_timeout: must be greater than 0")
	}
	cfg.RefreshWaitTimeout = refreshWaitTimeout
	if strings.TrimSpace(cfg.RefreshWaitIntervalRaw) == "" {
		cfg.RefreshWaitIntervalRaw = DefaultXRefreshWaitInterval.String()
	}
	refreshWaitInterval, err := time.ParseDuration(strings.TrimSpace(cfg.RefreshWaitIntervalRaw))
	if err != nil {
		return fmt.Errorf("parse x_accounts.refresh_wait_interval: %w", err)
	}
	if refreshWaitInterval <= 0 {
		return fmt.Errorf("validate x_accounts.refresh_wait_interval: must be greater than 0")
	}
	cfg.RefreshWaitInterval = refreshWaitInterval
	if cfg.MaxPostsPerAccount == 0 {
		cfg.MaxPostsPerAccount = 10
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = 8
	}
	if strings.TrimSpace(cfg.Category) == "" {
		cfg.Category = "AI/科技"
	}
	return nil
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

func (cfg *Config) Validate() error {
	if strings.TrimSpace(cfg.Output.Dir) == "" {
		return fmt.Errorf("validate output.dir: must not be empty")
	}
	if err := cfg.Output.Mode.Validate(); err != nil {
		return fmt.Errorf("validate output.mode: %w", err)
	}
	if strings.TrimSpace(cfg.AI.Command) == "" {
		return fmt.Errorf("validate ai.command: must not be empty")
	}
	for i, arg := range cfg.AI.Args {
		if strings.TrimSpace(arg) == "" {
			return fmt.Errorf("validate ai.args[%d]: must not be empty", i)
		}
	}
	for i, flag := range cfg.AI.ExtraFlags {
		if strings.TrimSpace(flag) == "" {
			return fmt.Errorf("validate ai.extra_flags[%d]: must not be empty", i)
		}
	}
	for i, expr := range cfg.Schedule {
		trimmed := strings.TrimSpace(expr)
		if trimmed == "" {
			return fmt.Errorf("validate schedule[%d]: must not be empty", i)
		}
		if _, err := cron.ParseStandard(trimmed); err != nil {
			return fmt.Errorf("validate schedule[%d] %q: %w", i, trimmed, err)
		}
	}
	for i, source := range cfg.Sources {
		if err := validateSource(i, source); err != nil {
			return err
		}
	}
	for i, site := range cfg.Watch.Sites {
		if err := validateWatchSite(i, site); err != nil {
			return err
		}
	}
	if err := validateXAccounts(cfg.XAccounts); err != nil {
		return err
	}
	if err := validateEmail(cfg.Email); err != nil {
		return err
	}
	if err := validateProxy(cfg.Proxy); err != nil {
		return err
	}
	return nil
}

func validateSource(index int, source Source) error {
	prefix := fmt.Sprintf("sources[%d]", index)
	if strings.TrimSpace(source.Name) == "" {
		return fmt.Errorf("validate %s.name: must not be empty", prefix)
	}
	if strings.TrimSpace(source.Category) == "" {
		return fmt.Errorf("validate %s.category: must not be empty", prefix)
	}
	kind := strings.TrimSpace(source.Type)
	if kind == "" {
		return fmt.Errorf("validate %s.type: must not be empty", prefix)
	}
	if _, ok := supportedSourceTypes[kind]; !ok {
		return fmt.Errorf("validate %s.type: unsupported source type %q", prefix, source.Type)
	}
	if strings.TrimSpace(source.URL) == "" {
		if kind == SourceTypeHackerNews {
			return nil
		}
		return fmt.Errorf("validate %s.url: must not be empty", prefix)
	}
	if err := validateHTTPURL(prefix+".url", source.URL); err != nil {
		return err
	}
	return nil
}

func validateWatchSite(index int, site WatchSite) error {
	prefix := fmt.Sprintf("watch.sites[%d]", index)
	if strings.TrimSpace(site.Name) == "" {
		return fmt.Errorf("validate %s.name: must not be empty", prefix)
	}
	if strings.TrimSpace(site.BriefingCategory) == "" {
		return fmt.Errorf("validate %s.briefing_category: must not be empty", prefix)
	}
	kind := strings.TrimSpace(site.Type)
	if kind == "" {
		return fmt.Errorf("validate %s.type: must not be empty", prefix)
	}
	if _, ok := supportedWatchTypes[kind]; !ok {
		return fmt.Errorf("validate %s.type: unsupported watch type %q", prefix, site.Type)
	}
	if err := validateHTTPURL(prefix+".home_url", site.HomeURL); err != nil {
		return err
	}
	return nil
}

func validateXAccounts(cfg XAccountsConfig) error {
	if cfg.MaxPostsPerAccount < 1 {
		return fmt.Errorf("validate x_accounts.max_posts_per_account: must be at least 1")
	}
	if cfg.Concurrency < 1 {
		return fmt.Errorf("validate x_accounts.concurrency: must be at least 1")
	}
	if strings.TrimSpace(cfg.Category) == "" {
		return fmt.Errorf("validate x_accounts.category: must not be empty")
	}
	if !cfg.Enabled {
		return nil
	}
	if len(cfg.Accounts) == 0 {
		return fmt.Errorf("validate x_accounts.accounts: must not be empty when enabled")
	}
	for i, account := range cfg.Accounts {
		if strings.TrimSpace(account.Handle) == "" {
			return fmt.Errorf("validate x_accounts.accounts[%d].handle: must not be empty", i)
		}
	}
	return nil
}

func validateEmail(email Email) error {
	if strings.TrimSpace(email.SMTPHost) == "" && email.SMTPPort == 0 && strings.TrimSpace(email.From) == "" && strings.TrimSpace(email.To) == "" {
		return nil
	}
	return ValidateEmailForSending(email)
}

func ValidateEmailForSending(email Email) error {
	if strings.TrimSpace(email.SMTPHost) == "" {
		return fmt.Errorf("validate email.smtp_host: must not be empty")
	}
	if email.SMTPPort < 1 || email.SMTPPort > 65535 {
		return fmt.Errorf("validate email.smtp_port: must be between 1 and 65535")
	}
	if err := validateEmailAddress("email.from", email.From); err != nil {
		return err
	}
	if err := validateEmailAddress("email.to", email.To); err != nil {
		return err
	}
	return nil
}

func validateEmailAddress(field string, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("validate %s: must not be empty", field)
	}
	if _, err := mail.ParseAddress(trimmed); err != nil {
		return fmt.Errorf("validate %s: %w", field, err)
	}
	return nil
}

func validateProxy(proxy Proxy) error {
	if err := validateOptionalURLScheme("proxy.http", proxy.HTTP, map[string]struct{}{"http": {}, "https": {}}); err != nil {
		return err
	}
	if err := validateOptionalURLScheme("proxy.socks5", proxy.Socks5, map[string]struct{}{"socks5": {}, "socks5h": {}}); err != nil {
		return err
	}
	return nil
}

func validateHTTPURL(field string, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("validate %s: must not be empty", field)
	}
	return validateURLScheme(field, trimmed, map[string]struct{}{"http": {}, "https": {}})
}

func validateOptionalURLScheme(field string, value string, allowed map[string]struct{}) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return validateURLScheme(field, trimmed, allowed)
}

func validateURLScheme(field string, value string, allowed map[string]struct{}) error {
	u, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("validate %s: %w", field, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("validate %s: must be an absolute URL", field)
	}
	if _, ok := allowed[strings.ToLower(u.Scheme)]; !ok {
		return fmt.Errorf("validate %s: unsupported scheme %q", field, u.Scheme)
	}
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
	if err := applyScheduleDelay(&cfg); err != nil {
		return nil, err
	}
	if err := applyFetchDefaults(&cfg.Fetch); err != nil {
		return nil, err
	}
	if err := applyXAccountsDefaults(&cfg.XAccounts); err != nil {
		return nil, err
	}
	if err := applyEmailDefaults(&cfg.Email); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}
