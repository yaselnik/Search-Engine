package frontier

import (
	"net/http"
	"runtime"
	"time"
)

// Config holds serializable configuration parameters for Frontier.
// It can be embedded directly into a global YAML/ENV application config.
type Config struct {
	ShardsCount         int           `yaml:"shards_count" env:"FRONTIER_SHARDS_COUNT"`
	ReaperInterval      time.Duration `yaml:"reaper_interval" env:"FRONTIER_REAPER_INTERVAL"`
	LeaseTimeout        time.Duration `yaml:"lease_timeout" env:"FRONTIER_LEASE_TIMEOUT"`
	DefaultCrawlDelay   time.Duration `yaml:"default_crawl_delay" env:"FRONTIER_DEFAULT_CRAWL_DELAY"`
	MaxFailCount        int           `yaml:"max_fail_count" env:"FRONTIER_MAX_FAIL_COUNT"`
	MaxHostQueueSize    int           `yaml:"max_host_queue_size" env:"MAX_HOST_QUEUE_SIZE"`
	BaseRefreshInterval time.Duration `yaml:"base_refresh_interval" env:"FRONTIER_BASE_REFRESH_INTERVAL"`
	MinRefreshInterval  time.Duration `yaml:"min_refresh_interval" env:"FRONTIER_MIN_REFRESH_INTERVAL"`
	MaxRefreshInterval  time.Duration `yaml:"max_refresh_interval" env:"FRONTIER_MAX_REFRESH_INTERVAL"`
	RobotsUserAgent     string        `yaml:"robots_user_agent" env:"FRONTIER_ROBOTS_USER_AGENT"`
	RobotsTimeout       time.Duration `yaml:"robots_timeout" env:"FRONTIER_ROBOTS_TIMEOUT"`
	RobotsCacheCapacity int           `yaml:"robots_cache_capacity" env:"FRONTIER_ROBOTS_CACHE_CAPACITY"`
	RobotsCacheTTL      time.Duration `yaml:"robots_cache_ttl" env:"FRONTIER_ROBOTS_CACHE_TTL"`
	RobotsFailedRetry   time.Duration `yaml:"robots_failed_retry" env:"FRONTIER_ROBOTS_FAILED_RETRY"`
}

// Option configures runtime dependencies of Frontier that do not belong in config files.
type Option func(*options)

type options struct {
	httpClient *http.Client
	store      *PostgresStore
}

// WithPostgresStore attaches a persistent PostgreSQL store to the Frontier.
func WithPostgresStore(store *PostgresStore) Option {
	return func(o *options) {
		o.store = store
	}
}

// WithRobotsHTTPClient sets a custom *http.Client for fetching robots.txt.
func WithRobotsHTTPClient(client *http.Client) Option {
	return func(o *options) {
		o.httpClient = client
	}
}

// DefaultConfig returns sensible production defaults.
func DefaultConfig() Config {
	return Config{
		ShardsCount:         2 * runtime.NumCPU(),
		ReaperInterval:      30 * time.Second,
		LeaseTimeout:        2 * time.Minute,
		DefaultCrawlDelay:   1 * time.Second,
		MaxFailCount:        5,
		MaxHostQueueSize:    1000,
		BaseRefreshInterval: 24 * time.Hour,
		MinRefreshInterval:  1 * time.Hour,
		MaxRefreshInterval:  7 * 24 * time.Hour,
		RobotsUserAgent:     "Polzun/1.0",
		RobotsTimeout:       5 * time.Second,
		RobotsCacheCapacity: 1 << 14,
		RobotsCacheTTL:      24 * time.Hour,
		RobotsFailedRetry:   15 * time.Minute,
	}
}

func (c Config) withDefaults() Config {
	def := DefaultConfig()
	if c.ShardsCount <= 0 {
		c.ShardsCount = def.ShardsCount
	}
	if c.ReaperInterval <= 0 {
		c.ReaperInterval = def.ReaperInterval
	}
	if c.LeaseTimeout <= 0 {
		c.LeaseTimeout = def.LeaseTimeout
	}
	if c.DefaultCrawlDelay <= 0 {
		c.DefaultCrawlDelay = def.DefaultCrawlDelay
	}
	if c.MaxFailCount <= 0 {
		c.MaxFailCount = def.MaxFailCount
	}
	if c.MaxHostQueueSize <= 0 {
		c.MaxHostQueueSize = def.MaxHostQueueSize
	}
	if c.BaseRefreshInterval <= 0 {
		c.BaseRefreshInterval = def.BaseRefreshInterval
	}
	if c.MinRefreshInterval <= 0 {
		c.MinRefreshInterval = def.MinRefreshInterval
	}
	if c.MaxRefreshInterval <= 0 {
		c.MaxRefreshInterval = def.MaxRefreshInterval
	}
	if c.RobotsUserAgent == "" {
		c.RobotsUserAgent = def.RobotsUserAgent
	}
	if c.RobotsTimeout <= 0 {
		c.RobotsTimeout = def.RobotsTimeout
	}
	if c.RobotsCacheCapacity <= 0 {
		c.RobotsCacheCapacity = def.RobotsCacheCapacity
	}
	if c.RobotsCacheTTL <= 0 {
		c.RobotsCacheTTL = def.RobotsCacheTTL
	}
	if c.RobotsFailedRetry <= 0 {
		c.RobotsFailedRetry = def.RobotsFailedRetry
	}
	return c
}
