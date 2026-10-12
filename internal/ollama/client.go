package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	EmbedModel string
	ChatModel  string
	Dimensions int
	HTTP       *http.Client
}

func New(baseURL, embedModel, chatModel string, dimensions int) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		EmbedModel: embedModel,
		ChatModel:  chatModel,
		Dimensions: dimensions,
		HTTP:       &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, _ := json.Marshal(map[string]any{
		"model": c.EmbedModel,
		"input": texts,
	})
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := c.post(ctx, "/api/embed", body, &out); err != nil {
		return nil, err
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama returned %d embeddings for %d inputs", len(out.Embeddings), len(texts))
	}
	for i, vec := range out.Embeddings {
		if c.Dimensions > 0 && len(vec) != c.Dimensions {
			return nil, fmt.Errorf("embedding %d has %d dimensions, want %d", i, len(vec), c.Dimensions)
		}
	}
	return out.Embeddings, nil
}

func (c *Client) Chat(ctx context.Context, system, user string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":  c.ChatModel,
		"stream": false,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	})
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := c.post(ctx, "/api/chat", body, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.Message.Content) == "" {
		return "", fmt.Errorf("ollama returned an empty chat response")
	}
	return out.Message.Content, nil
}

func (c *Client) post(ctx context.Context, path string, body []byte, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("ollama %s: %s", res.Status, strings.TrimSpace(string(payload)))
	}
	if err := json.Unmarshal(payload, dest); err != nil {
		return fmt.Errorf("decode ollama response: %w", err)
	}
	return nil
}
