package domain

import "context"

type ParsedPage struct {
	URL      string
	Title    string
	Text     string
	Links    []string
	Language string
	NoIndex  bool
}

type Parser interface {
	Parse(ctx context.Context, page *FetchedPage) (*ParsedPage, error)
}
