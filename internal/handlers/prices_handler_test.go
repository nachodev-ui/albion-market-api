package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/service"
)

type fakePricesService struct {
	response domain.PriceQueryResponse
	err      error
}

func (f fakePricesService) QueryCurrentPrices(context.Context, domain.PriceQueryRequest) (domain.PriceQueryResponse, error) {
	return f.response, f.err
}

func TestPricesHandlerAcceptsValidJSON(t *testing.T) {
	t.Parallel()

	handler := NewPricesHandler(fakePricesService{}, 1024)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/prices/query", strings.NewReader(`{"server":"west"}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()

	handler.QueryCurrentPrices(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
}

func TestPricesHandlerRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	handler := NewPricesHandler(fakePricesService{}, 8)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/prices/query", strings.NewReader(`{"server":"west"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.QueryCurrentPrices(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusRequestEntityTooLarge, response.Body.String())
	}
}

func TestPricesHandlerRejectsUnsupportedContentType(t *testing.T) {
	t.Parallel()

	handler := NewPricesHandler(fakePricesService{}, 1024)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/prices/query", strings.NewReader(`{"server":"west"}`))
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()

	handler.QueryCurrentPrices(response, request)

	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnsupportedMediaType)
	}
}

func TestPricesHandlerReturnsValidationDetail(t *testing.T) {
	t.Parallel()

	handler := NewPricesHandler(fakePricesService{
		err: fmt.Errorf("%w: server is required", service.ErrInvalidPriceQuery),
	}, 1024)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/prices/query", strings.NewReader(`{"server":""}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.QueryCurrentPrices(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if !strings.Contains(response.Body.String(), "server is required") {
		t.Fatalf("body = %q, want validation detail", response.Body.String())
	}
}

func TestPricesHandlerDoesNotExposeInternalErrors(t *testing.T) {
	t.Parallel()

	handler := NewPricesHandler(fakePricesService{err: errors.New("password authentication failed for user postgres")}, 1024)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/prices/query", strings.NewReader(`{"server":"west"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.QueryCurrentPrices(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	body := response.Body.String()
	if strings.Contains(body, "postgres") || !strings.Contains(body, "internal server error") {
		t.Fatalf("body = %q, want generic internal error", body)
	}
}
