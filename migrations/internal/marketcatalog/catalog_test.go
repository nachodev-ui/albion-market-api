package marketcatalog

import "testing"

func TestDefaultCatalogListsEnabledMarketsWithoutInternalIDs(t *testing.T) {
	t.Parallel()

	catalog := NewDefault()
	markets := catalog.List(false)

	if len(markets) != 7 {
		t.Fatalf("enabled markets = %d, want 7", len(markets))
	}
	if markets[0].Key != "brecilien" || markets[len(markets)-1].Key != "thetford" {
		t.Fatalf("unexpected alphabetical order: %#v", markets)
	}
}

func TestResolveEnabledNormalizesAndDeduplicatesMarketKeys(t *testing.T) {
	t.Parallel()

	catalog := NewDefault()
	locationIDs, keysByLocation, err := catalog.ResolveEnabled([]string{
		" Fort_Sterling ",
		"fort_sterling",
		"thetford",
	})
	if err != nil {
		t.Fatalf("ResolveEnabled: %v", err)
	}
	if len(locationIDs) != 2 || locationIDs[0] != 4002 || locationIDs[1] != 7 {
		t.Fatalf("locationIDs = %#v, want [4002 7]", locationIDs)
	}
	if keysByLocation[4002] != "fort_sterling" || keysByLocation[7] != "thetford" {
		t.Fatalf("keysByLocation = %#v", keysByLocation)
	}
}

func TestResolveEnabledRejectsUnknownAndDisabledMarkets(t *testing.T) {
	t.Parallel()

	catalog := NewDefault()
	if _, _, err := catalog.ResolveEnabled([]string{"unknown"}); err == nil {
		t.Fatal("unknown marketKey did not fail")
	}
	if _, _, err := catalog.ResolveEnabled([]string{"black_market"}); err == nil {
		t.Fatal("disabled marketKey did not fail")
	}
}
