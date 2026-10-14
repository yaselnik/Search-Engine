package frontier

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/temoto/robotstxt"
)

const (
	defaultCrawlDelay = 1 * time.Second
	robotsTimeout     = 5 * time.Second
	robotsUserAgent   = "SearchEngineBot/0.1"
)

type robotsEntry struct {
	data    *robotstxt.RobotsData
	err     error
	loaded  bool
	loading chan struct{}
}

type RobotsCache struct {
	mu      sync.Mutex
	entries map[string]*robotsEntry
	client  *http.Client
	logger  *slog.Logger
}

func NewRobotsCache(client *http.Client, logger *slog.Logger) *RobotsCache {
	if client == nil {
		client = &http.Client{Timeout: robotsTimeout}
	}
	return &RobotsCache{
		entries: make(map[string]*robotsEntry),
		client:  client,
		logger:  logger.With("component", "robotsCache"),
	}
}

func (c *RobotsCache) get(ctx context.Context, host string) (*robotstxt.RobotsData, error) {
	c.mu.Lock()
	e, ok := c.entries[host]

	if !ok {
		e = &robotsEntry{loading: make(chan struct{})}
		c.entries[host] = e
		c.mu.Unlock()
		c.load(ctx, host, e)
		return e.data, e.err
	}
	if e.loaded {
		c.mu.Unlock()
		return e.data, e.err
	}

	ch := e.loading
	c.mu.Unlock()
	select {
	case <-ch:
		return e.data, e.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *RobotsCache) load(ctx context.Context, host string, e *robotsEntry) {
	defer func() {
		c.mu.Lock()
		e.loaded = true
		close(e.loading)
		c.mu.Unlock()
	}()

	robotsURL := "https://" + host + "/robots.txt"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		e.err = err
		return
	}

	req.Header.Set("User-Agent", robotsUserAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		c.logger.Warn("robots fetch failed", "host", host, "error", err)
		e.err = err
		return
	}
	defer resp.Body.Close()

	data, err := robotstxt.FromResponse(resp)
	if err != nil {
		e.err = err
	}

	e.data = data
	c.logger.Debug("robots loaded", "host", host)
}


func (r *RobotsCache) GetRobots(ctx context.Context, host, url string) (bool, time.Duration) {
	robots, err := r.get(ctx, host)
	if err != nil || robots == nil {
		return true, defaultCrawlDelay
	}

	group := robots.FindGroup(robotsUserAgent)
	if group == nil {
		return true, defaultCrawlDelay
	}

	delay := group.CrawlDelay
	if delay == 0 {
		delay = defaultCrawlDelay
	}

	return group.Test(url), delay
}
