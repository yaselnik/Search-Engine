package ranker

import (
	"testing"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

func TestBM25_Score(t *testing.T) {
	t.Parallel()
	r := NewBM25()

	corpus := domain.CorpusStats{
		TotalDocs:        100,
		AvgTitleLength:   5.0,
		AvgContentLength: 100.0,
	}
	avgDoc := domain.DocStats{
		TitleLength:   5.0,
		ContentLength: 100.0,
	}

	t.Run("edge cases return zero", func(t *testing.T) {
		posting := domain.Posting{DocID: 1, ContentFrequency: 2}

		if got := r.Score(posting, avgDoc, domain.CorpusStats{TotalDocs: 0}, 5); got != 0 {
			t.Errorf("expected 0 when TotalDocs is 0, got %f", got)
		}
		if got := r.Score(posting, avgDoc, corpus, 0); got != 0 {
			t.Errorf("expected 0 when docsContainingTerm is 0, got %f", got)
		}
		emptyPosting := domain.Posting{DocID: 1}
		if got := r.Score(emptyPosting, avgDoc, corpus, 10); got != 0 {
			t.Errorf("expected 0 when frequencies are 0, got %f", got)
		}
	})

	t.Run("higher term frequency yields higher score with saturation", func(t *testing.T) {
		p1 := domain.Posting{DocID: 1, ContentFrequency: 1}
		p2 := domain.Posting{DocID: 1, ContentFrequency: 3}
		p10 := domain.Posting{DocID: 1, ContentFrequency: 10}

		s1 := r.Score(p1, avgDoc, corpus, 10)
		s2 := r.Score(p2, avgDoc, corpus, 10)
		s10 := r.Score(p10, avgDoc, corpus, 10)

		if !(s1 < s2 && s2 < s10) {
			t.Errorf("expected strictly increasing scores with TF, got s1=%f, s2=%f, s10=%f", s1, s2, s10)
		}
		// BM25 saturation check: 10x frequency must not produce 10x score.
		if s10 >= s1*10 {
			t.Errorf("expected TF saturation, but s10 (%f) >= 10 * s1 (%f)", s10, s1)
		}
	})

	t.Run("shorter document scores higher than longer document with same TF", func(t *testing.T) {
		posting := domain.Posting{DocID: 1, ContentFrequency: 2}
		shortDoc := domain.DocStats{TitleLength: 5.0, ContentLength: 30.0}
		longDoc := domain.DocStats{TitleLength: 5.0, ContentLength: 300.0}

		shortScore := r.Score(posting, shortDoc, corpus, 10)
		longScore := r.Score(posting, longDoc, corpus, 10)

		if shortScore <= longScore {
			t.Errorf("expected short doc score (%f) > long doc score (%f)", shortScore, longScore)
		}
	})

	t.Run("rarer term (higher IDF) scores higher than common term", func(t *testing.T) {
		posting := domain.Posting{DocID: 1, ContentFrequency: 1}

		rareScore := r.Score(posting, avgDoc, corpus, 2)
		commonScore := r.Score(posting, avgDoc, corpus, 80)

		if rareScore <= commonScore {
			t.Errorf("expected rare term score (%f) > common term score (%f)", rareScore, commonScore)
		}
	})

	t.Run("title match scores significantly higher than content match", func(t *testing.T) {
		titleMatch := domain.Posting{DocID: 1, TitleFrequency: 1}
		contentMatch := domain.Posting{DocID: 2, ContentFrequency: 1}

		titleScore := r.Score(titleMatch, avgDoc, corpus, 10)
		contentScore := r.Score(contentMatch, avgDoc, corpus, 10)

		if titleScore <= contentScore*2.0 {
			t.Errorf("expected title match (%f) to be boosted over content match (%f)", titleScore, contentScore)
		}
	})

	t.Run("handles zero average lengths gracefully without NaN", func(t *testing.T) {
		zeroAvgCorpus := domain.CorpusStats{TotalDocs: 10, AvgTitleLength: 0, AvgContentLength: 0}
		posting := domain.Posting{DocID: 1, TitleFrequency: 1, ContentFrequency: 1}

		score := r.Score(posting, avgDoc, zeroAvgCorpus, 2)
		if score <= 0 {
			t.Errorf("expected positive score even with zero avg lengths, got %f", score)
		}
	})
}
