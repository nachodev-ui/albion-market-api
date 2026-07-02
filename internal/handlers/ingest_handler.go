package handlers

import (
	"compress/gzip"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/observability"
	"github.com/nachodev-ui/albion-market-api/internal/service"
)

type ingestService interface {
	IngestPrices(ctx context.Context, req domain.IngestPricesRequest) (domain.IngestPricesResponse, error)
	IngestHistory(ctx context.Context, req domain.IngestHistoryRequest) (domain.IngestHistoryResponse, error)
}

type IngestHandler struct {
	service            ingestService
	bearerTokens       []string
	maxRequestBodySize int64
	metrics            *observability.IngestMetrics
	historyMetrics     *observability.HistoryIngestMetrics
	logger             *observability.Logger
}

func NewIngestHandler(
	service ingestService,
	bearerTokens []string,
	maxRequestBodySize int64,
	metrics *observability.IngestMetrics,
	logger *observability.Logger,
	historyMetrics ...*observability.HistoryIngestMetrics,
) *IngestHandler {
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
	if metrics == nil {
		metrics = observability.NewIngestMetrics()
	}
	if logger == nil {
		logger = observability.NewLogger(io.Discard, "never")
	}
	historyMetricSet := observability.NewHistoryIngestMetrics()
	if len(historyMetrics) > 0 && historyMetrics[0] != nil {
		historyMetricSet = historyMetrics[0]
	}
	return &IngestHandler{
		service:            service,
		bearerTokens:       cleanTokens,
		maxRequestBodySize: maxRequestBodySize,
		metrics:            metrics,
		historyMetrics:     historyMetricSet,
		logger:             logger,
	}
}

func (h *IngestHandler) IngestPrices(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	h.metrics.RequestStarted(startedAt)

	statusCode := http.StatusInternalServerError
	requestID := ""
	serverName := ""
	entries := 0
	accepted := 0
	currentRowsTouched := int64(0)
	duplicate := false
	errorKind := "internal_error"
	errorDetail := "request ended without a response"

	defer func() {
		duration := time.Since(startedAt)
		h.metrics.RequestFinished(observability.IngestObservation{
			CompletedAt:        time.Now(),
			Duration:           duration,
			StatusCode:         statusCode,
			Accepted:           accepted,
			CurrentRowsTouched: currentRowsTouched,
			Duplicate:          duplicate,
			ErrorKind:          errorKind,
		})
		h.logOutcome(
			requestID,
			serverName,
			entries,
			accepted,
			currentRowsTouched,
			duplicate,
			statusCode,
			duration,
			errorKind,
			errorDetail,
		)
	}()

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		statusCode = http.StatusMethodNotAllowed
		errorKind = "method_not_allowed"
		errorDetail = "method not allowed"
		writeJSON(w, statusCode, ingestErrorResponse{Error: errorDetail})
		return
	}

	if !authorizedBearer(r.Header.Get("Authorization"), h.bearerTokens) {
		statusCode = http.StatusUnauthorized
		errorKind = "unauthorized"
		errorDetail = "unauthorized"
		writeJSON(w, statusCode, ingestErrorResponse{Error: errorDetail})
		return
	}
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		statusCode = http.StatusUnsupportedMediaType
		errorKind = "unsupported_content_type"
		errorDetail = "content type must be application/json"
		writeJSON(w, statusCode, ingestErrorResponse{Error: errorDetail})
		return
	}

	defer r.Body.Close()

	bodyReader, err := h.requestBodyReader(w, r)
	if err != nil {
		statusCode = http.StatusBadRequest
		errorKind = "invalid_content_encoding"
		if errors.Is(err, errUnsupportedEncoding) {
			statusCode = http.StatusUnsupportedMediaType
			errorKind = "unsupported_content_encoding"
		}
		errorDetail = err.Error()
		writeJSON(w, statusCode, ingestErrorResponse{Error: errorDetail})
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
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			statusCode = http.StatusRequestEntityTooLarge
			errorKind = "payload_too_large"
			errorDetail = "request body too large"
		} else {
			statusCode = http.StatusBadRequest
			errorKind = "invalid_json"
			errorDetail = "invalid json"
		}
		writeJSON(w, statusCode, ingestErrorResponse{Error: errorDetail})
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		statusCode = http.StatusBadRequest
		errorKind = "invalid_json"
		errorDetail = "invalid json"
		writeJSON(w, statusCode, ingestErrorResponse{Error: errorDetail})
		return
	}

	requestID = req.RequestID
	serverName = string(req.Server)
	entries = len(req.Entries)

	resp, err := h.service.IngestPrices(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidIngestRequest):
			statusCode = http.StatusBadRequest
			errorKind = "validation_error"
			errorDetail = strings.TrimPrefix(err.Error(), service.ErrInvalidIngestRequest.Error()+": ")
			writeJSON(w, statusCode, ingestErrorResponse{Error: errorDetail, RequestID: requestID})
		case errors.Is(err, service.ErrIngestRequestAlreadyProcessing):
			statusCode = http.StatusConflict
			errorKind = "request_already_processing"
			errorDetail = err.Error()
			writeJSON(w, statusCode, ingestErrorResponse{Error: errorDetail, RequestID: requestID})
		case errors.Is(err, service.ErrIngestRequestPayloadConflict):
			statusCode = http.StatusConflict
			errorKind = "request_payload_conflict"
			errorDetail = err.Error()
			writeJSON(w, statusCode, ingestErrorResponse{Error: errorDetail, RequestID: requestID})
		default:
			statusCode = http.StatusInternalServerError
			errorKind = "internal_error"
			errorDetail = err.Error()
			writeJSON(w, statusCode, ingestErrorResponse{Error: "internal server error", RequestID: requestID})
		}
		return
	}

	statusCode = http.StatusAccepted
	if resp.Duplicate {
		statusCode = http.StatusOK
	}
	accepted = resp.Accepted
	currentRowsTouched = resp.CurrentRowsTouched
	duplicate = resp.Duplicate
	errorKind = ""
	errorDetail = ""
	writeJSON(w, statusCode, resp)
}

type ingestErrorResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}

func (h *IngestHandler) logOutcome(
	requestID string,
	serverName string,
	entries int,
	accepted int,
	currentRowsTouched int64,
	duplicate bool,
	statusCode int,
	duration time.Duration,
	errorKind string,
	errorDetail string,
) {
	if requestID == "" {
		requestID = "-"
	}
	if serverName == "" {
		serverName = "-"
	}

	fields := []observability.Field{
		observability.F("request_id", requestID),
		observability.F("server", serverName),
		observability.F("entries", entries),
		observability.F("accepted", accepted),
		observability.F("current_rows_touched", currentRowsTouched),
		observability.F("duplicate", duplicate),
		observability.F("status", statusCode),
		observability.F("duration_ms", durationMilliseconds(duration)),
	}

	switch {
	case statusCode >= 500:
		fields = append(fields,
			observability.F("error_kind", errorKind),
			observability.F("error", errorDetail),
		)
		h.logger.Error("ingest.failed", fields...)
	case statusCode >= 400:
		fields = append(fields,
			observability.F("error_kind", errorKind),
			observability.F("error", errorDetail),
		)
		h.logger.Warn("ingest.rejected", fields...)
	case duplicate:
		h.logger.Duplicate("ingest.duplicate", fields...)
	default:
		h.logger.Success("ingest.completed", fields...)
	}
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple json values")
		}
		return err
	}
	return nil
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}

var errUnsupportedEncoding = errors.New("unsupported content encoding")

func (h *IngestHandler) requestBodyReader(w http.ResponseWriter, r *http.Request) (io.Reader, error) {
	compressedBody := http.MaxBytesReader(w, r.Body, h.maxRequestBodySize)

	switch strings.TrimSpace(strings.ToLower(r.Header.Get("Content-Encoding"))) {
	case "", "identity":
		return compressedBody, nil
	case "gzip":
		gzipReader, err := gzip.NewReader(compressedBody)
		if err != nil {
			return nil, errors.New("invalid gzip body")
		}
		return http.MaxBytesReader(w, gzipReader, h.maxRequestBodySize), nil
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
