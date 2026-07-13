package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxProviderResponseBytes = 1 << 20

var ErrProviderUnavailable = errors.New("billing provider unavailable")

type CheckoutRequest struct {
	UserID      string
	Email       string
	DisplayName string
}

type CheckoutSession struct {
	URL string `json:"url"`
}

type PortalSession struct {
	URL string `json:"url"`
}

type Provider interface {
	CreateCheckout(context.Context, CheckoutRequest) (CheckoutSession, error)
	CustomerPortal(context.Context, string) (PortalSession, error)
}

type LemonProviderConfig struct {
	APIBaseURL          string
	APIKey              string
	StoreID             string
	VariantID           string
	CheckoutRedirectURL string
	TestMode            bool
	HTTPTimeout         time.Duration
}

type LemonProvider struct {
	apiBaseURL          string
	apiKey              string
	storeID             string
	variantID           string
	checkoutRedirectURL string
	testMode            bool
	client              *http.Client
}

func NewLemonProvider(cfg LemonProviderConfig) (*LemonProvider, error) {
	if strings.TrimSpace(cfg.APIBaseURL) == "" || strings.TrimSpace(cfg.APIKey) == "" ||
		strings.TrimSpace(cfg.StoreID) == "" || strings.TrimSpace(cfg.VariantID) == "" ||
		strings.TrimSpace(cfg.CheckoutRedirectURL) == "" || cfg.HTTPTimeout <= 0 {
		return nil, fmt.Errorf("configure Lemon Squeezy provider: incomplete configuration")
	}
	return &LemonProvider{
		apiBaseURL:          strings.TrimRight(cfg.APIBaseURL, "/"),
		apiKey:              cfg.APIKey,
		storeID:             cfg.StoreID,
		variantID:           cfg.VariantID,
		checkoutRedirectURL: cfg.CheckoutRedirectURL,
		testMode:            cfg.TestMode,
		client:              &http.Client{Timeout: cfg.HTTPTimeout},
	}, nil
}

func (p *LemonProvider) CreateCheckout(ctx context.Context, input CheckoutRequest) (CheckoutSession, error) {
	if p == nil || p.client == nil || strings.TrimSpace(input.UserID) == "" {
		return CheckoutSession{}, ErrProviderUnavailable
	}

	payload := lemonCheckoutRequest{}
	payload.Data.Type = "checkouts"
	payload.Data.Attributes.ProductOptions.RedirectURL = p.checkoutRedirectURL
	payload.Data.Attributes.ProductOptions.EnabledVariants = []int64{mustParseID(p.variantID)}
	payload.Data.Attributes.CheckoutOptions.Media = false
	payload.Data.Attributes.CheckoutOptions.Logo = true
	payload.Data.Attributes.CheckoutOptions.Description = true
	payload.Data.Attributes.CheckoutOptions.Discount = true
	payload.Data.Attributes.CheckoutOptions.SubscriptionPreview = true
	payload.Data.Attributes.CheckoutOptions.BackgroundColor = "#0b1220"
	payload.Data.Attributes.CheckoutOptions.HeadingsColor = "#f8fafc"
	payload.Data.Attributes.CheckoutOptions.PrimaryTextColor = "#e2e8f0"
	payload.Data.Attributes.CheckoutOptions.SecondaryTextColor = "#94a3b8"
	payload.Data.Attributes.CheckoutOptions.LinksColor = "#38bdf8"
	payload.Data.Attributes.CheckoutOptions.BordersColor = "#334155"
	payload.Data.Attributes.CheckoutOptions.CheckboxColor = "#38bdf8"
	payload.Data.Attributes.CheckoutOptions.ActiveStateColor = "#38bdf8"
	payload.Data.Attributes.CheckoutOptions.ButtonColor = "#0284c7"
	payload.Data.Attributes.CheckoutOptions.ButtonTextColor = "#ffffff"
	payload.Data.Attributes.CheckoutOptions.TermsPrivacyColor = "#94a3b8"
	payload.Data.Attributes.CheckoutOptions.Locale = "es"
	payload.Data.Attributes.CheckoutData.Email = strings.TrimSpace(input.Email)
	payload.Data.Attributes.CheckoutData.Name = strings.TrimSpace(input.DisplayName)
	payload.Data.Attributes.CheckoutData.Custom = map[string]string{"user_id": input.UserID}
	payload.Data.Attributes.TestMode = p.testMode
	payload.Data.Relationships.Store.Data.Type = "stores"
	payload.Data.Relationships.Store.Data.ID = p.storeID
	payload.Data.Relationships.Variant.Data.Type = "variants"
	payload.Data.Relationships.Variant.Data.ID = p.variantID

	var response lemonCheckoutResponse
	if err := p.doJSON(ctx, http.MethodPost, "/v1/checkouts", payload, &response); err != nil {
		return CheckoutSession{}, err
	}
	checkoutURL := strings.TrimSpace(response.Data.Attributes.URL)
	if checkoutURL == "" {
		return CheckoutSession{}, fmt.Errorf("%w: checkout response did not contain a URL", ErrProviderUnavailable)
	}
	return CheckoutSession{URL: checkoutURL}, nil
}

