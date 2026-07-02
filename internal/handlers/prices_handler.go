package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/service"
)

const defaultPublicQueryBodyBytes int64 = 1 << 20

type pricesService interface {
	Markets(includeDisabled bool) domain.MarketCatalogResponse
	QueryCurrentPrices(context.Context, domain.PriceQueryRequest) (domain.PriceQueryResponse, error)
}

type PricesHandler struct {
	service            pricesService
	maxRequestBodySize int64
}

func NewPricesHandler(service pricesService, bodyLimits ...int64) *PricesHandler {
	maxRequestBodySize := defaultPublicQueryBodyBytes
	if len(bodyLimits) > 0 && bodyLimits[0] > 0 {
		maxRequestBodySize = bodyLimits[0]
	}
	return &PricesHandler{service: service, maxRequestBodySize: maxRequestBodySize}
}

func (h *PricesHandler) ListMarkets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	includeDisabled := false
	if raw := strings.TrimSpace(r.URL.Query().Get("includeDisabled")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "includeDisabled must be true or false",
			})
			return
		}
		includeDisabled = parsed
	}

	writeJSON(w, http.StatusOK, h.service.Markets(includeDisabled))
}

// GetCurrentPrices mirrors the receiver-local GET contract for simple browser
// requests. The batch POST endpoint remains preferable when querying several
// markets or mixed qualities.
func (h *PricesHandler) GetCurrentPrices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	quality, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("quality")), 10, 16)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "quality must be an integer between 1 and 5",
		})
		return
	}

	itemIDs := splitCommaSeparated(r.URL.Query().Get("itemIds"))
	entries := make([]domain.PriceQueryEntry, 0, len(itemIDs))
	for _, itemID := range itemIDs {
		entries = append(entries, domain.PriceQueryEntry{
			ItemKey: itemID,
			Quality: int16(quality),
		})
	}

	req := domain.PriceQueryRequest{
		Server:     domain.Server(strings.TrimSpace(r.URL.Query().Get("server"))),
		MarketKeys: []string{r.URL.Query().Get("marketKey")},
		Entries:    entries,
	}
	h.query(w, r, req)
}

func (h *PricesHandler) QueryCurrentPrices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"error": "content type must be application/json"})
		return
	}
	defer r.Body.Close()

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, h.maxRequestBodySize))
	decoder.DisallowUnknownFields()

	var req domain.PriceQueryRequest
	if err := decoder.Decode(&req); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
				"error": "request body too large",
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}

	h.query(w, r, req)
}

func (h *PricesHandler) query(w http.ResponseWriter, r *http.Request, req domain.PriceQueryRequest) {
	resp, err := h.service.QueryCurrentPrices(r.Context(), req)
	if err != nil {
		if errors.Is(err, service.ErrInvalidPriceQuery) {
			message := strings.TrimPrefix(err.Error(), service.ErrInvalidPriceQuery.Error()+": ")
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": message})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal server error",
		})
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

func splitCommaSeparated(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
