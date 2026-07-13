package billing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLemonProviderCreatesSandboxCheckout(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/checkouts" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sandbox-key" {
			t.Fatal("missing provider authorization")
		}
		var request lemonCheckoutRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if !request.Data.Attributes.TestMode {
			t.Fatal("checkout did not enable test mode")
		}
		if request.Data.Attributes.CheckoutData.Custom["user_id"] != "user-123" {
			t.Fatal("checkout did not include internal user id")
		}
		if request.Data.Relationships.Store.Data.ID != "100" || request.Data.Relationships.Variant.Data.ID != "200" {
			t.Fatal("checkout relationships are incorrect")
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		_, _ = w.Write([]byte(`{"data":{"attributes":{"url":"https://example.lemonsqueezy.com/checkout/abc"}}}`))
	}))
	defer server.Close()

	provider, err := NewLemonProvider(LemonProviderConfig{
		APIBaseURL:          server.URL,
		APIKey:              "sandbox-key",
		StoreID:             "100",
		VariantID:           "200",
		CheckoutRedirectURL: "https://app.example.com/account?checkout=success",
		TestMode:            true,
		HTTPTimeout:         time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := provider.CreateCheckout(t.Context(), CheckoutRequest{
		UserID:      "user-123",
		Email:       "user@example.com",
		DisplayName: "Test User",
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.URL != "https://example.lemonsqueezy.com/checkout/abc" {
		t.Fatalf("checkout URL = %q", session.URL)
	}
}

func TestLemonProviderRequestsFreshPortalURL(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/subscriptions/sub-123" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		_, _ = w.Write([]byte(`{"data":{"attributes":{"urls":{"customer_portal":"https://example.lemonsqueezy.com/billing?signed=1"}}}}`))
	}))
	defer server.Close()

	provider, err := NewLemonProvider(LemonProviderConfig{
		APIBaseURL:          server.URL,
		APIKey:              "sandbox-key",
		StoreID:             "100",
		VariantID:           "200",
		CheckoutRedirectURL: "https://app.example.com/account",
		TestMode:            true,
		HTTPTimeout:         time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := provider.CustomerPortal(t.Context(), "sub-123")
	if err != nil {
		t.Fatal(err)
	}
	if session.URL != "https://example.lemonsqueezy.com/billing?signed=1" {
		t.Fatalf("portal URL = %q", session.URL)
	}
}
