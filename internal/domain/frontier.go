package domain

import (
    "context"
    "time"
)

type Frontier interface {
    Push(ctx context.Context, items []FrontierItem) bool
    Pop(ctx context.Context, workerID string, n int) ([]FrontierItem, error)
    Ack(ctx context.Context, url string) error
    Nack(ctx context.Context, url string, retryAfter time.Duration, permanent bool) error
}

type FrontierItem struct {
    URL           string
    Host          string
}
