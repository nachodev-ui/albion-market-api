package authn

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
)

// RequireScope validates the bearer token through Require and then enforces a
// delegated OAuth scope from the already-validated token payload.
func (a *Authenticator) RequireScope(required string, next http.Handler) http.Handler {
	required = strings.TrimSpace(required)
	if required == "" {
		return a.Require(next)
	}

	return a.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !bearerTokenHasScope(r.Header.Get("Authorization"), required) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

type scopeClaims struct {
	Scope       string   `json:"scope,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

func bearerTokenHasScope(authorizationHeader, required string) bool {
	header := strings.TrimSpace(authorizationHeader)
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	parts := strings.Split(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")), ".")
	if len(parts) != 3 {
		return false
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims scopeClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return false
	}

	for _, scope := range strings.Fields(claims.Scope) {
		if scope == required {
			return true
		}
	}
	for _, permission := range claims.Permissions {
		if strings.TrimSpace(permission) == required {
			return true
		}
	}
	return false
}
