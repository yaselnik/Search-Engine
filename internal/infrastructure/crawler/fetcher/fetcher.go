package fetcher

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

var _ domain.Fetcher = (*Fetcher)(nil)

// Config holds serializable configuration parameters for the HTTP Fetcher.
type Config struct {
	UserAgent             string        `yaml:"user_agent" env:"FETCHER_USER_AGENT"`
	Timeout               time.Duration `yaml:"timeout" env:"FETCHER_TIMEOUT"`
	DialTimeout           time.Duration `yaml:"dial_timeout" env:"FETCHER_DIAL_TIMEOUT"`
	TLSHandshakeTimeout   time.Duration `yaml:"tls_handshake_timeout" env:"FETCHER_TLS_HANDSHAKE_TIMEOUT"`
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout" env:"FETCHER_RESPONSE_HEADER_TIMEOUT"`
	MaxBodySize           int64         `yaml:"max_body_size" env:"FETCHER_MAX_BODY_SIZE"`
	MaxRedirects          int           `yaml:"max_redirects" env:"FETCHER_MAX_REDIRECTS"`
}

// DefaultConfig returns production-ready defaults for web page fetching.
func DefaultConfig() Config {
	return Config{
		UserAgent:             "Polzun/1.0",
		Timeout:               15 * time.Second,
		DialTimeout:           5 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		MaxBodySize:           2 << 20, // 2 MB
		MaxRedirects:          5,
	}
}

func (c Config) withDefaults() Config {
	def := DefaultConfig()
	if c.UserAgent == "" {
		c.UserAgent = def.UserAgent
	}
	if c.Timeout <= 0 {
		c.Timeout = def.Timeout
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = def.DialTimeout
	}
	if c.TLSHandshakeTimeout <= 0 {
		c.TLSHandshakeTimeout = def.TLSHandshakeTimeout
	}
	if c.ResponseHeaderTimeout <= 0 {
		c.ResponseHeaderTimeout = def.ResponseHeaderTimeout
	}
	if c.MaxBodySize <= 0 {
		c.MaxBodySize = def.MaxBodySize
	}
	if c.MaxRedirects <= 0 {
		c.MaxRedirects = def.MaxRedirects
	}
	return c
}

// Fetcher implements domain.Fetcher using a tuned http.Client.
type Fetcher struct {
	cfg    Config
	client *http.Client
}

// NewFetcher creates a Fetcher with default configuration, optionally overriding
// the HTTP client and User-Agent string.
func NewFetcher(client *http.Client, userAgent string) *Fetcher {
	cfg := DefaultConfig()
	if userAgent != "" {
		cfg.UserAgent = userAgent
	}
	return NewFetcherWithConfig(cfg, client)
}

// NewFetcherWithConfig creates a Fetcher using the provided Config and optional custom http.Client.
func NewFetcherWithConfig(cfg Config, client *http.Client) *Fetcher {
	cfg = cfg.withDefaults()

	if client == nil {
		transport := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   cfg.DialTimeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   cfg.TLSHandshakeTimeout,
			ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
			ExpectContinueTimeout: 1 * time.Second,
			MaxIdleConns:          128,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       30 * time.Second,
		}
		client = &http.Client{
			Transport: transport,
			Timeout:   cfg.Timeout,
		}
	}

	// Copy client to attach redirect policy without mutating shared caller state.
	cCopy := *client
	if cCopy.CheckRedirect == nil {
		cCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= cfg.MaxRedirects {
				return fmt.Errorf("%w: exceeded max redirects (%d)", domain.ErrPermanentFetchFailure, cfg.MaxRedirects)
			}
			scheme := strings.ToLower(req.URL.Scheme)
			if scheme != "http" && scheme != "https" {
				return fmt.Errorf("%w: redirect to unsupported scheme %q", domain.ErrPermanentFetchFailure, scheme)
			}
			req.Header.Set("User-Agent", cfg.UserAgent)
			return nil
		}
	}

	return &Fetcher{
		cfg:    cfg,
		client: &cCopy,
	}
}

// Fetch downloads the web page at rawURL, verifies status and HTML MIME type,
// reads up to MaxBodySize bytes, and computes a 64-bit FNV-1a content fingerprint.
// Permanent failures (404, 403, 410, non-HTML MIME, redirect loops) wrap domain.ErrPermanentFetchFailure.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (*domain.FetchedPage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetcher: build request (%w): %v", domain.ErrPermanentFetchFailure, err)
	}
	req.Header.Set("User-Agent", f.cfg.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.1")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetcher: do request: %w", err)
	}
	defer resp.Body.Close()

	if err := classifyStatusCode(resp.StatusCode); err != nil {
		return nil, err
	}

	ct := resp.Header.Get("Content-Type")
	if !isHTMLContentType(ct) {
		return nil, fmt.Errorf("fetcher: unsupported content-type %q: %w", ct, domain.ErrPermanentFetchFailure)
	}

	hasher := fnv.New64a()
	limitedReader := io.LimitReader(resp.Body, f.cfg.MaxBodySize)
	teeReader := io.TeeReader(limitedReader, hasher)

	body, err := io.ReadAll(teeReader)
	if err != nil {
		return nil, fmt.Errorf("fetcher: read body: %w", err)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("fetcher: empty response body: %w", domain.ErrPermanentFetchFailure)
	}

	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	return &domain.FetchedPage{
		URL:         rawURL,
		FinalURL:    finalURL,
		StatusCode:  resp.StatusCode,
		ContentType: ct,
		Body:        body,
		ContentHash: hasher.Sum64(),
		Headers:     resp.Header,
		FetchedAt:   time.Now(),
	}, nil
}

// IsPermanentError reports whether err represents an unrecoverable fetch failure
// for which the URL should be marked dead rather than retried.
func IsPermanentError(err error) bool {
	return errors.Is(err, domain.ErrPermanentFetchFailure)
}

func classifyStatusCode(code int) error {
	if code == http.StatusOK {
		return nil
	}

	// Transient statuses that merit retry with backoff.
	switch code {
	case http.StatusRequestTimeout,      // 408
		http.StatusTooEarly,            // 425
		http.StatusTooManyRequests,     // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return fmt.Errorf("fetcher: transient HTTP status %d", code)
	}

	// All other 4xx/5xx codes (400, 401, 403, 404, 405, 406, 410, 451, etc.) are treated as permanent.
	return fmt.Errorf("fetcher: permanent HTTP status %d: %w", code, domain.ErrPermanentFetchFailure)
}

func isHTMLContentType(ct string) bool {
	if idx := strings.IndexByte(ct, ';'); idx != -1 {
		ct = ct[:idx]
	}
	ct = strings.ToLower(strings.TrimSpace(ct))
	return ct == "text/html" || ct == "application/xhtml+xml"
}
