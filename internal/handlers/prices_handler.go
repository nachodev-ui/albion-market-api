package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strings"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/service"
)

type pricesService interface {
	QueryCurrentPrices(context.Context, domain.PriceQueryRequest) (domain.PriceQueryResponse, error)
}

type PricesHandler struct {
	service            pricesService
	maxRequestBodySize int64
}

func NewPricesHandler(service pricesService, maxRequestBodySize int64) *PricesHandler {
	if maxRequestBodySize <= 0 {
		maxRequestBodySize = 64 << 10
	}
	return &PricesHandler{service: service, maxRequestBodySize: maxRequestBodySize}
}

func (h *PricesHandler) QueryCurrentPrices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "content type must be application/json"})
		return
	}

	defer r.Body.Close()
	body := http.MaxBytesReader(w, r.Body, h.maxRequestBodySize)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()

	var req domain.PriceQueryRequest
	if err := decoder.Decode(&req); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	resp, err := h.service.QueryCurrentPrices(r.Context(), req)
	if err != nil {
		if errors.Is(err, service.ErrInvalidPriceQuery) {
			detail := strings.TrimPrefix(err.Error(), service.ErrInvalidPriceQuery.Error()+": ")
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": detail})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func isJSONContentType(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && mediaType == "application/json"
}
