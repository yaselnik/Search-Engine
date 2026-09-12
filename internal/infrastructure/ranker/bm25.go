package ranker

import (
	"github.com/yaselnik/Search-Engine/internal/domain"
	"math"
)

// BM25 (Best Matching 25) is a probabilistic ranking function used by
// search engines to estimate the relevance of documents to a given search query.
// It improves upon TF-IDF by introducing term frequency saturation and
// document length normalization.
type BM25 struct {
	// K1 controls term frequency saturation.
	// Standard value is between 1.2 and 2.0. Default: 1.2.
	K1 float64
	// B controls the influence of document length normalization.
	// 0.0 means no normalization, 1.0 means full normalization. Default: 0.75.
	B float64
}

func NewBM25() *BM25 {
	return &BM25{K1: 1.2, B: 0.75}
}

// Calculates the BM25 relevance score for a single term in a document.
// It combines Inverse Document Frequency (IDF) with a normalized Term Frequency (TF).
func (r *BM25) Score(posting domain.Posting, docLength float64, avgDocLength float64, totalDocs uint64, docsContainingTerm uint64) float64 {
	if docsContainingTerm == 0 || totalDocs == 0 {
		return 0
	}

	// Calculate IDF: ln((N - n + 0.5) / (n + 0.5) + 1)
	idf := math.Log((float64(totalDocs-docsContainingTerm)+0.5)/(float64(docsContainingTerm)+0.5) + 1.0)

	// Calculate normalized TF with length penalty
	tf := float64(posting.Frequency)
	norm := 1.0 - r.B + r.B*(docLength/avgDocLength)
	tfScore := (tf * (r.K1 + 1.0)) / (tf + r.K1*norm)

	return idf * tfScore
}
