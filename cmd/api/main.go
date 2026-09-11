package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/yaselnik/Search-Engine/internal/infrastructure/analyzer"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/index"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/loader"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/storage"
	"github.com/yaselnik/Search-Engine/internal/usecase"
)

func main() {
	dataPath := flag.String("data", "./data", "path to directory with documents")
	logLevel := flag.String("log-level", "info", "log level (debug, info, warn, error)")
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

	logger.Info("search engine is ready and running. Press Ctrl+C to stop.")
	<-ctx.Done()

	logger.Info("shutting down gracefully...")
}
