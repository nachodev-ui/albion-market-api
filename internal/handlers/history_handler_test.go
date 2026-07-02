package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/service"
)

type fakeHistoryService struct {
	request  domain.HistoryQueryRequest
	response domain.HistoryQueryResponse
	err      error
}

func (f *fakeHistoryService) QueryMarketHistory(_ context.Context, req domain.HistoryQueryRequest) (domain.HistoryQueryResponse, error) {
	f.request = req
	return f.response, f.err
}

func TestHistoryHandlerAcceptsBatchMarketKeyContract(t *testing.T) {
	t.Parallel()

	price := int64(4500)
	fake := &fakeHistoryService{response: domain.HistoryQueryResponse{
		RequestedAt: time.Date(2026, time.June, 26, 15, 0, 0, 0, time.UTC),
		RangeStart:  "2026-06-01",
		RangeEnd:    "2026-06-25",
		Count:       1,
		BucketCount: 1,
		Data: []domain.MarketHistorySeries{{
			Server: domain.ServerWest, MarketKey: "martlock", LocationID: 3008,
			ItemKey: "T4_BAG", Quality: 1,
			History: []domain.MarketHistoryPoint{{
				Timestamp: time.Date(2026, time.June, 25, 0, 0, 0, 0, time.UTC),
				ItemCount: 12, AverageUnitPrice: &price,
			}},
		}},
	}}
	handler := NewHistoryHandler(fake)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/history/query", strings.NewReader(`{
		"server":"west",
		"marketKeys":["martlock"],
		"entries":[{"itemIdentifier":"T4_BAG","quality":1}],
		"rangeStart":"2026-06-01",
		"rangeEnd":"2026-06-25"
	}`))
	response := httptest.NewRecorder()

	handler.QueryMarketHistory(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if fake.request.MarketKeys[0] != "martlock" || fake.request.Entries[0].ItemKey != "T4_BAG" {
		t.Fatalf("request = %#v", fake.request)
	}
	if strings.Contains(response.Body.String(), "location") || strings.Contains(response.Body.String(), "3008") {
		t.Fatalf("response leaked internal location id: %s", response.Body.String())
	}

	var body domain.HistoryQueryResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data[0].MarketKey != "martlock" || body.BucketCount != 1 {
		t.Fatalf("body = %#v", body)
	}
}

func TestHistoryHandlerGETMirrorsReceiverContractAndUsesCompletedDays(t *testing.T) {
	t.Parallel()

	fake := &fakeHistoryService{response: domain.HistoryQueryResponse{Data: []domain.MarketHistorySeries{}}}
	handler := NewHistoryHandler(fake)
	handler.now = func() time.Time {
		return time.Date(2026, time.June, 26, 23, 30, 0, 0, time.FixedZone("CL", -4*60*60))
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/history?server=west&marketKey=brecilien&itemId=T5_LEATHER_LEVEL4%404&quality=4&period=7-days&limit=1",
		nil,
	)
	response := httptest.NewRecorder()

	handler.GetMarketHistory(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if fake.request.Server != domain.ServerWest || fake.request.MarketKeys[0] != "brecilien" ||
		fake.request.Entries[0].ItemKey != "T5_LEATHER_LEVEL4@4" || fake.request.Entries[0].Quality != 4 {
		t.Fatalf("request = %#v", fake.request)
	}
	// h.now().UTC() is 2026-06-27 03:30, so the seven completed UTC days
	// end on 2026-06-26.
	if fake.request.RangeStart != "2026-06-20" || fake.request.RangeEnd != "2026-06-26" {
		t.Fatalf("range = %s to %s", fake.request.RangeStart, fake.request.RangeEnd)
	}
}

func TestHistoryHandlerGETAcceptsExplicitRange(t *testing.T) {
	t.Parallel()

	fake := &fakeHistoryService{response: domain.HistoryQueryResponse{Data: []domain.MarketHistorySeries{}}}
	handler := NewHistoryHandler(fake)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/history?server=west&marketKey=martlock&itemId=T4_BAG&quality=1&rangeStart=2026-06-01&rangeEnd=2026-06-25",
		nil,
	)
	response := httptest.NewRecorder()

	handler.GetMarketHistory(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if fake.request.RangeStart != "2026-06-01" || fake.request.RangeEnd != "2026-06-25" {
		t.Fatalf("range = %#v", fake.request)
	}
}

func TestHistoryHandlerRejectsInvalidJSONAndUnknownFields(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{"server":"west"`,
		`{"server":"west","marketKeys":["martlock"],"entries":[],"rangeStart":"2026-06-01","rangeEnd":"2026-06-25","location_id":3008}`,
		`{"server":"west","marketKeys":["martlock"],"entries":[],"rangeStart":"2026-06-01","rangeEnd":"2026-06-25"} {}`,
	} {
		handler := NewHistoryHandler(&fakeHistoryService{})
		request := httptest.NewRequest(http.MethodPost, "/api/v1/history/query", strings.NewReader(body))
		response := httptest.NewRecorder()
		handler.QueryMarketHistory(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %q status = %d, want %d", body, response.Code, http.StatusBadRequest)
		}
	}
}

func TestHistoryHandlerMapsValidationAndHidesInternalErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
		notBody    string
	}{
		{
			name:       "validation",
			err:        errors.Join(service.ErrInvalidHistoryQuery, errors.New("marketKeys is required")),
			wantStatus: http.StatusBadRequest,
			wantBody:   "marketKeys is required",
		},
		{
			name:       "internal",
			err:        errors.New("password authentication failed"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   "internal server error",
			notBody:    "password authentication failed",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := NewHistoryHandler(&fakeHistoryService{err: test.err})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/history/query", strings.NewReader(`{
				"server":"west","marketKeys":["martlock"],
				"entries":[{"itemIdentifier":"T4_BAG","quality":1}],
				"rangeStart":"2026-06-01","rangeEnd":"2026-06-25"
			}`))
			response := httptest.NewRecorder()
			handler.QueryMarketHistory(response, request)

			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), test.wantBody) {
				t.Fatalf("status/body = %d %s", response.Code, response.Body.String())
			}
			if test.notBody != "" && strings.Contains(response.Body.String(), test.notBody) {
				t.Fatalf("response leaked internal error: %s", response.Body.String())
			}
		})
	}
}

func TestQueryMarketHistoryRejectsUnsupportedContentType(t *testing.T) {
	t.Parallel()

	handler := NewHistoryHandler(&fakeHistoryService{}, 1024)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/history/query", strings.NewReader(`{"server":"west"}`))
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()

	handler.QueryMarketHistory(response, request)

	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnsupportedMediaType)
	}
}

func TestQueryMarketHistoryUsesConfiguredBodyLimit(t *testing.T) {
	t.Parallel()

	handler := NewHistoryHandler(&fakeHistoryService{}, 8)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/history/query", strings.NewReader(`{"server":"west"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.QueryMarketHistory(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusRequestEntityTooLarge, response.Body.String())
	}
}
