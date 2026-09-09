package domain

// Represents a lexical unit extracted from the source text.
type Token struct {
	Value    string // Normalized value of token
	Origin   string // Original substring from the source text before any normalization.
	Position uint32 // The 1-based ordinal position of the token in the source text.
	Offset   uint32 // The starting byte position of the token in the source text.
	Length   uint16 // The length of token in bytes
}
