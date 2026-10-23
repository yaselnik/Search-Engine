package searcher

import (
	"context"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yaselnik/Search-Engine/internal/domain"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/analyzer/stemmer"
)

// Searcher orchestrates the search process: it analyzes and deduplicates the query,
// retrieves postings in batches, ranks documents using BM25F + term proximity + query coverage,
// and enriches the top-K results with document metadata and contextual snippets.
type Searcher struct {
	storage  domain.DocumentStorage
	index    domain.Index
	analyzer domain.Analyzer
	ranker   domain.Ranker
}

func NewSearcher(
	storage domain.DocumentStorage,
	index domain.Index,
	analyzer domain.Analyzer,
	ranker domain.Ranker,
) *Searcher {
	return &Searcher{
		storage:  storage,
		index:    index,
		analyzer: analyzer,
		ranker:   ranker,
	}
}

type docHit struct {
	score            float64
	matchedTerms     int
	titlePositions   map[int][]uint32 // termIndex -> positions in title
	contentPositions map[int][]uint32 // termIndex -> positions in content
}

// Search executes a full-text search against the index and returns the top 'limit' results.
func (s *Searcher) Search(ctx context.Context, query string, limit int) ([]domain.SearchResult, error) {
	rawTokens := s.analyzer.Analyze(query)
	if len(rawTokens) == 0 {
		return []domain.SearchResult{}, nil
	}

	// Filter stop words only when the query also contains non-stop words.
	// Queries composed exclusively of stop words (e.g., "to be or not to be") are preserved intact.
	filteredTokens := filterQueryStopWords(rawTokens)

	// Deduplicate query terms while preserving their sequential order for phrase/proximity scoring.
	uniqueTerms := make([]string, 0, len(filteredTokens))
	seenTerms := make(map[string]struct{}, len(filteredTokens))
	for _, t := range filteredTokens {
		if t.Value == "" {
			continue
		}
		if _, exists := seenTerms[t.Value]; !exists {
			seenTerms[t.Value] = struct{}{}
			uniqueTerms = append(uniqueTerms, t.Value)
		}
	}
	if len(uniqueTerms) == 0 {
		return []domain.SearchResult{}, nil
	}

	postingsByTerm, matchedTermsList, err := s.fetchPostingsWithTypoFallback(ctx, uniqueTerms)
	if err != nil {
		return nil, err
	}

	corpusStats := s.index.GetCorpusStats()
	docHits := make(map[domain.DocID]*docHit)

	for termIdx, term := range uniqueTerms {
		postings := postingsByTerm[term]
		if len(postings) == 0 {
			continue
		}

		docsContainingTerm := uint64(len(postings))

		for _, posting := range postings {
			docStats := s.index.GetDocStats(posting.DocID)
			score := s.ranker.Score(posting, docStats, corpusStats, docsContainingTerm)

			hit, ok := docHits[posting.DocID]
			if !ok {
				hit = &docHit{
					titlePositions:   make(map[int][]uint32),
					contentPositions: make(map[int][]uint32),
				}
				docHits[posting.DocID] = hit
			}

			hit.score += score
			hit.matchedTerms++

			if len(posting.TitlePositions) > 0 {
				hit.titlePositions[termIdx] = posting.TitlePositions
			}
			if len(posting.ContentPositions) > 0 {
				hit.contentPositions[termIdx] = posting.ContentPositions
			}
		}
	}

	if len(docHits) == 0 {
		return []domain.SearchResult{}, nil
	}

	// Apply multi-term ranking boosts (Query Coverage and Term Proximity).
	numQueryTerms := len(uniqueTerms)
	if numQueryTerms > 1 {
		for _, hit := range docHits {
			// 1. Coordination / Query Coverage factor: reward documents matching more unique query terms.
			coverage := float64(hit.matchedTerms) / float64(numQueryTerms)
			hit.score *= (0.5 + 0.5*coverage)

			// 2. Term Proximity boost: reward documents where query terms appear close to each other.
			if hit.matchedTerms > 1 {
				titleProx := calcProximityBoost(hit.titlePositions, numQueryTerms)
				contentProx := calcProximityBoost(hit.contentPositions, numQueryTerms)
				hit.score *= (1.0 + 0.5*titleProx + 0.25*contentProx)
			}
		}
	}

	type scoredDoc struct {
		id  domain.DocID
		hit *docHit
	}
	sortedDocs := make([]scoredDoc, 0, len(docHits))
	for id, hit := range docHits {
		sortedDocs = append(sortedDocs, scoredDoc{id, hit})
	}
	sort.Slice(sortedDocs, func(i, j int) bool {
		if sortedDocs[i].hit.score == sortedDocs[j].hit.score {
			return sortedDocs[i].id < sortedDocs[j].id
		}
		return sortedDocs[i].hit.score > sortedDocs[j].hit.score
	})

	if limit > 0 && len(sortedDocs) > limit {
		sortedDocs = sortedDocs[:limit]
	}

	topDocIDs := make([]domain.DocID, len(sortedDocs))
	for i, sd := range sortedDocs {
		topDocIDs[i] = sd.id
	}

	// Lazy-load byte offsets exclusively for the winning top-K documents.
	offsetsMap, err := s.index.GetOffsetsBatch(ctx, matchedTermsList, topDocIDs)
	if err != nil {
		offsetsMap = nil
	}

	docsMap, err := s.storage.GetDocumentsByIDs(ctx, topDocIDs)
	if err != nil {
		return nil, err
	}

	results := make([]domain.SearchResult, 0, len(sortedDocs))
	for _, sd := range sortedDocs {
		doc, ok := docsMap[sd.id]
		if !ok {
			continue
		}

		var docOffsets []uint32
		if offsetsMap != nil {
			docOffsets = offsetsMap[sd.id]
		}

		results = append(results, domain.SearchResult{
			Document: doc,
			Score:    sd.hit.score,
			Snippet:  s.generateSnippet(doc.Content, docOffsets, 200),
		})
	}

	return results, nil
}

