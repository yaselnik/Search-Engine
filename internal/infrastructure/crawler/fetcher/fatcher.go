package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

var _ domain.Fetcher = (*Fetcher)(nil)

const (
	defaultTimeout     = 10 * time.Second
	defaultMaxBodySize = 5 << 20 // 5 MB
	defaultUserAgent   = "SearchEngineCrawler/0.1"
)

type Fetcher struct {
	client      *http.Client
	userAgent   string
	maxBodySize int64
}

func NewFetcher(client *http.Client, userAgent string) *Fetcher {
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}

	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	return &Fetcher{
		client:      client,
		userAgent:   userAgent,
		maxBodySize: defaultMaxBodySize,
	}
}

func (f *Fetcher) Fetch(ctx context.Context, url string) (*domain.FetchedPage, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return nil, fmt.Errorf("fetcher: build request: %w", err)
    }
    req.Header.Set("User-Agent", f.userAgent)
    req.Header.Set("Accept", "text/html")

    resp, err := f.client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("fetcher: do request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("fetcher: unexpected status %d", resp.StatusCode)
    }

    ct := resp.Header.Get("Content-Type")
    if !isHTMLContentType(ct) {
        return nil, fmt.Errorf("fetcher: unsupported content-type %q", ct)
    }

    body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBodySize))
    if err != nil {
        return nil, fmt.Errorf("fetcher: read body: %w", err)
    }

    return &domain.FetchedPage{
        URL:         url,
        FinalURL:    resp.Request.URL.String(),
        StatusCode:  resp.StatusCode,
        ContentType: ct,
        Body:        body,
        Headers:     resp.Header,
        FetchedAt:   time.Now(),
    }, nil
}

func isHTMLContentType(ct string) bool {
    for i := 0; i < len(ct); i++ {
        if ct[i] == ';' {
            ct = ct[:i]
            break
        }
    }
    ct = strings.TrimSpace(ct)
    return ct == "text/html"
}
