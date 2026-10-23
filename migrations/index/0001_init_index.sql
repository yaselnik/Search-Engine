-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS index_documents (
    doc_id         BIGINT PRIMARY KEY,
    title_length   INTEGER NOT NULL DEFAULT 0,
    content_length INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS index_postings (
    term              TEXT NOT NULL,
    doc_id            BIGINT NOT NULL REFERENCES index_documents(doc_id) ON DELETE CASCADE,
    title_frequency   INTEGER NOT NULL DEFAULT 0,
    content_frequency INTEGER NOT NULL DEFAULT 0,
    title_positions   INTEGER[] NOT NULL DEFAULT '{}',
    content_positions INTEGER[] NOT NULL DEFAULT '{}',
    offsets           INTEGER[] NOT NULL DEFAULT '{}',
    PRIMARY KEY (term, doc_id)
);

-- Index on doc_id enables fast O(K) cascade deletion when a document is updated or removed.
CREATE INDEX IF NOT EXISTS idx_index_postings_doc_id
    ON index_postings (doc_id);

-- Dictionary of unique terms for fast fuzzy search (handling typos)
CREATE TABLE IF NOT EXISTS index_terms (
    term TEXT PRIMARY KEY
);

CREATE INDEX IF NOT EXISTS idx_index_terms_trgm
    ON index_terms USING GIN (term gin_trgm_ops);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_index_terms_trgm;
DROP TABLE IF EXISTS index_terms;
DROP TABLE IF EXISTS index_postings;
DROP TABLE IF EXISTS index_documents;
-- +goose StatementEnd
