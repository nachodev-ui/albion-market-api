package ingestauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
)

const maxBearerTokenLength = 16 << 10

type Credential struct {
	ID    string
	Token string
}

type Options struct {
	RequireHTTPS      bool
	TrustProxyHeaders bool
}

type FailureReason string

const (
	FailureNone              FailureReason = ""
	FailureMissingHeader     FailureReason = "missing_authorization"
	FailureMalformedHeader   FailureReason = "malformed_authorization"
	FailureInvalidToken      FailureReason = "invalid_token"
	FailureInsecureTransport FailureReason = "https_required"
)

type Result struct {
	Authenticated bool
	KeyID         string
	Failure       FailureReason
}

type storedCredential struct {
	id     string
	digest [sha256.Size]byte
}

type Authenticator struct {
	credentials       []storedCredential
	requireHTTPS      bool
	trustProxyHeaders bool
}

func New(credentials []Credential, options Options) (*Authenticator, error) {
	if len(credentials) == 0 {
		return nil, fmt.Errorf("at least one ingest credential is required")
	}

	stored := make([]storedCredential, 0, len(credentials))
	seenIDs := make(map[string]struct{}, len(credentials))
	seenDigests := make(map[[sha256.Size]byte]struct{}, len(credentials))
	for _, credential := range credentials {
		id := strings.TrimSpace(credential.ID)
		token := strings.TrimSpace(credential.Token)
		if id == "" {
			return nil, fmt.Errorf("ingest credential id is required")
		}
		if token == "" {
			return nil, fmt.Errorf("ingest credential %q token is required", id)
		}
		if _, exists := seenIDs[id]; exists {
			return nil, fmt.Errorf("duplicate ingest credential id %q", id)
		}
		digest := sha256.Sum256([]byte(token))
		if _, exists := seenDigests[digest]; exists {
			return nil, fmt.Errorf("ingest credentials must use different tokens")
		}
		seenIDs[id] = struct{}{}
		seenDigests[digest] = struct{}{}
		stored = append(stored, storedCredential{id: id, digest: digest})
	}

	return &Authenticator{
		credentials:       stored,
		requireHTTPS:      options.RequireHTTPS,
		trustProxyHeaders: options.TrustProxyHeaders,
	}, nil
}

func (a *Authenticator) Authenticate(r *http.Request) Result {
	if a == nil {
		return Result{Failure: FailureInvalidToken}
	}
	if a.requireHTTPS && !requestUsesHTTPS(r, a.trustProxyHeaders) {
		return Result{Failure: FailureInsecureTransport}
	}

	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return Result{Failure: FailureMissingHeader}
	}

	fields := strings.Fields(header)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return Result{Failure: FailureMalformedHeader}
	}
	provided := fields[1]
	if provided == "" || len(provided) > maxBearerTokenLength {
		return Result{Failure: FailureMalformedHeader}
	}

	providedDigest := sha256.Sum256([]byte(provided))
	matched := 0
	matchedIndex := 0
	for index, credential := range a.credentials {
		equal := subtle.ConstantTimeCompare(providedDigest[:], credential.digest[:])
		matchedIndex = subtle.ConstantTimeSelect(equal, index, matchedIndex)
		matched |= equal
	}
	if matched != 1 {
		return Result{Failure: FailureInvalidToken}
	}

	return Result{
		Authenticated: true,
		KeyID:         a.credentials[matchedIndex].id,
	}
}

func requestUsesHTTPS(r *http.Request, trustProxyHeaders bool) bool {
	if r != nil && r.TLS != nil {
		return true
	}
	if r == nil || !trustProxyHeaders {
		return false
	}

	forwardedProto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	return strings.EqualFold(forwardedProto, "https")
}
