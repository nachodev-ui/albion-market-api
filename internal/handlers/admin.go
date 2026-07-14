package handlers

import (
	"net/http"

	"github.com/nachodev-ui/albion-market-api/internal/adminpanel"
)

type AuthenticatedAdminHandler struct {
	handler       *adminpanel.Handler
	authenticator AccountAuthenticator
}

func NewAuthenticatedAdminHandler(handler *adminpanel.Handler, authenticator AccountAuthenticator) *AuthenticatedAdminHandler {
	return &AuthenticatedAdminHandler{handler: handler, authenticator: authenticator}
}

func (h *AuthenticatedAdminHandler) Session(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	h.serve(w, r, h.handler.Session)
}

func (h *AuthenticatedAdminHandler) Users(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	h.serve(w, r, h.handler.Users)
}

func (h *AuthenticatedAdminHandler) UserDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	h.serve(w, r, h.handler.UserDetail)
}

func (h *AuthenticatedAdminHandler) GrantPro(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	h.serve(w, r, h.handler.GrantPro)
}

func (h *AuthenticatedAdminHandler) RevokePro(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	h.serve(w, r, h.handler.RevokePro)
}

func (h *AuthenticatedAdminHandler) AuditEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	h.serve(w, r, h.handler.AuditEvents)
}

func (h *AuthenticatedAdminHandler) serve(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if h == nil || h.handler == nil || h.authenticator == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("{\"error\":\"authentication unavailable\"}\n"))
		return
	}
	h.authenticator.RequireScope(accountReadScope, next).ServeHTTP(w, r)
}
