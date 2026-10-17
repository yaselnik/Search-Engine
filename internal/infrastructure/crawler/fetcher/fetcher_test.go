package fetcher

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newMockClient(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func TestFetcher_SuccessAndContentHash(t *testing.T) {
	htmlBody := "<html><head><title>Test</title></head><body>Hello World</body></html>"
	client := newMockClient(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Content-Type", "TEXT/HTML; charset=utf-8")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     h,
			Body:       io.NopCloser(strings.NewReader(htmlBody)),
			Request:    req,
		}, nil
	})

	f := NewFetcher(client, "TestBot/1.0")
	page, err := f.Fetch(context.Background(), "https://example.com/page")
	if err != nil {
		t.Fatalf("unexpected fetch error: %v", err)
	}
	if string(page.Body) != htmlBody {
		t.Fatalf("body mismatch: got %q, want %q", string(page.Body), htmlBody)
	}
	if page.ContentHash == 0 {
		t.Fatalf("expected non-zero ContentHash")
	}

	// Fetching identical content from another URL must yield the exact same ContentHash.
	page2, err := f.Fetch(context.Background(), "https://example.com/mirror")
	if err != nil {
		t.Fatalf("unexpected fetch error: %v", err)
	}
	if page.ContentHash != page2.ContentHash {
		t.Fatalf("expected identical ContentHash for identical body, got %d vs %d", page.ContentHash, page2.ContentHash)
	}
}

func TestFetcher_ErrorClassification(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		contentType   string
		body          string
		wantPermanent bool
	}{
		{
			name:          "404 Not Found is permanent",
			statusCode:    http.StatusNotFound,
			contentType:   "text/html",
			body:          "not found",
			wantPermanent: true,
		},
		{
			name:          "403 Forbidden is permanent",
			statusCode:    http.StatusForbidden,
			contentType:   "text/html",
			body:          "forbidden",
			wantPermanent: true,
		},
		{
			name:          "non-HTML MIME type is permanent",
			statusCode:    http.StatusOK,
			contentType:   "application/pdf",
			body:          "%PDF-1.5",
			wantPermanent: true,
		},
		{
			name:          "429 Too Many Requests is transient",
			statusCode:    http.StatusTooManyRequests,
			contentType:   "text/html",
			body:          "rate limited",
			wantPermanent: false,
		},
		{
			name:          "503 Service Unavailable is transient",
			statusCode:    http.StatusServiceUnavailable,
			contentType:   "text/html",
			body:          "unavailable",
			wantPermanent: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := newMockClient(func(req *http.Request) (*http.Response, error) {
				h := make(http.Header)
				h.Set("Content-Type", tc.contentType)
				return &http.Response{
					StatusCode: tc.statusCode,
					Header:     h,
					Body:       io.NopCloser(strings.NewReader(tc.body)),
					Request:    req,
				}, nil
			})

			f := NewFetcher(client, "TestBot/1.0")
			_, err := f.Fetch(context.Background(), "https://example.com/test")
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if got := IsPermanentError(err); got != tc.wantPermanent {
				t.Fatalf("IsPermanentError(%v) = %v, want %v", err, got, tc.wantPermanent)
			}
		})
	}
}

func TestFetcher_MaxBodySizeLimit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxBodySize = 10 // Limit to 10 bytes

	client := newMockClient(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Content-Type", "text/html")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     h,
			Body:       io.NopCloser(strings.NewReader("0123456789ABCDEF")),
			Request:    req,
		}, nil
	})

	f := NewFetcherWithConfig(cfg, client)
	page, err := f.Fetch(context.Background(), "https://example.com/large")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Body) != 10 || string(page.Body) != "0123456789" {
		t.Fatalf("expected body truncated to 10 bytes, got %q", string(page.Body))
	}
}
