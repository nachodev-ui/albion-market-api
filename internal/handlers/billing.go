package handlers

import (
	"net/http"

	"github.com/nachodev-ui/albion-market-api/internal/billing"
)

type AuthenticatedBillingHandler struct {
	handler       *billing.Handler
	authenticator AccountAuthenticator
}

func NewAuthenticatedBillingHandler(
	handler *billing.Handler,
	authenticator AccountAuthenticator,
) *AuthenticatedBillingHandler {
	return &AuthenticatedBillingHandler{
		handler:       handler,
		authenticator: authenticator,
	}
}

func (h *AuthenticatedBillingHandler) Checkout(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.handler == nil {
		writeBillingUnavailable(w)
		return
	}
	if r.Method != http.MethodPost {
		h.serveAuthenticated(w, r, h.handler.Checkout)
		return
	}
	h.serveAuthenticated(w, r, h.handler.Checkout)
}

func (h *AuthenticatedBillingHandler) Portal(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.handler == nil {
		writeBillingUnavailable(w)
		return
	}
	if r.Method != http.MethodPost {
		h.serveAuthenticated(w, r, h.handler.Portal)
		return
	}
	h.serveAuthenticated(w, r, h.handler.Portal)
}

func (h *AuthenticatedBillingHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.handler == nil {
		writeBillingUnavailable(w)
		return
	}
	if r.Method != http.MethodPost {
		h.servePublic(w, r, h.handler.Webhook)
		return
	}
	h.servePublic(w, r, h.handler.Webhook)
}

func (h *AuthenticatedBillingHandler) serveAuthenticated(
	w http.ResponseWriter,
	r *http.Request,
	next http.HandlerFunc,
) {
	if h.authenticator == nil {
		writeBillingUnavailable(w)
		return
	}
	h.authenticator.RequireScope(accountReadScope, next).ServeHTTP(w, r)
}

func (h *AuthenticatedBillingHandler) servePublic(
	w http.ResponseWriter,
	r *http.Request,
	next http.HandlerFunc,
) {
	next.ServeHTTP(w, r)
}

func writeBillingUnavailable(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("{\"error\":\"billing unavailable\"}\n"))
}
