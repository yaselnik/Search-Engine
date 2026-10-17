package frontier

import (
	"container/heap"
	"context"
	"hash/fnv"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

const (
	// Multi-factor priority weights
	newPageBaseUrgency = 5.0
	waitingAgeFactor   = 0.1
	depthSlashPenalty  = 0.15

	dbFlushTimeout    = 20 * time.Second
	dbRefillBatchSize = 250
)

// Shard represents an isolated partition of the Frontier state.
// It manages per-host politeness timers, page priority queues, and a background
// reaper goroutine that recovers stalled in-flight requests and schedules due refreshes.
type Shard struct {
	mu    sync.Mutex
	id    int
	cfg   Config
	store *PostgresStore

	pages      map[string]*domain.FrontierItem
	hosts      map[string]*HostState
	hostsQueue HostsQueue
	pageQueues map[string]*PageQueue // Maps hostname -> priority queue of ready pages

	inflightPages  map[string]struct{} // Pages currently leased to workers
	scheduledPages map[string]struct{} // Pages in StateDone/StateFailed waiting for NextFetchAt
	deadPages      map[uint64]struct{} // Compact 8-byte tombstones for retired/disallowed URLs
	knownNextFetch map[uint64]int64    // 16-byte markers for pages evicted to PostgreSQL (fp -> nextFetchUnix)
	contentOwners  map[uint64]string   // Maps 64-bit content hash -> canonical owner URL (Content-Seen test)

	// Write-behind dirty buffers for PostgreSQL batch flushing
	dirtyHosts map[string]struct{}
	dirtyPages map[string]struct{}
	dirtyDead  map[int64]deadRecord
	rankDeltas map[int64]int

	robots *RobotsCache
	logger *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewShard creates and starts a Shard with default configuration.
func NewShard(parentCtx context.Context, logger *slog.Logger) *Shard {
	return NewShardWithConfig(parentCtx, 0, DefaultConfig(), nil, nil, logger)
}

// NewShardWithConfig creates and starts a Shard with custom configuration.
func NewShardWithConfig(parentCtx context.Context,
	id int,
	cfg Config,
	httpClient *http.Client,
	store *PostgresStore,
	logger *slog.Logger,
) *Shard {
	cfg = cfg.withDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(parentCtx)
	s := &Shard{
		id:             id,
		cfg:            cfg,
		store:          store,
		pages:          make(map[string]*domain.FrontierItem),
		hosts:          make(map[string]*HostState),
		pageQueues:     make(map[string]*PageQueue),
		inflightPages:  make(map[string]struct{}),
		scheduledPages: make(map[string]struct{}),
		deadPages:      make(map[uint64]struct{}),
		knownNextFetch: make(map[uint64]int64),
		contentOwners:  make(map[uint64]string),
		dirtyHosts:     make(map[string]struct{}),
		dirtyPages:     make(map[string]struct{}),
		dirtyDead:      make(map[int64]deadRecord),
		rankDeltas:     make(map[int64]int),
		robots:         NewRobotsCacheWithConfig(cfg, httpClient, logger),
		logger:         logger.With("component", "shard", "shard_id", id),
		ctx:            ctx,
		cancel:         cancel,
	}

	if s.store != nil {
		s.restoreFromStore(parentCtx)
	}

	s.wg.Add(1)
	go s.reaper()
	return s
}

func (s *Shard) restoreFromStore(ctx context.Context) {
	loadCtx, cancel := context.WithTimeout(ctx, dbFlushTimeout)
	defer cancel()

	snap, err := s.store.LoadShardState(loadCtx, s.id, s.cfg.MaxHostQueueSize)
	if err != nil {
		s.logger.Error("failed to restore shard state from postgres", "error", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.deadPages = snap.deadKeys
	s.knownNextFetch = snap.knownNextFetch
	s.contentOwners = snap.contentOwners

	for _, hs := range snap.hosts {
		s.hosts[hs.name] = hs
		q := &PageQueue{}
		heap.Init(q)
		s.pageQueues[hs.name] = q
	}

	for _, page := range snap.queuedPages {
		if _, ok := s.hosts[page.Host]; !ok {
			hs := &HostState{
				name:       page.Host,
				crawlDelay: s.cfg.DefaultCrawlDelay,
				readyAt:    time.Now(),
			}
			s.hosts[page.Host] = hs
			q := &PageQueue{}
			heap.Init(q)
			s.pageQueues[page.Host] = q
		}
		s.pages[page.URL] = page
		s.enqueuePageLocked(page)
	}

	clear(s.dirtyPages)
	clear(s.dirtyHosts)
}

// Close stops the background reaper goroutine and waits for it to exit.
func (s *Shard) Close() {
	s.cancel()
	s.wg.Wait()
	s.mu.Lock()
	s.logger.Debug("shard closed",
		"total_hosts", len(s.hosts),
		"active_pages", len(s.pages),
		"evicted_pages", len(s.knownNextFetch),
		"dead_pages", len(s.deadPages),
	)
	s.mu.Unlock()
}

func (s *Shard) stats() (hosts, activePages, deadPages int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.hosts), len(s.pages) + len(s.knownNextFetch), len(s.deadPages)
}

func urlFingerprint(rawURL string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(rawURL))
	return h.Sum64()
}

// retirePageLocked removes the full FrontierItem from memory and stores an 8-byte tombstone.
func (s *Shard) retirePageLocked(rawURL, host string) {
	if page, ok := s.pages[rawURL]; ok {
		if host == "" {
			host = page.Host
		}
		if page.ContentHash != 0 {
			if owner, exists := s.contentOwners[page.ContentHash]; exists && owner == rawURL {
				delete(s.contentOwners, page.ContentHash)
			}
		}
	}
	if host == "" {
		host = hostOf(rawURL)
	}

	fp := urlFingerprint(rawURL)
	delete(s.inflightPages, rawURL)
	delete(s.scheduledPages, rawURL)
	delete(s.pages, rawURL)
	delete(s.dirtyPages, rawURL)
	delete(s.knownNextFetch, fp)
	s.deadPages[fp] = struct{}{}

	if s.store != nil && host != "" {
		key := compositeURLKey(host, rawURL)
		s.dirtyDead[key] = deadRecord{
			urlKey: key,
			url:    rawURL,
			host:   host,
		}
	}
}

func (s *Shard) isDead(rawURL string) bool {
	fp := urlFingerprint(rawURL)
	s.mu.Lock()
	_, dead := s.deadPages[fp]
	s.mu.Unlock()
	return dead
}

// reaper periodically scans in-flight pages for lease timeouts and promotes
// scheduled pages (StateDone or StateFailed) back into the active queue once NextFetchAt arrives.
func (s *Shard) reaper() {
	defer s.wg.Done()
	t := time.NewTicker(s.cfg.ReaperInterval)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			s.requeue()
			if s.store != nil {
				s.flushAndRefill()
			}
		}
	}
}

