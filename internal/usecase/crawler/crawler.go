package crawler

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

type Options struct {
	FetcherWorkers int
	ParserWorkers  int
	MaxPages       int
	SameHost       bool
	RawBufferSize  int
}

type Crawler struct {
	frontier domain.Frontier
	fetcher  domain.Fetcher
	parser   domain.Parser
	storage  domain.DocumentStorage

	logger *slog.Logger
	opts   Options

	rawPages chan *domain.FetchedPage

	loaded        atomic.Int64
	inflight      atomic.Int64
	activeParsers atomic.Int64
	mu            sync.Mutex
}

func NewCrawler(
	frontier domain.Frontier,
	fetcher domain.Fetcher,
	parser domain.Parser,
	storage domain.DocumentStorage,
	opts Options,
	logger *slog.Logger,
) *Crawler {
	if opts.FetcherWorkers <= 0 {
		opts.FetcherWorkers = 5
	}
	if opts.ParserWorkers <= 0 {
		opts.ParserWorkers = 3
	}
	if opts.RawBufferSize <= 0 {
		opts.RawBufferSize = 200
	}
	if opts.MaxPages <= 0 {
		opts.MaxPages = 1000
	}

	return &Crawler{
		frontier: frontier,
		fetcher:  fetcher,
		parser:   parser,
		storage:  storage,
		logger:   logger.With("component", "crawler"),
		opts:     opts,
		rawPages: make(chan *domain.FetchedPage, opts.RawBufferSize),
	}
}

func (c *Crawler) Start(ctx context.Context) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	for i := 0; i < c.opts.FetcherWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c.runFetcher(ctx, fmt.Sprintf("fetcher-%d", id))
		}(i)
	}

	for i := 0; i < c.opts.ParserWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.runParser(ctx)
		}()
	}

	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		idleTicks := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			if c.loaded.Load() >= int64(c.opts.MaxPages) {
				c.logger.Info("max pages reached", "loaded", c.loaded.Load())
				cancel()
				return
			}

			empty := c.inflight.Load() == 0 &&
				c.activeParsers.Load() == 0 &&
				len(c.rawPages) == 0

			if empty {
				idleTicks++
				if idleTicks >= 4 {
					c.logger.Info("frontier exhausted")
					cancel()
					return
				}
			} else {
				idleTicks = 0
			}
		}
	}()

	wg.Wait()
	return int(c.loaded.Load()), nil
}

func (c *Crawler) shouldStop() bool {
	return c.loaded.Load() >= int64(c.opts.MaxPages)
}

func (c *Crawler) runFetcher(ctx context.Context, workerID string) {
	for {
		if c.shouldStop() {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}

		items, err := c.frontier.Pop(ctx, workerID, 10)
		if err != nil {
			c.logger.Error("frontier pop failed", "error", err)
			time.Sleep(time.Second)
			continue
		}
		if len(items) == 0 {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		for _, item := range items {
			if c.shouldStop() {
				_ = c.frontier.Nack(ctx, item.URL, 0, false)
				return
			}
			c.inflight.Add(1)
			page, err := c.fetcher.Fetch(ctx, item.URL)
			if err != nil {
				c.inflight.Add(-1)
				c.logger.Warn("fetch failed", "url", item.URL, "error", err)
				_ = c.frontier.Nack(ctx, item.URL, time.Minute, false)
				continue
			}

			select {
			case c.rawPages <- page:
				_ = c.frontier.Ack(ctx, item.URL)
			case <-ctx.Done():
				c.inflight.Add(-1)
				return
			}
			c.inflight.Add(-1)
		}
	}
}

func (c *Crawler) runParser(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case page, ok := <-c.rawPages:
			if !ok {
				return
			}
			c.activeParsers.Add(1)
			c.parseOne(ctx, page)
			c.activeParsers.Add(-1)
		}
	}
}

func (c *Crawler) parseOne(ctx context.Context, page *domain.FetchedPage) {
	parsed, err := c.parser.Parse(ctx, page)
	if err != nil {
		c.logger.Warn("parse failed", "url", page.URL, "error", err)
		return
	}

	doc := toDocument(parsed)
	if err := c.storage.Add(ctx, doc); err != nil {
		c.logger.Error("storage failed", "url", parsed.URL, "error", err)
		return
	}
	c.loaded.Add(1)

	if c.shouldStop() {
		return
	}

	baseHost := hostOf(page.URL)
	var newItems []domain.FrontierItem
	for _, link := range parsed.Links {
		if c.opts.SameHost && hostOf(link) != baseHost {
			continue
		}
		newItems = append(newItems, domain.FrontierItem{
			URL:  link,
			Host: hostOf(link),
		})
	}
	for len(newItems) > 10 {
		_ = c.frontier.Push(ctx, newItems[:10])
		newItems = newItems[10:]
	}
	_ = c.frontier.Push(ctx, newItems)
}

func toDocument(p *domain.ParsedPage) domain.Document {
	now := time.Now()
	title := p.Title
	if title == "" {
		title = p.URL
	}
	return domain.Document{
		ID:        idFromURL(p.URL),
		Title:     title,
		Content:   p.Text,
		URL:       p.URL,
		Language:  p.Language,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}

func idFromURL(u string) domain.DocID {
	h := fnv.New64a()
	_, _ = h.Write([]byte(u))
	return domain.DocID(h.Sum64())
}
