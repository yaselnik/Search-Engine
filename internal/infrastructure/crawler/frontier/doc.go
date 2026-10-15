// Package frontier provides a high-performance, sharded URL queue for polite web crawling.
//
// # Architecture
//
// The frontier enforces per-host politeness constraints (at most one active in-flight
// request per host and Crawl-Delay compliance) while maximizing throughput across
// multiple hosts via sharding:
//   - [Frontier]: The main orchestrator and router that partitions incoming URLs across
//     [Shard] instances based on an FNV-1a hash of the hostname.
//   - [Shard]: An isolated state partition managing host readiness queues ([HostsQueue]),
//     per-host priority queues ([PageQueue]), scheduled retry/refresh sets, compact
//     tombstones for dead URLs, and a background reaper goroutine.
//   - [RobotsCache]: A bounded, thread-safe LRU cache with singleflight request coalescing,
//     TTL/HTTP Expires support, and negative caching for robots.txt rules.
//   - [Config]: A serializable configuration struct (YAML/ENV compatible) paired with
//     functional [Option] helpers for injecting runtime dependencies such as custom HTTP clients.
//
// # State Machine
//
// URLs managed by the frontier follow a strict Lease/Ack/Nack/MarkDead lifecycle to
// guarantee fault tolerance, politeness, and bounded memory usage:
//
//  1. Push: A discovered URL is checked against the shard's dead-page tombstones and
//     the host's robots.txt rules (via URL path matching). Allowed URLs are scored by a
//     multi-factor priority formula (in-degree importance, Poisson staleness/novelty urgency,
//     failure penalty, and URL path depth) and placed into the host's [PageQueue] (StateQueued).
//  2. Lease: A worker leases a batch of ready URLs from hosts whose Crawl-Delay has expired
//     and which have no active in-flight requests. Both the page and the host transition to
//     an in-flight state (StateInFlight).
//  3. Ack / AckWithHash: The worker confirms a successful download. The page transitions to
//     StateDone, content change metrics are updated from the 64-bit content hash, the host lock
//     is released, and the page is placed into the scheduled set until its adaptive NextFetchAt.
//  4. Nack: The worker reports a transient failure. The page transitions to StateFailed,
//     the host lock is released, and the page is scheduled for retry using exponential backoff
//     with random jitter. Once FailCount exceeds MaxFailCount, the page is permanently retired.
//  5. MarkDead: The worker reports a permanent failure (e.g., HTTP 404/410/403 or unsupported
//     MIME type). The full FrontierItem is immediately evicted from memory and replaced with
//     an 8-byte FNV-64a tombstone in deadPages so the URL is never crawled again.
//
// If a worker crashes and never calls Ack, Nack, or MarkDead, the shard's background
// [Shard.reaper] detects the expired lease timeout, increments FailCount, releases the host,
// and either reschedules or retires the URL. The reaper also promotes due pages from
// StateDone and StateFailed back into [PageQueue] once their NextFetchAt timestamp arrives.
package frontier
