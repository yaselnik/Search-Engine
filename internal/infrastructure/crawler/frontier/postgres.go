package frontier

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yaselnik/Search-Engine/internal/domain"
	"github.com/yaselnik/Search-Engine/migrations"
)

// compositeURLKey implements host-locality fingerprinting:
// the high 24 bits derive from the hostname hash and the low 40 bits from the full URL hash,
// ensuring URLs from the same host are stored contiguously in PostgreSQL B-Tree pages.
func compositeURLKey(host, rawURL string) int64 {
	hHost := fnv.New32a()
	_, _ = hHost.Write([]byte(host))
	high24 := uint64(hHost.Sum32() & 0xFFFFFF)

	hURL := fnv.New64a()
	_, _ = hURL.Write([]byte(rawURL))
	low40 := hURL.Sum64() & 0xFFFFFFFFFF

	return int64((high24 << 40) | low40)
}

type deadRecord struct {
	urlKey int64
	url    string
	host   string
}

type shardSnapshot struct {
	hosts          []*HostState
	queuedPages    []*domain.FrontierItem
	knownNextFetch map[uint64]int64
	deadKeys       map[uint64]struct{}
	contentOwners  map[uint64]string
}

// PostgresStore manages persistent state for all Frontier shards in PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore connects to the frontier PostgreSQL database and runs embedded migrations.
func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("frontier postgres: create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("frontier postgres: ping: %w", err)
	}
	if err := migrations.Run(pool, "frontier"); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresStore{pool: pool}, nil
}

// Close closes the underlying PostgreSQL connection pool.
func (s *PostgresStore) Close() {
	s.pool.Close()
}

// LoadShardState restores hosts, queued/in-flight pages, content owners, and compact tombstones
// for a specific shard on startup.
func (s *PostgresStore) LoadShardState(ctx context.Context, shardID int, maxHostQueue int) (*shardSnapshot, error) {
	snap := &shardSnapshot{
		knownNextFetch: make(map[uint64]int64),
		deadKeys:       make(map[uint64]struct{}),
		contentOwners:  make(map[uint64]string),
	}

	// 1. Load host politeness states.
	hostRows, err := s.pool.Query(ctx, `
		SELECT host, crawl_delay, last_req_at, ready_at
		FROM frontier_hosts
		WHERE shard_id = $1`, int16(shardID))
	if err != nil {
		return nil, fmt.Errorf("frontier postgres: load hosts: %w", err)
	}
	defer hostRows.Close()

	for hostRows.Next() {
		var (
			name    string
			delayNs int64
			lastReq time.Time
			readyAt time.Time
		)
		if err := hostRows.Scan(&name, &delayNs, &lastReq, &readyAt); err != nil {
			return nil, fmt.Errorf("frontier postgres: scan host: %w", err)
		}
		snap.hosts = append(snap.hosts, &HostState{
			name:       name,
			crawlDelay: time.Duration(delayNs),
			lastReq:    lastReq,
			readyAt:    readyAt,
		})
	}
	if err := hostRows.Err(); err != nil {
		return nil, err
	}

	// 2. Load page states.
	pageRows, err := s.pool.Query(ctx, `
		SELECT url_key, url, host, state, priority, page_rank, fetch_count,
		       change_count, content_hash, fail_count, discovered_at, last_fetch_at, next_fetch_at
		FROM frontier_pages
		WHERE shard_id = $1`, int16(shardID))
	if err != nil {
		return nil, fmt.Errorf("frontier postgres: load pages: %w", err)
	}
	defer pageRows.Close()

	now := time.Now()
	hostCounts := make(map[string]int)

	for pageRows.Next() {
		var (
			urlKey      int64
			item        domain.FrontierItem
			stateRaw    int16
			contentHash int64
			failCount   int16
		)
		if err := pageRows.Scan(
			&urlKey,
			&item.URL,
			&item.Host,
			&stateRaw,
			&item.Priority,
			&item.PageRank,
			&item.FetchCount,
			&item.ChangeCount,
			&contentHash,
			&failCount,
			&item.DiscoveredAt,
			&item.LastFetchAt,
			&item.NextFetchAt,
		); err != nil {
			return nil, fmt.Errorf("frontier postgres: scan page: %w", err)
		}

		item.State = domain.PageState(stateRaw)
		item.ContentHash = uint64(contentHash)
		item.FailCount = int(failCount)
		fp := urlFingerprint(item.URL)

		if item.State == domain.StateNotAllowed {
			snap.deadKeys[fp] = struct{}{}
			continue
		}

		if item.ContentHash != 0 {
			snap.contentOwners[item.ContentHash] = item.URL
		}

		// Interrupted InFlight pages or ready Queued/Done/Failed pages are queued into RAM up to limit.
		if item.State == domain.StateQueued || item.State == domain.StateInFlight || !now.Before(item.NextFetchAt) {
			if hostCounts[item.Host] < maxHostQueue {
				itemCopy := item
				itemCopy.State = domain.StateQueued
				snap.queuedPages = append(snap.queuedPages, &itemCopy)
				hostCounts[item.Host]++
				continue
			}
		}

		// Pages scheduled for the future stay on disk; only keep a 16-byte marker in RAM.
		snap.knownNextFetch[fp] = item.NextFetchAt.Unix()
	}

	return snap, pageRows.Err()
}

