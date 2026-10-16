package domain

import (
    "context"
    "net/http"
    "time"
)

type FetchedPage struct {
	URL         string
	FinalURL    string
	StatusCode  int
	ContentType string
	Body        []byte
	ContentHash uint64
	Headers     http.Header
	FetchedAt   time.Time
}

type Fetcher interface {
	Fetch(ctx context.Context, url string) (*FetchedPage, error)
}
