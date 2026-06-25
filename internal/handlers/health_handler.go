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
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := h.service.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
