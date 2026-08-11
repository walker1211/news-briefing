package config

import (
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
	"github.com/walker1211/news-briefing/internal/model"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Sources          []Source          `yaml:"sources"`
	Keywords         []string          `yaml:"keywords"`
	Filters          FiltersConfig     `yaml:"filters"`
	Fetch            FetchConfig       `yaml:"fetch"`
	Watch            WatchConfig       `yaml:"watch"`
	XAccounts        XAccountsConfig   `yaml:"x_accounts"`
	Email            Email             `yaml:"email"`
	Schedule         Schedule          `yaml:"schedule"`
	ScheduleDelayRaw string            `yaml:"schedule_delay"`
	ScheduleDelay    time.Duration     `yaml:"-"`
	ScheduleTimezone string            `yaml:"schedule_timezone"`
	ScheduleLocation *time.Location    `yaml:"-"`
	Output           OutputCfg         `yaml:"output"`
	PublishHook      PublishHookConfig `yaml:"publish_hook"`
	Proxy            Proxy             `yaml:"proxy"`
	AI               AICfg             `yaml:"ai"`
}

const (
	SourceTypeRSS        = "rss"
	SourceTypeHackerNews = "hackernews"
	SourceTypeReddit     = "reddit"
	SourceTypeDocsPage   = "docs_page"
	SourceTypeRepoPage   = "repo_page"

	WatchTypeAnthropicSupport = "anthropic_support"
	WatchTypeAnnouncementPage = "announcement_page"

	WatchProxyProviderTypeBrowsebox = "browsebox"
	WatchProxyProviderModeProxy     = "proxy"

	DefaultFetchTimeout                   = 30 * time.Second
	DefaultFetchRetryTimes                = 3
	DefaultFetchRetryWaitTime             = 200 * time.Millisecond
	DefaultFetchRedditSourceDelayMin      = 2 * time.Second
	DefaultFetchRedditSourceDelayMax      = 4 * time.Second
	DefaultFetchRedditRateLimitWaitMax    = 120 * time.Second
	DefaultWatchBrowseboxCommand          = "browsebox"
	DefaultWatchBrowseboxNodesConcurrency = 12
	DefaultWatchBrowseboxDelayTimeoutMS   = 7000
	DefaultWatchBrowseboxStartupTimeout   = 2 * time.Minute
	DefaultWatchBrowseboxProxyPort        = 17997
	DefaultWatchBrowseboxControllerPort   = 17998
	DefaultWatchArticleConcurrency        = 8
	DefaultXRefreshWaitTimeout            = 10 * time.Minute
	DefaultXRefreshWaitInterval           = 5 * time.Second
	DefaultXRefreshReconcileInterval      = time.Minute
	DefaultXRefreshHeartbeatStaleAfter    = 3 * time.Minute
	DefaultXMaxPostsPerTarget             = 10
	DefaultAIModel                        = "gpt-5.6-sol"
	DefaultAITranslationModel             = "gpt-5.3-codex-spark"
)

var (
	defaultAIArgs = []string{
		"exec",
		"--ignore-user-config",
		"--ephemeral",
		"--skip-git-repo-check",
		"--sandbox", "read-only",
		"--color", "never",
		"--disable", "apps",
		"--disable", "plugins",
		"--disable", "remote_plugin",
	}
	legacyClaudeAIArgs   = []string{"--bare", "--disable-slash-commands"}
	DefaultAIRetryDelays = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 9 * time.Second, 17 * time.Second}
)

func cloneDurations(values []time.Duration) []time.Duration {
	return append([]time.Duration(nil), values...)
}

type Source struct {
	Name               string   `yaml:"name"`
	URL                string   `yaml:"url"`
	Type               string   `yaml:"type"`
	Category           string   `yaml:"category"`
	Keywords           []string `yaml:"keywords"`
	MaxItems           int      `yaml:"max_items"`
	PageKind           string   `yaml:"page_kind"`
	TimeHint           string   `yaml:"time_hint"`
	RSSHubAccessKeyEnv string   `yaml:"rsshub_access_key_env"`
}

type FiltersConfig struct {
	Categories map[string]CategoryFilterConfig `yaml:"categories"`
	Sources    map[string]SourceFilterConfig   `yaml:"sources"`
}

