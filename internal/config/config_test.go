package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "")
	t.Setenv("OPENAI_API_KEY", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLMProvider != "ollama" {
		t.Fatalf("provider %s", cfg.LLMProvider)
	}
	if cfg.OllamaEmbedModel != "nomic-embed-text" || cfg.OllamaEmbedDimensions != 768 {
		t.Fatalf("embed config %+v", cfg)
	}
	if len(cfg.ExtractionTables) != 3 || cfg.ExtractionTables[0] != "microservice" || cfg.ExtractionTables[2] != "pulsar_topic" {
		t.Fatalf("tables %#v", cfg.ExtractionTables)
	}
}

func TestOpenAIRequiresKey(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "openai")
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected missing key error")
	}
}
