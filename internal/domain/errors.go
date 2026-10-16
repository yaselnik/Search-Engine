package domain

import (
	"errors"
)

// Storage errors
var (
	ErrDocumentNotFound = errors.New("document not found")
	ErrInvalidDocument  = errors.New("invalid document")
)

// Crawler errors
var (
	ErrPageProcessingTimeoutExceeded = errors.New("page processing timeout exceeded")
	ErrPermanentFetchFailure         = errors.New("permanent fetch failure")
)
