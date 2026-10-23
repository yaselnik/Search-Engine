package index

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

type docLenPair struct {
	titleLen   uint32
	contentLen uint32
}

// PostgresIndex implements domain.Index backed by PostgreSQL.
// Corpus statistics and document field lengths are cached in memory (write-through)
// so BM25F scoring functions execute with zero database round-trips.
type PostgresIndex struct {
	pool *pgxpool.Pool

	mu              sync.RWMutex
	docLengths      map[domain.DocID]docLenPair
	docCount        uint64
	totalTitleLen   uint64
	totalContentLen uint64
}

// NewPostgresIndex connects to the index database, runs migrations, and preloads
// document length statistics into memory for fast BM25F ranking.
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
		docLengths: make(map[domain.DocID]docLenPair),
	}
	if err := idx.loadCorpusStats(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return idx, nil
}

// Close releases all database connections in the pool.
func (r *PostgresIndex) Close() {
	r.pool.Close()
}

func (r *PostgresIndex) loadCorpusStats(ctx context.Context) error {
	rows, err := r.pool.Query(ctx, `SELECT doc_id, title_length, content_length FROM index_documents`)
	if err != nil {
		return fmt.Errorf("postgres index: load stats: %w", err)
	}
	defer rows.Close()

	r.mu.Lock()
	defer r.mu.Unlock()

	for rows.Next() {
		var (
			rawID      int64
			titleLen   int32
			contentLen int32
		)
		if err := rows.Scan(&rawID, &titleLen, &contentLen); err != nil {
			return fmt.Errorf("postgres index: scan stats: %w", err)
		}
		id := domain.DocID(uint64(rawID))
		pair := docLenPair{
			titleLen:   uint32(max(0, titleLen)),
			contentLen: uint32(max(0, contentLen)),
		}
		r.docLengths[id] = pair
		r.totalTitleLen += uint64(pair.titleLen)
		r.totalContentLen += uint64(pair.contentLen)
		r.docCount++
	}
	return rows.Err()
}