func (s *Shard) requeue() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	threshold := now.Add(-s.cfg.LeaseTimeout)

	// 1. Recover stalled in-flight pages that exceeded LeaseTimeout.
	for url := range s.inflightPages {
		page := s.pages[url]
		if page.LastFetchAt.After(threshold) {
			continue
		}
		delete(s.inflightPages, url)
		page.State = domain.StateFailed
		page.FailCount++
		host := page.Host

		if page.FailCount <= s.cfg.MaxFailCount {
			page.NextFetchAt = now.Add(backoff(page.FailCount))
			s.scheduledPages[url] = struct{}{}
			s.logger.Warn("page lease expired, scheduled for retry",
				"url", page.URL,
				"fail_count", page.FailCount,
				"next_fetch_at", page.NextFetchAt,
			)
		} else {
			s.retirePageLocked(url, host)
			s.logger.Warn("page retired after max lease timeouts",
				"url", url,
				"fail_count", page.FailCount,
			)
		}
		s.releaseHostLocked(host)
	}

	// 2. Enqueue pages whose scheduled refresh or retry time (NextFetchAt) has arrived.
	for url := range s.scheduledPages {
		page, ok := s.pages[url]
		if !ok {
			delete(s.scheduledPages, url)
			continue
		}
		if now.Before(page.NextFetchAt) {
			continue
		}
		delete(s.scheduledPages, url)
		s.enqueuePageLocked(page)
	}
}

