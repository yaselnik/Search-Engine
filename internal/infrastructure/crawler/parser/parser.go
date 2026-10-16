package parser

import (
	"bytes"
	"context"
	"net/url"
	"strings"
	"unicode"

	"github.com/yaselnik/Search-Engine/internal/domain"
	"golang.org/x/net/html"
)

var _ domain.Parser = (*Parser)(nil)

// Parser implements domain.Parser using golang.org/x/net/html.
type Parser struct{}

// NewParser creates a new HTML Parser instance.
func NewParser() *Parser { return &Parser{} }

// Parse performs a single-pass traversal of page.Body to extract metadata,
// clean text, language, and outgoing links.
func (p *Parser) Parse(ctx context.Context, page *domain.FetchedPage) (*domain.ParsedPage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	doc, err := html.Parse(bytes.NewReader(page.Body))
	if err != nil {
		return nil, err
	}

	ext := &extractor{
		baseURL: page.FinalURL,
	}
	if ext.baseURL == "" {
		ext.baseURL = page.URL
	}

	ext.walk(doc, false)

	var links []string
	if !ext.noFollow && len(ext.rawLinks) > 0 {
		links = resolveBaseIfNeeded(page.FinalURL, ext.baseHref, ext.rawLinks)
	}

	return &domain.ParsedPage{
		URL:      page.URL,
		Title:    ext.title,
		Text:     strings.TrimSpace(ext.textBuilder.String()),
		Links:    links,
		Language: ext.lang,
		NoIndex:  ext.noIndex,
	}, nil
}

type extractor struct {
	baseURL     string
	baseHref    string
	title       string
	lang        string
	noIndex     bool
	noFollow    bool
	rawLinks    []string
	textBuilder strings.Builder
}

func (e *extractor) walk(n *html.Node, skipText bool) {
	if n.Type == html.ElementNode {
		switch n.Data {
		case "html":
			if e.lang == "" {
				if lang := getAttr(n, "lang"); lang != "" {
					e.lang = normalizeLang(lang)
				}
			}
		case "title":
			if e.title == "" && n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
				e.title = collapseWhitespace(n.FirstChild.Data)
			}
			return
		case "base":
			if e.baseHref == "" {
				if href := strings.TrimSpace(getAttr(n, "href")); href != "" {
					e.baseHref = href
				}
			}
			return
		case "meta":
			e.handleMeta(n)
			return
		case "a":
			if !e.noFollow {
				if href := strings.TrimSpace(getAttr(n, "href")); href != "" {
					if !hasRelNoFollow(n) {
						e.rawLinks = append(e.rawLinks, href)
					}
				}
			}
		case "script", "style", "noscript", "svg", "canvas", "template", "iframe", "object", "embed", "head":
			// Never extract visible text from these subtrees, except we must still walk <head>
			// to find <title>, <base>, and <meta>.
			if n.Data != "head" {
				return
			}
			skipText = true
		case "nav", "footer", "header", "aside":
			// Do not include navigation/boilerplate text in the search index,
			// but continue walking children so outgoing <a href> links are still discovered!
			skipText = true
		}
	}

	if n.Type == html.TextNode && !skipText {
		appendCleanText(&e.textBuilder, n.Data)
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		e.walk(c, skipText)
	}
}

func (e *extractor) handleMeta(n *html.Node) {
	name := strings.ToLower(strings.TrimSpace(getAttr(n, "name")))
	if name == "robots" || name == "polzun" {
		content := strings.ToLower(getAttr(n, "content"))
		for _, token := range strings.Split(content, ",") {
			switch strings.TrimSpace(token) {
			case "noindex", "none":
				e.noIndex = true
			}
			switch strings.TrimSpace(token) {
			case "nofollow", "none":
				e.noFollow = true
			}
		}
	}
	if e.lang == "" && strings.EqualFold(getAttr(n, "http-equiv"), "content-language") {
		if content := getAttr(n, "content"); content != "" {
			e.lang = normalizeLang(content)
		}
	}
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func hasRelNoFollow(n *html.Node) bool {
	rel := strings.ToLower(getAttr(n, "rel"))
	if rel == "" {
		return false
	}
	for _, field := range strings.Fields(rel) {
		if field == "nofollow" {
			return true
		}
	}
	return false
}

// normalizeLang extracts the primary 2- or 3-letter ISO language code (e.g., "en-US" -> "en", "ru_RU" -> "ru").
func normalizeLang(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	for i, r := range raw {
		if r == '-' || r == '_' || r == ';' || r == ',' || unicode.IsSpace(r) {
			raw = raw[:i]
			break
		}
	}
	if len(raw) >= 2 && len(raw) <= 3 {
		return raw
	}
	return ""
}

// appendCleanText appends s to sb while collapsing all whitespace sequences into a single space.
func appendCleanText(sb *strings.Builder, s string) {
	inSpace := sb.Len() == 0 || strings.HasSuffix(sb.String(), " ")
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !inSpace {
				sb.WriteByte(' ')
				inSpace = true
			}
		} else {
			sb.WriteRune(r)
			inSpace = false
		}
	}
}

func collapseWhitespace(s string) string {
	var sb strings.Builder
	appendCleanText(&sb, s)
	return strings.TrimSpace(sb.String())
}

// resolveBaseIfNeeded rebases raw links only when the HTML page explicitly declares a <base href> tag.
func resolveBaseIfNeeded(finalURL, baseHref string, rawLinks []string) []string {
	if baseHref == "" {
		return rawLinks
	}
	base, err := url.Parse(finalURL)
	if err != nil {
		return rawLinks
	}
	refBase, err := url.Parse(baseHref)
	if err != nil {
		return rawLinks
	}
	effectiveBase := base.ResolveReference(refBase)

	out := make([]string, 0, len(rawLinks))
	for _, raw := range rawLinks {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		out = append(out, effectiveBase.ResolveReference(u).String())
	}
	return out
}
