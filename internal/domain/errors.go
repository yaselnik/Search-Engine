package domain

import (
	"errors"
)

// Storage errors
var (
	ErrDocumentNotFound = errors.New("document not found")
	ErrInvalidDocument  = errors.New("invalid document")
)

// Crawler Frontier errors
var (
	ErrPageProcessingTimeoutExceeded = errors.New("Page processing timeout exceeded")
)
