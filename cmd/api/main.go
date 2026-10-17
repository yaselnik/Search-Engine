package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpDelivery "github.com/yaselnik/Search-Engine/internal/delivery/http"
	"github.com/yaselnik/Search-Engine/internal/domain"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/analyzer"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/analyzer/stemmer"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/crawler/fetcher"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/crawler/frontier"
	parserHTML "github.com/yaselnik/Search-Engine/internal/infrastructure/crawler/parser"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/crawler/urlfilter"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/index/inmemory"
	pgIndex "github.com/yaselnik/Search-Engine/internal/infrastructure/index/postgres"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/ranker"
	memStorage "github.com/yaselnik/Search-Engine/internal/infrastructure/storage/inmemory"
	pgStorage "github.com/yaselnik/Search-Engine/internal/infrastructure/storage/postgres"
	"github.com/yaselnik/Search-Engine/internal/usecase/crawler"
	"github.com/yaselnik/Search-Engine/internal/usecase/indexer"
	"github.com/yaselnik/Search-Engine/internal/usecase/searcher"
)

var SeedURLs = []domain.NormalizedURL{
	{URL: "https://go.dev/", Host: "go.dev"},
	{URL: "https://go.dev/doc/", Host: "go.dev"},
	{URL: "https://go.dev/blog/", Host: "go.dev"},
	{URL: "https://pkg.go.dev/", Host: "pkg.go.dev"},
	{URL: "https://github.com/explore", Host: "github.com"},
	{URL: "https://github.com/topics", Host: "github.com"},
	{URL: "https://stackoverflow.com/questions", Host: "stackoverflow.com"},
	{URL: "https://news.ycombinator.com/", Host: "news.ycombinator.com"},
	{URL: "https://lobste.rs/", Host: "lobste.rs"},
	{URL: "https://dev.to/", Host: "dev.to"},
	{URL: "https://en.wikipedia.org/wiki/Main_Page", Host: "en.wikipedia.org"},
	{URL: "https://en.wikipedia.org/wiki/Go_(programming_language)", Host: "en.wikipedia.org"},
	{URL: "https://en.wikipedia.org/wiki/Search_engine", Host: "en.wikipedia.org"},
	{URL: "https://en.wiktionary.org/wiki/Wiktionary:Main_Page", Host: "en.wiktionary.org"},
	{URL: "https://www.britannica.com/", Host: "www.britannica.com"},
	{URL: "https://www.wikidata.org/wiki/Wikidata:Main_Page", Host: "www.wikidata.org"},
	{URL: "https://commons.wikimedia.org/wiki/Main_Page", Host: "commons.wikimedia.org"},
	{URL: "https://en.wikisource.org/wiki/Main_Page", Host: "en.wikisource.org"},
	{URL: "https://www.bbc.com/news", Host: "www.bbc.com"},
	{URL: "https://edition.cnn.com/", Host: "edition.cnn.com"},
	{URL: "https://www.reuters.com/", Host: "www.reuters.com"},
	{URL: "https://apnews.com/", Host: "apnews.com"},
	{URL: "https://www.theguardian.com/international", Host: "www.theguardian.com"},
	{URL: "https://www.nytimes.com/", Host: "www.nytimes.com"},
	{URL: "https://www.aljazeera.com/", Host: "www.aljazeera.com"},
	{URL: "https://www.npr.org/", Host: "www.npr.org"},
	{URL: "https://medium.com/", Host: "medium.com"},
	{URL: "https://www.reddit.com/r/programming/", Host: "www.reddit.com"},
	{URL: "https://www.reddit.com/r/golang/", Host: "www.reddit.com"},
	{URL: "https://arstechnica.com/", Host: "arstechnica.com"},
	{URL: "https://www.theverge.com/", Host: "www.theverge.com"},
	{URL: "https://www.wired.com/", Host: "www.wired.com"},
	{URL: "https://techcrunch.com/", Host: "techcrunch.com"},
	{URL: "https://www.infoq.com/", Host: "www.infoq.com"},
	{URL: "https://martinfowler.com/", Host: "martinfowler.com"},
	{URL: "https://blog.cloudflare.com/", Host: "blog.cloudflare.com"},
	{URL: "https://ru.wikipedia.org/wiki/Заглавная_страница", Host: "ru.wikipedia.org"},
	{URL: "https://ru.wikipedia.org/wiki/Поисковая_система", Host: "ru.wikipedia.org"},
	{URL: "https://habr.com/ru/", Host: "habr.com"},
	{URL: "https://habr.com/ru/all/", Host: "habr.com"},
	{URL: "https://habr.com/ru/hubs/go/", Host: "habr.com"},
	{URL: "https://lenta.ru/", Host: "lenta.ru"},
	{URL: "https://www.rbc.ru/", Host: "www.rbc.ru"},
	{URL: "https://tass.ru/", Host: "tass.ru"},
	{URL: "https://arxiv.org/", Host: "arxiv.org"},
	{URL: "https://www.nature.com/", Host: "www.nature.com"},
	{URL: "https://www.science.org/", Host: "www.science.org"},
	{URL: "https://developer.mozilla.org/en-US/", Host: "developer.mozilla.org"},
	{URL: "https://docs.python.org/3/", Host: "docs.python.org"},
	{URL: "https://doc.rust-lang.org/book/", Host: "doc.rust-lang.org"},
}

