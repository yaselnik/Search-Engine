package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpDelivery "github.com/yaselnik/Search-Engine/internal/delivery/http"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/analyzer"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/index"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/loader"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/ranker"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/storage"
	"github.com/yaselnik/Search-Engine/internal/usecase/indexer"
	"github.com/yaselnik/Search-Engine/internal/usecase/searcher"
)

func main() {
	dataPath := flag.String("data", "./data", "path to directory with documents")
	logLevel := flag.String("log-level", "info", "log level (debug, info, warn, error)")
	httpAddr := flag.String("addr", ":8080", "http server address")
	flag.Parse()

	var level slog.Level
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)

	logger.Info("starting search engine", "data_path", *dataPath, "log_level", level.String())

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	idx := index.NewInMemoryIndex()
	docStorage := storage.NewInMemoryStorage()

	source := loader.NewLoader(*dataPath, []string{".txt", ".md"}, docStorage, logger)
	loaded, err := source.Load(ctx)
	if err != nil {
		logger.Error("load failed, exiting", "error", err)
		os.Exit(1)
	}
	logger.Info("documents loaded successfully", "count", loaded)

	indexerUC := indexer.NewIndexer(docStorage, idx, analyzer.RegexpTokenize, logger)
	if err := indexerUC.IndexAll(ctx); err != nil {
		logger.Error("indexing failed, exiting", "error", err)
		os.Exit(1)
	}

	bm25 := ranker.NewBM25()
	searcher := searcher.NewSearcher(docStorage, idx, analyzer.RegexpTokenize, bm25)

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
		logger.Info("http server started", "addr", *httpAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server failed", "error", err)
			cancel()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown failed", "error", err)
	}

	logger.Info("search engine stopped")
}
