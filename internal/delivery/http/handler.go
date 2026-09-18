package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/yaselnik/Search-Engine/internal/domain"
	"github.com/yaselnik/Search-Engine/internal/usecase/searcher"
)

type Handler struct {
	searcher *searcher.Searcher
	storage  domain.DocumentStorage
	logger   *slog.Logger
}

func NewHandler(searcher *searcher.Searcher, storage domain.DocumentStorage, logger *slog.Logger) *Handler {
	return &Handler{
		searcher: searcher,
		storage:  storage,
		logger:   logger,
	}
}

// Handles GET /api/search?q=...&limit=...
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		h.respondError(w, http.StatusBadRequest, "Bad Request", "Query parameter 'q' is required")
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
			if limit > 50 {
				limit = 50
			}
		}
	}

	results, err := h.searcher.Search(r.Context(), query, limit)
	if err != nil {
		h.logger.Error("search failed", "error", err, "query", query)
		h.respondError(w, http.StatusInternalServerError, "Internal Server Error", "Failed to execute search")
		return
	}

	items := make([]SearchResultItem, 0, len(results))
	for _, res := range results {
		items = append(items, mapToSearchResultItem(res))
	}

	response := SearchResponse{
		Results: items,
		Count:   len(items),
		Query:   query,
	}

	h.respondJSON(w, http.StatusOK, response)
}

// Handles GET /api/documents/{id}
func (h *Handler) GetDocument(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	if idStr == "" {
		h.respondError(w, http.StatusBadRequest, "Bad Request", "Document ID is required")
		return
	}

	idUint, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Bad Request", "Invalid document ID format")
		return
	}

	doc, err := h.storage.GetDocumentByID(r.Context(), domain.DocID(idUint))
	if err != nil {
		if errors.Is(err, domain.ErrDocumentNotFound) {
			h.respondError(w, http.StatusNotFound, "Not Found", "Document not found")
			return
		}
		h.logger.Error("failed to get document", "error", err, "id", idStr)
		h.respondError(w, http.StatusInternalServerError, "Internal Server Error", "Failed to retrieve document")
		return
	}

	h.respondJSON(w, http.StatusOK, doc)
}

// --- Utils ---

func (h *Handler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("failed to encode json response", "error", err)
	}
}

func (h *Handler) respondError(w http.ResponseWriter, status int, errTitle, errMsg string) {
	h.respondJSON(w, status, ErrorResponse{
		Error:   errTitle,
		Message: errMsg,
	})
}