type CategoryFilterConfig struct {
	IncludeKeywords       []string `yaml:"include_keywords"`
	WeakKeywords          []string `yaml:"weak_keywords"`
	MinWeakKeywordMatches int      `yaml:"min_weak_keyword_matches"`
	ExcludeKeywords       []string `yaml:"exclude_keywords"`
}

type SourceFilterConfig struct {
	IncludeKeywords []string `yaml:"include_keywords"`
	ExcludeKeywords []string `yaml:"exclude_keywords"`
	MaxArticles     int      `yaml:"max_articles"`
	Priority        int      `yaml:"priority"`
}

type FetchConfig struct {
	TimeoutRaw                string        `yaml:"timeout"`
	RetryTimesRaw             *int          `yaml:"retry_times"`
	RetryWaitTimeRaw          string        `yaml:"retry_wait_time"`
	RedditSourceDelayMinRaw   string        `yaml:"reddit_source_delay_min"`
	RedditSourceDelayMaxRaw   string        `yaml:"reddit_source_delay_max"`
	RedditRateLimitWaitMaxRaw string        `yaml:"reddit_rate_limit_wait_max"`
	Timeout                   time.Duration `yaml:"-"`
	RetryTimes                int           `yaml:"-"`
	RetryWaitTime             time.Duration `yaml:"-"`
	RedditSourceDelayMin      time.Duration `yaml:"-"`
	RedditSourceDelayMax      time.Duration `yaml:"-"`
	RedditRateLimitWaitMax    time.Duration `yaml:"-"`
}

type WatchConfig struct {
	Sites                 []WatchSite        `yaml:"sites"`
	ArticleConcurrencyRaw *int               `yaml:"article_concurrency"`
	ArticleConcurrency    int                `yaml:"-"`
	ProxyProvider         WatchProxyProvider `yaml:"proxy_provider"`
}

type WatchProxyProvider struct {
	Enabled             bool          `yaml:"enabled"`
	TypeRaw             *string       `yaml:"type"`
	ModeRaw             *string       `yaml:"mode"`
	CommandRaw          *string       `yaml:"command"`
	Group               string        `yaml:"group"`
	HealthURLs          []string      `yaml:"health_urls"`
	NodesConcurrencyRaw *int          `yaml:"nodes_concurrency"`
	DelayTimeoutMSRaw   *int          `yaml:"delay_timeout_ms"`
	StartupTimeoutRaw   string        `yaml:"startup_timeout"`
	ProxyPortRaw        *int          `yaml:"proxy_port"`
	ControllerPortRaw   *int          `yaml:"controller_port"`
	Type                string        `yaml:"-"`
	Mode                string        `yaml:"-"`
	Command             string        `yaml:"-"`
	NodesConcurrency    int           `yaml:"-"`
	DelayTimeoutMS      int           `yaml:"-"`
	StartupTimeout      time.Duration `yaml:"-"`
	ProxyPort           int           `yaml:"-"`
	ControllerPort      int           `yaml:"-"`
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
	RefreshReconcileRaw    string           `yaml:"refresh_reconcile_interval"`
	HeartbeatStaleAfterRaw string           `yaml:"refresh_heartbeat_stale_after"`
	MaxPostsPerTarget      int              `yaml:"max_posts_per_target"`
	OriginalOnly           bool             `yaml:"original_only"`
	Category               string           `yaml:"category"`
	Accounts               []XAccountConfig `yaml:"accounts"`
	Lookback               time.Duration    `yaml:"-"`
	RefreshWaitTimeout     time.Duration    `yaml:"-"`
	RefreshWaitInterval    time.Duration    `yaml:"-"`
	RefreshReconcile       time.Duration    `yaml:"-"`
	HeartbeatStaleAfter    time.Duration    `yaml:"-"`
}

type XAccountConfig struct {
	Handle string `yaml:"handle"`
}

