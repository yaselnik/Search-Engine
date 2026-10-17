# Search Engine in Go

A scalable, full-text web search engine written in Go following Clean Architecture principles. The system combines an incremental polite web crawler, a single-pass HTML5 parser, a composable text analysis pipeline with Porter2 (Snowball) stemming, an inverted index, and a BM25 ranking engine backed by PostgreSQL.

---

## Key Features

### 1. Scalable & Polite Web Crawler
Designed around the architectural blueprints of **Mercator** (*Heydon & Najork, 1999*) and **Web Crawling** (*Olston & Najork, 2010*):
- **Sharded URL Frontier:** Partitions hosts across $2 \times \text{NumCPU}$ independent shards via `FNV-1a` hashing to eliminate global lock contention.
- **Strict Politeness Guarantees:** Enforces at most one active in-flight request per host and honors per-host `Crawl-Delay` directives using a min-heap host scheduler (`HostsQueue`) paired with per-host priority max-heaps (`PageQueue`).
- **Bounded LRU `robots.txt` Cache:** Coalesces concurrent requests to the same host (singleflight pattern) and caches parsed rules with TTL, HTTP `Expires`, and negative-caching support.
- **Multi-Factor Crawl Ordering:** Prioritizes pages by combining log-smoothed in-degree (`PageRank`), Poisson staleness/novelty urgency, consecutive error penalties, and URL path depth penalties.
- **Content-Seen Deduplication & Freshness Tracking:** Computes streaming 64-bit `FNV-1a` content fingerprints during download (`io.TeeReader`) to suppress mirror/alias processing and adapt recrawl intervals based on empirical page change rates.
- **Crawler Trap & SSRF Protection:** Canonicalizes URLs, strips default ports and fragments, rejects query strings and private/loopback network addresses, filters non-HTML extensions, caps per-host queue budgets (`MaxHostQueueSize`), and compacts dead/disallowed URLs into 8-byte tombstones.

### 2. Single-Pass HTML5 Parser
- Extracts `<title>`, visible body text, ISO 639-1 language codes (`<html lang>`), `<base href>`, and outgoing links in a single DOM traversal.
- Strips boilerplate text (`nav`, `header`, `footer`, `aside`, `script`, `style`, `svg`) from the text index while still discovering navigation links.
- Respects `<meta name="robots" content="noindex, nofollow">` and `<a rel="nofollow">` directives.

### 3. Composable Text Analyzer & BM25 Search
- **Tokenization & Filtering:** Splits text into tokens with positional and byte-offset metadata and applies chainable normalization filters (`LowercaseFilter`).
- **Snowball (Porter2) Stemming:** Full implementation of the English Porter2 stemming specification with language-aware routing (`MultiLanguageStemmer`).
- **Inverted Index & BM25 Ranking:** Stores positional postings lists and scores documents using the Okapi BM25 probabilistic retrieval model.

### 4. Microservice-Ready Persistence Layer
- Supports both **In-Memory** execution and **PostgreSQL** persistence split across three isolated databases (`frontier_db`, `storage_db`, `index_db`) with embedded `goose` SQL migrations.
- Uses Mercator's 64-bit composite keys `(HostHash24 << 40) | URLHash40` to preserve host locality in PostgreSQL B-Tree pages, batched `UNNEST` write-behind flushes, and read-ahead queue refills.

---

## Project Structure

```text
.
├── cmd/
│   └── api/                  # Application entrypoint & dependency wiring
├── internal/
│   ├── domain/               # Core entities, interfaces, and domain errors
│   ├── usecase/
│   │   ├── crawler/          # Concurrent crawler orchestrator (fetcher & parser pools)
│   │   ├── indexer/          # Incremental batch indexing use case
│   │   └── searcher/         # Query execution and BM25 ranking use case
│   ├── infrastructure/
│   │   ├── analyzer/         # Tokenizer, filters, and Porter2 (Snowball) stemmer
│   │   ├── crawler/
│   │   │   ├── fetcher/      # Tuned HTTP client with streaming content hashing
│   │   │   ├── frontier/     # Sharded URL frontier, LRU robots cache, Postgres store
│   │   │   ├── parser/       # Single-pass HTML5 parser
│   │   │   └── urlfilter/    # URL canonicalization and trap filtering
│   │   ├── index/            # In-memory and PostgreSQL inverted index implementations
│   │   ├── ranker/           # Okapi BM25 ranker
│   │   └── storage/          # In-memory and PostgreSQL document storage
│   └── delivery/
│       └── http/             # REST API handlers, DTOs, and logging middleware
├── migrations/               # Embedded goose SQL migrations (frontier, storage, index)
└── docker-compose.yml        # PostgreSQL databases setup
```

## Getting Started
**Prerequisites**
- Go 1.23+
- Docker & Docker Compose (for PostgreSQL persistence)

## Run with PostgreSQL (Default)
1. Start the PostgreSQL containers:
```bash
$ docker compose up -d
```
2. Run the application (database migrations are applied automatically on startup):
```bash
$ go run ./cmd/api -log-level info -addr :8080
```
When stopped via Ctrl+C (SIGINT/SIGTERM), the engine gracefully drains in-flight parsed pages, flushes all dirty frontier shards to PostgreSQL, and resumes from the saved state on the next run.

## Run in In-Memory Mode
To run the search engine purely in RAM without Docker or PostgreSQL:
```bash
$ go run ./cmd/api -in-memory -log-level info
```

## CLI Flags

|Flag|Default|Description|
|---|---|---|
|`-addr`|`:8080`|HTTP server listen address|
|`-log-level`|`info`|Structured log level (debug, info, warn, error)|
|`-index-interval`|`2s`|Background indexing poll interval|
|`-in-memory`|`false`|Use in-memory storage and index instead of PostgreSQL|
|`-frontier-dsn`|`postgres://postgres:password@localhost:5432/frontier_db?sslmode=disable`|PostgreSQL DSN for the URL frontier|
|`-storage-dsn`|`postgres://postgres:password@localhost:5432/storage_db?sslmode=disable`|PostgreSQL DSN for document storage|
|`-index-dsn`|`postgres://postgres:password@localhost:5432/index_db?sslmode=disable`|PostgreSQL DSN for the inverted index|


## API Endpoints
1. Search Documents
```HTTP
GET /api/search?q={query}&limit={limit}
```
Example:
```bash
$ curl -s "http://localhost:8080/api/search?q=concurrency+golang&limit=5" | jq
```
2. Get Document by ID
```HTTP
GET /api/documents/{id}
```
Example:
```bash
$ curl -s "http://localhost:8080/api/documents/14303878688496596720" | jq
```

## Running Tests
Run the full unit test suite across all packages (all network calls are mocked in-memory):
```bash
$ go test -v -race ./...
```

## References
1. A. Heydon and M. Najork, "Mercator: A scalable, extensible Web crawler," World Wide Web, vol. 2, no. 4, pp. 219–229, 1999.
2. C. Olston and M. Najork, "Web Crawling," Foundations and Trends in Information Retrieval, vol. 4, no. 3, pp. 175–246, 2010.
3. [The English (Porter2) stemming algorithm](https://snowballstem.org/algorithms/english/stemmer.html)
