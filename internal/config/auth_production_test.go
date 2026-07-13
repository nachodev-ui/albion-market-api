package config

import "testing"

func TestLoadAccountAuthPinsProductionIdentity(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "false")
	t.Setenv("AUTH_EMERGENCY_DISABLED", "false")
	t.Setenv("AUTH_ISSUER", "https://stale-tenant.example.invalid/")
	t.Setenv("AUTH_AUDIENCE", "https://stale-audience.example.invalid")

	cfg, err := LoadAccountAuth("production")
	if err != nil {
		t.Fatalf("LoadAccountAuth() error = %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("production authentication must be enabled")
	}
	if cfg.Issuer != productionAuthIssuer {
		t.Fatalf("Issuer = %q, want %q", cfg.Issuer, productionAuthIssuer)
	}
	if cfg.Audience != productionAuthAudience {
		t.Fatalf("Audience = %q, want %q", cfg.Audience, productionAuthAudience)
	}
}

func TestLoadAccountAuthEmergencyDisable(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "true")
	t.Setenv("AUTH_EMERGENCY_DISABLED", "true")
	t.Setenv("AUTH_ISSUER", "")
	t.Setenv("AUTH_AUDIENCE", "")

	cfg, err := LoadAccountAuth("production")
	if err != nil {
		t.Fatalf("LoadAccountAuth() error = %v", err)
	}
	if cfg.Enabled {
		t.Fatal("AUTH_EMERGENCY_DISABLED must disable production authentication")
	}
}
