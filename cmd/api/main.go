package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/yaselnik/Search-Engine/internal/infrastructure/analyzer"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/index"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/loader"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/ranker"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/storage"
	usecase_idx "github.com/yaselnik/Search-Engine/internal/usecase/indexer"
	usecase_search "github.com/yaselnik/Search-Engine/internal/usecase/searcher"
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

	indexerUC := usecase_idx.NewIndexer(docStorage, idx, analyzer.RegexpTokenize, logger)
	if err := indexerUC.IndexAll(ctx); err != nil {
		logger.Error("indexing failed, exiting", "error", err)
		os.Exit(1)
	}

	bm25 := ranker.NewBM25()
	searcher := usecase_search.NewSearcher(docStorage, idx, analyzer.RegexpTokenize, bm25)

	logger.Info("search engine is ready. Type your query or 'exit' to quit.")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\nsearch> ")

		if ctx.Err() != nil {
			fmt.Println("\nShutting down gracefully...")
			break
		}

		if !scanner.Scan() {
			break
		}

		query := strings.TrimSpace(scanner.Text())
		if query == "" {
			continue
		}

		lowerQuery := strings.ToLower(query)
		if lowerQuery == "exit" || lowerQuery == "quit" {
			fmt.Println("Goodbye!")
			break
		}

		results, err := searcher.Search(ctx, query, 5)
		if err != nil {
			logger.Error("search failed", "error", err)
			fmt.Println("An error occurred during search.")
			continue
		}

		if len(results) == 0 {
			fmt.Println("No results found.")
			continue
		}

		fmt.Printf("\nFound %d result(s):\n", len(results))
		for i, res := range results {
			fmt.Printf("  [%d] Score: %.4f | Title: %s\n", i+1, res.Score, res.Document.Title)
			fmt.Printf("      Source: %s\n", res.Document.URL)
			fmt.Printf("      Snippet: %s\n", res.Snippet)
			fmt.Println(strings.Repeat("-", 80))
		}
	}

	if err := scanner.Err(); err != nil {
		logger.Error("error reading input", "error", err)
	}

	logger.Info("shutting down gracefully...")
}
