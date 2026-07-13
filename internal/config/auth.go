package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/authn"
)

func LoadAccountAuth(appEnv string) (authn.Config, error) {
	enabled, err := boolEnv("AUTH_ENABLED", false)
	if err != nil {
		return authn.Config{}, err
	}
	cacheTTL, err := durationEnv("AUTH_JWKS_CACHE_TTL", 15*time.Minute)
	if err != nil {
		return authn.Config{}, err
	}
	timeout, err := durationEnv("AUTH_HTTP_TIMEOUT", 5*time.Second)
	if err != nil {
		return authn.Config{}, err
	}
	skew, err := durationEnv("AUTH_CLOCK_SKEW", 30*time.Second)
	if err != nil {
		return authn.Config{}, err
	}

	cfg := authn.Config{
		Enabled:     enabled,
		Issuer:      strings.TrimSpace(getEnv("AUTH_ISSUER", "")),
		Audience:    strings.TrimSpace(getEnv("AUTH_AUDIENCE", "")),
		CacheTTL:    cacheTTL,
		HTTPTimeout: timeout,
		ClockSkew:   skew,
	}
	if !enabled {
		return cfg, nil
	}

	issuer, err := url.Parse(cfg.Issuer)
	if err != nil || !issuer.IsAbs() || issuer.Host == "" {
		return authn.Config{}, fmt.Errorf("AUTH_ISSUER must be an absolute URL")
	}
	if strings.EqualFold(strings.TrimSpace(appEnv), "production") && issuer.Scheme != "https" {
		return authn.Config{}, fmt.Errorf("AUTH_ISSUER must use https in production")
	}
	if cfg.Audience == "" {
		return authn.Config{}, fmt.Errorf("AUTH_AUDIENCE is required when AUTH_ENABLED=true")
	}
	if cacheTTL < time.Minute || timeout < time.Second || skew < 0 {
		return authn.Config{}, fmt.Errorf("invalid authentication timing configuration")
	}

	cfg.Issuer = strings.TrimRight(issuer.String(), "/") + "/"
	return cfg, nil
}
