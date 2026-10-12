package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/a-silverson/clickhouse-rag/internal/pipeline"
	"github.com/a-silverson/clickhouse-rag/internal/store"
)

type Server struct {
	Store    store.Store
	Pipeline pipeline.Pipeline
	MCP      http.Handler
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/files", s.listFiles)
	mux.HandleFunc("POST /api/files", s.uploadFile)
	mux.HandleFunc("DELETE /api/files/{file_id}", s.deleteFile)
	mux.HandleFunc("GET /api/extractions", s.listExtractions)
	mux.HandleFunc("POST /api/extractions/{id}/approve", s.approve)
	mux.HandleFunc("POST /api/extractions/{id}/reject", s.reject)
	if s.MCP != nil {
		mux.Handle("POST /mcp", s.MCP)
	}
	return mux
}

func (s Server) listFiles(w http.ResponseWriter, r *http.Request) {
	files, err := s.Store.ListFiles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if files == nil {
		files = []store.File{}
	}
	writeJSON(w, http.StatusOK, files)
}

func (s Server) uploadFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("file field is required"))
		return
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	uploaded, err := s.Pipeline.Ingest(r.Context(), header.Filename, header.Header.Get("Content-Type"), body)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	code := http.StatusCreated
	if uploaded.Unchanged || uploaded.Replaced {
		code = http.StatusOK
	}
	writeJSON(w, code, uploaded)
}

func (s Server) deleteFile(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteFile(r.Context(), r.PathValue("file_id")); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s Server) listExtractions(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListPendingExtractions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if items == nil {
		items = []store.Extraction{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s Server) approve(w http.ResponseWriter, r *http.Request) {
	item, err := s.Pipeline.Approve(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s Server) reject(w http.ResponseWriter, r *http.Request) {
	item, err := s.Pipeline.Reject(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeError(w, http.StatusBadRequest, err)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
