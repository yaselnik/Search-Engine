package frontier

import (
	"context"
	"errors"
	"hash/fnv"
	"log/slog"
	"net/url"
	"sync"
	"sync/atomic"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

var _ domain.IFrontier = (*Frontier)(nil)

// Frontier is the main orchestrator for the sharded URL queue.
// It routes URLs to their owning shard by hostname hash so that politeness state
// and robots.txt caches for any given host are completely localized to a single shard.
type Frontier struct {
	shards []*Shard
	rr     atomic.Uint64 // round-robin counter for fair shard leasing
	logger *slog.Logger
}

// NewFrontier creates a new Frontier with the specified number of shards and default settings.
func NewFrontier(ctx context.Context, shardsCount int, logger *slog.Logger, opts ...Option) *Frontier {
	cfg := DefaultConfig()
	if shardsCount > 0 {
		cfg.ShardsCount = shardsCount
	}
	return NewFrontierWithConfig(ctx, cfg, logger, opts...)
}

// NewFrontierWithConfig creates a new Frontier using the provided Config and runtime Options.
func NewFrontierWithConfig(ctx context.Context, cfg Config, logger *slog.Logger, opts ...Option) *Frontier {
	cfg = cfg.withDefaults()
	if logger == nil {
		logger = slog.Default()
	}

	var o options
	for _, opt := range opts {
		opt(&o)
	}

	f := &Frontier{
		shards: make([]*Shard, cfg.ShardsCount),
		logger: logger.With("component", "frontier"),
	}
	for i := range f.shards {
		f.shards[i] = NewShardWithConfig(ctx, i, cfg, o.httpClient, logger)
	}
	f.logger.Info("frontier started", "shards", cfg.ShardsCount)
	return f
}

// Close gracefully shuts down all underlying shards and logs aggregated statistics.
func (f *Frontier) Close() {
	var totalHosts, activePages, deadPages int
	for _, s := range f.shards {
		h, a, d := s.stats()
		totalHosts += h
		activePages += a
		deadPages += d
		s.Close()
	}
	f.logger.Info("frontier closed",
		"shards", len(f.shards),
		"total_hosts", totalHosts,
		"active_pages", activePages,
		"dead_pages", deadPages,
	)
}

func (f *Frontier) hostToShard(host string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(host))
	return int(h.Sum32() % uint32(len(f.shards)))
}

// Push adds a batch of normalized URLs to the frontier.
// If the batch spans multiple shards, items are grouped by shard and pushed concurrently
// so a slow robots.txt fetch in one shard never blocks unrelated shards.
func (f *Frontier) Push(ctx context.Context, items []domain.NormalizedURL) error {
	if len(items) == 0 {
		return nil
	}

	buckets := make(map[int][]domain.NormalizedURL)
	for _, item := range items {
		idx := f.hostToShard(item.Host)
		buckets[idx] = append(buckets[idx], item)
	}

	// Fast-path: if all URLs target the same shard, avoid spawning goroutines.
	if len(buckets) == 1 {
		for idx, urls := range buckets {
			var errs []error
			for _, u := range urls {
				if err := f.shards[idx].Push(ctx, u); err != nil {
					errs = append(errs, err)
				}
			}
			return errors.Join(errs...)
		}
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)

	for shardIdx, batch := range buckets {
		wg.Add(1)
		go func(idx int, urls []domain.NormalizedURL) {
			defer wg.Done()
			for _, u := range urls {
				if err := f.shards[idx].Push(ctx, u); err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
			}
		}(shardIdx, batch)
	}

	wg.Wait()
	return errors.Join(errs...)
}

// Lease retrieves up to n URLs ready for crawling across all shards using round-robin order.
func (f *Frontier) Lease(ctx context.Context, n int) ([]string, error) {
	out := make([]string, 0, n)

	start := int(f.rr.Add(1)) % len(f.shards)
	for i := 0; i < len(f.shards) && len(out) < n; i++ {
		idx := (start + i) % len(f.shards)
		batch, err := f.shards[idx].Lease(ctx, n-len(out))
		if err != nil {
			return out, err
		}
		out = append(out, batch...)
	}
	return out, nil
}

// Ack acknowledges successful processing of a leased URL.
func (f *Frontier) Ack(ctx context.Context, rawURL string) error {
	return f.shards[f.hostToShard(hostOf(rawURL))].Ack(ctx, rawURL)
}

// AckWithHash acknowledges successful processing of a leased URL, records its content fingerprint,
// and returns true if the page should be parsed and indexed.
func (f *Frontier) AckWithHash(ctx context.Context, rawURL string, contentHash uint64) (bool, error) {
	return f.shards[f.hostToShard(hostOf(rawURL))].AckWithHash(ctx, rawURL, contentHash)
}

// Nack signals that processing of a leased URL failed and triggers retry/backoff logic.
func (f *Frontier) Nack(ctx context.Context, rawURL string) error {
	return f.shards[f.hostToShard(hostOf(rawURL))].Nack(ctx, rawURL)
}

// MarkDead permanently retires a URL and frees its metadata from active memory.
func (f *Frontier) MarkDead(ctx context.Context, rawURL string) error {
	return f.shards[f.hostToShard(hostOf(rawURL))].MarkDead(ctx, rawURL)
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}