func main() {
	logLevel := flag.String("log-level", "info", "log level (debug, info, warn, error)")
	httpAddr := flag.String("addr", ":8080", "http server address")
	indexInterval := flag.Duration("index-interval", 2*time.Second, "how often to index new documents")
	inMemory := flag.Bool("in-memory", false, "use in-memory storage instead of PostgreSQL")
	frontierDSN := flag.String("frontier-dsn", "postgres://postgres:password@localhost:5432/frontier_db?sslmode=disable", "PostgreSQL DSN for frontier")
	storageDSN := flag.String("storage-dsn", "postgres://postgres:password@localhost:5433/storage_db?sslmode=disable", "PostgreSQL DSN for document storage")
	indexDSN := flag.String("index-dsn", "postgres://postgres:password@localhost:5434/index_db?sslmode=disable", "PostgreSQL DSN for inverted index")
	flag.Parse()

	var level slog.Level
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)
	log.SetOutput(io.Discard)

	logger.Info("starting search engine", "log_level", level.String(), "in_memory", *inMemory)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var (
		idx          domain.Index
		docStorage   domain.DocumentStorage
		frontierOpts []frontier.Option
	)

	if *inMemory {
		idx = inmemoryIndex.NewInMemoryIndex()
		docStorage = memStorage.NewInMemoryStorage()
	} else {
		fStore, err := frontier.NewPostgresStore(ctx, *frontierDSN)
		if err != nil {
			logger.Error("failed to init frontier postgres store", "error", err)
			return
		}
		defer fStore.Close()
		frontierOpts = append(frontierOpts, frontier.WithPostgresStore(fStore))

		pStorage, err := pgStorage.NewPostgresStorage(ctx, *storageDSN)
		if err != nil {
			logger.Error("failed to init document postgres storage", "error", err)
			return
		}
		defer pStorage.Close()
		docStorage = pStorage

		pIdx, err := pgIndex.NewPostgresIndex(ctx, *indexDSN)
		if err != nil {
			logger.Error("failed to init postgres inverted index", "error", err)
			return
		}
		defer pIdx.Close()
		idx = pIdx

		logger.Info("connected to PostgreSQL databases (frontier_db, storage_db, index_db)")
	}

	engStemmer := stemmer.NewEngStemmer()
	multiStemmer := stemmer.NewMultiLanguageStemmer(engStemmer, stemmer.NoopStemmer{})
	textAnalyzer := analyzer.NewAnalyzer(analyzer.RegexpTokenize, analyzer.LowercaseFilter{}, multiStemmer)

	urlFilter := urlfilter.NewFilter()
	crFetcher := fetcher.NewFetcher(nil, "SearchEngineBot/0.1")
	crParser := parserHTML.NewParser()
	crFrontier := frontier.NewFrontier(ctx, 0, logger, frontierOpts...)
	defer crFrontier.Close()

	normalizedSeeds := make([]domain.NormalizedURL, 0, len(SeedURLs))
	for _, seed := range SeedURLs {
		if norm, ok := urlFilter.Normalize(seed.URL); ok {
			normalizedSeeds = append(normalizedSeeds, norm)
		}
	}

	if err := crFrontier.Push(ctx, normalizedSeeds); err != nil {
		logger.Error("seed push failed", "error", err)
		return
	}
	logger.Info("seeds pushed", "count", len(normalizedSeeds))

	cr := crawler.NewCrawler(
		urlFilter, crFrontier, crFetcher, crParser, docStorage,
		crawler.Config{
			FetcherWorkers: 20,
			ParserWorkers:  12,
			MaxPages:       0,
			RawBufferSize:  1000,
		},
		logger)

	indexerUC := indexer.NewIndexer(docStorage, idx, textAnalyzer, logger)
	bm25 := ranker.NewBM25()
	searchUC := searcher.NewSearcher(docStorage, idx, textAnalyzer, bm25)

	handler := httpDelivery.NewHandler(searchUC, docStorage, logger)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/search", handler.Search)
	mux.HandleFunc("GET /api/documents/{id}", handler.GetDocument)

	appHandler := httpDelivery.LoggingMiddleware(logger, mux)
	server := &http.Server{
		Addr:         *httpAddr,
		Handler:      appHandler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		logger.Info("background indexer started", "interval", indexInterval.String())
		ticker := time.NewTicker(*indexInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.Info("background indexer stopped")
				return
			case <-ticker.C:
				if _, err := indexerUC.IndexNew(ctx); err != nil && !errors.Is(err, context.Canceled) {
					logger.Warn("indexer tick failed", "error", err)
				}
			}
		}
	}()

	go func() {
		logger.Info("http server started", "addr", *httpAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server failed", "error", err)
			cancel()
		}
	}()

	logger.Info("starting crawler")
	loaded, err := cr.Start(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("crawler failed", "error", err)
	}
	logger.Info("crawler finished", "loaded", loaded)

	if n, err := indexerUC.IndexNew(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Warn("final index pass failed", "error", err)
	} else if n > 0 {
		logger.Info("final index pass", "count", n)
	}

	logger.Info("crawling complete, serving search results")
	<-ctx.Done()
	logger.Info("shutting down gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown failed", "error", err)
	}
	logger.Info("search engine stopped")
}