func (s *Shard) flushAndRefill() {
	s.mu.Lock()
	hosts := make([]*HostState, 0, len(s.dirtyHosts))
	for h := range s.dirtyHosts {
		if hs, ok := s.hosts[h]; ok {
			hsCopy := *hs
			hosts = append(hosts, &hsCopy)
		}
	}
	clear(s.dirtyHosts)

	pages := make([]*domain.FrontierItem, 0, len(s.dirtyPages))
	for u := range s.dirtyPages {
		if p, ok := s.pages[u]; ok {
			pCopy := *p
			pages = append(pages, &pCopy)
		}
	}
	clear(s.dirtyPages)

	dead := make([]deadRecord, 0, len(s.dirtyDead))
	for _, d := range s.dirtyDead {
		dead = append(dead, d)
	}
	clear(s.dirtyDead)

	deltas := make(map[int64]int, len(s.rankDeltas))
	for k, v := range s.rankDeltas {
		deltas[k] = v
	}
	clear(s.rankDeltas)

	// Evict completed/failed pages waiting for future NextFetchAt from RAM once queued for flush.
	now := time.Now()
	for u := range s.scheduledPages {
		if p, ok := s.pages[u]; ok && now.Before(p.NextFetchAt) {
			s.knownNextFetch[urlFingerprint(u)] = p.NextFetchAt.Unix()
			delete(s.scheduledPages, u)
			delete(s.pages, u)
		}
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), dbFlushTimeout)
	defer cancel()

	if err := s.store.FlushShard(ctx, s.id, hosts, pages, dead, deltas); err != nil {
		s.logger.Error("failed to flush shard state to postgres", "error", err)
		return
	}

	// Read-ahead refill: bring due pages from PostgreSQL back into the shard's active queue.
	duePages, err := s.store.FetchDuePages(ctx, s.id, time.Now(), dbRefillBatchSize)
	if err != nil {
		s.logger.Error("failed to fetch due pages from postgres", "error", err)
		return
	}
	if len(duePages) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range duePages {
		fp := urlFingerprint(item.URL)
		if _, isDead := s.deadPages[fp]; isDead {
			continue
		}
		if _, inRAM := s.pages[item.URL]; inRAM {
			continue
		}
		if q := s.pageQueues[item.Host]; q != nil && q.Len() >= s.cfg.MaxHostQueueSize {
			continue
		}
		if _, ok := s.hosts[item.Host]; !ok {
			hs := &HostState{
				name:       item.Host,
				crawlDelay: s.cfg.DefaultCrawlDelay,
				readyAt:    time.Now(),
			}
			s.hosts[item.Host] = hs
			q := &PageQueue{}
			heap.Init(q)
			s.pageQueues[item.Host] = q
		}
		delete(s.knownNextFetch, fp)
		s.pages[item.URL] = item
		s.enqueuePageLocked(item)
	}
}

