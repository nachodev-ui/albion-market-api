package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
)

func TestRawPriceCopySource(t *testing.T) {
	t.Parallel()

	requestID := mustUUID(t, "00112233445566778899aabbccddeeff")
	observedAt := time.Date(2026, time.June, 25, 12, 0, 0, 0, time.UTC)
	sellPrice := int64(1234)
	sellAt := observedAt.Add(-time.Minute)
	buyPrice := int64(987)
	buyAt := observedAt.Add(-2 * time.Minute)

	entries := []domain.PriceIngest{
		{
			ObservedAt:     observedAt,
			LocationID:     4002,
			ItemKey:        "T5_LEATHER_LEVEL4@4",
			Quality:        1,
			SellPriceMin:   &sellPrice,
			SellPriceMinAt: &sellAt,
		},
		{
			ObservedAt:    observedAt.Add(time.Second),
			LocationID:    3005,
			ItemKey:       "T4_BAG",
			Quality:       2,
			BuyPriceMax:   &buyPrice,
			BuyPriceMaxAt: &buyAt,
		},
	}

	source := newRawPriceCopySource(requestID, 1, entries)
	if _, err := source.Values(); !errors.Is(err, errCopySourceNotPositioned) {
		t.Fatalf("Values before Next error = %v, want %v", err, errCopySourceNotPositioned)
	}

	if !source.Next() {
		t.Fatal("Next returned false for first row")
	}
	first, err := source.Values()
	if err != nil {
		t.Fatalf("first Values: %v", err)
	}
	assertCopyRow(t, first, requestID, int16(1), entries[0])

	if !source.Next() {
		t.Fatal("Next returned false for second row")
	}
	second, err := source.Values()
	if err != nil {
		t.Fatalf("second Values: %v", err)
	}
	assertCopyRow(t, second, requestID, int16(1), entries[1])

	if source.Next() {
		t.Fatal("Next returned true after the final row")
	}
	if err := source.Err(); err != nil {
		t.Fatalf("Err = %v, want nil", err)
	}
}

func BenchmarkRawPriceCopySource500(b *testing.B) {
	requestID := mustUUID(b, "00112233445566778899aabbccddeeff")
	observedAt := time.Date(2026, time.June, 25, 12, 0, 0, 0, time.UTC)
	price := int64(1234)
	entries := make([]domain.PriceIngest, 500)
	for i := range entries {
		entries[i] = domain.PriceIngest{
			ObservedAt:     observedAt,
			LocationID:     int16(3000 + i%10),
			ItemKey:        "T5_LEATHER_LEVEL4@4",
			Quality:        int16(1 + i%5),
			SellPriceMin:   &price,
			SellPriceMinAt: &observedAt,
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		source := newRawPriceCopySource(requestID, 1, entries)
		for source.Next() {
			benchmarkCopyValues, benchmarkCopyErr = source.Values()
		}
	}
}

var (
	benchmarkCopyValues []any
	benchmarkCopyErr    error
)

func assertCopyRow(t *testing.T, values []any, requestID pgtype.UUID, serverID int16, entry domain.PriceIngest) {
	t.Helper()

	if len(values) != len(marketIngestRawColumns) {
		t.Fatalf("row length = %d, want %d", len(values), len(marketIngestRawColumns))
	}
	if values[0] != requestID {
		t.Fatalf("request_id = %#v, want %#v", values[0], requestID)
	}
	if values[1] != serverID {
		t.Fatalf("server = %#v, want %d", values[1], serverID)
	}

	want := []any{
		entry.ObservedAt,
		entry.LocationID,
		entry.ItemKey,
		entry.Quality,
		entry.SellPriceMin,
		entry.SellPriceMinAt,
		entry.BuyPriceMax,
		entry.BuyPriceMaxAt,
	}
	for i, expected := range want {
		if values[i+2] != expected {
			t.Fatalf("column %s = %#v, want %#v", marketIngestRawColumns[i+2], values[i+2], expected)
		}
	}
}

type testingTB interface {
	Helper()
	Fatalf(format string, args ...any)
}

func mustUUID(tb testingTB, value string) pgtype.UUID {
	tb.Helper()
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		tb.Fatalf("parse UUID %q: %v", value, err)
	}
	return id
}
