package adminpanel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/nachodev-ui/albion-market-api/internal/accountadmin"
	"github.com/nachodev-ui/albion-market-api/internal/authn"
)

const maxAdminRequestBodyBytes = 16 << 10

type serviceAPI interface {
	Session(context.Context, authn.Identity) (Session, error)
	SearchUsers(context.Context, authn.Identity, string, int) ([]UserSummary, error)
	UserDetail(context.Context, authn.Identity, string) (UserDetail, error)
	GrantPro(context.Context, authn.Identity, string, int, string, string) (accountadmin.OperationResult, error)
	RevokePro(context.Context, authn.Identity, string, string, string) (accountadmin.OperationResult, error)
	AuditEvents(context.Context, authn.Identity, int) ([]AuditEvent, error)
}

type Handler struct {
	service serviceAPI
}

func NewHandler(service serviceAPI) *Handler { return &Handler{service: service} }

func (h *Handler) Session(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	identity, ok := requestIdentity(w, r)
	if !ok {
		return
	}
	session, err := h.service.Session(r.Context(), identity)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (h *Handler) Users(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	identity, ok := requestIdentity(w, r)
	if !ok {
		return
	}
	limit, err := queryLimit(r, 50)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	users, err := h.service.SearchUsers(r.Context(), identity, r.URL.Query().Get("q"), limit)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (h *Handler) UserDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	identity, ok := requestIdentity(w, r)
	if !ok {
		return
	}
	detail, err := h.service.UserDetail(r.Context(), identity, r.PathValue("userId"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) GrantPro(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	identity, ok := requestIdentity(w, r)
	if !ok {
		return
	}
	var request struct {
		DurationDays int    `json:"durationDays"`
		Reason       string `json:"reason"`
		Confirmation string `json:"confirmation"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	result, err := h.service.GrantPro(r.Context(), identity, r.PathValue("userId"), request.DurationDays, request.Reason, request.Confirmation)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) RevokePro(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	identity, ok := requestIdentity(w, r)
	if !ok {
		return
	}
	var request struct {
		Reason       string `json:"reason"`
		Confirmation string `json:"confirmation"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	result, err := h.service.RevokePro(r.Context(), identity, r.PathValue("userId"), request.Reason, request.Confirmation)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) AuditEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	identity, ok := requestIdentity(w, r)
	if !ok {
		return
	}
	limit, err := queryLimit(r, 100)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	events, err := h.service.AuditEvents(r.Context(), identity, limit)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
	case errors.Is(err, ErrInvalidRequest):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, accountadmin.ErrUserNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
	case errors.Is(err, accountadmin.ErrAmbiguousUser):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "ambiguous user"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func requestIdentity(w http.ResponseWriter, r *http.Request) (authn.Identity, bool) {
	w.Header().Set("Cache-Control", "no-store")
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return authn.Identity{}, false
	}
	return identity, true
}

func queryLimit(r *http.Request, fallback int) (int, error) {
	value := strings.TrimSpace(r.URL.Query().Get("limit"))
	if value == "" {
		return fallback, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil {
		return 0, errors.New("limit must be an integer")
	}
	return limit, nil
}

func decodeJSON(r *http.Request, destination any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxAdminRequestBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("invalid JSON request")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