// FlushShard writes dirty hosts, active/updated pages, dead tombstones, and PageRank increments
// to PostgreSQL in batched queries.
func (s *PostgresStore) FlushShard(
	ctx context.Context,
	shardID int,
	hosts []*HostState,
	pages []*domain.FrontierItem,
	dead []deadRecord,
	rankDeltas map[int64]int,
) error {
	if len(hosts) == 0 && len(pages) == 0 && len(dead) == 0 && len(rankDeltas) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("frontier postgres: begin flush tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Batch upsert hosts.
	if len(hosts) > 0 {
		names := make([]string, len(hosts))
		shards := make([]int16, len(hosts))
		delays := make([]int64, len(hosts))
		lastReqs := make([]time.Time, len(hosts))
		readyAts := make([]time.Time, len(hosts))

		for i, h := range hosts {
			names[i] = h.name
			shards[i] = int16(shardID)
			delays[i] = int64(h.crawlDelay)
			lastReqs[i] = h.lastReq
			readyAts[i] = h.readyAt
		}

		const hostQuery = `
			INSERT INTO frontier_hosts (host, shard_id, crawl_delay, last_req_at, ready_at)
			SELECT * FROM UNNEST($1::text[], $2::smallint[], $3::bigint[], $4::timestamptz[], $5::timestamptz[])
			ON CONFLICT (host) DO UPDATE SET
				crawl_delay = EXCLUDED.crawl_delay,
				last_req_at = EXCLUDED.last_req_at,
				ready_at    = EXCLUDED.ready_at`

		if _, err := tx.Exec(ctx, hostQuery, names, shards, delays, lastReqs, readyAts); err != nil {
			return fmt.Errorf("frontier postgres: flush hosts: %w", err)
		}
	}

	// 2. Batch upsert active/completed pages.
	if len(pages) > 0 {
		keys := make([]int64, len(pages))
		shards := make([]int16, len(pages))
		urls := make([]string, len(pages))
		hostNames := make([]string, len(pages))
		states := make([]int16, len(pages))
		priorities := make([]float64, len(pages))
		ranks := make([]int32, len(pages))
		fetches := make([]int32, len(pages))
		changes := make([]int32, len(pages))
		hashes := make([]int64, len(pages))
		fails := make([]int16, len(pages))
		discAts := make([]time.Time, len(pages))
		lastAts := make([]time.Time, len(pages))
		nextAts := make([]time.Time, len(pages))

		for i, p := range pages {
			keys[i] = compositeURLKey(p.Host, p.URL)
			shards[i] = int16(shardID)
			urls[i] = p.URL
			hostNames[i] = p.Host
			states[i] = int16(p.State)
			priorities[i] = p.Priority
			ranks[i] = int32(p.PageRank)
			fetches[i] = int32(p.FetchCount)
			changes[i] = int32(p.ChangeCount)
			hashes[i] = int64(p.ContentHash)
			fails[i] = int16(p.FailCount)
			discAts[i] = p.DiscoveredAt
			lastAts[i] = p.LastFetchAt
			nextAts[i] = p.NextFetchAt
		}

		const pageQuery = `
			INSERT INTO frontier_pages (
				url_key, shard_id, url, host, state, priority, page_rank,
				fetch_count, change_count, content_hash, fail_count,
				discovered_at, last_fetch_at, next_fetch_at
			)
			SELECT * FROM UNNEST(
				$1::bigint[], $2::smallint[], $3::text[], $4::text[], $5::smallint[],
				$6::double precision[], $7::integer[], $8::integer[], $9::integer[],
				$10::bigint[], $11::smallint[], $12::timestamptz[], $13::timestamptz[], $14::timestamptz[]
			)
			ON CONFLICT (url_key) DO UPDATE SET
				state         = EXCLUDED.state,
				priority      = EXCLUDED.priority,
				page_rank     = GREATEST(frontier_pages.page_rank, EXCLUDED.page_rank),
				fetch_count   = EXCLUDED.fetch_count,
				change_count  = EXCLUDED.change_count,
				content_hash  = EXCLUDED.content_hash,
				fail_count    = EXCLUDED.fail_count,
				last_fetch_at = EXCLUDED.last_fetch_at,
				next_fetch_at = EXCLUDED.next_fetch_at`

		if _, err := tx.Exec(ctx, pageQuery,
			keys, shards, urls, hostNames, states, priorities, ranks,
			fetches, changes, hashes, fails, discAts, lastAts, nextAts,
		); err != nil {
			return fmt.Errorf("frontier postgres: flush pages: %w", err)
		}
	}

	// 3. Batch upsert dead/disallowed tombstones.
	if len(dead) > 0 {
		keys := make([]int64, len(dead))
		shards := make([]int16, len(dead))
		urls := make([]string, len(dead))
		hostNames := make([]string, len(dead))

		for i, d := range dead {
			keys[i] = d.urlKey
			shards[i] = int16(shardID)
			urls[i] = d.url
			hostNames[i] = d.host
		}

		const deadQuery = `
			INSERT INTO frontier_pages (url_key, shard_id, url, host, state, content_hash)
			SELECT k, s, u, h, 5, 0
			FROM UNNEST($1::bigint[], $2::smallint[], $3::text[], $4::text[]) AS t(k, s, u, h)
			ON CONFLICT (url_key) DO UPDATE SET
				state        = 5,
				content_hash = 0`

		if _, err := tx.Exec(ctx, deadQuery, keys, shards, urls, hostNames); err != nil {
			return fmt.Errorf("frontier postgres: flush dead pages: %w", err)
		}
	}

	// 4. Apply PageRank increments for evicted pages residing on disk.
	if len(rankDeltas) > 0 {
		keys := make([]int64, 0, len(rankDeltas))
		deltas := make([]int32, 0, len(rankDeltas))
		for k, d := range rankDeltas {
			keys = append(keys, k)
			deltas = append(deltas, int32(d))
		}

		const deltaQuery = `
			UPDATE frontier_pages AS fp
			SET page_rank = fp.page_rank + u.delta
			FROM UNNEST($1::bigint[], $2::integer[]) AS u(url_key, delta)
			WHERE fp.url_key = u.url_key`

		if _, err := tx.Exec(ctx, deltaQuery, keys, deltas); err != nil {
			return fmt.Errorf("frontier postgres: flush rank deltas: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// FetchDuePages reads a batch of pages from PostgreSQL whose scheduled NextFetchAt time has arrived.
func (s *PostgresStore) FetchDuePages(ctx context.Context, shardID int, now time.Time, limit int) ([]*domain.FrontierItem, error) {
	if limit <= 0 {
		limit = 200
	}

	const query = `
		SELECT url, host, state, priority, page_rank, fetch_count,
		       change_count, content_hash, fail_count, discovered_at, last_fetch_at, next_fetch_at
		FROM frontier_pages
		WHERE shard_id = $1
		  AND state IN (1, 3, 4)
		  AND next_fetch_at <= $2
		ORDER BY next_fetch_at ASC, priority DESC
		LIMIT $3`

	rows, err := s.pool.Query(ctx, query, int16(shardID), now, limit)
	if err != nil {
		return nil, fmt.Errorf("frontier postgres: fetch due pages: %w", err)
	}
	defer rows.Close()

	var out []*domain.FrontierItem
	for rows.Next() {
		var (
			item        domain.FrontierItem
			stateRaw    int16
			contentHash int64
			failCount   int16
		)
		if err := rows.Scan(
			&item.URL,
			&item.Host,
			&stateRaw,
			&item.Priority,
			&item.PageRank,
			&item.FetchCount,
			&item.ChangeCount,
			&contentHash,
			&failCount,
			&item.DiscoveredAt,
			&item.LastFetchAt,
			&item.NextFetchAt,
		); err != nil {
			return nil, fmt.Errorf("frontier postgres: scan due page: %w", err)
		}
		item.State = domain.PageState(stateRaw)
		item.ContentHash = uint64(contentHash)
		item.FailCount = int(failCount)
		out = append(out, &item)
	}
	return out, rows.Err()
}