func (p *LemonProvider) CustomerPortal(ctx context.Context, providerSubscriptionID string) (PortalSession, error) {
	providerSubscriptionID = strings.TrimSpace(providerSubscriptionID)
	if p == nil || p.client == nil || providerSubscriptionID == "" {
		return PortalSession{}, ErrProviderUnavailable
	}

	var response lemonSubscriptionResponse
	path := "/v1/subscriptions/" + url.PathEscape(providerSubscriptionID)
	if err := p.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return PortalSession{}, err
	}
	portalURL := strings.TrimSpace(response.Data.Attributes.URLs.CustomerPortal)
	if portalURL == "" {
		return PortalSession{}, fmt.Errorf("%w: subscription does not expose a customer portal URL", ErrProviderUnavailable)
	}
	return PortalSession{URL: portalURL}, nil
}

func (p *LemonProvider) doJSON(ctx context.Context, method, path string, payload, target any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode billing provider request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, p.apiBaseURL+path, body)
	if err != nil {
		return fmt.Errorf("create billing provider request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.api+json")
	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	if payload != nil {
		request.Header.Set("Content-Type", "application/vnd.api+json")
	}

	response, err := p.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	defer response.Body.Close()

	limited := io.LimitReader(response.Body, maxProviderResponseBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("%w: read provider response", ErrProviderUnavailable)
	}
	if len(raw) > maxProviderResponseBytes {
		return fmt.Errorf("%w: provider response is too large", ErrProviderUnavailable)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%w: provider returned HTTP %d", ErrProviderUnavailable, response.StatusCode)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("%w: decode provider response", ErrProviderUnavailable)
	}
	return nil
}

func mustParseID(value string) int64 {
	var parsed int64
	_, _ = fmt.Sscan(value, &parsed)
	return parsed
}

type lemonCheckoutRequest struct {
	Data struct {
		Type       string `json:"type"`
		Attributes struct {
			ProductOptions struct {
				RedirectURL     string  `json:"redirect_url"`
				EnabledVariants []int64 `json:"enabled_variants"`
			} `json:"product_options"`
			CheckoutOptions struct {
				Media               bool   `json:"media"`
				Logo                bool   `json:"logo"`
				Description         bool   `json:"desc"`
				Discount            bool   `json:"discount"`
				SubscriptionPreview bool   `json:"subscription_preview"`
				BackgroundColor     string `json:"background_color"`
				HeadingsColor       string `json:"headings_color"`
				PrimaryTextColor    string `json:"primary_text_color"`
				SecondaryTextColor  string `json:"secondary_text_color"`
				LinksColor          string `json:"links_color"`
				BordersColor        string `json:"borders_color"`
				CheckboxColor       string `json:"checkbox_color"`
				ActiveStateColor    string `json:"active_state_color"`
				ButtonColor         string `json:"button_color"`
				ButtonTextColor     string `json:"button_text_color"`
				TermsPrivacyColor   string `json:"terms_privacy_color"`
				Locale              string `json:"locale"`
			} `json:"checkout_options"`
			CheckoutData struct {
				Email  string            `json:"email,omitempty"`
				Name   string            `json:"name,omitempty"`
				Custom map[string]string `json:"custom"`
			} `json:"checkout_data"`
			TestMode bool `json:"test_mode"`
		} `json:"attributes"`
		Relationships struct {
			Store struct {
				Data struct {
					Type string `json:"type"`
					ID   string `json:"id"`
				} `json:"data"`
			} `json:"store"`
			Variant struct {
				Data struct {
					Type string `json:"type"`
					ID   string `json:"id"`
				} `json:"data"`
			} `json:"variant"`
		} `json:"relationships"`
	} `json:"data"`
}

type lemonCheckoutResponse struct {
	Data struct {
		Attributes struct {
			URL string `json:"url"`
		} `json:"attributes"`
	} `json:"data"`
}

type lemonSubscriptionResponse struct {
	Data struct {
		Attributes struct {
			URLs struct {
				CustomerPortal string `json:"customer_portal"`
			} `json:"urls"`
		} `json:"attributes"`
	} `json:"data"`
}
