-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS frontier_hosts (
    host        TEXT PRIMARY KEY,
    shard_id    SMALLINT NOT NULL,
    crawl_delay BIGINT NOT NULL,
    last_req_at TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    ready_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_frontier_hosts_shard
    ON frontier_hosts (shard_id);

CREATE TABLE IF NOT EXISTS frontier_pages (
    url_key       BIGINT PRIMARY KEY, -- (HostHash24 << 40) | URLHash40 for B-Tree host locality
    shard_id      SMALLINT NOT NULL,
    url           TEXT NOT NULL,
    host          TEXT NOT NULL,
    state         SMALLINT NOT NULL,  -- 0=New, 1=Queued, 2=InFlight, 3=Done, 4=Failed, 5=NotAllowed/Dead
    priority      DOUBLE PRECISION NOT NULL DEFAULT 0,
    page_rank     INTEGER NOT NULL DEFAULT 1,
    fetch_count   INTEGER NOT NULL DEFAULT 0,
    change_count  INTEGER NOT NULL DEFAULT 0,
    content_hash  BIGINT NOT NULL DEFAULT 0,
    fail_count    SMALLINT NOT NULL DEFAULT 0,
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_fetch_at TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    next_fetch_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Partial index for fast read-ahead refills of due pages (dead/disallowed pages are excluded).
CREATE INDEX IF NOT EXISTS idx_frontier_pages_due
    ON frontier_pages (shard_id, next_fetch_at ASC, priority DESC)
    WHERE state IN (1, 2, 3, 4);

-- Partial index for Content-Seen duplicate detection.
CREATE INDEX IF NOT EXISTS idx_frontier_pages_content_hash
    ON frontier_pages (shard_id, content_hash)
    WHERE content_hash <> 0 AND state <> 5;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS frontier_pages;
DROP TABLE IF EXISTS frontier_hosts;
-- +goose StatementEnd
