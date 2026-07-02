package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/nachodev-ui/albion-market-api/internal/service"
)

type HealthHandler struct {
	service *service.MarketService
}

func NewHealthHandler(service *service.MarketService) *HealthHandler {
	return &HealthHandler{service: service}
}

func (h *HealthHandler) Healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	if err := h.service.Ping(r.Context()); err != nil {
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
