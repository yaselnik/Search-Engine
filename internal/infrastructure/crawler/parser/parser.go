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
		baseURL:      page.FinalURL,
		lastWasSpace: true,
	}
	if ext.baseURL == "" {
		ext.baseURL = page.URL
	}

	ext.walk(doc, false, false)

	title := ext.title
	if title == "" {
		title = ext.firstH1
	}

	var links []string
	if !ext.noFollow && len(ext.rawLinks) > 0 {
		links = resolveBaseIfNeeded(ext.baseURL, ext.baseHref, ext.rawLinks)
	}

	return &domain.ParsedPage{
		URL:      page.URL,
		Title:    title,
		Text:     strings.TrimSpace(ext.textBuilder.String()),
		Links:    links,
		Language: ext.lang,
		NoIndex:  ext.noIndex,
	}, nil
}

type extractor struct {
	baseURL      string
	baseHref     string
	title        string
	firstH1      string
	lang         string
	noIndex      bool
	noFollow     bool
	rawLinks     []string
	textBuilder  strings.Builder
	lastWasSpace bool
}

func (e *extractor) walk(n *html.Node, skipText bool, inArticleOrMain bool) {
	isBlock := false

	if n.Type == html.ElementNode {
		tag := n.Data
		isBlock = isBlockElement(tag)
		if isBlock {
			e.ensureSpace()
		}

		switch tag {
		case "html":
			if e.lang == "" {
				if lang := getAttr(n, "lang"); lang != "" {
					e.lang = normalizeLang(lang)
				}
			}
		case "article", "main":
			inArticleOrMain = true
		case "title":
			if e.title == "" {
				e.title = extractNodeText(n)
			}
			return
		case "h1":
			if e.firstH1 == "" {
				e.firstH1 = extractNodeText(n)
			}
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
			if tag != "head" {
				return
			}
			skipText = true
		case "nav", "footer", "aside":
			// Do not include navigation/boilerplate text in the search index,
			// but continue walking children so outgoing <a href> links are still discovered.
			skipText = true
		case "header":
			// Skip page-level <header> boilerplate, but preserve <header> inside <article> or <main>
			// because semantic HTML5 articles often place their <h1> and lead text inside <header>.
			if !inArticleOrMain {
				skipText = true
			}
		}
	}

	if n.Type == html.TextNode && !skipText {
		e.appendCleanText(n.Data)
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		e.walk(c, skipText, inArticleOrMain)
	}

	if isBlock {
		e.ensureSpace()
	}
}

func (e *extractor) ensureSpace() {
	if !e.lastWasSpace && e.textBuilder.Len() > 0 {
		e.textBuilder.WriteByte(' ')
		e.lastWasSpace = true
	}
}

// appendCleanText appends s to textBuilder while collapsing all whitespace sequences into a single space.
func (e *extractor) appendCleanText(s string) {
	for _, r := range s {
		if unicode.IsSpace(r) {
			e.ensureSpace()
		} else {
			e.textBuilder.WriteRune(r)
			e.lastWasSpace = false
		}
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

// isBlockElement returns true for HTML elements that visually separate words in rendered documents.
func isBlockElement(tag string) bool {
	switch tag {
	case "p", "div", "br", "hr", "li", "ul", "ol", "dl", "dt", "dd",
		"h1", "h2", "h3", "h4", "h5", "h6",
		"table", "tr", "td", "th", "thead", "tbody", "tfoot",
		"blockquote", "pre", "section", "article", "main", "figure", "figcaption", "form":
		return true
	}
	return false
}

// extractNodeText recursively collects and normalizes all text nodes inside an element.
func extractNodeText(n *html.Node) string {
	var sb strings.Builder
	lastSpace := true
	var collect func(*html.Node)
	collect = func(curr *html.Node) {
		if curr.Type == html.TextNode {
			for _, r := range curr.Data {
				if unicode.IsSpace(r) {
					if !lastSpace && sb.Len() > 0 {
						sb.WriteByte(' ')
						lastSpace = true
					}
				} else {
					sb.WriteRune(r)
					lastSpace = false
				}
			}
		}
		for c := curr.FirstChild; c != nil; c = c.NextSibling {
			collect(c)
		}
	}
	collect(n)
	return strings.TrimSpace(sb.String())
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