// Push adds or updates a URL in the shard. Dead or disallowed URLs are ignored without
// keeping full FrontierItem structs in memory.
func (s *Shard) Push(ctx context.Context, item domain.NormalizedURL) error {
	if s.isDead(item.URL) {
		return nil
	}
	allowed, delay := s.robots.GetRobots(ctx, item.Host, item.URL)

	s.mu.Lock()
	defer s.mu.Unlock()

	// double check
	fp := urlFingerprint(item.URL)
	if _, dead := s.deadPages[fp]; dead {
		return nil
	}

	if !allowed {
		s.retirePageLocked(item.URL, item.Host)
		s.logger.DebugContext(ctx, "url disallowed by robots.txt", "url", item.URL, "host", item.Host)
		return nil
	}

	now := time.Now()

	hs, ok := s.hosts[item.Host]
	if !ok {
		hs = &HostState{
			name:       item.Host,
			crawlDelay: delay,
			readyAt:    now,
		}
		s.hosts[item.Host] = hs
		q := &PageQueue{}
		heap.Init(q)
		s.pageQueues[item.Host] = q
		s.dirtyHosts[item.Host] = struct{}{}
	} else if delay > 0 && hs.crawlDelay != delay {
		hs.crawlDelay = delay
		s.dirtyHosts[item.Host] = struct{}{}
	}

	// If the page was already crawled and evicted to PostgreSQL until its NextFetchAt,
	// record the PageRank increment without allocating a FrontierItem in RAM.
	if nextUnix, evicted := s.knownNextFetch[fp]; evicted && now.Unix() < nextUnix {
		if s.store != nil {
			s.rankDeltas[compositeURLKey(item.Host, item.URL)]++
		}
		return nil
	}

	page, exists := s.pages[item.URL]
	if !exists {
		if q := s.pageQueues[item.Host]; q != nil && q.Len() >= s.cfg.MaxHostQueueSize {
			return nil
		}
		page = &domain.FrontierItem{
			URL:          item.URL,
			Host:         item.Host,
			State:        domain.StateNew,
			PageRank:     1,
			DiscoveredAt: now,
		}
		s.pages[item.URL] = page
	} else {
		page.PageRank++
		s.dirtyPages[item.URL] = struct{}{}
	}

	switch page.State {
	case domain.StateQueued, domain.StateInFlight:
		return nil
	case domain.StateDone, domain.StateFailed:
		if now.Before(page.NextFetchAt) {
			return nil
		}
		delete(s.scheduledPages, item.URL)
	}

	s.enqueuePageLocked(page)
	return nil
}

func (s *Shard) enqueuePageLocked(page *domain.FrontierItem) {
	now := time.Now()
	page.Priority = s.countPagePriority(page, now)

	q := s.pageQueues[page.Host]
	heap.Push(q, &QueuedPage{
		URL:      page.URL,
		priority: page.Priority,
	})

	page.State = domain.StateQueued
	page.NextFetchAt = now
	s.dirtyPages[page.URL] = struct{}{}

	hs := s.hosts[page.Host]
	if q.Len() == s.cfg.MaxHostQueueSize && !hs.trapWarned {
		hs.trapWarned = true
		s.logger.Warn("host queue exceeded threshold, potential crawler trap",
			"host", page.Host,
			"queue_len", q.Len(),
		)
	}

	// Only place the host in HostsQueue if it is not already queued and has no active in-flight request.
	if !hs.inQueue && !hs.inFlight {
		earliestReady := hs.lastReq.Add(hs.crawlDelay)
		if now.After(earliestReady) {
			hs.readyAt = now
		} else {
			hs.readyAt = earliestReady
		}
		heap.Push(&s.hostsQueue, hs)
		hs.inQueue = true
		s.dirtyHosts[page.Host] = struct{}{}
	}
}

// Lease pops up to n ready URLs from hosts whose Crawl-Delay has expired.
// Each leased host is marked as inFlight until Ack, Nack, or lease expiration,
// guaranteeing at most one concurrent request per host.
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

		heap.Pop(&s.hostsQueue)
		top.inQueue = false

		q := s.pageQueues[top.name]
		var selected *domain.FrontierItem

		// Pop from the host's priority queue, skipping any stale entries
		// that were retired into deadPages while sitting in the heap.
		for q != nil && q.Len() > 0 {
			page := heap.Pop(q).(*QueuedPage)
			item, ok := s.pages[page.URL]
			if !ok || item == nil || item.State != domain.StateQueued {
				continue
			}
			selected = item
			break
		}

		if selected == nil {
			continue
		}

		selected.State = domain.StateInFlight
		selected.LastFetchAt = now
		s.inflightPages[selected.URL] = struct{}{}
		s.dirtyPages[selected.URL] = struct{}{}

		top.inFlight = true
		top.lastReq = now
		s.dirtyHosts[top.name] = struct{}{}

		out = append(out, selected.URL)
	}
	return out, nil
}

