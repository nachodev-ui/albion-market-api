package server

import (
	"encoding/json"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SecurityOptions struct {
	AllowedOrigins []string
	RateLimit      RateLimitOptions
}

type RateLimitOptions struct {
	Enabled           bool
	RequestsPerSecond float64
	Burst             int
	ClientTTL         time.Duration
	TrustProxyHeaders bool
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "0")
		next.ServeHTTP(w, r)
	})
}

func withCORS(next http.Handler, allowedOrigins []string) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	allowAll := false
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if origin == "*" {
			allowAll = true
			continue
		}
		allowed[origin] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Add("Vary", "Origin")
		w.Header().Add("Vary", "Access-Control-Request-Method")
		w.Header().Add("Vary", "Access-Control-Request-Headers")

		_, originAllowed := allowed[origin]
		if !allowAll && !originAllowed {
			writeServerJSON(w, http.StatusForbidden, map[string]string{"error": "origin not allowed"})
			return
		}

		if allowAll {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
		w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Max-Age", "600")

		if r.Method == http.MethodOptions {
			requestedMethod := strings.ToUpper(strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")))
			if requestedMethod != "" && requestedMethod != http.MethodGet && requestedMethod != http.MethodPost && requestedMethod != http.MethodPut && requestedMethod != http.MethodDelete {
				writeServerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type clientBucket struct {
	tokens   float64
	updated  time.Time
	lastSeen time.Time
}

type ipRateLimiter struct {
	mu                sync.Mutex
	clients           map[string]*clientBucket
	requestsPerSecond float64
	burst             float64
	clientTTL         time.Duration
	trustProxyHeaders bool
	lastCleanup       time.Time
}

func newIPRateLimiter(options RateLimitOptions) *ipRateLimiter {
	clientTTL := options.ClientTTL
	if clientTTL <= 0 {
		clientTTL = 10 * time.Minute
	}
	return &ipRateLimiter{
		clients:           make(map[string]*clientBucket),
		requestsPerSecond: options.RequestsPerSecond,
		burst:             float64(options.Burst),
		clientTTL:         clientTTL,
		trustProxyHeaders: options.TrustProxyHeaders,
		lastCleanup:       time.Now(),
	}
}

func withRateLimit(next http.Handler, limiter *ipRateLimiter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || isRateLimitExemptPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		allowed, remaining, retryAfter := limiter.allow(clientIdentifier(r, limiter.trustProxyHeaders), time.Now())
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(int(limiter.burst)))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		if !allowed {
			retrySeconds := int(math.Ceil(retryAfter.Seconds()))
			if retrySeconds < 1 {
				retrySeconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
			writeServerJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isRateLimitExemptPath(path string) bool {
	switch path {
	case "/healthz", "/readyz", "/metrics", "/api/v1/webhooks/lemonsqueezy":
		return true
	default:
		return false
	}
}

func isOperationalProbe(path string) bool {
	switch path {
	case "/healthz", "/readyz", "/metrics":
		return true
	default:
		return false
	}
}

func (l *ipRateLimiter) allow(client string, now time.Time) (bool, int, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastCleanup) >= l.clientTTL {
		for key, bucket := range l.clients {
			if now.Sub(bucket.lastSeen) >= l.clientTTL {
				delete(l.clients, key)
			}
		}
		l.lastCleanup = now
	}

	bucket, exists := l.clients[client]
	if !exists {
		bucket = &clientBucket{tokens: l.burst, updated: now, lastSeen: now}
		l.clients[client] = bucket
	}

	elapsed := now.Sub(bucket.updated).Seconds()
	if elapsed > 0 {
		bucket.tokens = math.Min(l.burst, bucket.tokens+elapsed*l.requestsPerSecond)
		bucket.updated = now
	}
	bucket.lastSeen = now

	if bucket.tokens >= 1 {
		bucket.tokens--
		return true, int(math.Floor(bucket.tokens)), 0
	}

	missing := 1 - bucket.tokens
	retryAfter := time.Duration((missing / l.requestsPerSecond) * float64(time.Second))
	return false, 0, retryAfter
}

func clientIdentifier(r *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		for _, candidate := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
			candidate = strings.TrimSpace(candidate)
			if net.ParseIP(candidate) != nil {
				return candidate
			}
		}
		if candidate := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(candidate) != nil {
			return candidate
		}
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return "unknown"
}

func writeServerJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
