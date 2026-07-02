package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/observability"
)

const metricsCollectionTimeout = 2 * time.Second

type MetricsHandler struct {
	exporter *observability.PrometheusExporter
}

func NewMetricsHandler(exporter *observability.PrometheusExporter) *MetricsHandler {
	return &MetricsHandler{exporter: exporter}
}

func (h *MetricsHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if h == nil || h.exporter == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "metrics unavailable"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), metricsCollectionTimeout)
	defer cancel()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = h.exporter.Write(ctx, w)
}