// Add atomically replaces the document's title and content postings in PostgreSQL
// using a single batch transaction and updates the in-memory corpus statistics.
func (r *PostgresIndex) Add(docID domain.DocID, titleTokens, contentTokens []domain.Token) error {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	const maxTermByteLen = 64

	tokenStats := make(map[string]*domain.Posting)
	getOrCreate := func(term string) *domain.Posting {
		p, exists := tokenStats[term]
		if !exists {
			p = &domain.Posting{
				DocID:            docID,
				TitlePositions:   make([]uint32, 0),
				ContentPositions: make([]uint32, 0),
				Offsets:          make([]uint32, 0),
			}
			tokenStats[term] = p
		}
		return p
	}

	for _, token := range titleTokens {
		if len(token.Value) == 0 || len(token.Value) > maxTermByteLen {
			continue
		}
		p := getOrCreate(token.Value)
		p.TitleFrequency++
		p.TitlePositions = append(p.TitlePositions, token.Position)
	}

	for _, token := range contentTokens {
		if len(token.Value) == 0 || len(token.Value) > maxTermByteLen {
			continue
		}
		p := getOrCreate(token.Value)
		p.ContentFrequency++
		p.ContentPositions = append(p.ContentPositions, token.Position)
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

	titleLen := uint32(len(titleTokens))
	contentLen := uint32(len(contentTokens))

	if _, err := tx.Exec(ctx,
		`INSERT INTO index_documents (doc_id, title_length, content_length) VALUES ($1, $2, $3)`,
		int64(docID), int32(titleLen), int32(contentLen),
	); err != nil {
		return fmt.Errorf("postgres index: insert doc lengths: %w", err)
	}

	if len(tokenStats) > 0 {
		uniqueTerms := make([]string, 0, len(tokenStats))
		batch := &pgx.Batch{}

		const insertPostingSQL = `
			INSERT INTO index_postings (
				term, doc_id, title_frequency, content_frequency,
				title_positions, content_positions, offsets
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`

		for term, p := range tokenStats {
			uniqueTerms = append(uniqueTerms, term)
			batch.Queue(insertPostingSQL,
				term,
				int64(docID),
				int32(p.TitleFrequency),
				int32(p.ContentFrequency),
				uint32SliceToInt32(p.TitlePositions),
				uint32SliceToInt32(p.ContentPositions),
				uint32SliceToInt32(p.Offsets),
			)
		}

		// Populate the dictionary table for fast trigram fuzzy matching.
		batch.Queue(
			`INSERT INTO index_terms (term) SELECT UNNEST($1::text[]) ON CONFLICT (term) DO NOTHING`,
			uniqueTerms,
		)

		br := tx.SendBatch(ctx, batch)
		for i := 0; i < len(tokenStats)+1; i++ {
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
	if old, exists := r.docLengths[docID]; exists {
		if r.totalTitleLen >= uint64(old.titleLen) {
			r.totalTitleLen -= uint64(old.titleLen)
		} else {
			r.totalTitleLen = 0
		}
		if r.totalContentLen >= uint64(old.contentLen) {
			r.totalContentLen -= uint64(old.contentLen)
		} else {
			r.totalContentLen = 0
		}
	} else {
		r.docCount++
	}
	r.docLengths[docID] = docLenPair{titleLen: titleLen, contentLen: contentLen}
	r.totalTitleLen += uint64(titleLen)
	r.totalContentLen += uint64(contentLen)
	r.mu.Unlock()

	return nil
}

// GetPostingsBatch retrieves postings for multiple query terms in a single database query,
// omitting byte offsets to minimize network payload during the ranking phase.
func (r *PostgresIndex) GetPostingsBatch(ctx context.Context, terms []string) (map[string][]domain.Posting, error) {
	if len(terms) == 0 {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	const query = `
		SELECT term, doc_id, title_frequency, content_frequency, title_positions, content_positions
		FROM index_postings
		WHERE term = ANY($1)`

	rows, err := r.pool.Query(ctx, query, terms)
	if err != nil {
		return nil, fmt.Errorf("postgres index: query postings batch: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]domain.Posting, len(terms))
	for rows.Next() {
		var (
			term             string
			rawDocID         int64
			titleFreq        int32
			contentFreq      int32
			titlePositions   []int32
			contentPositions []int32
		)
		if err := rows.Scan(&term, &rawDocID, &titleFreq, &contentFreq, &titlePositions, &contentPositions); err != nil {
			return nil, fmt.Errorf("postgres index: scan posting batch: %w", err)
		}
		result[term] = append(result[term], domain.Posting{
			DocID:            domain.DocID(uint64(rawDocID)),
			TitleFrequency:   uint32(titleFreq),
			ContentFrequency: uint32(contentFreq),
			TitlePositions:   int32SliceToUint32(titlePositions),
			ContentPositions: int32SliceToUint32(contentPositions),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres index: batch rows err: %w", err)
	}
	return result, nil
}

// GetOffsetsBatch fetches token byte offsets only for the top-K ranked documents
// to generate contextual snippets without loading offsets for the entire corpus.
func (r *PostgresIndex) GetOffsetsBatch(ctx context.Context, terms []string, docIDs []domain.DocID) (map[domain.DocID][]uint32, error) {
	if len(terms) == 0 || len(docIDs) == 0 {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	rawIDs := make([]int64, len(docIDs))
	for i, id := range docIDs {
		rawIDs[i] = int64(id)
	}

	const query = `
		SELECT doc_id, offsets
		FROM index_postings
		WHERE term = ANY($1) AND doc_id = ANY($2)`

	rows, err := r.pool.Query(ctx, query, terms, rawIDs)
	if err != nil {
		return nil, fmt.Errorf("postgres index: query offsets batch: %w", err)
	}
	defer rows.Close()

	res := make(map[domain.DocID][]uint32, len(docIDs))
	for rows.Next() {
		var (
			rawDocID int64
			offsets  []int32
		)
		if err := rows.Scan(&rawDocID, &offsets); err != nil {
			return nil, fmt.Errorf("postgres index: scan offsets: %w", err)
		}
		id := domain.DocID(uint64(rawDocID))
		res[id] = append(res[id], int32SliceToUint32(offsets)...)
	}
	return res, rows.Err()
}

// SuggestTerms finds similar terms in the index dictionary using trigram similarity (pg_trgm).
func (r *PostgresIndex) SuggestTerms(ctx context.Context, term string, limit int) ([]string, error) {
	if len(term) < 3 || limit <= 0 {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	const query = `
		SELECT term
		FROM index_terms
		WHERE term % $1
		ORDER BY similarity(term, $1) DESC
		LIMIT $2`

	rows, err := r.pool.Query(ctx, query, term, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres index: suggest terms: %w", err)
	}
	defer rows.Close()

	var suggestions []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("postgres index: scan suggested term: %w", err)
		}
		suggestions = append(suggestions, s)
	}
	return suggestions, rows.Err()
}

// Remove deletes a document and its postings from PostgreSQL (via CASCADE) and updates stats.
func (r *PostgresIndex) Remove(id domain.DocID) error {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	if _, err := r.pool.Exec(ctx, `DELETE FROM index_documents WHERE doc_id = $1`, int64(id)); err != nil {
		return fmt.Errorf("postgres index: remove doc: %w", err)
	}

	r.mu.Lock()
	if old, exists := r.docLengths[id]; exists {
		if r.totalTitleLen >= uint64(old.titleLen) {
			r.totalTitleLen -= uint64(old.titleLen)
		} else {
			r.totalTitleLen = 0
		}
		if r.totalContentLen >= uint64(old.contentLen) {
			r.totalContentLen -= uint64(old.contentLen)
		} else {
			r.totalContentLen = 0
		}
		delete(r.docLengths, id)
		if r.docCount > 0 {
			r.docCount--
		}
	}
	r.mu.Unlock()

	return nil
}

// GetCorpusStats returns aggregated document count and average field lengths across the corpus.
func (r *PostgresIndex) GetCorpusStats() domain.CorpusStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.docCount == 0 {
		return domain.CorpusStats{}
	}
	return domain.CorpusStats{
		TotalDocs:        r.docCount,
		AvgTitleLength:   float64(r.totalTitleLen) / float64(r.docCount),
		AvgContentLength: float64(r.totalContentLen) / float64(r.docCount),
	}
}

// GetDocStats returns the title and content lengths in tokens for a specific document.
func (r *PostgresIndex) GetDocStats(docID domain.DocID) domain.DocStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pair := r.docLengths[docID]
	return domain.DocStats{
		TitleLength:   float64(pair.titleLen),
		ContentLength: float64(pair.contentLen),
	}
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
