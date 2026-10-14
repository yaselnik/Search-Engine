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

type Frontier struct {
	shards []*Shard
	rr     atomic.Uint64 // for round-robin shard lease
	logger *slog.Logger
}

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

func (f *Frontier) hostToShard(host string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(host))
	return int(h.Sum32()%uint32(len(f.shards)))
}

func (f *Frontier) Push(ctx context.Context, items []domain.NormalizedURL) error {
	for _, item := range items {
		shardIndex := f.hostToShard(item.Host)
		if err := f.shards[shardIndex].Push(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

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

func (f *Frontier) Ack(ctx context.Context, url string) error {
	return f.shards[f.hostToShard(hostOf(url))].Ack(ctx, url)
}

func (f *Frontier) Nack(ctx context.Context, url string) error {
	return f.shards[f.hostToShard(hostOf(url))].Nack(ctx, url)
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}
