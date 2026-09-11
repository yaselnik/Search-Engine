package domain

type Ranker interface {
	Score(posting Posting, ) float64
}
