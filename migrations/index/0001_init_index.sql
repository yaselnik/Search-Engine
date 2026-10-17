-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS index_documents (
    doc_id     BIGINT PRIMARY KEY,
    doc_length INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS index_postings (
    term      TEXT NOT NULL,
    doc_id    BIGINT NOT NULL REFERENCES index_documents(doc_id) ON DELETE CASCADE,
    frequency INTEGER NOT NULL,
    positions INTEGER[] NOT NULL,
    offsets   INTEGER[] NOT NULL,
    PRIMARY KEY (term, doc_id)
);

-- Index on doc_id enables fast O(K) cascade deletion when a document is updated or removed.
CREATE INDEX IF NOT EXISTS idx_index_postings_doc_id
    ON index_postings (doc_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS index_postings;
DROP TABLE IF EXISTS index_documents;
-- +goose StatementEnd
