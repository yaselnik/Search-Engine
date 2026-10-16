package crawler

import (
	"context"
	"errors"
	"hash/fnv"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

type Options struct {
	FetcherWorkers int
	ParserWorkers  int
	MaxPages       int
	RawBufferSize  int
}

type Crawler struct {
	urlFilter domain.URLFilter
	frontier  domain.IFrontier
	fetcher   domain.Fetcher
	parser    domain.Parser
	storage   domain.DocumentStorage

	logger *slog.Logger
	opts   Options

	rawPages chan *domain.FetchedPage

	loaded        atomic.Int64
	inflight      atomic.Int64
	activeParsers atomic.Int64
	mu            sync.Mutex
}

func NewCrawler(
	filter domain.URLFilter,
	frontier domain.IFrontier,
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
		urlFilter: filter,
		frontier:  frontier,
		fetcher:   fetcher,
		parser:    parser,
		storage:   storage,
		logger:    logger.With("component", "crawler"),
		opts:      opts,
		rawPages:  make(chan *domain.FetchedPage, opts.RawBufferSize),
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
			c.runFetcher(ctx, id)
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
		progressTicks := 0
		startTime := time.Now()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			loaded := c.loaded.Load()
			if loaded >= int64(c.opts.MaxPages) {
				c.logger.Info("max pages reached", "loaded", loaded)
				cancel()
				return
			}

			progressTicks++
			if progressTicks >= 20 { // Каждые 10 секунд (20 * 500ms)
				progressTicks = 0
				elapsed := time.Since(startTime).Seconds()
				rate := 0.0
				if elapsed > 0 {
					rate = float64(loaded) / elapsed
				}
				c.logger.Info("crawler progress",
					"loaded", loaded,
					"max_pages", c.opts.MaxPages,
					"pages_per_sec", int(rate*10)/10,
					"inflight_fetches", c.inflight.Load(),
					"active_parsers", c.activeParsers.Load(),
					"raw_buffer_len", len(c.rawPages),
				)
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

func (c *Crawler) runFetcher(ctx context.Context, _ int) {
	for {
		if c.shouldStop() {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Lease 1 URL per synchronous worker so other hosts are not held idle in-flight.
		URLs, err := c.frontier.Lease(ctx, 1)
		if err != nil {
			c.logger.ErrorContext(ctx, "frontier lease failed", "error", err)
			time.Sleep(time.Second)
			continue
		}
		if len(URLs) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}

		URL := URLs[0]
		if c.shouldStop() {
			_ = c.frontier.Nack(ctx, URL)
			return
		}

		c.inflight.Add(1)
		page, err := c.fetcher.Fetch(ctx, URL)
		if err != nil {
			c.inflight.Add(-1)
			if errors.Is(err, domain.ErrPermanentFetchFailure) {
				c.logger.DebugContext(ctx, "permanent fetch failure, marking URL dead", "url", URL, "error", err)
				_ = c.frontier.MarkDead(ctx, URL)
			} else {
				c.logger.DebugContext(ctx, "transient fetch failure, scheduling retry", "url", URL, "error", err)
				_ = c.frontier.Nack(ctx, URL)
			}
			continue
		}

		shouldProcess, err := c.frontier.AckWithHash(ctx, URL, page.ContentHash)
		if err != nil {
			c.inflight.Add(-1)
			c.logger.DebugContext(ctx, "frontier ack failed", "url", URL, "error", err)
			continue
		}
		if !shouldProcess {
			// Content is either unchanged since last crawl or a duplicate of another URL.
			c.inflight.Add(-1)
			continue
		}

		select {
		case c.rawPages <- page:
		case <-ctx.Done():
			c.inflight.Add(-1)
			return
		}
		c.inflight.Add(-1)
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

	newItems := c.urlFilter.ProcessLinks(page.FinalURL, parsed.Links)
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

func idFromURL(u string) domain.DocID {
	h := fnv.New64a()
	_, _ = h.Write([]byte(u))
	return domain.DocID(h.Sum64())
}
