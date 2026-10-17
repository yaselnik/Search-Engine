package urlfilter

import (
	"net"
	"net/url"
	"path"
	"strings"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

var _ domain.URLFilter = (*Filter)(nil)

// Config holds serializable configuration parameters for URL normalization and filtering.
type Config struct {
	MaxURLLength int  `yaml:"max_url_length" env:"URLFILTER_MAX_URL_LENGTH"`
	MaxPathDepth int  `yaml:"max_path_depth" env:"URLFILTER_MAX_PATH_DEPTH"`
}

// DefaultConfig returns sensible production defaults for web crawling.
func DefaultConfig() Config {
	return Config{
		MaxURLLength: 512,
		MaxPathDepth: 8,
	}
}

func (c Config) withDefaults() Config {
	def := DefaultConfig()
	if c.MaxURLLength <= 0 {
		c.MaxURLLength = def.MaxURLLength
	}
	if c.MaxPathDepth <= 0 {
		c.MaxPathDepth = def.MaxPathDepth
	}
	return c
}

// ignoredExtensions lists file extensions that do not contain indexable HTML content.
var ignoredExtensions = map[string]struct{}{
	".jpg": {}, ".jpeg": {}, ".png": {}, ".gif": {}, ".webp": {}, ".svg": {}, ".ico": {}, ".bmp": {}, ".avif": {},
	".pdf": {}, ".doc": {}, ".docx": {}, ".xls": {}, ".xlsx": {}, ".ppt": {}, ".pptx": {},
	".zip": {}, ".tar": {}, ".gz": {}, ".tgz": {}, ".rar": {}, ".7z": {}, ".bz2": {}, ".xz": {},
	".mp3": {}, ".wav": {}, ".ogg": {}, ".flac": {}, ".mp4": {}, ".avi": {}, ".mkv": {}, ".mov": {}, ".webm": {},
	".css": {}, ".js": {}, ".mjs": {}, ".map": {}, ".json": {}, ".xml": {}, ".rss": {}, ".atom": {},
	".woff": {}, ".woff2": {}, ".ttf": {}, ".eot": {}, ".otf": {},
	".exe": {}, ".msi": {}, ".dmg": {}, ".pkg": {}, ".deb": {}, ".rpm": {}, ".iso": {}, ".bin": {},
}

// Filter implements domain.URLFilter.
type Filter struct {
	cfg Config
}

// NewFilter creates a Filter with default settings.
func NewFilter() *Filter {
	return NewFilterWithConfig(DefaultConfig())
}

// NewFilterWithConfig creates a Filter with the provided configuration.
func NewFilterWithConfig(cfg Config) *Filter {
	return &Filter{cfg: cfg.withDefaults()}
}

// Normalize canonicalizes and validates a single absolute URL string.
func (f *Filter) Normalize(rawURL string) (domain.NormalizedURL, bool) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || len(rawURL) > f.cfg.MaxURLLength {
		return domain.NormalizedURL{}, false
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return domain.NormalizedURL{}, false
	}

	return f.normalizeParsed(u)
}

// ProcessLinks resolves raw href links against baseURL, normalizes and filters
// each link, and returns a deduplicated slice of NormalizedURLs.
func (f *Filter) ProcessLinks(baseURL string, rawLinks []string) []domain.NormalizedURL {
	if len(rawLinks) == 0 {
		return nil
	}

	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{}, len(rawLinks))
	out := make([]domain.NormalizedURL, 0, len(rawLinks))

	for _, raw := range rawLinks {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		ref, err := url.Parse(raw)
		if err != nil {
			continue
		}

		abs := base.ResolveReference(ref)
		norm, ok := f.normalizeParsed(abs)
		if !ok {
			continue
		}

		if _, exists := seen[norm.URL]; exists {
			continue
		}
		seen[norm.URL] = struct{}{}
		out = append(out, norm)
	}

	return out
}

func (f *Filter) normalizeParsed(u *url.URL) (domain.NormalizedURL, bool) {
	// 1. Reject URLs with query parameters or user credentials.
	if u.RawQuery != "" || u.ForceQuery || u.User != nil {
		return domain.NormalizedURL{}, false
	}

	// 2. Validate and normalize scheme.
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return domain.NormalizedURL{}, false
	}
	u.Scheme = scheme
	u.Fragment = ""

	// 3. Normalize hostname and strip default protocol ports (:80, :443).
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" {
		return domain.NormalizedURL{}, false
	}

	if isLocalOrPrivateHost(host) {
		return domain.NormalizedURL{}, false
	}

	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else {
		u.Host = host
	}

	// 4. Clean path and enforce depth and extension rules.
	cleanPath := u.Path
	if cleanPath == "" {
		cleanPath = "/"
	} else {
		trailing := len(cleanPath) > 1 && strings.HasSuffix(cleanPath, "/")
		cleanPath = path.Clean(cleanPath)
		if !strings.HasPrefix(cleanPath, "/") {
			cleanPath = "/" + cleanPath
		}
		if trailing && cleanPath != "/" {
			cleanPath += "/"
		}
	}

	if strings.Count(cleanPath, "/") > f.cfg.MaxPathDepth {
		return domain.NormalizedURL{}, false
	}

	if ext := strings.ToLower(path.Ext(strings.TrimSuffix(cleanPath, "/"))); ext != "" {
		if _, blocked := ignoredExtensions[ext]; blocked {
			return domain.NormalizedURL{}, false
		}
	}

	u.Path = cleanPath
	u.RawPath = ""

	finalURL := u.String()
	if len(finalURL) > f.cfg.MaxURLLength {
		return domain.NormalizedURL{}, false
	}

	return domain.NormalizedURL{
		URL:  finalURL,
		Host: u.Host,
	}, true
}

// isLocalOrPrivateHost rejects localhost, loopback, link-local, and private network hosts
// to prevent SSRF and stop the crawler from crawling its own local HTTP server.
func isLocalOrPrivateHost(host string) bool {
	if host == "localhost" ||
		strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal") {
		return true
	}

	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() ||
			ip.IsPrivate() ||
			ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() ||
			ip.IsUnspecified()
	}

	return false
}
