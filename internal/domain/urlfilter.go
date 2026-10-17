package domain

// URLFilter encapsulates all string-level URL processing: resolving relative
// references against a base URL, canonicalization, and filtering out non-HTML
// resources, query-based URLs, and syntactic crawler traps.
type URLFilter interface {
	// Normalize canonicalizes and validates a single absolute URL string.
	// Returns the normalized URL and true if valid, or false if rejected.
	Normalize(rawURL string) (NormalizedURL, bool)

	// ProcessLinks resolves raw href strings extracted from an HTML page against
	// baseURL, filters out undesirable links, and returns a deduplicated slice
	// of canonical URLs ready to be pushed to the Frontier.
	ProcessLinks(baseURL string, rawLinks []string) []NormalizedURL
}
