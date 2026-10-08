package frontier

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

var _ domain.Frontier = (*MemFrontier)(nil)

const (
	defaultCrawlDelay = time.Second
)

type hostState struct {
	queue      []domain.FrontierItem
	lastReq    time.Time
	crawlDelay time.Duration
}

type MemFrontier struct {
	mu sync.RWMutex

	hosts map[string]*hostState

	seen map[string]struct{}

	robots *RobotsCache

	logger *slog.Logger

	client *http.Client

	maxSize int
}

func NewMemFrontier(client *http.Client, logger *slog.Logger) *MemFrontier {
	if client == nil {
		client = &http.Client{Timeout: robotsTimeout}
	}
	return &MemFrontier{
		hosts:   make(map[string]*hostState),
		seen:    make(map[string]struct{}),
		robots:  NewRobotsCache(client, logger),
		client:  client,
		logger:  logger.With("component", "frontier"),
		maxSize: 1_000_000,
	}
}

func (f *MemFrontier) Push(ctx context.Context, items []domain.FrontierItem) bool {
	for _, it := range items {
		host := it.Host
		if host == "" {
			host = hostOf(it.URL)
		}
		allowed, crawlDelay := f.checkRobots(ctx, host, it.URL)

		f.mu.Lock()

		if len(f.seen) >= f.maxSize {
			f.mu.Unlock()
			return false
		}

		if _, ok := f.seen[it.URL]; ok {
			f.mu.Unlock()
			continue
		}

		if !allowed {
			f.seen[it.URL] = struct{}{}
			f.mu.Unlock()
			continue
		}

		st, ok := f.hosts[host]
		if !ok {
			st = &hostState{crawlDelay: defaultCrawlDelay}
			f.hosts[host] = st
		}
		if crawlDelay > 0 {
			st.crawlDelay = crawlDelay
		}
		st.queue = append(st.queue, it)
		f.seen[it.URL] = struct{}{}

		f.mu.Unlock()
	}

	return true
}

func (f *MemFrontier) Pop(ctx context.Context, workerID string, n int) ([]domain.FrontierItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()
	out := make([]domain.FrontierItem, 0, n)

	for _, st := range f.hosts {
		if len(out) >= n {
			break
		}
		if len(st.queue) == 0 {
			continue
		}
		if now.Sub(st.lastReq) < st.crawlDelay {
			continue
		}

		item := st.queue[0]
		st.queue = st.queue[1:]
		st.lastReq = now
		out = append(out, item)
	}

	return out, nil
}

func (f *MemFrontier) Ack(ctx context.Context, url string) error {
	return nil
}

func (f *MemFrontier) Nack(ctx context.Context, url string, retryAfter time.Duration, permanent bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if permanent {
		f.seen[url] = struct{}{}
		return nil
	}

	host := hostOf(url)
	st := f.hosts[host]

	st.lastReq = time.Now().Add(retryAfter - st.crawlDelay)
	st.queue = append(st.queue, domain.FrontierItem{
		URL:  url,
		Host: host,
	})
	return nil
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}

func (f *MemFrontier) checkRobots(ctx context.Context, host, rawURL string) (allowed bool, crawlDelay time.Duration) {
	robots, err := f.robots.Get(ctx, host)
	if err != nil || robots == nil {
		return true, 0
	}

	group := robots.FindGroup(robotsUserAgent)
	if group == nil {
		return true, 0
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return false, 0
	}

	return group.Test(u.Path), group.CrawlDelay
}
