package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/service"
)

type PricesHandler struct {
	service *service.MarketService
}

func NewPricesHandler(service *service.MarketService) *PricesHandler {
	return &PricesHandler{service: service}
}

func (h *PricesHandler) QueryCurrentPrices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	defer r.Body.Close()

	var req domain.PriceQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid json",
		})
		return
	}

	resp, err := h.service.QueryCurrentPrices(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}
