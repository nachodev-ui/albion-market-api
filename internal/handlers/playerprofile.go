package handlers

import (
	"net/http"

	"github.com/nachodev-ui/albion-market-api/internal/playerprofile"
)

type AuthenticatedPlayerProfileHandler struct {
	handler       *playerprofile.Handler
	authenticator AccountAuthenticator
}

func NewAuthenticatedPlayerProfileHandler(handler *playerprofile.Handler, authenticator AccountAuthenticator) *AuthenticatedPlayerProfileHandler {
	return &AuthenticatedPlayerProfileHandler{handler: handler, authenticator: authenticator}
}

func (h *AuthenticatedPlayerProfileHandler) Search(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.handler.Search(w, r)
}

func (h *AuthenticatedPlayerProfileHandler) Current(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.serve(w, r, h.handler.Current)
}

func (h *AuthenticatedPlayerProfileHandler) Link(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", http.MethodPut)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.serve(w, r, h.handler.Current)
}

func (h *AuthenticatedPlayerProfileHandler) Unlink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.serve(w, r, h.handler.Current)
}

func (h *AuthenticatedPlayerProfileHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.serve(w, r, h.handler.Refresh)
}

func (h *AuthenticatedPlayerProfileHandler) serve(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if h == nil || h.handler == nil || h.authenticator == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("{\"error\":\"authentication unavailable\"}\n"))
		return
	}
	h.authenticator.RequireScope(accountReadScope, next).ServeHTTP(w, r)
}
