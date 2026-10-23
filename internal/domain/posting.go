package domain

// Contains information about the occurrence of a token in a specific document.
type Posting struct {
	DocID            DocID
	TitleFrequency   uint32
	ContentFrequency uint32
	TitlePositions   []uint32
	ContentPositions []uint32
	Offsets          []uint32
}

type DocStats struct {
	TitleLength   float64
	ContentLength float64
}

type CorpusStats struct {
	TotalDocs        uint64
	AvgTitleLength   float64
	AvgContentLength float64
}
