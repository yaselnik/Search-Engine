package main

import (
	"context"
	"errors"
	"flag"
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
	"github.com/yaselnik/Search-Engine/internal/infrastructure/index"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/ranker"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/storage"
	"github.com/yaselnik/Search-Engine/internal/usecase/crawler"
	"github.com/yaselnik/Search-Engine/internal/usecase/indexer"
	"github.com/yaselnik/Search-Engine/internal/usecase/searcher"
)

func main() {
	logLevel := flag.String("log-level", "info", "log level (debug, info, warn, error)")
	httpAddr := flag.String("addr", ":8080", "http server address")
	indexInterval := flag.Duration("index-interval", 2*time.Second, "how often to index new documents")
	flag.Parse()

	var level slog.Level
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)

	logger.Info("starting search engine", "log_level", level.String())

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	idx := index.NewInMemoryIndex()
	docStorage := storage.NewInMemoryStorage()

	engStemmer := stemmer.NewEngStemmer()
	stemmer := stemmer.NewMultiLanguageStemmer(engStemmer, stemmer.NoopStemmer{})
	analyzer := analyzer.NewAnalyzer(analyzer.RegexpTokenize, analyzer.LowercaseFilter{}, stemmer)

	crFetcher := fetcher.NewFetcher(nil, "SearchEngineBot/0.1")
	crParser := parserHTML.NewParser()
	crFrontier := frontier.NewMemFrontier(nil, logger)

	_ = crFrontier.Push(ctx, []domain.FrontierItem{
		{URL: "https://go.dev", Host: "go.dev"},
		{URL: "https://en.wikipedia.org/wiki/Main_Page", Host: "en.wikipedia.org"},
	})

	cr := crawler.NewCrawler(
		crFrontier, crFetcher, crParser, docStorage,
		crawler.Options{
			FetcherWorkers: 20,
			ParserWorkers:  5,
			MaxPages:       1000,
			SameHost:       false,
			RawBufferSize:  500,
		},
		logger)

	indexerUC := indexer.NewIndexer(docStorage, idx, analyzer, logger)

	bm25 := ranker.NewBM25()
	searcher := searcher.NewSearcher(docStorage, idx, analyzer, bm25)

	handler := httpDelivery.NewHandler(searcher, docStorage, logger)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/search", handler.Search)
	mux.HandleFunc("GET /api/documents/{id}", handler.GetDocument)

	appHandler := httpDelivery.LoggingMiddleware(logger, mux)

	server := &http.Server{
		Addr:    *httpAddr,
		Handler: appHandler,

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
				n, err := indexerUC.IndexNew(ctx)
				if err != nil {
					if errors.Is(err, context.Canceled) {
						return
					}
					logger.Warn("indexer tick failed", "error", err)
					continue
				}
				if n > 0 {
					logger.Info("indexed new documents", "count", n)
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

	if n, err := indexerUC.IndexNew(ctx); err != nil {
		if !errors.Is(err, context.Canceled) {
			logger.Warn("final index pass failed", "error", err)
		}
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
