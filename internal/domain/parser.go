package domain

import "context"

type Parser interface {
    Parse(ctx context.Context, page *FetchedPage) (*ParsedPage, error)
}

type ParsedPage struct {
    URL      string
    Title    string
    Text     string
    Language string
    Links    []string
}
