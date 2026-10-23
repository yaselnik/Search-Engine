# Search Engine in Go

A full-text web search engine written in Go and backed by PostgreSQL. It features a concurrent web crawler, a single-pass HTML5 content extractor, a bilingual morphological analysis pipeline (English & Russian Snowball stemmers), an inverted index with positional and zoned postings, and a **BM25F + Term Proximity** ranking engine with trigram typo tolerance.

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
- **Single-pass HTML5 parser** (`golang.org/x/net/html`) that strips navigation boilerplate (`<nav>`, `<footer>`, `<aside>`), preserves semantic article headers, prevents word concatenation across block elements, and respects `noindex` / `nofollow` directives.

### 2. Multilingual Text Analysis Pipeline
- Fast single-pass UTF-8 tokenizer that preserves compound technical tokens (`utf-8`, `go-1.22`, `wi-fi`, `user_id`) while recording exact 1-based ordinal positions and byte offsets.
- Case folding and Cyrillic normalization (`ё` $\rightarrow$ `е`).
- Full **Porter2 (English)** and **Snowball (Russian)** morphological stemmers verified against the official Snowball test suites.

### 3. Zoned Inverted Index & Storage (PostgreSQL)
- Separates document **Title** and **Content** token frequencies and positions to prevent title length from skewing content byte offsets.
- Write-through in-memory cache of corpus statistics and document field lengths for zero-latency ranking calculations.
- Batched SQL operations (`pgx.Batch` and `ANY($1)` array queries) eliminating $N+1$ round-trips.
- Lazy loading of byte offsets (`GetOffsetsBatch`) exclusively for the final top-$K$ ranked documents.

### 4. Ranking & Search Quality
- **BM25F Ranking:** Independent length normalization and weighting for title matches (`TitleWeight = 3.0`, `BTitle = 0.3`) and body content (`BContent = 0.75`).
- **Term Proximity & Phrase Boosting:** Two-pointer positional distance evaluation ($O(L_1 + L_2)$) rewarding documents where query terms appear consecutively or within a tight window.
- **Query Coverage (Coordination):** Rewards documents matching a higher ratio of unique query terms.
- **Typo Tolerance (`pg_trgm`):** Automatic fuzzy fallback against a GIN-indexed dictionary of unique terms (`index_terms`) when exact query tokens yield zero hits.
- **Smart Contextual Snippets:** Sliding-window snippet generator that maximizes distinct query term diversity, aligns boundaries to word edges, and guarantees UTF-8 rune safety.
- **$O(N \log K)$ Top-$K$ Selection:** Bounded min-heap (`container/heap`) to select top results without sorting the entire candidate set.

---

## Architecture & Project Structure

The project follows Clean Architecture principles with strict separation between domain models, application use cases, infrastructure adapters, and HTTP delivery:

```text
.
├── cmd/
│   └── api/                    # Application entrypoint & dependency wiring
├── internal/
│   ├── domain/                 # Core entities (Document, Token, Posting), interfaces, and errors
│   ├── usecase/
│   │   ├── crawler/            # Concurrent crawler orchestrator (fetcher & parser pools)
│   │   ├── indexer/            # Incremental batch indexing use case (separate title & content)
│   │   └── searcher/           # Query execution, BM25F + proximity ranking, top-K heap & snippets
│   ├── infrastructure/
│   │   ├── analyzer/           # UTF-8 tokenizer, filters, and English/Russian Snowball stemmers
│   │   ├── crawler/
│   │   │   ├── fetcher/        # Tuned HTTP client with streaming content hashing
│   │   │   ├── frontier/       # Sharded URL frontier, LRU robots cache, Postgres store
│   │   │   ├── parser/         # Single-pass HTML5 parser and boilerplate filter
│   │   │   └── urlfilter/      # URL canonicalization and trap filtering
│   │   ├── index/              # PostgreSQL inverted index implementation & trigram dictionary
│   │   ├── ranker/             # Field-weighted Okapi BM25 (BM25F) ranker
│   │   └── storage/            # PostgreSQL document storage with partial unindexed index
│   └── delivery/
│       └── http/               # REST API handlers, DTOs, and logging middleware
├── migrations/                 # Embedded goose SQL migrations (frontier, storage, index)
└── docker-compose.yml          # PostgreSQL databases setup
```

## How Search Works

### 1. Query Analysis
- The incoming query string is tokenized, normalized (`LowercaseFilter`), and stemmed (`MultiLanguageStemmer`). If the query contains meaningful terms alongside stop words, noise stop words are filtered out while pure stop-word queries (e.g., _"to be or not to be"_) are kept intact.

### 2. Batch Posting Retrieval & Fuzzy Fallback
- Unique query terms are fetched in a single round-trip via `GetPostingsBatch` (excluding heavy byte offsets). Any term with zero hits triggers `SuggestTerms` using PostgreSQL's `pg_trgm` similarity operator (`%`) to recover from typos.

### 3. Scoring (BM25F + Coverage + Proximity)
- Base score is computed across `Title` and `Content` zones using cached document lengths.
- For multi-word queries, scores are scaled by **Query Coverage** and boosted by **Term Proximity** based on minimum pairwise token distances in both title and body.

### 4. Top-K Selection & Enrichment
- A bounded min-heap selects the top `limit` documents in $O(N \log K)$ time.
- `GetOffsetsBatch` and `GetDocumentsByIDs` fetch byte offsets and full document records only for those `limit` winners.
- `generateSnippet` locates the 200-byte window with the highest diversity and density of matched query terms.


## Getting Started

### Prerequisites

- **Go** 1.23+

- **Docker** & **Docker Compose** (for PostgreSQL 16+ with `pg_trgm`)


### 1. Start PostgreSQL Databases

```bash
$ docker compose up -d
```

### 2. Run the Application

Database migrations are embedded and executed automatically on startup via `goose`:

```bash
$ go run ./cmd/api
```
When stopped via Ctrl+C (SIGINT/SIGTERM), the engine gracefully drains in-flight parsed pages, flushes all dirty frontier shards to PostgreSQL, and resumes from the saved state on the next run.

## CLI Flags

|Flag|Default|Description|
|---|---|---|
|`-addr`|`:8080`|HTTP server listen address|
|`-log-level`|`info`|Structured log level (debug, info, warn, error)|
|`-index-interval`|`2s`|Background indexing poll interval|
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
```bash
# Run all unit tests (including official English & Russian Snowball test suites)
$ go test -v ./...

# Run tokenizer benchmarks
go test -bench=. -benchmem ./internal/infrastructure/analyzer/...
```

## References
1. A. Heydon and M. Najork, "Mercator: A scalable, extensible Web crawler," World Wide Web, vol. 2, no. 4, pp. 219–229, 1999.
2. C. Olston and M. Najork, "Web Crawling," Foundations and Trends in Information Retrieval, vol. 4, no. 3, pp. 175–246, 2010.
3. [The English (Porter2) stemming algorithm](https://snowballstem.org/algorithms/english/stemmer.html)
