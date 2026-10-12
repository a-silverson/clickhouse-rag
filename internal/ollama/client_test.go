package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbedAndChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/embed":
			_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2]]}`))
		case "/api/chat":
			_, _ = w.Write([]byte(`{"message":{"content":"{\"confidence\":1,\"rows\":[]}"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := New(srv.URL, "nomic-embed-text", "llama3.2", 2)
	vecs, err := client.Embed(context.Background(), []string{"hello"})
	if err != nil || len(vecs) != 1 || len(vecs[0]) != 2 {
		t.Fatalf("embed %#v %v", vecs, err)
	}
	text, err := client.Chat(context.Background(), "sys", "user")
	if err != nil || text == "" {
		t.Fatalf("chat %q %v", text, err)
	}
}
