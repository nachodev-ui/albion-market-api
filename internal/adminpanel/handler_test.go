package adminpanel

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nachodev-ui/albion-market-api/internal/accountadmin"
	"github.com/nachodev-ui/albion-market-api/internal/authn"
)

type fakeService struct {
	sessionErr    error
	grantResult   accountadmin.OperationResult
	grantIdentity authn.Identity
	grantUserID   string
	grantDays     int
	grantReason   string
	grantConfirm  string
	grantErr      error
}

func (f *fakeService) Session(context.Context, authn.Identity) (Session, error) {
	if f.sessionErr != nil {
		return Session{}, f.sessionErr
	}
	return Session{AdminID: "admin-1", UserID: "user-1", Active: true}, nil
}
func (f *fakeService) SearchUsers(context.Context, authn.Identity, string, int) ([]UserSummary, error) {
	return []UserSummary{}, nil
}
func (f *fakeService) UserDetail(context.Context, authn.Identity, string) (UserDetail, error) {
	return UserDetail{}, nil
}
func (f *fakeService) GrantPro(_ context.Context, identity authn.Identity, userID string, days int, reason, confirmation string) (accountadmin.OperationResult, error) {
	f.grantIdentity, f.grantUserID, f.grantDays = identity, userID, days
	f.grantReason, f.grantConfirm = reason, confirmation
	if f.grantErr != nil {
		return accountadmin.OperationResult{}, f.grantErr
	}
	return f.grantResult, nil
}
func (f *fakeService) RevokePro(context.Context, authn.Identity, string, string, string) (accountadmin.OperationResult, error) {
	return accountadmin.OperationResult{}, nil
}
func (f *fakeService) AuditEvents(context.Context, authn.Identity, int) ([]AuditEvent, error) {
	return []AuditEvent{}, nil
}

func TestAdminSessionRejectsNonAdministrator(t *testing.T) {
	t.Parallel()
	handler := NewHandler(&fakeService{sessionErr: ErrForbidden})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/session", nil)
	request = request.WithContext(authn.WithIdentity(request.Context(), authn.Identity{Subject: "auth0|non-admin"}))
	response := httptest.NewRecorder()
	handler.Session(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestAdminSessionRequiresAuthenticatedIdentity(t *testing.T) {
	t.Parallel()
	handler := NewHandler(&fakeService{})
	response := httptest.NewRecorder()
	handler.Session(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/session", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestGrantProUsesJWTSubjectAsActor(t *testing.T) {
	t.Parallel()
	service := &fakeService{grantResult: accountadmin.OperationResult{Action: "grant_pro", Changed: true}}
	handler := NewHandler(service)
	body := `{"durationDays":30,"reason":"Acceso beta aprobado","confirmation":"GRANT PRO"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/7d6fd4e1-02c9-46f6-a98a-437fa77ea36d/grant-pro", strings.NewReader(body))
	request.SetPathValue("userId", "7d6fd4e1-02c9-46f6-a98a-437fa77ea36d")
	request = request.WithContext(authn.WithIdentity(request.Context(), authn.Identity{Subject: "google-oauth2|administrator"}))
	response := httptest.NewRecorder()
	handler.GrantPro(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	if service.grantIdentity.Subject != "google-oauth2|administrator" || service.grantDays != 30 || service.grantConfirm != GrantConfirmation {
		t.Fatalf("grant arguments were not derived correctly: %#v", service)
	}
}

func TestHandlerMapsInvalidRequestToBadRequest(t *testing.T) {
	t.Parallel()
	service := &fakeService{grantErr: fmt.Errorf("%w: invalid duration", ErrInvalidRequest)}
	handler := NewHandler(service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/7d6fd4e1-02c9-46f6-a98a-437fa77ea36d/grant-pro", strings.NewReader(`{"durationDays":0}`))
	request.SetPathValue("userId", "7d6fd4e1-02c9-46f6-a98a-437fa77ea36d")
	request = request.WithContext(authn.WithIdentity(request.Context(), authn.Identity{Subject: "auth0|admin"}))
	response := httptest.NewRecorder()
	handler.GrantPro(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
