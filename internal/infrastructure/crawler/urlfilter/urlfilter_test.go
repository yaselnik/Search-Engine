package urlfilter

import "testing"

func TestFilter_Normalize(t *testing.T) {
	f := NewFilter()

	tests := []struct {
		name     string
		rawURL   string
		wantURL  string
		wantHost string
		wantOK   bool
	}{
		{
			name:     "lowercase scheme and host, strip default https port 443 and fragment",
			rawURL:   "HTTPS://Go.Dev:443/doc/../blog/#top",
			wantURL:  "https://go.dev/blog/",
			wantHost: "go.dev",
			wantOK:   true,
		},
		{
			name:     "strip default http port 80 and add root slash",
			rawURL:   "http://example.com:80",
			wantURL:  "http://example.com/",
			wantHost: "example.com",
			wantOK:   true,
		},
		{
			name:     "preserve non-standard port",
			rawURL:   "http://example.com:8080/api",
			wantURL:  "http://example.com:8080/api",
			wantHost: "example.com:8080",
			wantOK:   true,
		},
		{
			name:   "reject URL with query parameters",
			rawURL: "https://en.wikipedia.org/w/index.php?title=Go&action=edit",
			wantOK: false,
		},
		{
			name:   "reject non-HTML extension",
			rawURL: "https://example.com/files/report.PDF",
			wantOK: false,
		},
		{
			name:   "reject excessive path depth",
			rawURL: "https://example.com/1/2/3/4/5/6/7/8/9",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := f.Normalize(tc.rawURL)
			if ok != tc.wantOK {
				t.Fatalf("Normalize(%q) ok = %v, want %v", tc.rawURL, ok, tc.wantOK)
			}
			if ok && (got.URL != tc.wantURL || got.Host != tc.wantHost) {
				t.Fatalf("Normalize(%q) = (%q, %q), want (%q, %q)", tc.rawURL, got.URL, got.Host, tc.wantURL, tc.wantHost)
			}
		})
	}
}

func TestFilter_ProcessLinks(t *testing.T) {
	cfg := DefaultConfig()
	f := NewFilterWithConfig(cfg)

	baseURL := "https://go.dev/doc/effective_go"
	rawLinks := []string{
		"/blog/news",                        // Relative link on same host -> ok
		"../pkg#section",                    // Relative parent link with fragment -> keep as https://go.dev/pkg
		"https://GO.DEV:443/blog/news",      // Duplicate of first link after canonicalization -> deduplicate
		"https://go.dev/images/logo.svg",    // Blocked extension -> reject
		"https://go.dev/search?q=slices",    // Query param -> reject
		"https://external.com/article",      // Different host -> ok
		"mailto:info@go.dev",                // Non-http scheme -> reject
	}

	got := f.ProcessLinks(baseURL, rawLinks)
	if len(got) != 3 {
		t.Fatalf("expected 3 links after filtering and deduplication, got %v", got)
	}
	if got[0].URL != "https://go.dev/blog/news" || got[1].URL != "https://go.dev/pkg" {
		t.Fatalf("unexpected filtered links: %+v", got)
	}
}
