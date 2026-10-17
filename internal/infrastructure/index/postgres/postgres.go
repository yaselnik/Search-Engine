package postgresIndex

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yaselnik/Search-Engine/internal/domain"
	"github.com/yaselnik/Search-Engine/migrations"
)

var _ domain.Index = (*PostgresIndex)(nil)

const opTimeout = 10 * time.Second

// PostgresIndex implements domain.Index backed by PostgreSQL.
// Corpus statistics and document lengths are cached in memory (write-through)
// so BM25 scoring functions execute with zero database round-trips.
type PostgresIndex struct {
	pool *pgxpool.Pool

	mu          sync.RWMutex
	docLengths  map[domain.DocID]uint32
	docCount    uint64
	totalLength uint64
}

// NewPostgresIndex connects to the index database, runs migrations, and preloads
// document length statistics into memory for fast BM25 ranking.
func NewPostgresIndex(ctx context.Context, dsn string) (*PostgresIndex, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres index: create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres index: ping: %w", err)
	}
	if err := migrations.Run(pool, "index"); err != nil {
		pool.Close()
		return nil, err
	}

	idx := &PostgresIndex{
		pool:       pool,
		docLengths: make(map[domain.DocID]uint32),
	}
	if err := idx.loadCorpusStats(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return idx, nil
}

func (r *PostgresIndex) Close() {
	r.pool.Close()
}

func (r *PostgresIndex) loadCorpusStats(ctx context.Context) error {
	rows, err := r.pool.Query(ctx, `SELECT doc_id, doc_length FROM index_documents`)
	if err != nil {
		return fmt.Errorf("postgres index: load stats: %w", err)
	}
	defer rows.Close()

	r.mu.Lock()
	defer r.mu.Unlock()

	for rows.Next() {
		var (
			rawID  int64
			docLen int32
		)
		if err := rows.Scan(&rawID, &docLen); err != nil {
			return fmt.Errorf("postgres index: scan stats: %w", err)
		}
		id := domain.DocID(uint64(rawID))
		uLen := uint32(docLen)
		r.docLengths[id] = uLen
		r.totalLength += uint64(uLen)
		r.docCount++
	}
	return rows.Err()
}

// Add atomically replaces the document's postings in PostgreSQL using a single batch transaction
// and updates the in-memory corpus statistics.
func (r *PostgresIndex) Add(docID domain.DocID, tokens []domain.Token) error {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	const maxTermByteLen = 64

	tokenStats := make(map[string]*domain.Posting)
	for _, token := range tokens {
		if len(token.Value) == 0 || len(token.Value) > maxTermByteLen {
			continue
		}
		p, exists := tokenStats[token.Value]
		if !exists {
			p = &domain.Posting{
				DocID:     docID,
				Frequency: 0,
				Positions: make([]uint32, 0, 1),
				Offsets:   make([]uint32, 0, 1),
			}
			tokenStats[token.Value] = p
		}
		p.Frequency++
		p.Positions = append(p.Positions, token.Position)
		p.Offsets = append(p.Offsets, token.Offset)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres index: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Remove old document entry (ON DELETE CASCADE automatically removes old postings).
	if _, err := tx.Exec(ctx, `DELETE FROM index_documents WHERE doc_id = $1`, int64(docID)); err != nil {
		return fmt.Errorf("postgres index: delete old doc: %w", err)
	}

	docLen := uint32(len(tokens))
	if _, err := tx.Exec(ctx,
		`INSERT INTO index_documents (doc_id, doc_length) VALUES ($1, $2)`,
		int64(docID), int32(docLen),
	); err != nil {
		return fmt.Errorf("postgres index: insert doc length: %w", err)
	}

	if len(tokenStats) > 0 {
		batch := &pgx.Batch{}
		const insertPostingSQL = `
			INSERT INTO index_postings (term, doc_id, frequency, positions, offsets)
			VALUES ($1, $2, $3, $4, $5)`

		for term, p := range tokenStats {
			batch.Queue(insertPostingSQL,
				term,
				int64(docID),
				int32(p.Frequency),
				uint32SliceToInt32(p.Positions),
				uint32SliceToInt32(p.Offsets),
			)
		}

		br := tx.SendBatch(ctx, batch)
		for range tokenStats {
			if _, err := br.Exec(); err != nil {
				_ = br.Close()
				return fmt.Errorf("postgres index: batch insert posting: %w", err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("postgres index: close batch: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres index: commit tx: %w", err)
	}

	r.mu.Lock()
	if oldLen, exists := r.docLengths[docID]; exists {
		if r.totalLength >= uint64(oldLen) {
			r.totalLength -= uint64(oldLen)
		}
	} else {
		r.docCount++
	}
	r.docLengths[docID] = docLen
	r.totalLength += uint64(docLen)
	r.mu.Unlock()

	return nil
}

// GetPostings queries PostgreSQL for all postings matching term.
func (r *PostgresIndex) GetPostings(term string) ([]domain.Posting, error) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	const query = `
		SELECT doc_id, frequency, positions, offsets
		FROM index_postings
		WHERE term = $1`

	rows, err := r.pool.Query(ctx, query, term)
	if err != nil {
		return nil, fmt.Errorf("postgres index: query postings: %w", err)
	}
	defer rows.Close()

	var postings []domain.Posting
	for rows.Next() {
		var (
			rawDocID  int64
			freq      int32
			positions []int32
			offsets   []int32
		)
		if err := rows.Scan(&rawDocID, &freq, &positions, &offsets); err != nil {
			return nil, fmt.Errorf("postgres index: scan posting: %w", err)
		}
		postings = append(postings, domain.Posting{
			DocID:     domain.DocID(uint64(rawDocID)),
			Frequency: uint32(freq),
			Positions: int32SliceToUint32(positions),
			Offsets:   int32SliceToUint32(offsets),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres index: rows err: %w", err)
	}
	return postings, nil
}

// Remove deletes a document and its postings from PostgreSQL (via CASCADE) and updates stats.
func (r *PostgresIndex) Remove(id domain.DocID) error {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	if _, err := r.pool.Exec(ctx, `DELETE FROM index_documents WHERE doc_id = $1`, int64(id)); err != nil {
		return fmt.Errorf("postgres index: remove doc: %w", err)
	}

	r.mu.Lock()
	if oldLen, exists := r.docLengths[id]; exists {
		if r.totalLength >= uint64(oldLen) {
			r.totalLength -= uint64(oldLen)
		} else {
			r.totalLength = 0
		}
		delete(r.docLengths, id)
		if r.docCount > 0 {
			r.docCount--
		}
	}
	r.mu.Unlock()

	return nil
}

func (r *PostgresIndex) GetDocCount() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.docCount
}

func (r *PostgresIndex) GetAvgDocLength() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.docCount == 0 {
		return 0
	}
	return float64(r.totalLength) / float64(r.docCount)
}

func (r *PostgresIndex) GetDocLength(docID domain.DocID) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return float64(r.docLengths[docID])
}

func uint32SliceToInt32(in []uint32) []int32 {
	out := make([]int32, len(in))
	for i, v := range in {
		out[i] = int32(v)
	}
	return out
}

func int32SliceToUint32(in []int32) []uint32 {
	out := make([]uint32, len(in))
	for i, v := range in {
		out[i] = uint32(v)
	}
	return out
}
