package domain

import (
    "context"
    "net/http"
    "time"
)

type Fetcher interface {
    Fetch(ctx context.Context, url string) (*FetchedPage, error)
}

type FetchedPage struct {
    URL         string
    FinalURL    string
    StatusCode  int
    ContentType string
    Body        []byte
    Headers     http.Header
    FetchedAt   time.Time
}
