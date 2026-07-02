package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/observability"
)

type healthReadinessChecker struct {
	snapshot observability.ReadinessSnapshot
}

func (c healthReadinessChecker) Check(context.Context) observability.ReadinessSnapshot {
	return c.snapshot
}

func TestHealthHandlerIsLivenessOnly(t *testing.T) {
	t.Parallel()

	handler := NewHealthHandler(healthReadinessChecker{snapshot: observability.ReadinessSnapshot{
		Err: errors.New("database unavailable"),
	}})
	response := httptest.NewRecorder()

	handler.Healthz(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), `"status":"ok"`) {
		t.Fatalf("body = %q, want liveness success", response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestReadyHandlerDoesNotExposeDatabaseErrors(t *testing.T) {
	t.Parallel()

	handler := NewHealthHandler(healthReadinessChecker{snapshot: observability.ReadinessSnapshot{
		FailedComponent: observability.ReadinessComponentDatabase,
		Err:             errors.New("dial tcp database.internal:5432: connection refused"),
	}})
	response := httptest.NewRecorder()

	handler.Readyz(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	body := response.Body.String()
	if strings.Contains(body, "database.internal") || !strings.Contains(body, "service unavailable") {
		t.Fatalf("body = %q, want generic unavailable response", body)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestReadyHandlerAcceptsFullyReadySnapshot(t *testing.T) {
	t.Parallel()

	handler := NewHealthHandler(healthReadinessChecker{snapshot: observability.ReadinessSnapshot{Ready: true}})
	response := httptest.NewRecorder()

	handler.Readyz(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), `"status":"ok"`) {
		t.Fatalf("body = %q, want readiness success", response.Body.String())
	}
}

func TestReadyHandlerUsesBoundedTimeout(t *testing.T) {
	t.Parallel()

	checker := readinessCheckerFunc(func(ctx context.Context) observability.ReadinessSnapshot {
		<-ctx.Done()
		return observability.ReadinessSnapshot{
			FailedComponent: observability.ReadinessComponentPool,
			Err:             ctx.Err(),
		}
	})
	handler := NewHealthHandler(checker, 5*time.Millisecond)
	response := httptest.NewRecorder()

	handler.Readyz(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

type readinessCheckerFunc func(context.Context) observability.ReadinessSnapshot

func (f readinessCheckerFunc) Check(ctx context.Context) observability.ReadinessSnapshot {
	return f(ctx)
}

func TestHealthAndReadyHandlersSetAllowHeader(t *testing.T) {
	t.Parallel()

	handler := NewHealthHandler(healthReadinessChecker{snapshot: observability.ReadinessSnapshot{Ready: true}})
	for _, test := range []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{name: "health", path: "/healthz", handler: handler.Healthz},
		{name: "ready", path: "/readyz", handler: handler.Readyz},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			test.handler(response, httptest.NewRequest(http.MethodPost, test.path, nil))
			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
			}
			if got := response.Header().Get("Allow"); got != http.MethodGet {
				t.Fatalf("Allow = %q, want GET", got)
			}
		})
	}
}
