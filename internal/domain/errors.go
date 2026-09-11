package domain

import (
	"errors"
)

// Storage errors
var (
	ErrDocumentNotFound = errors.New("document not found")
	ErrInvalidDocument  = errors.New("invalid document")
)
