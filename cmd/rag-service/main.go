package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/a-silverson/clickhouse-rag/internal/config"
	"github.com/a-silverson/clickhouse-rag/internal/extract"
	"github.com/a-silverson/clickhouse-rag/internal/httpapi"
	"github.com/a-silverson/clickhouse-rag/internal/mcpapi"
	"github.com/a-silverson/clickhouse-rag/internal/ollama"
	"github.com/a-silverson/clickhouse-rag/internal/openai"
	"github.com/a-silverson/clickhouse-rag/internal/pipeline"
	"github.com/a-silverson/clickhouse-rag/internal/store"
)

func main() {
	stdio := flag.Bool("mcp-stdio", false, "serve MCP on stdin/stdout instead of HTTP")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := openStore(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if cfg.AutoMigrate {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = db.Migrate(ctx)
		cancel()
		if err != nil {
			log.Fatal(err)
		}
	}

	ollamaClient := ollama.New(cfg.OllamaURL, cfg.OllamaEmbedModel, cfg.OllamaLLMModel, cfg.OllamaEmbedDimensions)
	var chat extract.ChatClient = ollamaClient
	if cfg.LLMProvider == "openai" {
		chat = openai.New(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.OpenAIModel)
	}
	pipe := pipeline.Pipeline{
		Store:          db,
		Embed:          ollamaClient,
		Extract:        extract.Extractor{Chat: chat},
		Tables:         cfg.ExtractionTables,
		MinConfidence:  cfg.ExtractionMinConfidence,
		ChunksPerTable: cfg.ExtractionChunksPerTable,
		ChunkRunes:     cfg.ChunkRunes,
		ChunkOverlap:   cfg.ChunkOverlap,
	}
	mcp := mcpapi.Server{DB: db, Embed: ollamaClient}
	if *stdio {
		log.SetOutput(os.Stderr)
		if err := mcpapi.ServeStdio(os.Stdin, os.Stdout, mcp); err != nil {
			log.Fatal(err)
		}
		return
	}
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.Server{Store: db, Pipeline: pipe, MCP: mcp.Handler()}.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("rag-service listening on %s", cfg.HTTPAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func openStore(cfg config.Config) (*store.ClickHouse, error) {
	admin := store.Credentials{Addr: cfg.ClickHouseAddr, User: cfg.ClickHouseUser, Password: cfg.ClickHousePassword}
	var reader, writer *store.Credentials
	if cfg.SeparateMCPRoles() {
		reader = &store.Credentials{Addr: cfg.ClickHouseAddr, User: cfg.MCPReaderUser, Password: cfg.MCPReaderPassword}
		writer = &store.Credentials{Addr: cfg.ClickHouseAddr, User: cfg.MCPWriterUser, Password: cfg.MCPWriterPassword}
	}
	return store.Open(admin, reader, writer)
}
