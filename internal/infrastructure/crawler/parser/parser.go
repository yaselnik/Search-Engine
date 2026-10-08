package parser

import (
	"context"
	"net/url"
	"strings"

	"github.com/yaselnik/Search-Engine/internal/domain"
	"golang.org/x/net/html"
)

var _ domain.Parser = (*Parser)(nil)

type Parser struct{}

func NewParser() *Parser { return &Parser{} }

func (p *Parser) Parse(ctx context.Context, page *domain.FetchedPage) (*domain.ParsedPage, error) {
    doc, err := html.Parse(strings.NewReader(string(page.Body)))
    if err != nil {
        return nil, err
    }
    base, _ := url.Parse(page.FinalURL)
    return &domain.ParsedPage{
        URL:   page.URL,
        Title: extractTitle(doc),
        Text:  extractText(doc),
        Links: extractLinks(doc, base),
    }, nil
}


func extractTitle(n *html.Node) string {
    var title string
    var walk func(*html.Node)
    walk = func(n *html.Node) {
        if title != "" { return }
        if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil {
            title = strings.TrimSpace(n.FirstChild.Data)
            return
        }
        for c := n.FirstChild; c != nil; c = c.NextSibling {
            walk(c)
        }
    }
    walk(n)
    return title
}

func extractText(n *html.Node) string {
    var sb strings.Builder
    var walk func(*html.Node)
    walk = func(n *html.Node) {
        if n.Type == html.ElementNode {
            switch n.Data {
            case "script", "style", "noscript", "nav", "footer", "header", "aside":
                return
            }
        }
        if n.Type == html.TextNode {
            text := strings.TrimSpace(n.Data)
            if text != "" {
                sb.WriteString(text)
                sb.WriteByte(' ')
            }
        }
        for c := n.FirstChild; c != nil; c = c.NextSibling {
            walk(c)
        }
    }
    walk(n)
    return strings.TrimSpace(sb.String())
}

func extractLinks(n *html.Node, base *url.URL) []string {
    var links []string
    var walk func(*html.Node)
    walk = func(n *html.Node) {
        if n.Type == html.ElementNode && n.Data == "a" {
            for _, a := range n.Attr {
                if a.Key == "href" {
                    ref, err := url.Parse(a.Val)
                    if err != nil { continue }
                    abs := base.ResolveReference(ref)
                    if abs.Scheme == "http" || abs.Scheme == "https" {
                        abs.Fragment = ""
                        links = append(links, abs.String())
                    }
                }
            }
        }
        for c := n.FirstChild; c != nil; c = c.NextSibling {
            walk(c)
        }
    }
    walk(n)
    return links
}
