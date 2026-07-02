package ingestauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthenticatorMatchesCurrentAndPreviousCredentials(t *testing.T) {
	t.Parallel()

	authenticator, err := New([]Credential{
		{ID: "current", Token: "current-token-value"},
		{ID: "previous", Token: "previous-token-value"},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	for _, testCase := range []struct {
		name  string
		token string
		id    string
	}{
		{name: "current", token: "current-token-value", id: "current"},
		{name: "previous", token: "previous-token-value", id: "previous"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/prices", nil)
			request.Header.Set("Authorization", "Bearer "+testCase.token)
			result := authenticator.Authenticate(request)
			if !result.Authenticated || result.KeyID != testCase.id || result.Failure != FailureNone {
				t.Fatalf("result = %+v, want authenticated key %q", result, testCase.id)
			}
		})
	}
}

func TestAuthenticatorRejectsMalformedAndInvalidCredentials(t *testing.T) {
	t.Parallel()

	authenticator, err := New([]Credential{{ID: "current", Token: "current-token-value"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	for _, testCase := range []struct {
		name    string
		header  string
		failure FailureReason
	}{
		{name: "missing", failure: FailureMissingHeader},
		{name: "wrong scheme", header: "Basic abc", failure: FailureMalformedHeader},
		{name: "extra fields", header: "Bearer abc extra", failure: FailureMalformedHeader},
		{name: "invalid token", header: "Bearer other-token", failure: FailureInvalidToken},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/prices", nil)
			if testCase.header != "" {
				request.Header.Set("Authorization", testCase.header)
			}
			result := authenticator.Authenticate(request)
			if result.Authenticated || result.Failure != testCase.failure {
				t.Fatalf("result = %+v, want failure %q", result, testCase.failure)
			}
		})
	}
}

func TestAuthenticatorRequiresHTTPSAndOnlyTrustsConfiguredProxyHeaders(t *testing.T) {
	secureOnly, err := New(
		[]Credential{{ID: "current", Token: "current-token-value"}},
		Options{RequireHTTPS: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://api.example.test/api/v1/ingest/prices", nil)
	request.Header.Set("Authorization", "Bearer current-token-value")
	request.Header.Set("X-Forwarded-Proto", "https")
	if result := secureOnly.Authenticate(request); result.Failure != FailureInsecureTransport {
		t.Fatalf("untrusted proxy result = %+v, want https_required", result)
	}

	trustedProxy, err := New(
		[]Credential{{ID: "current", Token: "current-token-value"}},
		Options{RequireHTTPS: true, TrustProxyHeaders: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result := trustedProxy.Authenticate(request); !result.Authenticated {
		t.Fatalf("trusted proxy result = %+v, want authenticated", result)
	}
}

func TestAuthenticatorRejectsDuplicateIDsAndTokens(t *testing.T) {
	t.Parallel()

	if _, err := New([]Credential{
		{ID: "same", Token: "token-one"},
		{ID: "same", Token: "token-two"},
	}, Options{}); err == nil {
		t.Fatal("expected duplicate id error")
	}
	if _, err := New([]Credential{
		{ID: "one", Token: "same-token"},
		{ID: "two", Token: "same-token"},
	}, Options{}); err == nil {
		t.Fatal("expected duplicate token error")
	}
}
