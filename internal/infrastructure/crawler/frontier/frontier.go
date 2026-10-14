package frontier

import (
	"context"
	"hash/fnv"
	"log/slog"
	"net/url"
	"runtime"
	"sync/atomic"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

var _ domain.IFrontier = (*Frontier)(nil)

// Frontier is the main orchestrator for the URL queue.
// It acts as a router, distributing incoming URLs across multiple [Shard] instances
// based on the hostname hash. This design ensures that operations on different hosts
// do not block each other.
type Frontier struct {
	shards []*Shard
	rr     atomic.Uint64 // round-robin counter for fair shard leasing
	logger *slog.Logger
}

// NewFrontier creates a new sharded Frontier.
// If shardsCount is <= 0, it defaults to 2 * runtime.NumCPU() to maximize
// parallel processing capabilities of the host machine.
func NewFrontier(ctx context.Context, shardsCount int, logger *slog.Logger) *Frontier {
	if shardsCount <= 0 {
		shardsCount = 2 * runtime.NumCPU()
	}
	f := &Frontier{
		shards: make([]*Shard, shardsCount),
		logger: logger.With("component", "frontier"),
	}
	for i := range f.shards {
		f.shards[i] = NewShard(ctx, logger)
	}
	f.logger.Info("frontier started", "shards", shardsCount)
	return f
}

func (f *Frontier) Close() {
	for _, s := range f.shards {
		s.Close()
	}
}

// Close gracefully shuts down all shards
func (f *Frontier) hostToShard(host string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(host))
	return int(h.Sum32()%uint32(len(f.shards)))
}

// Push adds a batch of normalized URLs to the frontier.
// Each URL is routed to the appropriate shard based on its host.
// Returns an error if any underlying shard fails to enqueue the item.
func (f *Frontier) Push(ctx context.Context, items []domain.NormalizedURL) error {
	for _, item := range items {
		shardIndex := f.hostToShard(item.Host)
		if err := f.shards[shardIndex].Push(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

// Lease retrieves up to 'n' URLs ready for crawling.
// It uses a round-robin algorithm across shards to ensure fair distribution
// of work and prevent starvation of specific hosts.
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


// Ack acknowledges successful processing of a URL.
func (f *Frontier) Ack(ctx context.Context, url string) error {
	return f.shards[f.hostToShard(hostOf(url))].Ack(ctx, url)
}

// Nack signals that processing of a URL failed.
func (f *Frontier) Nack(ctx context.Context, url string) error {
	return f.shards[f.hostToShard(hostOf(url))].Nack(ctx, url)
}

// hostOf extracts the host component from a raw URL string.
func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}
