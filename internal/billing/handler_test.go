package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestValidSignature(t *testing.T) {
	t.Parallel()
	secret := []byte("sandbox-signing-secret")
	body := []byte(`{"meta":{"event_name":"subscription_created"}}`)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))

	if !validSignature(secret, body, signature) {
		t.Fatal("valid Lemon Squeezy signature was rejected")
	}
	if validSignature(secret, []byte("changed"), signature) {
		t.Fatal("signature accepted a modified payload")
	}
	if validSignature(secret, body, "not-hex") {
		t.Fatal("malformed signature was accepted")
	}
}
