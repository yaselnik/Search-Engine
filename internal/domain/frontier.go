package domain

import (
	"context"
	"time"
)

type PageState int

const (
	StateNew PageState = iota
	StateQueued
	StateInFlight
	StateDone
	StateFailed
	StateNotAllowed
)

type NormalizedURL struct {
	URL  string
	Host string
}

type FrontierItem struct {
	URL          string
	Host         string
	State        PageState
	Priority     float64
	PageRank     int
	FetchCount   int
	ChangeCount  int
	ContentHash  uint64
	FailCount    int
	DiscoveredAt time.Time
	LastFetchAt  time.Time
	NextFetchAt  time.Time
}

type IFrontier interface {
	Push(ctx context.Context, items []NormalizedURL) error
	Lease(ctx context.Context, n int) ([]string, error)
	Ack(ctx context.Context, url string) error
	// AckWithHash records the fetched page's content fingerprint and returns true
	// if the page content is new or changed (should be parsed and indexed),
	// or false if the content is unchanged or a duplicate of another URL.
	AckWithHash(ctx context.Context, url string, contentHash uint64) (bool, error)
	Nack(ctx context.Context, url string) error
	MarkDead(ctx context.Context, url string) error
	Close()
}
