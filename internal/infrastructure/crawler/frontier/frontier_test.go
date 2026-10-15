package frontier

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newMockHTTPClient(handler func(req *http.Request) (int, string)) *http.Client {
	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			status, body := handler(req)
			return &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestFrontier_PolitenessAndSingleInFlight(t *testing.T) {
	ctx := context.Background()
	client := newMockHTTPClient(func(req *http.Request) (int, string) {
		return http.StatusOK, "User-agent: *\nAllow: /\n"
	})

	cfg := DefaultConfig()
	cfg.ShardsCount = 2
	cfg.DefaultCrawlDelay = 40 * time.Millisecond

	f := NewFrontierWithConfig(ctx, cfg, testLogger(), WithRobotsHTTPClient(client))
	defer f.Close()

	err := f.Push(ctx, []domain.NormalizedURL{
		{URL: "https://example.com/page1", Host: "example.com"},
		{URL: "https://example.com/page2", Host: "example.com"},
	})
	if err != nil {
		t.Fatalf("unexpected push error: %v", err)
	}

	leased, err := f.Lease(ctx, 10)
	if err != nil || len(leased) != 1 {
		t.Fatalf("expected 1 leased URL, got %v (err: %v)", leased, err)
	}

	_ = f.Push(ctx, []domain.NormalizedURL{
		{URL: "https://example.com/page3", Host: "example.com"},
	})
	leasedWhileInFlight, _ := f.Lease(ctx, 10)
	if len(leasedWhileInFlight) != 0 {
		t.Fatalf("expected 0 leased URLs while host is in-flight, got %v", leasedWhileInFlight)
	}

	if err := f.Ack(ctx, leased[0]); err != nil {
		t.Fatalf("ack failed: %v", err)
	}

	leasedBeforeDelay, _ := f.Lease(ctx, 10)
	if len(leasedBeforeDelay) != 0 {
		t.Fatalf("expected 0 leased URLs before crawl delay elapsed, got %v", leasedBeforeDelay)
	}

	time.Sleep(50 * time.Millisecond)
	leasedAfterDelay, _ := f.Lease(ctx, 10)
	if len(leasedAfterDelay) != 1 {
		t.Fatalf("expected 1 leased URL after crawl delay, got %v", leasedAfterDelay)
	}
}

func TestFrontier_PriorityOrderingAndDeduplication(t *testing.T) {
	ctx := context.Background()
	client := newMockHTTPClient(func(req *http.Request) (int, string) {
		return http.StatusOK, "User-agent: *\nAllow: /\n"
	})

	cfg := DefaultConfig()
	cfg.ShardsCount = 1
	cfg.DefaultCrawlDelay = 5 * time.Millisecond
	cfg.BaseRefreshInterval = 1 * time.Hour

	f := NewFrontierWithConfig(ctx, cfg, testLogger(), WithRobotsHTTPClient(client))
	defer f.Close()

	_ = f.Push(ctx, []domain.NormalizedURL{
		{URL: "https://go.dev/a/b/c/d/e/deep", Host: "go.dev"},
		{URL: "https://go.dev/shallow", Host: "go.dev"},
	})

	leased, _ := f.Lease(ctx, 1)
	if len(leased) != 1 || leased[0] != "https://go.dev/shallow" {
		t.Fatalf("expected shallow URL to be prioritized first, got %v", leased)
	}
	_ = f.AckWithHash(ctx, leased[0], 12345)

	_ = f.Push(ctx, []domain.NormalizedURL{
		{URL: "https://go.dev/shallow", Host: "go.dev"},
	})

	time.Sleep(10 * time.Millisecond)
	nextLeased, _ := f.Lease(ctx, 10)
	if len(nextLeased) != 1 || nextLeased[0] != "https://go.dev/a/b/c/d/e/deep" {
		t.Fatalf("expected only the deep URL to be leased, got %v", nextLeased)
	}
}

func TestFrontier_RobotsDisallowAndLRUCache(t *testing.T) {
	ctx := context.Background()
	var requestsCount atomic.Int32

	client := newMockHTTPClient(func(req *http.Request) (int, string) {
		requestsCount.Add(1)
		return http.StatusOK, "User-agent: *\nDisallow: /private\n"
	})

	cfg := DefaultConfig()
	cfg.ShardsCount = 1
	cfg.RobotsCacheCapacity = 2

	f := NewFrontierWithConfig(ctx, cfg, testLogger(), WithRobotsHTTPClient(client))
	defer f.Close()

	_ = f.Push(ctx, []domain.NormalizedURL{
		{URL: "https://site1.com/private/secret", Host: "site1.com"},
		{URL: "https://site1.com/public", Host: "site1.com"},
	})

	if requestsCount.Load() != 1 {
		t.Fatalf("expected 1 robots.txt request, got %d", requestsCount.Load())
	}

	leased, _ := f.Lease(ctx, 10)
	if len(leased) != 1 || leased[0] != "https://site1.com/public" {
		t.Fatalf("expected only /public to be leased, got %v", leased)
	}

	// Verify disallowed URL was retired into deadPages without allocating FrontierItem in s.pages.
	shard := f.shards[0]
	shard.mu.Lock()
	_, inPages := shard.pages["https://site1.com/private/secret"]
	_, inDead := shard.deadPages[urlFingerprint("https://site1.com/private/secret")]
	shard.mu.Unlock()
	if inPages || !inDead {
		t.Fatalf("expected disallowed URL to be stored only as tombstone, inPages=%v inDead=%v", inPages, inDead)
	}
}

func TestFrontier_ReaperMaxFailAndMarkDead(t *testing.T) {
	ctx := context.Background()
	client := newMockHTTPClient(func(req *http.Request) (int, string) {
		return http.StatusOK, "User-agent: *\nAllow: /\n"
	})

	cfg := DefaultConfig()
	cfg.ShardsCount = 1
	cfg.DefaultCrawlDelay = 5 * time.Millisecond
	cfg.LeaseTimeout = 30 * time.Millisecond
	cfg.ReaperInterval = 10 * time.Millisecond
	cfg.MaxFailCount = 1

	f := NewFrontierWithConfig(ctx, cfg, testLogger(), WithRobotsHTTPClient(client))
	defer f.Close()

	url1 := "https://flaky.com/page1"
	url2 := "https://flaky.com/notfound"
	_ = f.Push(ctx, []domain.NormalizedURL{
		{URL: url1, Host: "flaky.com"},
		{URL: url2, Host: "flaky.com"},
	})

	// Lease first URL and mark it dead immediately (simulating HTTP 404).
	leased, _ := f.Lease(ctx, 1)
	if len(leased) != 1 {
		t.Fatalf("expected 1 leased URL")
	}
	deadURL := leased[0]
	otherURL := url1
	if deadURL == url1 {
		otherURL = url2
	}

	if err := f.MarkDead(ctx, deadURL); err != nil {
		t.Fatalf("MarkDead failed: %v", err)
	}

	// Re-pushing deadURL must be ignored.
	_ = f.Push(ctx, []domain.NormalizedURL{{URL: deadURL, Host: "flaky.com"}})

	time.Sleep(10 * time.Millisecond)
	leasedNext, _ := f.Lease(ctx, 1)
	if len(leasedNext) != 1 || leasedNext[0] != otherURL {
		t.Fatalf("expected %s to be leased, got %v", otherURL, leasedNext)
	}

	// First failure on otherURL (FailCount = 1 <= MaxFailCount): stays in scheduledPages.
	_ = f.Nack(ctx, otherURL)
	shard := f.shards[0]
	shard.mu.Lock()
	_, scheduledAfterFirstFail := shard.scheduledPages[otherURL]
	shard.mu.Unlock()
	if !scheduledAfterFirstFail {
		t.Fatalf("expected page to be scheduled after 1st failure")
	}

	// Force immediate re-enqueue for 2nd attempt and let lease expire via reaper.
	shard.mu.Lock()
	delete(shard.scheduledPages, otherURL)
	shard.enqueuePageLocked(shard.pages[otherURL])
	shard.mu.Unlock()

	time.Sleep(10 * time.Millisecond)
	leased2, _ := f.Lease(ctx, 1)
	if len(leased2) != 1 {
		t.Fatalf("expected page to be leased for 2nd attempt")
	}

	// Wait for reaper to detect expired lease and retire the page into deadPages.
	time.Sleep(60 * time.Millisecond)
	shard.mu.Lock()
	_, inPages := shard.pages[otherURL]
	_, inDead := shard.deadPages[urlFingerprint(otherURL)]
	shard.mu.Unlock()

	if inPages || !inDead {
		t.Fatalf("expected page to be evicted from pages and added to deadPages, inPages=%v inDead=%v", inPages, inDead)
	}
}
