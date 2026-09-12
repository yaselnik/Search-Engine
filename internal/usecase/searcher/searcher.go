package searcher

import (
	"context"
	"sort"
	"strings"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

// Orchestrates the search process: it tokenizes the query,
// retrieves postings from the index, ranks the results, and enriches
// them with full document data and snippets.
type Searcher struct {
	storage   domain.DocumentStorage
	index     domain.Index
	tokenizer domain.Tokenizer
	ranker    domain.Ranker
}

func NewSearcher(storage domain.DocumentStorage, index domain.Index, tokenizer domain.Tokenizer, ranker domain.Ranker) *Searcher {
	return &Searcher{
		storage:   storage,
		index:     index,
		tokenizer: tokenizer,
		ranker:    ranker,
	}
}

// Executes a full-text search against the index.
// It returns a list of SearchResult sorted by relevance score in descending order,
// limited by the specified 'limit' parameter.
func (s *Searcher) Search(ctx context.Context, query string, limit int) ([]domain.SearchResult, error) {
	queryTokens := s.tokenizer(query)
	if len(queryTokens) == 0 {
		return []domain.SearchResult{}, nil
	}

	totalDocs := s.index.GetDocCount()
	avgDocLen := s.index.GetAvgDocLength()

	docScores := make(map[domain.DocID]float64)

	for _, qToken := range queryTokens {
		postings, err := s.index.GetPostings(qToken.Value)
		if err != nil || postings == nil {
			continue
		}

		docsContainingTerm := uint64(len(postings))

		for _, posting := range postings {
			docLen := s.index.GetDocLength(posting.DocID)
			score := s.ranker.Score(posting, docLen, avgDocLen, totalDocs, docsContainingTerm)
			docScores[posting.DocID] += score
		}
	}

	type scoredDoc struct {
		id    domain.DocID
		score float64
	}
	var sortedDocs []scoredDoc
	for id, score := range docScores {
		sortedDocs = append(sortedDocs, scoredDoc{id, score})
	}
	sort.Slice(sortedDocs, func(i, j int) bool {
		return sortedDocs[i].score > sortedDocs[j].score
	})

	if limit > 0 && len(sortedDocs) > limit {
		sortedDocs = sortedDocs[:limit]
	}

	var results []domain.SearchResult
	for _, sd := range sortedDocs {
		doc, err := s.storage.GetDocumentByID(ctx, sd.id)
		if err != nil {
			continue
		}

		results = append(results, domain.SearchResult{
			Document: doc,
			Score:    sd.score,
			Snippet:  s.generateSnippet(doc.Content, queryTokens),
		})
	}

	return results, nil
}

// Extracts a contextual fragment of the document content
// surrounding the first occurrence of any query token.
// It adds "..." ellipsis if the snippet is truncated.
func (s *Searcher) generateSnippet(content string, queryTokens []domain.Token) string {
	contentLower := strings.ToLower(content)
	for _, qt := range queryTokens {
		idx := strings.Index(contentLower, qt.Value)
		if idx != -1 {
			start := idx - 40
			if start < 0 {
				start = 0
			}
			end := idx + len(qt.Value) + 40
			if end > len(content) {
				end = len(content)
			}

			snippet := content[start:end]
			if start > 0 {
				snippet = "..." + snippet
			}
			if end < len(content) {
				snippet = snippet + "..."
			}
			return snippet
		}
	}
	if len(content) > 100 {
		return content[:100] + "..."
	}
	return content
}