// Ack marks an in-flight URL as successfully processed without content deduplication
func (s *Shard) Ack(ctx context.Context, url string) error {
	_, err := s.AckWithHash(ctx, url, 0)
	return err
}

// AckWithHash marks an in-flight URL as successfully processed, performs the Content-Seen
// duplicate test, and tracks content changes across recrawls.
// Returns true if the document is new or its content has changed.
func (s *Shard) AckWithHash(ctx context.Context, url string, contentHash uint64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.inflightPages[url]; !ok {
		return false, domain.ErrPageProcessingTimeoutExceeded
	}
	delete(s.inflightPages, url)

	page := s.pages[url]
	host := page.Host

	if contentHash != 0 {
		// 1. Content-Seen Test: check if another URL already owns this exact content fingerprint.
		if owner, exists := s.contentOwners[contentHash]; exists && owner != url {
			if ownerPage, ok := s.pages[owner]; ok && ownerPage != nil {
				ownerPage.PageRank += page.PageRank
				s.dirtyPages[owner] = struct{}{}
			} else if s.store != nil {
				s.rankDeltas[compositeURLKey(hostOf(owner), owner)] += page.PageRank
			}
			s.retirePageLocked(url, host)
			s.releaseHostLocked(host)
			s.logger.DebugContext(ctx, "duplicate content detected, retiring alias URL",
				"url", url,
				"canonical_owner", owner,
			)
			return false, nil
		}
	}

	now := time.Now()
	prevFetchCount := page.FetchCount
	prevHash := page.ContentHash

	page.LastFetchAt = now
	page.FetchCount++
	page.FailCount = 0
	page.State = domain.StateDone

	shouldProcess := true
	if contentHash != 0 {
		if prevFetchCount == 0 || prevHash == 0 {
			// First successful fetch of this URL.
			page.ContentHash = contentHash
			s.contentOwners[contentHash] = url
		} else if prevHash != contentHash {
			// Page content changed since last crawl.
			if owner, exists := s.contentOwners[prevHash]; exists && owner == url {
				delete(s.contentOwners, prevHash)
			}
			page.ContentHash = contentHash
			page.ChangeCount++
			s.contentOwners[contentHash] = url
		} else {
			// Page was recrawled and content is identical to the previous snapshot.
			shouldProcess = false
		}
	}

	page.NextFetchAt = now.Add(s.nextInterval(page))
	s.scheduledPages[url] = struct{}{}
	s.dirtyPages[url] = struct{}{}

	s.releaseHostLocked(host)
	return shouldProcess, nil
}

// Nack marks an in-flight URL as failed. If FailCount has not exceeded MaxFailCount,
// the page is scheduled for retry with exponential backoff and jitter; otherwise it is retired.
func (s *Shard) Nack(ctx context.Context, url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.inflightPages[url]; !ok {
		return domain.ErrPageProcessingTimeoutExceeded
	}
	delete(s.inflightPages, url)

	now := time.Now()
	page := s.pages[url]
	page.LastFetchAt = now
	page.FailCount++
	page.State = domain.StateFailed
	host := page.Host

	if page.FailCount <= s.cfg.MaxFailCount {
		page.NextFetchAt = now.Add(backoff(page.FailCount))
		s.scheduledPages[url] = struct{}{}
		s.dirtyPages[url] = struct{}{}
	} else {
		s.retirePageLocked(url, host)
		s.logger.WarnContext(ctx, "page retired after exceeding max fail count",
			"url", url,
			"fail_count", page.FailCount,
		)
	}

	s.releaseHostLocked(host)
	return nil
}