// filterQueryStopWords removes stop words from the query token slice only if at least one
// meaningful (non-stop) token exists. If all tokens are stop words, it returns the original slice.
func filterQueryStopWords(tokens []domain.Token) []domain.Token {
	meaningful := make([]domain.Token, 0, len(tokens))
	for _, t := range tokens {
		raw := strings.ToLower(t.Origin)
		if raw == "" {
			raw = t.Value
		}
		if stemmer.IsStopWord(raw) || stemmer.IsRussianStopWord(raw) {
			continue
		}
		meaningful = append(meaningful, t)
	}
	if len(meaningful) == 0 {
		return tokens
	}
	return meaningful
}

// fetchPostingsWithTypoFallback fetches postings in a single batch and uses
// trigram suggestions for any query terms that produced zero hits.
func (s *Searcher) fetchPostingsWithTypoFallback(
	ctx context.Context,
	terms []string,
) (map[string][]domain.Posting, []string, error) {
	batchResult, err := s.index.GetPostingsBatch(ctx, terms)
	if err != nil {
		return nil, nil, err
	}
	if batchResult == nil {
		batchResult = make(map[string][]domain.Posting)
	}

	effectiveTerms := make([]string, 0, len(terms))
	var missingTerms []string

	for _, term := range terms {
		if len(batchResult[term]) > 0 {
			effectiveTerms = append(effectiveTerms, term)
		} else {
			missingTerms = append(missingTerms, term)
		}
	}

	// Fuzzy fallback for missing terms via pg_trgm dictionary.
	for _, missing := range missingTerms {
		suggestions, err := s.index.SuggestTerms(ctx, missing, 1)
		if err != nil || len(suggestions) == 0 {
			continue
		}
		suggested := suggestions[0]
		suggestedBatch, err := s.index.GetPostingsBatch(ctx, []string{suggested})
		if err == nil && len(suggestedBatch[suggested]) > 0 {
			batchResult[missing] = suggestedBatch[suggested]
			effectiveTerms = append(effectiveTerms, suggested)
		}
	}

	return batchResult, effectiveTerms, nil
}

// calcProximityBoost evaluates how closely consecutive query terms appear in a document field.
// It returns a normalized boost factor in the range [0.0, 1.0].
func calcProximityBoost(positionsByTerm map[int][]uint32, totalQueryTerms int) float64 {
	if len(positionsByTerm) < 2 {
		return 0
	}

	var totalPairScore float64
	var pairsChecked int

	for i := 0; i < totalQueryTerms-1; i++ {
		posA, okA := positionsByTerm[i]
		posB, okB := positionsByTerm[i+1]
		if !okA || !okB || len(posA) == 0 || len(posB) == 0 {
			continue
		}

		pairsChecked++
		minDist := minPositionDistance(posA, posB)
		if minDist > 0 && minDist <= 10 {
			// Exact phrase match (posB - posA == 1) yields 1.0; wider gaps decay quadratically.
			totalPairScore += 1.0 / float64(minDist*minDist)
		}
	}

	if pairsChecked == 0 {
		return 0
	}
	return totalPairScore / float64(pairsChecked)
}

// minPositionDistance finds the minimum distance between two sorted position slices,
// giving slight preference when posB appears after posA (preserving query word order).
func minPositionDistance(posA, posB []uint32) uint32 {
	i, j := 0, 0
	var bestDist uint32 = ^uint32(0)

	for i < len(posA) && j < len(posB) {
		a, b := posA[i], posB[j]
		var dist uint32
		if b >= a {
			dist = b - a
			if dist == 0 {
				dist = 1
			}
		} else {
			// Penalize reversed word order by 1 extra position.
			dist = (a - b) + 1
		}

		if dist < bestDist {
			bestDist = dist
			if bestDist == 1 {
				return 1
			}
		}

		if a < b {
			i++
		} else {
			j++
		}
	}
	return bestDist
}

// generateSnippet extracts a contextual fragment of the document containing the densest
// cluster of query token offsets within a window of snippetSize bytes.
func (s *Searcher) generateSnippet(content string, offsets []uint32, snippetSize uint16) string {
	if len(content) <= int(snippetSize) {
		return cleanSnippet(content, false, false)
	}

	if len(offsets) == 0 {
		end := alignToWordEnd(content, int(snippetSize))
		return cleanSnippet(content[:end], false, end < len(content))
	}

	sorted := slices.Clone(offsets)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)

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
		start = max(0, end-int(snippetSize))
	}

	start = alignToWordStart(content, start)
	end = alignToWordEnd(content, end)

	snippet := content[start:end]
	return cleanSnippet(snippet, start > 0, end < len(content))
}

func alignToWordStart(content string, start int) int {
	if start <= 0 {
		return 0
	}
	if start >= len(content) {
		return len(content)
	}
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
	if end >= len(content) {
		return len(content)
	}
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
