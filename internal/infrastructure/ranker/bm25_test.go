package ranker

import (
	"github.com/yaselnik/Search-Engine/internal/domain"
	"testing"
)

func TestBM25_Score(t *testing.T) {
	ranker := NewBM25()
	avgDocLen := 100.0
	totalDocs := uint64(1000)

	tests := []struct {
		name               string
		posting            domain.Posting
		docLength          float64
		docsContainingTerm uint64
		expectedMinScore   float64
	}{
		{
			name: "standard match",
			posting: domain.Posting{
				DocID:     1,
				Frequency: 5,
			},
			docLength:          100.0,
			docsContainingTerm: 10,
			expectedMinScore:   1.0,
		},
		{
			name: "short document should score higher than long document with same frequency",
			posting: domain.Posting{
				DocID:     2,
				Frequency: 3,
			},
			docLength:          50.0,
			docsContainingTerm: 50,
			expectedMinScore:   0.5,
		},
		{
			name: "zero documents containing term",
			posting: domain.Posting{
				DocID:     3,
				Frequency: 1,
			},
			docLength:          100.0,
			docsContainingTerm: 0,
			expectedMinScore:   0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ranker.Score(tt.posting, tt.docLength, avgDocLen, totalDocs, tt.docsContainingTerm)

			if tt.docsContainingTerm == 0 {
				if score != 0.0 {
					t.Errorf("expected score 0.0, got %f", score)
				}
			} else {
				if score < tt.expectedMinScore {
					t.Errorf("expected score >= %f, got %f", tt.expectedMinScore, score)
				}
			}
		})
	}
}
