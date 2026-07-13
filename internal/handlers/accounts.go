package handlers

import (
	"net/http"

	"github.com/nachodev-ui/albion-market-api/internal/accounts"
)

type AccountAuthenticator interface {
	Require(http.Handler) http.Handler
}

type AuthenticatedAccountHandler struct {
	handler       *accounts.Handler
	authenticator AccountAuthenticator
}

func NewAuthenticatedAccountHandler(
	handler *accounts.Handler,
	authenticator AccountAuthenticator,
) *AuthenticatedAccountHandler {
	return &AuthenticatedAccountHandler{
		handler:       handler,
		authenticator: authenticator,
	}
}

func (h *AuthenticatedAccountHandler) Me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.serve(w, r, h.handler.Me)
		return
	}
	h.serve(w, r, h.handler.Me)
}

func (h *AuthenticatedAccountHandler) Entitlements(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.serve(w, r, h.handler.Entitlements)
		return
	}
	h.serve(w, r, h.handler.Entitlements)
}

func (h *AuthenticatedAccountHandler) serve(
	w http.ResponseWriter,
	r *http.Request,
	next http.HandlerFunc,
) {
	if h == nil || h.handler == nil || h.authenticator == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("{\"error\":\"authentication unavailable\"}\n"))
		return
	}
	h.authenticator.Require(next).ServeHTTP(w, r)
}
