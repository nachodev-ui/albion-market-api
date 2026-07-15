package blackmarket

import (
	"testing"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/marketcatalog"
)

func TestNormalizeOpportunitiesRequestDefaults(t *testing.T) {
	t.Parallel()

	service := NewService(nil, marketcatalog.NewDefault())
	request, err := service.normalizeOpportunitiesRequest(OpportunitiesRequest{
		Server: domain.ServerWest,
	})
	if err != nil {
		t.Fatal(err)
	}

	if request.limit != defaultOpportunityLimit {
		t.Fatalf("limit = %d, want %d", request.limit, defaultOpportunityLimit)
	}
	if request.maximumCityAgeMinutes != defaultMaximumCityAgeMinutes {
		t.Fatalf("maximum city age = %d", request.maximumCityAgeMinutes)
	}
	if request.maximumBlackMarketAgeMinutes != defaultMaximumBlackMarketAgeMinutes {
		t.Fatalf("maximum Black Market age = %d", request.maximumBlackMarketAgeMinutes)
	}
	if len(request.purchaseLocationIDs) != len(defaultOpportunityMarkets) {
		t.Fatalf("purchase markets = %#v", request.purchaseMarketKeys)
	}
	if !request.includeWeapon || !request.includeArmor || !request.includeOffhand || !request.includeAccessory {
		t.Fatalf("default categories were not enabled: %#v", request.categories)
	}
	if request.sort != "profit" {
		t.Fatalf("sort = %q, want profit", request.sort)
	}
}

func TestNormalizeOpportunitiesRequestRejectsUnsafeFilters(t *testing.T) {
	t.Parallel()

	service := NewService(nil, marketcatalog.NewDefault())
	tests := []struct {
		name    string
		request OpportunitiesRequest
	}{
		{
			name: "unknown market",
			request: OpportunitiesRequest{
				Server:             domain.ServerWest,
				PurchaseMarketKeys: []string{"black_market"},
			},
		},
		{
			name: "unsupported category",
			request: OpportunitiesRequest{
				Server:     domain.ServerWest,
				Categories: []string{"resource"},
			},
		},
		{
			name: "tier outside equipment range",
			request: OpportunitiesRequest{
				Server: domain.ServerWest,
				Tiers:  []int16{3},
			},
		},
		{
			name: "excessive pagination",
			request: OpportunitiesRequest{
				Server: domain.ServerWest,
				Offset: maximumOpportunityOffset + 1,
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := service.normalizeOpportunitiesRequest(test.request); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestOpportunityTierEnchantmentAndCategory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		item        string
		tier        int16
		enchantment int16
		category    string
	}{
		{item: "T8_2H_HALBERD@2", tier: 8, enchantment: 2, category: "weapon"},
		{item: "T6_ARMOR_PLATE_ROYAL", tier: 6, enchantment: 0, category: "armor"},
		{item: "T5_OFF_SHIELD@1", tier: 5, enchantment: 1, category: "offhand"},
		{item: "T4_BAG", tier: 4, enchantment: 0, category: "accessory"},
	}

	for _, test := range tests {
		tier, enchantment := opportunityTierAndEnchantment(test.item)
		if tier != test.tier || enchantment != test.enchantment {
			t.Fatalf("%s parsed as tier=%d enchantment=%d", test.item, tier, enchantment)
		}
		if category := opportunityCategory(test.item); category != test.category {
			t.Fatalf("%s category = %q, want %q", test.item, category, test.category)
		}
	}
}

func TestOpportunityRiskIncludesCaerleonCompetition(t *testing.T) {
	t.Parallel()

	profit := int64(25_000)
	risk, reasons := opportunityRisk(
		"martlock",
		2,
		2,
		30,
		20,
		nil,
		100_000,
		OpportunityCompetition{Available: true, Profit: &profit, CanFillProfitably: true},
	)
	if risk != "medium" {
		t.Fatalf("risk = %q, want medium", risk)
	}
	if len(reasons) == 0 {
		t.Fatal("expected competition reason")
	}
}

func TestAgeMinutesClampsFutureCapture(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	if got := ageMinutes(now, now.Add(time.Minute)); got != 0 {
		t.Fatalf("future age = %d, want 0", got)
	}
	if got := ageMinutes(now, now.Add(-95*time.Minute)); got != 95 {
		t.Fatalf("age = %d, want 95", got)
	}
}
