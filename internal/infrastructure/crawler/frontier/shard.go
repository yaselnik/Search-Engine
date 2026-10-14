package frontier

import (
	"container/heap"
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

const (
	reaperInterval = 30 * time.Second
	leaseTimeout   = 2 * time.Minute
)

type Shard struct {
	mu sync.Mutex

	pages map[string]*domain.FrontierItem
	hosts map[string]*HostState

	hostsQueue HostsQueue
	pageQueues map[string]*PageQueue // Maps hostnames to their url queues

	inFlightHosts map[string]struct{}
	inflightPages map[string]struct{}

	robots *RobotsCache
	logger *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewShard(parentCtx context.Context, logger *slog.Logger) *Shard {
	ctx, cancel := context.WithCancel(parentCtx)
	s := &Shard{
		pages:         make(map[string]*domain.FrontierItem),
		hosts:         make(map[string]*HostState),
		pageQueues:    make(map[string]*PageQueue),
		inFlightHosts: make(map[string]struct{}),
		inflightPages: make(map[string]struct{}),
		robots:        NewRobotsCache(nil, logger),
		logger:        logger.With("component", "shard"),
		ctx:           ctx,
		cancel:        cancel,
	}
	s.wg.Add(1)
	go s.reaper()
	return s
}

func (s *Shard) Close() {
	s.logger.Info("Shard closed", "total hosts", len(s.hosts))
	s.cancel()
	s.wg.Wait()
}

func (s *Shard) reaper() {
	defer s.wg.Done()
	t := time.NewTicker(reaperInterval)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			s.requeue()
		}
	}
}

func (s *Shard) requeue() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	threshold := now.Add(-leaseTimeout)
	for url := range s.inflightPages {
		page := s.pages[url]
		if page.LastFetchAt.After(threshold) {
			continue
		}
		delete(s.inflightPages, url)
		page.State = domain.StateFailed
		page.FailCount++
		page.NextFetchAt = now.Add(backoff(page.FailCount))
		s.releaseHostLocked(page.Host)
		s.enqueuePageLocked(page)
		s.logger.Warn("Page requeued", "url", page.URL)
	}
}

func (s *Shard) Push(ctx context.Context, item domain.NormalizedURL) error {
	allowed, delay := s.robots.GetRobots(ctx, item.Host, item.URL)

	s.mu.Lock()
	defer s.mu.Unlock()

	hs, ok := s.hosts[item.Host]
	if !ok {
		hs = &HostState{
			name:       item.Host,
			crawlDelay: delay,
			readyAt:    time.Now(),
		}
		s.hosts[item.Host] = hs
		q := &PageQueue{}
		heap.Init(q)
		s.pageQueues[item.Host] = q
		heap.Push(&s.hostsQueue, hs)
		hs.inQueue = true
	}

	page, exists := s.pages[item.URL]
	if !exists {
		page = &domain.FrontierItem{
			URL:          item.URL,
			Host:         item.Host,
			State:        domain.StateNew,
			LastFetchAt:  time.Time{},
			DiscoveredAt: time.Now(),
		}
		s.pages[item.URL] = page
	} else {
		page.PageRank++
	}

	if !allowed {
		page.State = domain.StateNotAllowed
		return nil
	}

	switch page.State {
	case domain.StateQueued, domain.StateInFlight, domain.StateNotAllowed:
		return nil
	}

	s.enqueuePageLocked(page)
	return nil
}

func (s *Shard) enqueuePageLocked(page *domain.FrontierItem) {
	page.Priority = countPagePriority(page)

	heap.Push(s.pageQueues[page.Host], &QueuedPage{
		URL:      page.URL,
		priority: page.Priority,
	})

	page.State = domain.StateQueued

	hs := s.hosts[page.Host]
	if !hs.inQueue {
		hs.readyAt = time.Now()
		heap.Push(&s.hostsQueue, hs)
		hs.inQueue = true
	}
}

// ToDo: a fair heuristic estimate
func countPagePriority(page *domain.FrontierItem) float64 {
	p := float64(page.PageRank)
	return p
}

func (s *Shard) Lease(ctx context.Context, n int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	out := make([]string, 0, n)

	for len(out) < n && s.hostsQueue.Len() > 0 {
		top := s.hostsQueue[0]
		if top.readyAt.After(now) {
			break
		}

		q := s.pageQueues[top.name]
		if q.Len() == 0 {
			heap.Pop(&s.hostsQueue)
			top.inQueue = false
			continue
		}

		heap.Pop(&s.hostsQueue)
		top.inQueue = false

		page := heap.Pop(q).(*QueuedPage)
		s.pages[page.URL].State = domain.StateInFlight
		s.pages[page.URL].LastFetchAt = now
		s.inflightPages[page.URL] = struct{}{}

		top.readyAt = now.Add(top.crawlDelay)

		out = append(out, page.URL)
	}
	return out, nil
}

func (s *Shard) Ack(ctx context.Context, url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.inflightPages[url]; !ok {
		return domain.ErrPageProcessingTimeoutExceeded
	}
	delete(s.inflightPages, url)

	page := s.pages[url]
	page.LastFetchAt = time.Now()
	page.FetchCount++
	page.State = domain.StateDone
	page.NextFetchAt = page.LastFetchAt.Add(nextInterval(page))

	s.releaseHostLocked(page.Host)
	//s.enqueuePageLocked(page)
	return nil
}

func (s *Shard) Nack(ctx context.Context, url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.inflightPages[url]; !ok {
		return domain.ErrPageProcessingTimeoutExceeded
	}
	delete(s.inflightPages, url)

	page := s.pages[url]
	page.LastFetchAt = time.Now()
	page.FailCount++
	page.State = domain.StateFailed
	page.NextFetchAt = page.LastFetchAt.Add(backoff(page.FailCount))

	s.releaseHostLocked(page.Host)
	s.enqueuePageLocked(page)
	return nil
}

func (s *Shard) releaseHostLocked(host string) {
	hs, ok := s.hosts[host]
	if !ok {
		return
	}

	hs.readyAt = time.Now().Add(hs.crawlDelay)
	if !hs.inQueue {
		heap.Push(&s.hostsQueue, hs)
		hs.inQueue = true
	}
}

// ToDo: a fair heuristic estimate
func nextInterval(page *domain.FrontierItem) time.Duration {
	base := 24 * time.Hour
	return base
}

// ToDo: a fair heuristic estimate
func backoff(fails int) time.Duration {
	if fails < 1 {
		fails = 1
	}
	if fails > 6 {
		fails = 6
	}
	return time.Duration(1<<fails) * time.Minute
}
