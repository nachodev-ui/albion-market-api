package handlers

import (
	"compress/gzip"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/service"
)

type IngestHandler struct {
	service            *service.MarketService
	bearerTokens       []string
	maxRequestBodySize int64
}

func NewIngestHandler(service *service.MarketService, bearerTokens []string, maxRequestBodySize int64) *IngestHandler {
	cleanTokens := make([]string, 0, len(bearerTokens))
	for _, token := range bearerTokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		cleanTokens = append(cleanTokens, token)
	}
	if maxRequestBodySize <= 0 {
		maxRequestBodySize = 5 << 20
	}
	return &IngestHandler{
		service:            service,
		bearerTokens:       cleanTokens,
		maxRequestBodySize: maxRequestBodySize,
	}
}

func (h *IngestHandler) IngestPrices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !authorizedBearer(r.Header.Get("Authorization"), h.bearerTokens) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "unauthorized",
		})
		return
	}

	defer r.Body.Close()

	bodyReader, err := h.requestBodyReader(w, r)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errUnsupportedEncoding) {
			status = http.StatusUnsupportedMediaType
		}
		writeJSON(w, status, map[string]any{
			"error": err.Error(),
		})
		return
	}
	defer func() {
		if closer, ok := bodyReader.(io.Closer); ok {
			_ = closer.Close()
		}
	}()

	decoder := json.NewDecoder(bodyReader)
	decoder.DisallowUnknownFields()

	var req domain.IngestPricesRequest
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid json",
		})
		return
	}
	if decoder.More() {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid json",
		})
		return
	}

	resp, err := h.service.IngestPrices(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrIngestRequestAlreadyProcessing):
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":      err.Error(),
				"request_id": req.RequestID,
			})
		case errors.Is(err, service.ErrIngestRequestPayloadConflict):
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":      err.Error(),
				"request_id": req.RequestID,
			})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": err.Error(),
			})
		}
		return
	}

	status := http.StatusAccepted
	if resp.Duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, resp)
}

var errUnsupportedEncoding = errors.New("unsupported content encoding")

func (h *IngestHandler) requestBodyReader(w http.ResponseWriter, r *http.Request) (io.Reader, error) {
	limitedBody := http.MaxBytesReader(w, r.Body, h.maxRequestBodySize)

	switch strings.TrimSpace(strings.ToLower(r.Header.Get("Content-Encoding"))) {
	case "", "identity":
		return limitedBody, nil
	case "gzip":
		reader, err := gzip.NewReader(limitedBody)
		if err != nil {
			return nil, errors.New("invalid gzip body")
		}
		return reader, nil
	default:
		return nil, errUnsupportedEncoding
	}
}

func authorizedBearer(headerValue string, expectedTokens []string) bool {
	if len(expectedTokens) == 0 {
		return false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(headerValue, prefix) {
		return false
	}
	provided := strings.TrimSpace(strings.TrimPrefix(headerValue, prefix))
	if provided == "" {
		return false
	}
	for _, expectedToken := range expectedTokens {
		if subtle.ConstantTimeCompare([]byte(provided), []byte(expectedToken)) == 1 {
			return true
		}
	}
	return false
}
