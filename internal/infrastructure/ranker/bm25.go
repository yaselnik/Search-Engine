package ranker

import (
	"math"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

// BM25 implements field-weighted Best Matching 25 (BM25F) ranking.
// It scores matches in both document title and body content with independent
// length normalization and boosts title matches.
type BM25 struct {
	// K1 controls term frequency saturation. Default: 1.2.
	K1 float64
	// BTitle controls length normalization for document titles. Default: 0.3.
	BTitle float64
	// BContent controls length normalization for document body content. Default: 0.75.
	BContent float64
	// TitleWeight is the multiplier applied to title term scores. Default: 3.0.
	TitleWeight float64
}

func NewBM25() *BM25 {
	return &BM25{
		K1:          1.2,
		BTitle:      0.3,
		BContent:    0.75,
		TitleWeight: 3.0,
	}
}

// Score calculates the multi-field BM25F relevance score for a term in a document.
func (r *BM25) Score(
	posting domain.Posting,
	docStats domain.DocStats,
	corpusStats domain.CorpusStats,
	docsContainingTerm uint64,
) float64 {
	if docsContainingTerm == 0 || corpusStats.TotalDocs == 0 {
		return 0
	}

	if docsContainingTerm > corpusStats.TotalDocs {
		docsContainingTerm = corpusStats.TotalDocs
	}

	// Calculate IDF: ln((N - n + 0.5) / (n + 0.5) + 1)
	idf := math.Log((float64(corpusStats.TotalDocs-docsContainingTerm)+0.5)/(float64(docsContainingTerm)+0.5) + 1.0)

	var totalTFScore float64

	if posting.TitleFrequency > 0 {
		tf := float64(posting.TitleFrequency)
		norm := 1.0
		if corpusStats.AvgTitleLength > 0 {
			norm = 1.0 - r.BTitle + r.BTitle*(docStats.TitleLength/corpusStats.AvgTitleLength)
		}
		totalTFScore += r.TitleWeight * (tf * (r.K1 + 1.0)) / (tf + r.K1*norm)
	}

	if posting.ContentFrequency > 0 {
		tf := float64(posting.ContentFrequency)
		norm := 1.0
		if corpusStats.AvgContentLength > 0 {
			norm = 1.0 - r.BContent + r.BContent*(docStats.ContentLength/corpusStats.AvgContentLength)
		}
		totalTFScore += (tf * (r.K1 + 1.0)) / (tf + r.K1*norm)
	}

	return idf * totalTFScore
}
