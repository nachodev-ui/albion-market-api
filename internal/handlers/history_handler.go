package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/service"
)

type historyService interface {
	QueryMarketHistory(context.Context, domain.HistoryQueryRequest) (domain.HistoryQueryResponse, error)
}

type HistoryHandler struct {
	service            historyService
	now                func() time.Time
	maxRequestBodySize int64
}

func NewHistoryHandler(service historyService, bodyLimits ...int64) *HistoryHandler {
	maxRequestBodySize := defaultPublicQueryBodyBytes
	if len(bodyLimits) > 0 && bodyLimits[0] > 0 {
		maxRequestBodySize = bodyLimits[0]
	}
	return &HistoryHandler{
		service:            service,
		now:                time.Now,
		maxRequestBodySize: maxRequestBodySize,
	}
}

// GetMarketHistory mirrors the receiver-local GET contract used by the current
// frontend. The explicit batch POST route should be preferred by optimizers and
// other callers that need several item/market combinations.
func (h *HistoryHandler) GetMarketHistory(w http.ResponseWriter, r *http.Request) {
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

	rangeStart, rangeEnd, err := resolveHistoryGETRange(r.URL.Query(), h.now().UTC())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "limit must be a positive integer",
			})
			return
		}
	}

	req := domain.HistoryQueryRequest{
		Server:     domain.Server(strings.TrimSpace(r.URL.Query().Get("server"))),
		MarketKeys: []string{r.URL.Query().Get("marketKey")},
		Entries: []domain.HistoryQueryEntry{{
			ItemKey: strings.TrimSpace(r.URL.Query().Get("itemId")),
			Quality: int16(quality),
		}},
		RangeStart: rangeStart,
		RangeEnd:   rangeEnd,
	}

	h.query(w, r, req)
}

func (h *HistoryHandler) QueryMarketHistory(w http.ResponseWriter, r *http.Request) {
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

	var req domain.HistoryQueryRequest
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

func (h *HistoryHandler) query(w http.ResponseWriter, r *http.Request, req domain.HistoryQueryRequest) {
	resp, err := h.service.QueryMarketHistory(r.Context(), req)
	if err != nil {
		if errors.Is(err, service.ErrInvalidHistoryQuery) {
			message := strings.TrimPrefix(err.Error(), service.ErrInvalidHistoryQuery.Error()+": ")
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

type queryValues interface {
	Get(string) string
}

func resolveHistoryGETRange(values queryValues, now time.Time) (string, string, error) {
	rangeStart := strings.TrimSpace(values.Get("rangeStart"))
	rangeEnd := strings.TrimSpace(values.Get("rangeEnd"))
	if rangeStart != "" || rangeEnd != "" {
		if rangeStart == "" || rangeEnd == "" {
			return "", "", errors.New("rangeStart and rangeEnd must be provided together")
		}
		return rangeStart, rangeEnd, nil
	}

	period := strings.TrimSpace(values.Get("period"))
	if period == "" {
		period = "4-weeks"
	}

	days := 0
	switch period {
	case "7-days":
		days = 7
	case "4-weeks":
		days = 28
	default:
		return "", "", fmt.Errorf("period must be 7-days or 4-weeks")
	}

	currentUTCDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	end := currentUTCDate.AddDate(0, 0, -1)
	start := end.AddDate(0, 0, -(days - 1))
	return start.Format("2006-01-02"), end.Format("2006-01-02"), nil
}
