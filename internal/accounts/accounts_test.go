package accounts

import "testing"

func TestBoolEnabled(t *testing.T) {
	t.Parallel()
	if !BoolEnabled(true) {
		t.Fatal("true entitlement was rejected")
	}
	for _, value := range []any{false, 1, "true", nil} {
		if BoolEnabled(value) {
			t.Fatalf("unexpected enabled value: %#v", value)
		}
	}
}

func TestNumberAtLeast(t *testing.T) {
	t.Parallel()
	require := NumberAtLeast(28)
	if !require(float64(28)) || !require(float64(500)) {
		t.Fatal("valid numeric entitlement was rejected")
	}
	if require(float64(7)) || require("28") {
		t.Fatal("invalid numeric entitlement was accepted")
	}
}
