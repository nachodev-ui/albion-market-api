package authn

import (
	"encoding/base64"
	"testing"
)

func unsignedToken(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test"}`)) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".signature"
}

func TestBearerTokenHasScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		header   string
		required string
		want     bool
	}{
		{
			name:     "scope claim contains permission",
			header:   "Bearer " + unsignedToken(`{"scope":"openid profile email read:account"}`),
			required: "read:account",
			want:     true,
		},
		{
			name:     "permissions claim contains permission",
			header:   "Bearer " + unsignedToken(`{"permissions":["read:account"]}`),
			required: "read:account",
			want:     true,
		},
		{
			name:     "permission missing",
			header:   "Bearer " + unsignedToken(`{"scope":"openid profile email"}`),
			required: "read:account",
			want:     false,
		},
		{
			name:     "malformed token",
			header:   "Bearer invalid",
			required: "read:account",
			want:     false,
		},
		{
			name:     "missing bearer header",
			header:   "",
			required: "read:account",
			want:     false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := bearerTokenHasScope(test.header, test.required); got != test.want {
				t.Fatalf("bearerTokenHasScope() = %v, want %v", got, test.want)
			}
		})
	}
}
