package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/a-silverson/clickhouse-rag/internal/schema"
)

type ChatClient interface {
	Chat(ctx context.Context, system, user string) (string, error)
}

type Result struct {
	Confidence float64
	Rows       []map[string]any
}

type Extractor struct {
	Chat ChatClient
}

func (e Extractor) Extract(ctx context.Context, tableName string, chunks []string) (Result, error) {
	table, err := schema.Lookup(tableName)
	if err != nil {
		return Result{}, err
	}
	fields := make([]string, 0, len(table.Columns))
	for _, col := range table.Columns {
		req := "optional"
		if col.Required {
			req = "required"
		}
		fields = append(fields, fmt.Sprintf("%s (%s, %s)", col.Name, col.Kind, req))
	}
	system := "You extract microservices, their APIs, and Pulsar topics from source text. Reply with JSON only, no markdown. Shape: {\"confidence\":0.0,\"rows\":[{...}]}. confidence is your certainty from 0 to 1. Omit a row when the text does not support it. Use only these fields."
	user := fmt.Sprintf("Table %s fields: %s\n\nSource:\n%s", table.Name, strings.Join(fields, ", "), strings.Join(chunks, "\n---\n"))
	raw, err := e.Chat.Chat(ctx, system, user)
	if err != nil {
		return Result{}, err
	}
	return Parse(raw)
}

func Parse(raw string) (Result, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var decoded struct {
		Confidence float64          `json:"confidence"`
		Rows       []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return Result{}, fmt.Errorf("parse extraction json: %w", err)
	}
	if decoded.Confidence < 0 || decoded.Confidence > 1 {
		return Result{}, fmt.Errorf("confidence out of range")
	}
	if decoded.Rows == nil {
		decoded.Rows = []map[string]any{}
	}
	return Result{Confidence: decoded.Confidence, Rows: decoded.Rows}, nil
}
