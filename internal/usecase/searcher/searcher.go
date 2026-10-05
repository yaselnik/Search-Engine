package searcher

import (
	"context"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

// Orchestrates the search process: it analyzes the query,
// retrieves postings from the index, ranks the results, and enriches
// them with full document data and snippets.
type Searcher struct {
	storage  domain.DocumentStorage
	index    domain.Index
	analyzer domain.Analyzer
	ranker   domain.Ranker
}

func NewSearcher(storage domain.DocumentStorage, index domain.Index, analyzer domain.Analyzer, ranker domain.Ranker) *Searcher {
	return &Searcher{
		storage:  storage,
		index:    index,
		analyzer: analyzer,
		ranker:   ranker,
	}
}

// Executes a full-text search against the index.
// It returns a list of SearchResult sorted by relevance score in descending order,
// limited by the specified 'limit' parameter.
func (s *Searcher) Search(ctx context.Context, query string, limit int) ([]domain.SearchResult, error) {
	queryTokens := s.analyzer.Analyze(query)
	if len(queryTokens) == 0 {
		return []domain.SearchResult{}, nil
	}

	totalDocs := s.index.GetDocCount()
	avgDocLen := s.index.GetAvgDocLength()

	type docHit struct {
		score   float64
		offsets []uint32
	}
	docHits := make(map[domain.DocID]*docHit)

	for _, qToken := range queryTokens {
		postings, err := s.index.GetPostings(qToken.Value)
		if err != nil || postings == nil {
			continue
		}

		docsContainingTerm := uint64(len(postings))

		for _, posting := range postings {
			docLen := s.index.GetDocLength(posting.DocID)
			score := s.ranker.Score(posting, docLen, avgDocLen, totalDocs, docsContainingTerm)

			hit, ok := docHits[posting.DocID]
			if !ok {
				hit = &docHit{}
				docHits[posting.DocID] = hit
			}
			hit.score += score
			hit.offsets = append(hit.offsets, posting.Offsets...)
		}
	}

	type scoredDoc struct {
		id  domain.DocID
		hit *docHit
	}
	var sortedDocs []scoredDoc
	for id, hit := range docHits {
		sortedDocs = append(sortedDocs, scoredDoc{id, hit})
	}
	sort.Slice(sortedDocs, func(i, j int) bool {
		return sortedDocs[i].hit.score > sortedDocs[j].hit.score
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
			Score:    sd.hit.score,
			Snippet:  s.generateSnippet(doc.Content, sd.hit.offsets, 200),
		})
	}

	return results, nil
}

// Extracts a contextual fragment of the document
// that contains the maximum number of query token offsets within a window
// of snippetSize characters. It centers the snippet around the densest
// cluster of offsets, aligns boundaries to word edges, and adds "..."
// ellipsis if the snippet is truncated.
func (s *Searcher) generateSnippet(content string, offsets []uint32, snippetSize uint16) string {
	if len(content) < int(snippetSize) {
		cleanSnippet(content, false, false)
	}

	if len(offsets) == 0 {
		return cleanSnippet(content[:min(len(content), int(snippetSize))], false, len(content) > int(snippetSize))
	}

	sorted := slices.Clone(offsets)
	slices.Sort(sorted)

	bestLeft := sorted[0]
	bestRight := sorted[0]
	bestCount := 1

	left := 0
	for right := 0; right < len(sorted); right++ {
		for sorted[right]-sorted[left] > uint32(snippetSize) {
			left++
		}
		count := right - left + 1
		if count > bestCount {
			bestCount = count
			bestLeft = sorted[left]
			bestRight = sorted[right]
		}
	}

	span := int(bestRight - bestLeft)
	extra := int(snippetSize) - span
	start := int(bestLeft) - extra/2
	if start < 0 {
		start = 0
	}
	end := start + int(snippetSize)
	if end > len(content) {
		end = len(content)
		start = end - int(snippetSize)
		if start < 0 {
			start = 0
		}
	}

	start = alignToWordStart(content, start)
	end = alignToWordEnd(content, end)

	snippet := content[start:end]
	return cleanSnippet(snippet, start > 0, end < len(content))
}

func alignToWordStart(content string, start int) int {
	for start > 0 {
        r, size := utf8.DecodeLastRuneInString(content[:start])
        if unicode.IsSpace(r) {
            break
        }
        start -= size
    }
    return start
}

func alignToWordEnd(content string, end int) int {
	for end < len(content) {
        r, size := utf8.DecodeRuneInString(content[end:])
        if unicode.IsSpace(r) {
            break
        }
        end += size
    }
    return end
}

func cleanSnippet(s string, prefixDots, suffixDots bool) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSpace(s)
	if prefixDots {
		s = "..." + s
	}
	if suffixDots {
		s = s + "..."
	}
	return s
}
