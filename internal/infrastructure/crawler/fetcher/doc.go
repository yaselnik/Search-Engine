// Package fetcher provides an HTTP client wrapper tailored for polite, high-throughput
// web crawling.
//
// # Features
//
//   - Granular Timeouts: configures dial, TLS handshake, response header, and overall
//     request timeouts to prevent slow or malicious servers from hanging worker goroutines.
//   - Error Classification: distinguishes between transient network/server errors
//     (HTTP 408, 429, 5xx, timeouts -> Nack with backoff) and permanent errors
//     (HTTP 400, 401, 403, 404, 410, non-HTML MIME types -> MarkDead), wrapping permanent
//     failures with [domain.ErrPermanentFetchFailure].
//   - Streaming Content Fingerprinting: computes a 64-bit FNV-1a content hash on the fly
//     via io.TeeReader while reading the bounded response body, enabling zero-copy
//     Content-Seen deduplication and change tracking in the Frontier.
//   - Bounded Body Reads & Redirect Policy: enforces a strict maximum body size and caps
//     redirect chains while preserving the crawler's User-Agent header.
package fetcher
