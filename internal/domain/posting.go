package domain

// Contains information about the occurrence of a token in a specific document.
type Posting struct {
	DocID     DocID
	Frequency uint32
	Positions []uint32
}
