package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/nachodev-ui/albion-market-api/internal/authn"
)

type Handler struct {
	service             *Service
	webhookSecret       []byte
	maxWebhookBodyBytes int64
}

func NewHandler(service *Service, webhookSecret string, maxWebhookBodyBytes int64) *Handler {
	return &Handler{
		service:             service,
		webhookSecret:       []byte(webhookSecret),
		maxWebhookBodyBytes: maxWebhookBodyBytes,
	}
}

func (h *Handler) Checkout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	session, err := h.service.CreateCheckout(r.Context(), identity)
	if err != nil {
		switch {
		case errors.Is(err, ErrAlreadySubscribed):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "subscription already exists"})
		case errors.Is(err, ErrProviderUnavailable):
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "billing provider unavailable"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func (h *Handler) Portal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	session, err := h.service.CustomerPortal(r.Context(), identity)
	if err != nil {
		switch {
		case errors.Is(err, ErrSubscriptionMissing):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "subscription not found"})
		case errors.Is(err, ErrProviderUnavailable):
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "billing provider unavailable"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (h *Handler) Webhook(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if h == nil || h.service == nil || len(h.webhookSecret) == 0 || h.maxWebhookBodyBytes <= 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "billing unavailable"})
		return
	}

	body := http.MaxBytesReader(w, r.Body, h.maxWebhookBodyBytes)
	raw, err := io.ReadAll(body)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if !validSignature(h.webhookSecret, raw, r.Header.Get("X-Signature")) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid webhook signature"})
		return
	}

	result, err := h.service.ProcessWebhook(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrInvalidWebhook) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid webhook payload"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "webhook processing failed"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func validSignature(secret, raw []byte, signature string) bool {
	signature = strings.TrimSpace(signature)
	provided, err := hex.DecodeString(signature)
	if err != nil || len(provided) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(raw)
	return hmac.Equal(mac.Sum(nil), provided)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
