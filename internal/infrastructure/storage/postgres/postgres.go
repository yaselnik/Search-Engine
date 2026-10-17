package postgresStorage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yaselnik/Search-Engine/internal/domain"
	"github.com/yaselnik/Search-Engine/migrations"
)

var _ domain.DocumentStorage = (*PostgresStorage)(nil)

// PostgresStorage implements domain.DocumentStorage backed by PostgreSQL.
type PostgresStorage struct {
	pool *pgxpool.Pool
}

// NewPostgresStorage connects to the documents PostgreSQL database, runs embedded
// goose migrations, and returns a ready PostgresStorage instance.
func NewPostgresStorage(ctx context.Context, dsn string) (*PostgresStorage, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: create pgx pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("storage: ping database: %w", err)
	}

	if err := migrations.Run(pool, "storage"); err != nil {
		pool.Close()
		return nil, fmt.Errorf("storage: run migrations: %w", err)
	}

	return &PostgresStorage{pool: pool}, nil
}

// Close closes the underlying PostgreSQL connection pool.
func (s *PostgresStorage) Close() {
	s.pool.Close()
}

// Add inserts a new document or updates an existing document if its content or title changed,
// resetting the indexed flag to FALSE so the Indexer re-processes it.
func (s *PostgresStorage) Add(ctx context.Context, doc domain.Document) error {
	const query = `
		INSERT INTO documents (id, url, title, content, language, indexed, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, FALSE, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			url        = EXCLUDED.url,
			title      = EXCLUDED.title,
			content    = EXCLUDED.content,
			language   = EXCLUDED.language,
			updated_at = EXCLUDED.updated_at,
			indexed    = CASE
				WHEN documents.content <> EXCLUDED.content OR documents.title <> EXCLUDED.title
				THEN FALSE
				ELSE documents.indexed
			END`

	_, err := s.pool.Exec(ctx, query,
		int64(doc.ID),
		doc.URL,
		doc.Title,
		doc.Content,
		doc.Language,
		doc.CreatedAt,
		doc.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("storage: add document: %w", err)
	}
	return nil
}

func (s *PostgresStorage) Count(ctx context.Context) (int64, error) {
	var count int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM documents`).Scan(&count); err != nil {
		return 0, fmt.Errorf("storage: count documents: %w", err)
	}
	return count, nil
}

// GetDocumentByID fetches a single document by its ID.
func (s *PostgresStorage) GetDocumentByID(ctx context.Context, id domain.DocID) (domain.Document, error) {
	const query = `
		SELECT id, url, title, content, language, created_at, updated_at
		FROM documents
		WHERE id = $1`

	var (
		rawID int64
		doc   domain.Document
	)
	err := s.pool.QueryRow(ctx, query, int64(id)).Scan(
		&rawID,
		&doc.URL,
		&doc.Title,
		&doc.Content,
		&doc.Language,
		&doc.CreatedAt,
		&doc.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Document{}, domain.ErrDocumentNotFound
		}
		return domain.Document{}, fmt.Errorf("storage: get document by id: %w", err)
	}
	doc.ID = domain.DocID(uint64(rawID))
	return doc, nil
}

// GetAll retrieves all documents from the database.
func (s *PostgresStorage) GetAll(ctx context.Context) ([]domain.Document, error) {
	const query = `
		SELECT id, url, title, content, language, created_at, updated_at
		FROM documents`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("storage: query all documents: %w", err)
	}
	defer rows.Close()

	return scanDocuments(rows)
}

// GetUnindexed retrieves up to limit documents where indexed = FALSE using the partial index.
func (s *PostgresStorage) GetUnindexed(ctx context.Context, limit int) ([]domain.Document, error) {
	if limit <= 0 {
		limit = 500
	}
	const query = `
		SELECT id, url, title, content, language, created_at, updated_at
		FROM documents
		WHERE indexed = FALSE
		ORDER BY updated_at ASC, id ASC
		LIMIT $1`

	rows, err := s.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: query unindexed documents: %w", err)
	}
	defer rows.Close()

	return scanDocuments(rows)
}

// MarkIndexed sets indexed = TRUE for a batch of document IDs in a single query.
func (s *PostgresStorage) MarkIndexed(ctx context.Context, ids []domain.DocID) error {
	if len(ids) == 0 {
		return nil
	}

	rawIDs := make([]int64, len(ids))
	for i, id := range ids {
		rawIDs[i] = int64(id)
	}

	const query = `UPDATE documents SET indexed = TRUE WHERE id = ANY($1)`
	if _, err := s.pool.Exec(ctx, query, rawIDs); err != nil {
		return fmt.Errorf("storage: mark indexed: %w", err)
	}
	return nil
}

// Delete removes a document by ID.
func (s *PostgresStorage) Delete(ctx context.Context, id domain.DocID) error {
	const query = `DELETE FROM documents WHERE id = $1`
	if _, err := s.pool.Exec(ctx, query, int64(id)); err != nil {
		return fmt.Errorf("storage: delete document: %w", err)
	}
	return nil
}

// Exists checks whether a document with the given ID exists.
func (s *PostgresStorage) Exists(ctx context.Context, id domain.DocID) (bool, error) {
	const query = `SELECT EXISTS(SELECT 1 FROM documents WHERE id = $1)`
	var exists bool
	if err := s.pool.QueryRow(ctx, query, int64(id)).Scan(&exists); err != nil {
		return false, fmt.Errorf("storage: check exists: %w", err)
	}
	return exists, nil
}

func scanDocuments(rows pgx.Rows) ([]domain.Document, error) {
	var docs []domain.Document
	for rows.Next() {
		var (
			rawID int64
			doc   domain.Document
		)
		if err := rows.Scan(
			&rawID,
			&doc.URL,
			&doc.Title,
			&doc.Content,
			&doc.Language,
			&doc.CreatedAt,
			&doc.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("storage: scan document: %w", err)
		}
		doc.ID = domain.DocID(uint64(rawID))
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: rows iteration: %w", err)
	}
	return docs, nil
}
