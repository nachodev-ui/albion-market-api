package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSecurityHeadersAreAlwaysPresent(t *testing.T) {
	t.Parallel()

	handler := withSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	for header, expected := range map[string]string{
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'; base-uri 'none'",
		"Permissions-Policy":      "camera=(), geolocation=(), microphone=()",
		"Referrer-Policy":         "no-referrer",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"X-XSS-Protection":        "0",
	} {
		if got := response.Header().Get(header); got != expected {
			t.Fatalf("%s = %q, want %q", header, got, expected)
		}
	}
}

func TestCORSAllowsConfiguredOrigin(t *testing.T) {
	t.Parallel()

	handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("preflight request reached wrapped handler")
	}), []string{"https://app.example.com"})

	request := httptest.NewRequest(http.MethodOptions, "/api/v1/prices/query", nil)
	request.Header.Set("Origin", "https://app.example.com")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("allow origin = %q, want configured origin", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("allow credentials = %q, want empty", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Headers"); got != "Authorization, Content-Type, X-Request-ID" {
		t.Fatalf("allow headers = %q", got)
	}
	if got := response.Header().Get("Access-Control-Expose-Headers"); got != "X-Request-ID" {
		t.Fatalf("expose headers = %q", got)
	}
}

func TestCORSRejectsUnknownOrigin(t *testing.T) {
	t.Parallel()

	called := false
	handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}), []string{"https://app.example.com"})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/prices/query", nil)
	request.Header.Set("Origin", "https://evil.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if called {
		t.Fatal("wrapped handler was called for a rejected origin")
	}
}

func TestRateLimiterUsesTokenBucket(t *testing.T) {
	t.Parallel()

	limiter := newIPRateLimiter(RateLimitOptions{
		RequestsPerSecond: 2,
		Burst:             1,
		ClientTTL:         time.Minute,
	})
	now := time.Now()

	allowed, remaining, _ := limiter.allow("127.0.0.1", now)
	if !allowed || remaining != 0 {
		t.Fatalf("first request allowed=%v remaining=%d, want true/0", allowed, remaining)
	}
	allowed, _, retryAfter := limiter.allow("127.0.0.1", now)
	if allowed || retryAfter <= 0 {
		t.Fatalf("second request allowed=%v retry=%s, want rejected with retry", allowed, retryAfter)
	}
	allowed, _, _ = limiter.allow("127.0.0.1", now.Add(500*time.Millisecond))
	if !allowed {
		t.Fatal("request was not allowed after one token refilled")
	}
}

func TestRateLimitMiddlewareReturns429AndExemptsOperationalEndpoints(t *testing.T) {
	t.Parallel()

	limiter := newIPRateLimiter(RateLimitOptions{
		RequestsPerSecond: 1,
		Burst:             1,
		ClientTTL:         time.Minute,
	})
	handler := withRateLimit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), limiter)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	if first.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusNoContent)
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header is missing")
	}

	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNoContent {
			t.Fatalf("%s status = %d, want %d", path, response.Code, http.StatusNoContent)
		}
	}
}

func TestClientIdentifierTrustsProxyHeadersOnlyWhenEnabled(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.25, 192.0.2.20")

	if got := clientIdentifier(request, false); got != "192.0.2.10" {
		t.Fatalf("untrusted proxy client = %q, want remote address", got)
	}
	if got := clientIdentifier(request, true); got != "203.0.113.25" {
		t.Fatalf("trusted proxy client = %q, want first forwarded IP", got)
	}
}
