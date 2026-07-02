package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/observability"
)

const defaultReadinessTimeout = 2 * time.Second

type HealthHandler struct {
	readiness        observability.ReadinessChecker
	readinessTimeout time.Duration
}

func NewHealthHandler(
	readiness observability.ReadinessChecker,
	readinessTimeout ...time.Duration,
) *HealthHandler {
	timeout := defaultReadinessTimeout
	if len(readinessTimeout) > 0 && readinessTimeout[0] > 0 {
		timeout = readinessTimeout[0]
	}
	return &HealthHandler{readiness: readiness, readinessTimeout: timeout}
}

// Healthz is a liveness probe. It intentionally does not depend on PostgreSQL,
// so a brief database outage does not cause the container runtime to restart a
// healthy API process.
func (h *HealthHandler) Healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Readyz verifies that the pool can provide a connection, PostgreSQL answers
// through that connection, and the schema marker and required relations exist.
func (h *HealthHandler) Readyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	if h == nil || h.readiness == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "error",
			"error":  "service unavailable",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.readinessTimeout)
	defer cancel()
	snapshot := h.readiness.Check(ctx)
	if !snapshot.Ready {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "error",
			"error":  "service unavailable",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
