package authn

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var ErrInvalidToken = errors.New("invalid access token")

type Identity struct {
	Subject     string
	Email       string
	DisplayName string
}

type Config struct {
	Enabled     bool
	Issuer      string
	Audience    string
	CacheTTL    time.Duration
	HTTPTimeout time.Duration
	ClockSkew   time.Duration
}

type Authenticator struct {
	enabled   bool
	validator *Validator
}

func New(cfg Config) (*Authenticator, error) {
	if !cfg.Enabled {
		return &Authenticator{}, nil
	}

	issuer, err := url.Parse(strings.TrimRight(strings.TrimSpace(cfg.Issuer), "/") + "/")
	if err != nil || issuer.Scheme == "" || issuer.Host == "" || strings.TrimSpace(cfg.Audience) == "" {
		return nil, fmt.Errorf("configure authentication: issuer and audience are required")
	}

	provider := &JWKSProvider{
		url:    issuer.ResolveReference(&url.URL{Path: ".well-known/jwks.json"}),
		client: &http.Client{Timeout: cfg.HTTPTimeout},
		ttl:    cfg.CacheTTL,
	}
	return &Authenticator{
		enabled: true,
		validator: &Validator{
			issuer:    issuer.String(),
			audience:  cfg.Audience,
			keys:      provider,
			clockSkew: cfg.ClockSkew,
			now:       time.Now,
		},
	}, nil
}

func (a *Authenticator) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if a == nil || !a.enabled || a.validator == nil {
			writeError(w, http.StatusServiceUnavailable, "authentication unavailable")
			return
		}

		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		identity, err := a.validator.Validate(
			r.Context(),
			strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")),
		)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), identity)))
	})
}

type contextKey struct{}

func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, identity)
}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(contextKey{}).(Identity)
	return identity, ok
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

type keyProvider interface {
	Key(context.Context, string) (*rsa.PublicKey, error)
}

type Validator struct {
	issuer    string
	audience  string
	keys      keyProvider
	clockSkew time.Duration
	now       func() time.Time
}

type tokenHeader struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
}

type tokenClaims struct {
	Issuer      string   `json:"iss"`
	Subject     string   `json:"sub"`
	Audience    audience `json:"aud"`
	ExpiresAt   int64    `json:"exp"`
	NotBefore   int64    `json:"nbf,omitempty"`
	IssuedAt    int64    `json:"iat,omitempty"`
	Email       string   `json:"email,omitempty"`
	Name        string   `json:"name,omitempty"`
	Nickname    string   `json:"nickname,omitempty"`
}

type audience []string

func (a *audience) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*a = audience{single}
		return nil
	}

	var multiple []string
	if err := json.Unmarshal(data, &multiple); err != nil {
		return err
	}
	*a = audience(multiple)
	return nil
}

func (a audience) contains(expected string) bool {
	for _, value := range a {
		if value == expected {
			return true
		}
	}
	return false
}

func (v *Validator) Validate(ctx context.Context, token string) (Identity, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return Identity{}, ErrInvalidToken
	}

	decode := func(value string, target any) error {
		raw, err := base64.RawURLEncoding.DecodeString(value)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, target)
	}

	var header tokenHeader
	var claims tokenClaims
	if decode(parts[0], &header) != nil || decode(parts[1], &claims) != nil ||
		header.Algorithm != "RS256" || header.KeyID == "" {
		return Identity{}, ErrInvalidToken
	}

	key, err := v.keys.Key(ctx, header.KeyID)
	if err != nil {
		return Identity{}, ErrInvalidToken
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Identity{}, ErrInvalidToken
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return Identity{}, ErrInvalidToken
	}

	now := v.now().Unix()
	skew := int64(v.clockSkew.Seconds())
	if claims.Issuer != v.issuer || !claims.Audience.contains(v.audience) ||
		strings.TrimSpace(claims.Subject) == "" || claims.ExpiresAt == 0 ||
		now > claims.ExpiresAt+skew ||
		(claims.NotBefore != 0 && now+skew < claims.NotBefore) ||
		(claims.IssuedAt != 0 && claims.IssuedAt > now+skew) {
		return Identity{}, ErrInvalidToken
	}

	displayName := strings.TrimSpace(claims.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(claims.Nickname)
	}
	return Identity{
		Subject:     strings.TrimSpace(claims.Subject),
		Email:       strings.TrimSpace(claims.Email),
		DisplayName: displayName,
	}, nil
}

type JWKSProvider struct {
	url    *url.URL
	client *http.Client
	ttl    time.Duration

	mu      sync.RWMutex
	expires time.Time
	keys    map[string]*rsa.PublicKey
}

type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	KeyType  string `json:"kty"`
	Use      string `json:"use"`
	Algorithm string `json:"alg"`
	KeyID    string `json:"kid"`
	Modulus  string `json:"n"`
	Exponent string `json:"e"`
}

func (p *JWKSProvider) Key(ctx context.Context, keyID string) (*rsa.PublicKey, error) {
	p.mu.RLock()
	key := p.keys[keyID]
	fresh := time.Now().Before(p.expires)
	p.mu.RUnlock()
	if key != nil && fresh {
		return key, nil
	}

	if err := p.refresh(ctx); err != nil {
		return nil, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	key = p.keys[keyID]
	if key == nil {
		return nil, ErrInvalidToken
	}
	return key, nil
}

func (p *JWKSProvider) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url.String(), nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks status %d", resp.StatusCode)
	}

	var document jwksDocument
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&document); err != nil {
		return err
	}

	keys := make(map[string]*rsa.PublicKey)
	for _, item := range document.Keys {
		if item.KeyType != "RSA" || item.KeyID == "" || item.Modulus == "" || item.Exponent == "" {
			continue
		}
		modulus, err := base64.RawURLEncoding.DecodeString(item.Modulus)
		if err != nil {
			continue
		}
		exponentBytes, err := base64.RawURLEncoding.DecodeString(item.Exponent)
		if err != nil {
			continue
		}
		exponent := 0
		for _, value := range exponentBytes {
			exponent = exponent<<8 + int(value)
		}
		if exponent < 3 {
			continue
		}
		keys[item.KeyID] = &rsa.PublicKey{
			N: new(big.Int).SetBytes(modulus),
			E: exponent,
		}
	}
	if len(keys) == 0 {
		return errors.New("jwks contains no usable keys")
	}

	ttl := p.ttl
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	p.mu.Lock()
	p.keys = keys
	p.expires = time.Now().Add(ttl)
	p.mu.Unlock()
	return nil
}
