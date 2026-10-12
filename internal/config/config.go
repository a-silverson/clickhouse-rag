package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr string

	ClickHouseAddr     string
	ClickHouseDatabase string
	ClickHouseUser     string
	ClickHousePassword string

	MCPReaderUser     string
	MCPReaderPassword string
	MCPWriterUser     string
	MCPWriterPassword string

	OllamaURL             string
	OllamaEmbedModel      string
	OllamaEmbedDimensions int
	LLMProvider           string
	OllamaLLMModel        string
	OpenAIAPIKey          string
	OpenAIModel           string
	OpenAIBaseURL         string

	ExtractionTables         []string
	ExtractionMinConfidence  float64
	ExtractionChunksPerTable int
	ChunkRunes               int
	ChunkOverlap             int
	AutoMigrate              bool
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:                 env("HTTP_ADDR", ":8080"),
		ClickHouseAddr:           env("CLICKHOUSE_ADDR", "127.0.0.1:9000"),
		ClickHouseDatabase:       env("CLICKHOUSE_DATABASE", "rag"),
		ClickHouseUser:           env("CLICKHOUSE_USER", "default"),
		ClickHousePassword:       os.Getenv("CLICKHOUSE_PASSWORD"),
		MCPReaderUser:            os.Getenv("MCP_READER_USER"),
		MCPReaderPassword:        os.Getenv("MCP_READER_PASSWORD"),
		MCPWriterUser:            os.Getenv("MCP_WRITER_USER"),
		MCPWriterPassword:        os.Getenv("MCP_WRITER_PASSWORD"),
		OllamaURL:                env("OLLAMA_URL", "http://127.0.0.1:11434"),
		OllamaEmbedModel:         env("OLLAMA_EMBED_MODEL", "nomic-embed-text"),
		LLMProvider:              strings.ToLower(env("LLM_PROVIDER", "ollama")),
		OllamaLLMModel:           env("OLLAMA_LLM_MODEL", "llama3.2"),
		OpenAIAPIKey:             os.Getenv("OPENAI_API_KEY"),
		OpenAIModel:              env("OPENAI_MODEL", "gpt-4o-mini"),
		OpenAIBaseURL:            env("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		ExtractionMinConfidence:  0.8,
		ExtractionChunksPerTable: 3,
		ChunkRunes:               800,
		ChunkOverlap:             100,
		AutoMigrate:              true,
	}
	var err error
	if cfg.OllamaEmbedDimensions, err = envInt("OLLAMA_EMBED_DIMENSIONS", 768); err != nil {
		return Config{}, err
	}
	if cfg.ExtractionMinConfidence, err = envFloat("EXTRACTION_MIN_CONFIDENCE", cfg.ExtractionMinConfidence); err != nil {
		return Config{}, err
	}
	if cfg.ExtractionChunksPerTable, err = envInt("EXTRACTION_CHUNKS_PER_TABLE", cfg.ExtractionChunksPerTable); err != nil {
		return Config{}, err
	}
	if cfg.ChunkRunes, err = envInt("CHUNK_RUNES", cfg.ChunkRunes); err != nil {
		return Config{}, err
	}
	if cfg.ChunkOverlap, err = envInt("CHUNK_OVERLAP", cfg.ChunkOverlap); err != nil {
		return Config{}, err
	}
	if v := os.Getenv("AUTO_MIGRATE"); v != "" {
		cfg.AutoMigrate, err = strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("AUTO_MIGRATE: %w", err)
		}
	}
	cfg.ExtractionTables = splitList(env("EXTRACTION_TABLES", "microservice,api,pulsar_topic"))
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	switch c.LLMProvider {
	case "ollama", "openai":
	default:
		return fmt.Errorf("LLM_PROVIDER must be openai or ollama")
	}
	if c.LLMProvider == "openai" && c.OpenAIAPIKey == "" {
		return fmt.Errorf("OPENAI_API_KEY is required when LLM_PROVIDER=openai")
	}
	if c.OllamaEmbedDimensions < 1 {
		return fmt.Errorf("OLLAMA_EMBED_DIMENSIONS must be positive")
	}
	if c.ExtractionMinConfidence < 0 || c.ExtractionMinConfidence > 1 {
		return fmt.Errorf("EXTRACTION_MIN_CONFIDENCE must be between 0 and 1")
	}
	if c.ExtractionChunksPerTable < 1 {
		return fmt.Errorf("EXTRACTION_CHUNKS_PER_TABLE must be positive")
	}
	if c.ChunkRunes < 1 {
		return fmt.Errorf("CHUNK_RUNES must be positive")
	}
	if c.ChunkOverlap < 0 || c.ChunkOverlap >= c.ChunkRunes {
		return fmt.Errorf("CHUNK_OVERLAP must be >= 0 and smaller than CHUNK_RUNES")
	}
	if len(c.ExtractionTables) == 0 {
		return fmt.Errorf("EXTRACTION_TABLES must list at least one table")
	}
	if (c.MCPReaderUser == "") != (c.MCPWriterUser == "") {
		return fmt.Errorf("MCP_READER_USER and MCP_WRITER_USER must both be set or both be empty")
	}
	return nil
}

func (c Config) SeparateMCPRoles() bool {
	return c.MCPReaderUser != "" && c.MCPWriterUser != ""
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func envFloat(key string, fallback float64) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func splitList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
