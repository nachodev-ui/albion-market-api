package handlers

import (
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

func (h *IngestHandler) IngestHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	startedAt := time.Now()
	h.historyMetrics.RequestStarted(startedAt)

	statusCode := http.StatusInternalServerError
	requestID := ""
	serverName := ""
	entries := 0
	buckets := 0
	acceptedEntries := 0
	acceptedBuckets := 0
	historyRowsTouched := int64(0)
	duplicate := false
	errorKind := "internal_error"
	errorDetail := "request ended without a response"
	authKeyID := "-"

	defer func() {
		duration := time.Since(startedAt)
		h.historyMetrics.RequestFinished(observability.HistoryIngestObservation{
			CompletedAt:        time.Now(),
			Duration:           duration,
			StatusCode:         statusCode,
			AcceptedEntries:    acceptedEntries,
			AcceptedBuckets:    acceptedBuckets,
			HistoryRowsTouched: historyRowsTouched,
			Duplicate:          duplicate,
			ErrorKind:          errorKind,
		})
		h.logHistoryOutcome(
			requestID,
			serverName,
			entries,
			buckets,
			acceptedEntries,
			acceptedBuckets,
			historyRowsTouched,
			duplicate,
			statusCode,
			duration,
			errorKind,
			errorDetail,
			authKeyID,
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

	authResult := h.authenticator.Authenticate(r)
	if !authResult.Authenticated {
		statusCode, errorKind, errorDetail = writeAuthenticationFailure(w, authResult.Failure)
		return
	}
	authKeyID = authResult.KeyID
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

	var req domain.IngestHistoryRequest
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
	for _, entry := range req.Entries {
		buckets += len(entry.History)
	}

	resp, err := h.service.IngestHistory(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidHistoryIngestRequest):
			statusCode = http.StatusBadRequest
			errorKind = "validation_error"
			errorDetail = strings.TrimPrefix(
				err.Error(),
				service.ErrInvalidHistoryIngestRequest.Error()+": ",
			)
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
			writeJSON(w, statusCode, ingestErrorResponse{
				Error:     "internal server error",
				RequestID: requestID,
			})
		}
		return
	}

	statusCode = http.StatusAccepted
	if resp.Duplicate {
		statusCode = http.StatusOK
	}
	acceptedEntries = resp.AcceptedEntries
	acceptedBuckets = resp.AcceptedBuckets
	historyRowsTouched = resp.HistoryRowsTouched
	duplicate = resp.Duplicate
	errorKind = ""
	errorDetail = ""
	writeJSON(w, statusCode, resp)
}

func (h *IngestHandler) logHistoryOutcome(
	requestID string,
	serverName string,
	entries int,
	buckets int,
	acceptedEntries int,
	acceptedBuckets int,
	historyRowsTouched int64,
	duplicate bool,
	statusCode int,
	duration time.Duration,
	errorKind string,
	errorDetail string,
	authKeyID string,
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
		observability.F("buckets", buckets),
		observability.F("accepted_entries", acceptedEntries),
		observability.F("accepted_buckets", acceptedBuckets),
		observability.F("history_rows_touched", historyRowsTouched),
		observability.F("duplicate", duplicate),
		observability.F("status", statusCode),
		observability.F("duration_ms", durationMilliseconds(duration)),
		observability.F("auth_key_id", authKeyID),
	}

	switch {
	case statusCode >= 500:
		fields = append(fields,
			observability.F("error_kind", errorKind),
			observability.F("error", errorDetail),
		)
		h.logger.Error("ingest.history_failed", fields...)
	case statusCode >= 400:
		fields = append(fields,
			observability.F("error_kind", errorKind),
			observability.F("error", errorDetail),
		)
		h.logger.Warn("ingest.history_rejected", fields...)
	case duplicate:
		h.logger.Duplicate("ingest.history_duplicate", fields...)
	default:
		h.logger.Success("ingest.history_completed", fields...)
	}
}
