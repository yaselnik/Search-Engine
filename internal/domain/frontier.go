package domain

import (
	"context"
	"time"
)

type IFrontier interface {
	Push(ctx context.Context, items []NormalizedURL) error
	Lease(ctx context.Context, n int) ([]string, error)
	Ack(ctx context.Context, url string) error
	Nack(ctx context.Context, url string) error
}

type FrontierItemState int8

const (
	StateNew FrontierItemState = iota
	StateQueued
	StateInFlight
	StateDone
	StateFailed
	StateNotAllowed
)

type FrontierItem struct {
	URL          string
	Host         string
	State        FrontierItemState
	Priority     float64
	LastFetchAt  time.Time
	NextFetchAt  time.Time
	FetchCount   int
	FailCount    int
	LastResp     int
	ChangeFreq   time.Duration
	DiscoveredAt time.Time
	PageRank     int
}

type NormalizedURL struct {
	URL  string
	Host string
}
