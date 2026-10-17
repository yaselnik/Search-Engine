-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS documents (
    id         BIGINT PRIMARY KEY,
    url        text NOT NULL UNIQUE,
    title      text NOT NULL,
    content    text NOT NULL,
    language   VARCHAR(8) NOT NULL DEFAULT '',
    indexed    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Partial index for fast O(1) retrieval of newly crawled or updated documents by the Indexer.
CREATE INDEX IF NOT EXISTS idx_documents_unindexed
    ON documents (updated_at ASC, id ASC)
    WHERE indexed = FALSE;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS documents;
-- +goose StatementEnd