type Email struct {
	SMTPHost         string        `yaml:"smtp_host"`
	SMTPPort         int           `yaml:"smtp_port"`
	From             string        `yaml:"from"`
	To               string        `yaml:"to"`
	Recipients       []string      `yaml:"recipients"`
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
	Dir                     string            `yaml:"dir"`
	Mode                    model.OutputMode  `yaml:"mode"`
	IncludeFilteredArticles bool              `yaml:"include_filtered_articles"`
	MaxArticlesByCategory   map[string]int    `yaml:"max_articles"`
	Fallback                OutputFallbackCfg `yaml:"fallback"`
}

type OutputFallbackCfg struct {
	Enabled bool                `yaml:"enabled"`
	Levels  []ArticleLimitLevel `yaml:"levels"`
}

type ArticleLimitLevel struct {
	Name                  string         `yaml:"name"`
	MaxArticlesByCategory map[string]int `yaml:"max_articles"`
}

type PublishHookConfig struct {
	Enabled bool     `yaml:"enabled"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}

type Proxy struct {
	HTTP   string `yaml:"http"`
	Socks5 string `yaml:"socks5"`
}

type AICfg struct {
	Command            string       `yaml:"command"`
	Args               []string     `yaml:"args"`
	Models             AIModelsCfg  `yaml:"models"`
	AppendSystemPrompt *bool        `yaml:"append_system_prompt"`
	Retry              AIRetryCfg   `yaml:"retry"`
	Timeout            AITimeoutCfg `yaml:"timeout"`
}

type AIModelsCfg struct {
	Default     string `yaml:"default"`
	Translation string `yaml:"translation"`
}

type AIRetryCfg struct {
	Delays []time.Duration `yaml:"delays"`
}

type AITimeoutCfg struct {
	WarningAfter   time.Duration `yaml:"warning_after"`
	AttemptTimeout time.Duration `yaml:"attempt_timeout"`
	TotalBudget    time.Duration `yaml:"total_budget"`
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

var environmentVariableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var supportedWatchTypes = map[string]struct{}{
	WatchTypeAnthropicSupport: {},
	WatchTypeAnnouncementPage: {},
}

func ExpandHomePath(path string) string {
	return expandHomePath(path)
}

func expandHomePath(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return home + path[1:]
		}
	}
	return path
}

func applyWatchDefaults(watch *WatchConfig) error {
	if watch.ArticleConcurrencyRaw == nil {
		watch.ArticleConcurrency = DefaultWatchArticleConcurrency
	} else {
		watch.ArticleConcurrency = *watch.ArticleConcurrencyRaw
	}
	provider := &watch.ProxyProvider
	if !provider.Enabled {
		return nil
	}
	if provider.TypeRaw != nil {
		provider.Type = *provider.TypeRaw
	} else if strings.TrimSpace(provider.Type) == "" {
		provider.Type = WatchProxyProviderTypeBrowsebox
	}
	if provider.ModeRaw != nil {
		provider.Mode = *provider.ModeRaw
	} else if strings.TrimSpace(provider.Mode) == "" {
		provider.Mode = WatchProxyProviderModeProxy
	}
	if provider.CommandRaw != nil {
		provider.Command = expandHomePath(*provider.CommandRaw)
	} else if strings.TrimSpace(provider.Command) == "" {
		provider.Command = DefaultWatchBrowseboxCommand
	}
	if provider.NodesConcurrencyRaw == nil {
		provider.NodesConcurrency = DefaultWatchBrowseboxNodesConcurrency
	} else {
		provider.NodesConcurrency = *provider.NodesConcurrencyRaw
	}
	if provider.DelayTimeoutMSRaw == nil {
		provider.DelayTimeoutMS = DefaultWatchBrowseboxDelayTimeoutMS
	} else {
		provider.DelayTimeoutMS = *provider.DelayTimeoutMSRaw
	}
	if strings.TrimSpace(provider.StartupTimeoutRaw) == "" {
		provider.StartupTimeoutRaw = DefaultWatchBrowseboxStartupTimeout.String()
	}
	startupTimeout, err := time.ParseDuration(strings.TrimSpace(provider.StartupTimeoutRaw))
	if err != nil {
		return fmt.Errorf("parse watch.proxy_provider.startup_timeout: %w", err)
	}
	provider.StartupTimeout = startupTimeout
	if provider.ProxyPortRaw == nil {
		provider.ProxyPort = DefaultWatchBrowseboxProxyPort
	} else {
		provider.ProxyPort = *provider.ProxyPortRaw
	}
	if provider.ControllerPortRaw == nil {
		provider.ControllerPort = DefaultWatchBrowseboxControllerPort
	} else {
		provider.ControllerPort = *provider.ControllerPortRaw
	}
	return nil
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
	if strings.TrimSpace(fetch.RedditSourceDelayMinRaw) == "" {
		fetch.RedditSourceDelayMinRaw = DefaultFetchRedditSourceDelayMin.String()
	}
	if strings.TrimSpace(fetch.RedditSourceDelayMaxRaw) == "" {
		fetch.RedditSourceDelayMaxRaw = DefaultFetchRedditSourceDelayMax.String()
	}
	if strings.TrimSpace(fetch.RedditRateLimitWaitMaxRaw) == "" {
		fetch.RedditRateLimitWaitMaxRaw = DefaultFetchRedditRateLimitWaitMax.String()
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
	redditDelayMin, err := time.ParseDuration(strings.TrimSpace(fetch.RedditSourceDelayMinRaw))
	if err != nil {
		return fmt.Errorf("parse fetch.reddit_source_delay_min: %w", err)
	}
	if redditDelayMin <= 0 {
		return fmt.Errorf("validate fetch.reddit_source_delay_min: must be greater than 0")
	}
	redditDelayMax, err := time.ParseDuration(strings.TrimSpace(fetch.RedditSourceDelayMaxRaw))
	if err != nil {
		return fmt.Errorf("parse fetch.reddit_source_delay_max: %w", err)
	}
	if redditDelayMax <= 0 {
		return fmt.Errorf("validate fetch.reddit_source_delay_max: must be greater than 0")
	}
	if redditDelayMax < redditDelayMin {
		return fmt.Errorf("validate fetch.reddit_source_delay_max: must be greater than or equal to reddit_source_delay_min")
	}
	redditRateLimitWaitMax, err := time.ParseDuration(strings.TrimSpace(fetch.RedditRateLimitWaitMaxRaw))
	if err != nil {
		return fmt.Errorf("parse fetch.reddit_rate_limit_wait_max: %w", err)
	}
	if redditRateLimitWaitMax <= 0 {
		return fmt.Errorf("validate fetch.reddit_rate_limit_wait_max: must be greater than 0")
	}
	if *fetch.RetryTimesRaw < 1 {
		return fmt.Errorf("validate fetch.retry_times: must be at least 1")
	}

	fetch.Timeout = timeout
	fetch.RetryTimes = *fetch.RetryTimesRaw
	fetch.RetryWaitTime = wait
	fetch.RedditSourceDelayMin = redditDelayMin
	fetch.RedditSourceDelayMax = redditDelayMax
	fetch.RedditRateLimitWaitMax = redditRateLimitWaitMax
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
	if strings.TrimSpace(cfg.RefreshReconcileRaw) == "" {
		cfg.RefreshReconcileRaw = DefaultXRefreshReconcileInterval.String()
	}
	refreshReconcile, err := time.ParseDuration(strings.TrimSpace(cfg.RefreshReconcileRaw))
	if err != nil {
		return fmt.Errorf("parse x_accounts.refresh_reconcile_interval: %w", err)
	}
	if refreshReconcile <= 0 {
		return fmt.Errorf("validate x_accounts.refresh_reconcile_interval: must be greater than 0")
	}
	cfg.RefreshReconcile = refreshReconcile
	if strings.TrimSpace(cfg.HeartbeatStaleAfterRaw) == "" {
		cfg.HeartbeatStaleAfterRaw = DefaultXRefreshHeartbeatStaleAfter.String()
	}
	heartbeatStaleAfter, err := time.ParseDuration(strings.TrimSpace(cfg.HeartbeatStaleAfterRaw))
	if err != nil {
		return fmt.Errorf("parse x_accounts.refresh_heartbeat_stale_after: %w", err)
	}
	if heartbeatStaleAfter <= 0 {
		return fmt.Errorf("validate x_accounts.refresh_heartbeat_stale_after: must be greater than 0")
	}
	cfg.HeartbeatStaleAfter = heartbeatStaleAfter
	if cfg.MaxPostsPerTarget == 0 {
		cfg.MaxPostsPerTarget = DefaultXMaxPostsPerTarget
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
	if err := validateArticleLimits("output.max_articles", cfg.Output.MaxArticlesByCategory, false); err != nil {
		return err
	}
	if err := validateOutputFallback(cfg.Output.Fallback); err != nil {
		return err
	}
	if err := validateFilters(cfg.Filters); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.AI.Command) == "" {
		return fmt.Errorf("validate ai.command: must not be empty")
	}
	for i, arg := range cfg.AI.Args {
		if strings.TrimSpace(arg) == "" {
			return fmt.Errorf("validate ai.args[%d]: must not be empty", i)
		}
	}
	if strings.TrimSpace(cfg.AI.Models.Default) == "" {
		return fmt.Errorf("validate ai.models.default: must not be empty")
	}
	if strings.TrimSpace(cfg.AI.Models.Translation) == "" {
		return fmt.Errorf("validate ai.models.translation: must not be empty")
	}
	for i, delay := range cfg.AI.Retry.Delays {
		if delay <= 0 {
			return fmt.Errorf("validate ai.retry.delays[%d]: must be > 0", i)
		}
	}
	if err := validateAITimeout(cfg.AI.Timeout); err != nil {
		return err
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
	if cfg.Watch.ArticleConcurrency < 1 {
		return fmt.Errorf("validate watch.article_concurrency: must be at least 1")
	}
	for i, site := range cfg.Watch.Sites {
		if err := validateWatchSite(i, site); err != nil {
			return err
		}
	}
	if err := validateWatchProxyProvider(cfg.Watch.ProxyProvider); err != nil {
		return err
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

func validateArticleLimits(field string, limits map[string]int, requireNonEmpty bool) error {
	if len(limits) == 0 {
		if requireNonEmpty {
			return fmt.Errorf("validate %s: must not be empty", field)
		}
		return nil
	}
	for category, limit := range limits {
		if strings.TrimSpace(category) == "" {
			return fmt.Errorf("validate %s: category must not be empty", field)
		}
		if limit <= 0 {
			return fmt.Errorf("validate %s[%q]: must be greater than 0", field, category)
		}
	}
	return nil
}

func validateOutputFallback(fallback OutputFallbackCfg) error {
	if !fallback.Enabled && len(fallback.Levels) == 0 {
		return nil
	}
	if fallback.Enabled && len(fallback.Levels) == 0 {
		return fmt.Errorf("validate output.fallback.levels: must not be empty when fallback is enabled")
	}
	for i, level := range fallback.Levels {
		field := fmt.Sprintf("output.fallback.levels[%d]", i)
		if strings.TrimSpace(level.Name) == "" {
			return fmt.Errorf("validate %s.name: must not be empty", field)
		}
		if err := validateArticleLimits(field+".max_articles", level.MaxArticlesByCategory, true); err != nil {
			return err
		}
	}
	return nil
}

func validateFilters(filters FiltersConfig) error {
	for category, filter := range filters.Categories {
		field := fmt.Sprintf("filters.categories[%q]", category)
		if strings.TrimSpace(category) == "" {
			return fmt.Errorf("validate filters.categories: category must not be empty")
		}
		if err := validateKeywordList(field+".include_keywords", filter.IncludeKeywords); err != nil {
			return err
		}
		if err := validateKeywordList(field+".weak_keywords", filter.WeakKeywords); err != nil {
			return err
		}
		if filter.MinWeakKeywordMatches < 0 {
			return fmt.Errorf("validate %s.min_weak_keyword_matches: must be zero or greater", field)
		}
		if filter.MinWeakKeywordMatches > 0 && len(filter.WeakKeywords) == 0 {
			return fmt.Errorf("validate %s.min_weak_keyword_matches: requires weak_keywords", field)
		}
		if err := validateDistinctKeywordLists(field, filter.IncludeKeywords, filter.WeakKeywords); err != nil {
			return err
		}
		if err := validateKeywordList(field+".exclude_keywords", filter.ExcludeKeywords); err != nil {
			return err
		}
	}
	for source, filter := range filters.Sources {
		field := fmt.Sprintf("filters.sources[%q]", source)
		if strings.TrimSpace(source) == "" {
			return fmt.Errorf("validate filters.sources: source must not be empty")
		}
		if filter.MaxArticles < 0 {
			return fmt.Errorf("validate %s.max_articles: must be zero or greater", field)
		}
		if filter.Priority < 0 {
			return fmt.Errorf("validate %s.priority: must be zero or greater", field)
		}
		if err := validateKeywordList(field+".include_keywords", filter.IncludeKeywords); err != nil {
			return err
		}
		if err := validateKeywordList(field+".exclude_keywords", filter.ExcludeKeywords); err != nil {
			return err
		}
	}
	return nil
}

func validateDistinctKeywordLists(field string, strong []string, weak []string) error {
	seen := make(map[string]struct{}, len(strong))
	for _, keyword := range strong {
		seen[strings.ToLower(strings.TrimSpace(keyword))] = struct{}{}
	}
	for index, keyword := range weak {
		if _, ok := seen[strings.ToLower(strings.TrimSpace(keyword))]; ok {
			return fmt.Errorf("validate %s.weak_keywords[%d]: keyword %q also appears in include_keywords", field, index, keyword)
		}
	}
	return nil
}

func validateKeywordList(field string, keywords []string) error {
	seen := make(map[string]struct{}, len(keywords))
	for i, keyword := range keywords {
		trimmed := strings.TrimSpace(keyword)
		if trimmed == "" {
			return fmt.Errorf("validate %s[%d]: must not be empty", field, i)
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("validate %s[%d]: duplicate keyword %q", field, i, keyword)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateAITimeout(timeout AITimeoutCfg) error {
	if timeout.WarningAfter < 0 {
		return fmt.Errorf("validate ai.timeout.warning_after: must be zero or greater")
	}
	if timeout.AttemptTimeout < 0 {
		return fmt.Errorf("validate ai.timeout.attempt_timeout: must be zero or greater")
	}
	if timeout.TotalBudget < 0 {
		return fmt.Errorf("validate ai.timeout.total_budget: must be zero or greater")
	}
	if timeout.WarningAfter > 0 && timeout.AttemptTimeout > 0 && timeout.WarningAfter >= timeout.AttemptTimeout {
		return fmt.Errorf("validate ai.timeout.warning_after: must be less than attempt_timeout")
	}
	if timeout.AttemptTimeout > 0 && timeout.TotalBudget > 0 && timeout.AttemptTimeout > timeout.TotalBudget {
		return fmt.Errorf("validate ai.timeout.attempt_timeout: must be less than or equal to total_budget")
	}
	if timeout.WarningAfter > 0 && timeout.TotalBudget > 0 && timeout.WarningAfter >= timeout.TotalBudget {
		return fmt.Errorf("validate ai.timeout.warning_after: must be less than total_budget")
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
	if source.MaxItems < 0 {
		return fmt.Errorf("validate %s.max_items: must be zero or greater", prefix)
	}
	if source.MaxItems > 0 && kind != SourceTypeRSS {
		return fmt.Errorf("validate %s.max_items: only rss sources support item limits", prefix)
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
	accessKeyEnv := strings.TrimSpace(source.RSSHubAccessKeyEnv)
	if accessKeyEnv == "" {
		return nil
	}
	if kind != SourceTypeRSS {
		return fmt.Errorf("validate %s.rsshub_access_key_env: only rss sources support RSSHub authentication", prefix)
	}
	if !environmentVariableNamePattern.MatchString(accessKeyEnv) {
		return fmt.Errorf("validate %s.rsshub_access_key_env: must be a valid environment variable name", prefix)
	}
	parsedURL, err := url.Parse(source.URL)
	if err != nil {
		return fmt.Errorf("validate %s.url: %w", prefix, err)
	}
	if parsedURL.Scheme != "https" {
		return fmt.Errorf("validate %s.url: authenticated remote RSSHub sources must use https", prefix)
	}
	if parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return fmt.Errorf("validate %s.url: authenticated RSSHub sources must not contain credentials, a query, or a fragment", prefix)
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

func validateWatchProxyProvider(provider WatchProxyProvider) error {
	if !provider.Enabled {
		return nil
	}
	if strings.TrimSpace(provider.Type) != WatchProxyProviderTypeBrowsebox {
		return fmt.Errorf("validate watch.proxy_provider.type: unsupported provider type %q", provider.Type)
	}
	if strings.TrimSpace(provider.Mode) != WatchProxyProviderModeProxy {
		return fmt.Errorf("validate watch.proxy_provider.mode: unsupported provider mode %q", provider.Mode)
	}
	if strings.TrimSpace(provider.Command) == "" {
		return fmt.Errorf("validate watch.proxy_provider.command: must not be empty")
	}
	if provider.NodesConcurrency < 1 {
		return fmt.Errorf("validate watch.proxy_provider.nodes_concurrency: must be at least 1")
	}
	if provider.DelayTimeoutMS < 1 {
		return fmt.Errorf("validate watch.proxy_provider.delay_timeout_ms: must be at least 1")
	}
	if provider.StartupTimeout <= 0 {
		return fmt.Errorf("validate watch.proxy_provider.startup_timeout: must be greater than 0")
	}
	if err := validatePort("watch.proxy_provider.proxy_port", provider.ProxyPort); err != nil {
		return err
	}
	if err := validatePort("watch.proxy_provider.controller_port", provider.ControllerPort); err != nil {
		return err
	}
	if provider.ProxyPort == provider.ControllerPort {
		return fmt.Errorf("validate watch.proxy_provider.controller_port: must differ from proxy_port")
	}
	for i, rawURL := range provider.HealthURLs {
		if err := validateHTTPURL(fmt.Sprintf("watch.proxy_provider.health_urls[%d]", i), rawURL); err != nil {
			return err
		}
	}
	return nil
}

func validateXAccounts(cfg XAccountsConfig) error {
	if cfg.MaxPostsPerTarget < 1 {
		return fmt.Errorf("validate x_accounts.max_posts_per_target: must be at least 1")
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
	if strings.TrimSpace(email.SMTPHost) == "" && email.SMTPPort == 0 && strings.TrimSpace(email.From) == "" && strings.TrimSpace(email.To) == "" && len(email.Recipients) == 0 {
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
	for i, recipient := range email.Recipients {
		if err := validateEmailAddress(fmt.Sprintf("email.recipients[%d]", i), recipient); err != nil {
			return err
		}
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

func validatePort(field string, value int) error {
	if value < 1 || value > 65535 {
		return fmt.Errorf("validate %s: must be between 1 and 65535", field)
	}
	return nil
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
		cfg.AI.Command = "codex"
	}
	if len(cfg.AI.Args) == 0 {
		cfg.AI.Args = defaultAIArgsForCommand(cfg.AI.Command)
	}
	if cfg.AI.Models.Default == "" {
		cfg.AI.Models.Default = configuredModel(cfg.AI.Args)
		if cfg.AI.Models.Default == "" {
			cfg.AI.Models.Default = DefaultAIModel
		}
	}
	if cfg.AI.Models.Translation == "" {
		cfg.AI.Models.Translation = DefaultAITranslationModel
	}
	if cfg.AI.Retry.Delays == nil {
		cfg.AI.Retry.Delays = cloneDurations(DefaultAIRetryDelays)
	} else {
		cfg.AI.Retry.Delays = cloneDurations(cfg.AI.Retry.Delays)
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
	if err := applyWatchDefaults(&cfg.Watch); err != nil {
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

func defaultAIArgsForCommand(command string) []string {
	normalized := strings.ReplaceAll(strings.TrimSpace(command), `\`, "/")
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(normalized)), ".exe")
	switch name {
	case "codex":
		return append([]string(nil), defaultAIArgs...)
	case "claude":
		return append([]string(nil), legacyClaudeAIArgs...)
	default:
		return nil
	}
}

func configuredModel(args []string) string {
	var model string
	for i := 0; i < len(args); i++ {
		switch {
		case (args[i] == "--model" || args[i] == "-m") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-"):
			model = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--model="):
			model = strings.TrimPrefix(args[i], "--model=")
		case strings.HasPrefix(args[i], "-m="):
			model = strings.TrimPrefix(args[i], "-m=")
		}
	}
	return model
}
