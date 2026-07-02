package repository

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
)

func TestRawHistoryCopySourceWalksNestedBuckets(t *testing.T) {
	t.Parallel()

	requestID := mustUUID(t, "00112233445566778899aabbccddeeff")
	observedAt := time.Date(2026, time.June, 26, 12, 0, 0, 0, time.UTC)
	price := int64(4500)
	entries := []domain.HistoryIngest{
		{
			ObservedAt: observedAt,
			LocationID: 4002,
			ItemKey:    "T4_BAG",
			Quality:    1,
			History: []domain.HistoryBucketIngest{
				{Timestamp: observedAt.AddDate(0, 0, -2), ItemCount: 10, AverageUnitPrice: &price},
				{Timestamp: observedAt.AddDate(0, 0, -1), ItemCount: 12, AverageUnitPrice: nil},
			},
		},
		{
			ObservedAt: observedAt,
			LocationID: 3008,
			ItemKey:    "T5_BAG",
			Quality:    2,
			History:    nil,
		},
		{
			ObservedAt: observedAt.Add(time.Minute),
			LocationID: 5003,
			ItemKey:    "T6_BAG",
			Quality:    3,
			History: []domain.HistoryBucketIngest{
				{Timestamp: observedAt, ItemCount: 20, AverageUnitPrice: &price},
			},
		},
	}

	source := newRawHistoryCopySource(requestID, 1, entries)
	if _, err := source.Values(); !errors.Is(err, errHistoryCopySourceNotPositioned) {
		t.Fatalf("Values before Next error = %v", err)
	}

	want := []struct {
		entry  domain.HistoryIngest
		bucket domain.HistoryBucketIngest
	}{
		{entries[0], entries[0].History[0]},
		{entries[0], entries[0].History[1]},
		{entries[2], entries[2].History[0]},
	}
	for index, expected := range want {
		if !source.Next() {
			t.Fatalf("Next returned false at row %d", index)
		}
		values, err := source.Values()
		if err != nil {
			t.Fatalf("Values row %d: %v", index, err)
		}
		assertHistoryCopyRow(t, values, requestID, 1, expected.entry, expected.bucket)
	}
	if source.Next() {
		t.Fatal("Next returned true after final bucket")
	}
	if _, err := source.Values(); !errors.Is(err, errHistoryCopySourceNotPositioned) {
		t.Fatalf("Values after final Next error = %v", err)
	}
	if err := source.Err(); err != nil {
		t.Fatalf("Err = %v", err)
	}
}

func TestCountHistoryBuckets(t *testing.T) {
	t.Parallel()

	entries := []domain.HistoryIngest{
		{History: make([]domain.HistoryBucketIngest, 3)},
		{History: nil},
		{History: make([]domain.HistoryBucketIngest, 2)},
	}
	if got := countHistoryBuckets(entries); got != 5 {
		t.Fatalf("countHistoryBuckets = %d, want 5", got)
	}
}

func TestCanonicalHistoryRequestHashIsDeterministicAndPayloadSensitive(t *testing.T) {
	t.Parallel()

	price := int64(4500)
	req := domain.IngestHistoryRequest{
		RequestID: "00112233-4455-6677-8899-aabbccddeeff",
		Server:    domain.ServerWest,
		Entries: []domain.HistoryIngest{{
			ObservedAt: time.Date(2026, time.June, 26, 12, 0, 0, 0, time.UTC),
			LocationID: 4002,
			ItemKey:    "T4_BAG",
			Quality:    1,
			History: []domain.HistoryBucketIngest{{
				Timestamp: time.Date(2026, time.June, 25, 0, 0, 0, 0, time.UTC),
				ItemCount: 12, AverageUnitPrice: &price,
			}},
		}},
	}

	first, err := canonicalHistoryRequestHash(req)
	if err != nil {
		t.Fatalf("first hash: %v", err)
	}
	second, err := canonicalHistoryRequestHash(req)
	if err != nil {
		t.Fatalf("second hash: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same request produced different hashes: %x %x", first, second)
	}

	req.Entries[0].History[0].ItemCount++
	changed, err := canonicalHistoryRequestHash(req)
	if err != nil {
		t.Fatalf("changed hash: %v", err)
	}
	if reflect.DeepEqual(first, changed) {
		t.Fatalf("different payload produced the same hash: %x", first)
	}
}

func assertHistoryCopyRow(
	t *testing.T,
	values []any,
	requestID pgtype.UUID,
	serverID int16,
	entry domain.HistoryIngest,
	bucket domain.HistoryBucketIngest,
) {
	t.Helper()

	if len(values) != len(marketHistoryIngestRawColumns) {
		t.Fatalf("row length = %d, want %d", len(values), len(marketHistoryIngestRawColumns))
	}
	want := []any{
		requestID,
		serverID,
		entry.ObservedAt,
		entry.LocationID,
		entry.ItemKey,
		entry.Quality,
		bucket.Timestamp,
		bucket.ItemCount,
		bucket.AverageUnitPrice,
	}
	for index, expected := range want {
		if values[index] != expected {
			t.Fatalf("column %s = %#v, want %#v", marketHistoryIngestRawColumns[index], values[index], expected)
		}
	}
}
