package playerprofile

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/authn"
)

const maxRequestBodyBytes = 16 << 10

type serviceAPI interface {
	Search(context.Context, string, string) ([]SearchResult, error)
	Current(context.Context, authn.Identity) (CurrentResponse, error)
	Link(context.Context, authn.Identity, LinkRequest) (CurrentResponse, error)
	Refresh(context.Context, authn.Identity) (CurrentResponse, error)
	Delete(context.Context, authn.Identity) error
}

type Handler struct{ service serviceAPI }

func NewHandler(service serviceAPI) *Handler { return &Handler{service: service} }

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	results, err := h.service.Search(r.Context(), r.URL.Query().Get("server"), r.URL.Query().Get("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"players": results})
}

func (h *Handler) Current(w http.ResponseWriter, r *http.Request) {
	identity, ok := requestIdentity(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		response, err := h.service.Current(r.Context(), identity)
		if err != nil {
			h.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	case http.MethodPut:
		var request LinkRequest
		if err := decodeJSON(r, &request); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		response, err := h.service.Link(r.Context(), identity, request)
		if err != nil {
			h.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	case http.MethodDelete:
		if err := h.service.Delete(r.Context(), identity); err != nil {
			h.writeError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	identity, ok := requestIdentity(w, r)
	if !ok {
		return
	}
	response, err := h.service.Refresh(r.Context(), identity)
	if err != nil {
		var cooldown CooldownError
		if errors.As(err, &cooldown) {
			seconds := int(cooldown.RetryAfter.Round(time.Second).Seconds())
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "profile refresh is on cooldown"})
			return
		}
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrNotLinked):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Albion profile not linked"})
	case errors.Is(err, ErrProviderUnavailable):
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Albion profile provider unavailable"})
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

func decodeJSON(r *http.Request, destination any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes))
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
