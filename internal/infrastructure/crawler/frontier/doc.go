// Package frontier provides a high-performance, sharded URL queue for web crawling.
//
// # Architecture
//
// The frontier is designed to support "polite crawling" by enforcing per-host
// rate limits (Crawl-Delay) while maximizing throughput via concurrency.
// To avoid global lock contention under high load, the state is partitioned
// into multiple independent shards.
//
// The architecture consists of three main layers:
//   - [Frontier]: The main entry point that routes URLs to specific [Shard] instances
//     based on the host hash (FNV-1a).
//   - [Shard]: An independent worker managing its own state, host queues, and a
//     background reaper goroutine for fault tolerance.
//   - Priority Queues: Each shard uses a min-heap for hosts ([HostsQueue]) sorted
//     by readiness time, and a max-heap for pages ([PageQueue]) sorted by priority.
//
// # State Machine
//
// URLs processed by the crawler follow a Lease/Ack/Nack semantic to ensure
// no data is lost even if a worker crashes:
//
//  1. Push: A new URL is discovered and added to the queue (StateQueued).
//  2. Lease: A worker requests a batch of URLs. They transition to StateInFlight.
//  3. Ack: The worker successfully processed the URL. It transitions to StateDone.
//  4. Nack: The worker failed to process the URL. It is requeued with an exponential
//     backoff based on its failure count.
//
// If a worker crashes and never calls Ack or Nack, the shard's internal [Shard.reaper]
// will eventually detect the timeout and requeue the URL automatically.
package frontier
