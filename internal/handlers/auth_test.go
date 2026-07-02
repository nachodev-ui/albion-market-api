package handlers

import (
	"testing"

	"github.com/nachodev-ui/albion-market-api/internal/ingestauth"
)

func testAuthenticator(t *testing.T, tokens ...string) *ingestauth.Authenticator {
	t.Helper()
	credentials := make([]ingestauth.Credential, 0, len(tokens))
	for index, token := range tokens {
		id := "current"
		if index > 0 {
			id = "previous"
		}
		credentials = append(credentials, ingestauth.Credential{ID: id, Token: token})
	}
	authenticator, err := ingestauth.New(credentials, ingestauth.Options{})
	if err != nil {
		t.Fatalf("create test authenticator: %v", err)
	}
	return authenticator
}