// MarkDead immediately retires a URL (e.g., on HTTP 404/410/403 or unsupported MIME type),
// freeing its FrontierItem memory and leaving an 8-byte tombstone so it is not recrawled.
func (s *Shard) MarkDead(ctx context.Context, url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var host string
	if page, ok := s.pages[url]; ok && page != nil {
		host = page.Host
	} else {
		host = hostOf(url)
	}

	_, wasInFlight := s.inflightPages[url]
	s.retirePageLocked(url, host)

	if wasInFlight && host != "" {
		s.releaseHostLocked(host)
	}
	s.logger.DebugContext(ctx, "page marked dead and retired", "url", url, "host", host)
	return nil
}

func (s *Shard) releaseHostLocked(host string) {
	hs, ok := s.hosts[host]
	if !ok {
		return
	}

	now := time.Now()
	hs.inFlight = false
	hs.lastReq = now
	hs.readyAt = now.Add(hs.crawlDelay)
	s.dirtyHosts[host] = struct{}{}

	// Re-insert the host into HostsQueue only if it still has queued pages waiting.
	if !hs.inQueue && s.pageQueues[host].Len() > 0 {
		heap.Push(&s.hostsQueue, hs)
		hs.inQueue = true
	}
}

// countPagePriority computes a page's crawl priority combining:
//  1. Log-smoothed link importance (PageRank / in-degree)
//  2. Novelty boost for uncrawled pages or Poisson staleness urgency for recrawls
//  3. Quadratic penalty for consecutive fetch failures
//  4. Structural URL depth penalty to mitigate crawler traps
func (s *Shard) countPagePriority(page *domain.FrontierItem, now time.Time) float64 {
	importance := 1.0 + math.Log1p(float64(page.PageRank))

	var urgency float64
	if page.FetchCount == 0 {
		waitHours := math.Max(0, now.Sub(page.DiscoveredAt).Hours())
		urgency = newPageBaseUrgency + waitingAgeFactor*waitHours
	} else {
		changeRate := estimateChangeRate(page)
		interval := s.nextInterval(page)
		elapsed := math.Max(0, float64(now.Sub(page.LastFetchAt)))
		stalenessProb := 1.0 - math.Exp(-elapsed/float64(interval))
		urgency = (1.0 + changeRate) * stalenessProb
	}

	failPenalty := 1.0 / (1.0 + float64(page.FailCount*page.FailCount))

	slashes := strings.Count(page.URL, "/")
	extraDepth := math.Max(0, float64(slashes-3))
	depthPenalty := 1.0 / (1.0 + depthSlashPenalty*extraDepth)

	return importance * urgency * failPenalty * depthPenalty
}

// estimateChangeRate computes the Laplace-smoothed empirical change frequency of a page.
func estimateChangeRate(page *domain.FrontierItem) float64 {
	return float64(page.ChangeCount+1) / float64(page.FetchCount+2)
}

// nextInterval computes an adaptive recrawl interval bounded by [MinRefreshInterval, MaxRefreshInterval].
func (s *Shard) nextInterval(page *domain.FrontierItem) time.Duration {
	if page.FetchCount == 0 {
		return s.cfg.BaseRefreshInterval
	}

	changeRate := estimateChangeRate(page)
	importanceBoost := 1.0 + 0.25*math.Log1p(float64(page.PageRank))
	scale := (1.0 - 0.75*changeRate) / importanceBoost

	interval := time.Duration(float64(s.cfg.BaseRefreshInterval) * scale)
	if interval < s.cfg.MinRefreshInterval {
		return s.cfg.MinRefreshInterval
	}
	if interval > s.cfg.MaxRefreshInterval {
		return s.cfg.MaxRefreshInterval
	}
	return interval
}

// backoff computes an exponential retry delay (2^fails minutes, capped at 64m)
// with +/-20% uniform jitter to prevent synchronized retry storms.
func backoff(fails int) time.Duration {
	if fails < 1 {
		fails = 1
	}
	if fails > 6 {
		fails = 6
	}
	base := float64(time.Duration(1<<fails) * time.Minute)
	// Jitter in range [0.8 * base, 1.2 * base]
	jitter := 0.8 + 0.4*rand.Float64()
	return time.Duration(base * jitter)
}
