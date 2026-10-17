package frontier

import (
	"container/list"
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/temoto/robotstxt"
)

type robotsEntry struct {
	host      string
	data      *robotstxt.RobotsData
	err       error
	loaded    bool
	expiresAt time.Time
	loading   chan struct{}
	element   *list.Element
}

// RobotsCache is a bounded, thread-safe LRU cache for robots.txt rules.
// It coalesces concurrent requests to the same host (singleflight pattern)
// so that only one HTTP request is issued per host on a cache miss, and
// honors both positive TTL (or HTTP Expires header) and negative caching on errors.
type RobotsCache struct {
	mu          sync.Mutex
	capacity    int
	ttl         time.Duration
	failedRetry time.Duration
	defaultWait time.Duration
	userAgent   string
	entries     map[string]*robotsEntry
	lru         *list.List
	client      *http.Client
	logger      *slog.Logger
}

// NewRobotsCache creates a RobotsCache using default settings and the provided HTTP client.
func NewRobotsCache(client *http.Client, logger *slog.Logger) *RobotsCache {
	return NewRobotsCacheWithConfig(DefaultConfig(), client, logger)
}

// NewRobotsCacheWithConfig creates a RobotsCache configured via Config.
func NewRobotsCacheWithConfig(cfg Config, client *http.Client, logger *slog.Logger) *RobotsCache {
	cfg = cfg.withDefaults()
	if client == nil {
		client = &http.Client{Timeout: cfg.RobotsTimeout}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RobotsCache{
		capacity:    cfg.RobotsCacheCapacity,
		ttl:         cfg.RobotsCacheTTL,
		failedRetry: cfg.RobotsFailedRetry,
		defaultWait: cfg.DefaultCrawlDelay,
		userAgent:   cfg.RobotsUserAgent,
		entries:     make(map[string]*robotsEntry),
		lru:         list.New(),
		client:      client,
		logger:      logger.With("component", "robotsCache"),
	}
}

func (c *RobotsCache) get(ctx context.Context, scheme, host string) (*robotstxt.RobotsData, error) {
	now := time.Now()

	c.mu.Lock()
	e, ok := c.entries[host]
	if ok {
		// Cache hit: if already loaded and not expired, promote to front of LRU and return.
		if e.loaded && now.Before(e.expiresAt) {
			c.lru.MoveToFront(e.element)
			c.mu.Unlock()
			return e.data, e.err
		}
		// Another goroutine is currently fetching this host's robots.txt; wait for it.
		if !e.loaded {
			ch := e.loading
			c.mu.Unlock()
			select {
			case <-ch:
				return e.data, e.err
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		// Entry has expired; remove it before issuing a fresh request.
		c.removeEntryLocked(e)
	}

	// Evict the least-recently-used loaded entry if capacity is reached.
	if len(c.entries) >= c.capacity {
		c.evictOldestLocked()
	}

	e = &robotsEntry{
		host:    host,
		loading: make(chan struct{}),
	}
	e.element = c.lru.PushFront(e)
	c.entries[host] = e
	c.mu.Unlock()

	c.load(ctx, scheme, host, e)
	return e.data, e.err
}

func (c *RobotsCache) evictOldestLocked() {
	for elem := c.lru.Back(); elem != nil; elem = elem.Prev() {
		entry := elem.Value.(*robotsEntry)
		if entry.loaded {
			c.removeEntryLocked(entry)
			return
		}
	}
}

func (c *RobotsCache) removeEntryLocked(e *robotsEntry) {
	if e.element != nil {
		c.lru.Remove(e.element)
		e.element = nil
	}
	delete(c.entries, e.host)
}

func (c *RobotsCache) load(ctx context.Context, scheme, host string, e *robotsEntry) {
	var (
		data      *robotstxt.RobotsData
		fetchErr  error
		expiresAt = time.Now().Add(c.ttl)
	)

	defer func() {
		c.mu.Lock()
		e.data = data
		e.err = fetchErr
		e.expiresAt = expiresAt
		e.loaded = true
		close(e.loading)
		c.mu.Unlock()
	}()

	if scheme != "http" && scheme != "https" {
		scheme = "https"
	}
	robotsURL := scheme + "://" + host + "/robots.txt"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		fetchErr = err
		expiresAt = time.Now().Add(c.failedRetry)
		return
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		c.logger.DebugContext(ctx, "robots fetch failed, using default policy", "host", host, "error", err)
		fetchErr = err
		expiresAt = time.Now().Add(c.failedRetry)
		return
	}
	defer resp.Body.Close()

	// Honor the HTTP Expires header if provided by the remote server.
	if expHeader := resp.Header.Get("Expires"); expHeader != "" {
		if parsedExp, err := http.ParseTime(expHeader); err == nil && parsedExp.After(time.Now()) {
			expiresAt = parsedExp
		}
	}

	data, fetchErr = robotstxt.FromResponse(resp)
	if fetchErr != nil {
		expiresAt = time.Now().Add(c.failedRetry)
		return
	}
	c.logger.DebugContext(ctx, "robots loaded", "host", host)
}

// GetRobots checks whether rawURL is allowed to be crawled according to the host's
// robots.txt rules and returns the politeness Crawl-Delay for the host.
// If fetching or parsing robots.txt fails, crawling is permitted with DefaultCrawlDelay.
func (c *RobotsCache) GetRobots(ctx context.Context, host, rawURL string) (bool, time.Duration) {
	scheme := "https"
	path := "/"
	if parsed, err := url.Parse(rawURL); err == nil {
		if parsed.Scheme != "" {
			scheme = parsed.Scheme
		}
		if reqURI := parsed.RequestURI(); reqURI != "" {
			path = reqURI
		}
	}

	robots, err := c.get(ctx, scheme, host)
	if err != nil || robots == nil {
		return true, c.defaultWait
	}

	group := robots.FindGroup(c.userAgent)
	if group == nil {
		return true, c.defaultWait
	}

	delay := group.CrawlDelay
	if delay <= 0 {
		delay = c.defaultWait
	}

	return group.Test(path), delay
}
