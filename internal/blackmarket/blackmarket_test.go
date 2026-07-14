package blackmarket

import (
	"math"
	"testing"
	"time"
)

func TestCalculateEconomics(t *testing.T) {
	purchase := int64(1_000)
	sale := int64(1_500)

	result := calculateEconomics(10, &purchase, &sale, "manual", 0.04, 500)
	if !result.Ready {
		t.Fatal("expected complete economics")
	}
	if result.PurchaseCost == nil || *result.PurchaseCost != 10_000 {
		t.Fatalf("unexpected purchase cost: %#v", result.PurchaseCost)
	}
	if result.GrossRevenue == nil || *result.GrossRevenue != 15_000 {
		t.Fatalf("unexpected gross revenue: %#v", result.GrossRevenue)
	}
	if result.SalesTax == nil || *result.SalesTax != 600 {
		t.Fatalf("unexpected tax: %#v", result.SalesTax)
	}
	if result.Profit == nil || *result.Profit != 3_900 {
		t.Fatalf("unexpected profit: %#v", result.Profit)
	}
	if result.BreakEvenUnitPrice == nil || *result.BreakEvenUnitPrice != 1_094 {
		t.Fatalf("unexpected break-even price: %#v", result.BreakEvenUnitPrice)
	}
	if result.ReturnOnCostPercent == nil || math.Abs(*result.ReturnOnCostPercent-37.142857) > 0.001 {
		t.Fatalf("unexpected ROI: %#v", result.ReturnOnCostPercent)
	}
}

func TestCalculateEconomicsRequiresBothPrices(t *testing.T) {
	purchase := int64(1_000)
	result := calculateEconomics(10, &purchase, nil, "missing", 0.04, 0)
	if result.Ready {
		t.Fatal("expected incomplete economics")
	}
	if result.Profit != nil {
		t.Fatalf("expected no profit, got %#v", result.Profit)
	}
}

func TestFreshness(t *testing.T) {
	now := mustTime(t, "2026-07-14T12:00:00Z")
	fresh := mustTime(t, "2026-07-14T01:00:00Z")
	stale := mustTime(t, "2026-07-12T01:00:00Z")

	if got := freshness(now, &fresh); got != "fresh" {
		t.Fatalf("expected fresh, got %s", got)
	}
	if got := freshness(now, &stale); got != "stale" {
		t.Fatalf("expected stale, got %s", got)
	}
	if got := freshness(now, nil); got != "missing" {
		t.Fatalf("expected missing, got %s", got)
	}
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
