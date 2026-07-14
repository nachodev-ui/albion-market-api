package playerprofile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/authn"
)

type fakeService struct {
	searchResults []SearchResult
	current       CurrentResponse
	err           error
}

func (f fakeService) Search(context.Context, string, string) ([]SearchResult, error) {
	return f.searchResults, f.err
}
func (f fakeService) Current(context.Context, authn.Identity) (CurrentResponse, error) {
	return f.current, f.err
}
func (f fakeService) Link(context.Context, authn.Identity, LinkRequest) (CurrentResponse, error) {
	return f.current, f.err
}
func (f fakeService) Refresh(context.Context, authn.Identity) (CurrentResponse, error) {
	return f.current, f.err
}
func (f fakeService) Delete(context.Context, authn.Identity) error { return f.err }

func TestSearchIsPublic(t *testing.T) {
	handler := NewHandler(fakeService{searchResults: []SearchResult{{Server: ServerAmericas, PlayerID: "p1", PlayerName: "Hero"}}})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/albion/players/search?server=americas&name=Hero", nil)
	response := httptest.NewRecorder()
	handler.Search(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"playerId":"p1"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCurrentRequiresIdentity(t *testing.T) {
	handler := NewHandler(fakeService{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/me/albion-profile", nil)
	response := httptest.NewRecorder()
	handler.Current(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestRefreshReturnsCooldown(t *testing.T) {
	handler := NewHandler(fakeService{err: CooldownError{RetryAfter: 90 * time.Second}})
	ctx := authn.WithIdentity(context.Background(), authn.Identity{Subject: "auth0|user"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/me/albion-profile/refresh", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	handler.Refresh(response, request)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("status=%d headers=%v", response.Code, response.Header())
	}
}
