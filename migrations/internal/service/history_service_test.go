package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/repository"
)

type historyRepository struct {
	ingestRequest domain.IngestHistoryRequest
	ingestResult  domain.IngestHistoryResult
	ingestErr     error
	lookup        domain.MarketHistoryLookup
	histories     []domain.MarketHistorySeries
	queryErr      error
}

func (r *historyRepository) Ping(context.Context) error { return nil }

func (r *historyRepository) IngestPrices(context.Context, domain.IngestPricesRequest) (domain.IngestPricesResult, error) {
	return domain.IngestPricesResult{}, nil
}

func (r *historyRepository) QueryCurrentPrices(context.Context, domain.CurrentPriceLookup) ([]domain.CurrentPrice, error) {
	return []domain.CurrentPrice{}, nil
}

func (r *historyRepository) IngestHistory(_ context.Context, req domain.IngestHistoryRequest) (domain.IngestHistoryResult, error) {
	r.ingestRequest = req
	return r.ingestResult, r.ingestErr
}

func (r *historyRepository) QueryMarketHistory(_ context.Context, lookup domain.MarketHistoryLookup) ([]domain.MarketHistorySeries, error) {
	r.lookup = lookup
	return append([]domain.MarketHistorySeries(nil), r.histories...), r.queryErr
}

func TestIngestHistoryNormalizesAndMapsRepositoryResult(t *testing.T) {
	t.Parallel()

	zeroPrice := int64(0)
	observedAt := time.Date(2026, time.June, 26, 10, 30, 0, 0, time.FixedZone("test", -4*60*60))
	bucketAt := time.Date(2026, time.June, 25, 0, 0, 0, 0, time.FixedZone("test", -4*60*60))
	repo := &historyRepository{ingestResult: domain.IngestHistoryResult{
		AcceptedEntries:    1,
		AcceptedBuckets:    1,
		HistoryRowsTouched: 1,
	}}
	svc := NewMarketService(repo)

	response, err := svc.IngestHistory(context.Background(), domain.IngestHistoryRequest{
		RequestID: " 00112233-4455-6677-8899-aabbccddeeff ",
		Server:    domain.ServerWest,
		Entries: []domain.HistoryIngest{{
			ObservedAt: observedAt,
			LocationID: 4002,
			ItemKey:    " T4_BAG ",
			Quality:    1,
			History: []domain.HistoryBucketIngest{{
				Timestamp:        bucketAt,
				ItemCount:        12,
				AverageUnitPrice: &zeroPrice,
			}},
		}},
	})
	if err != nil {
		t.Fatalf("IngestHistory: %v", err)
	}

	if response.RequestID != "00112233-4455-6677-8899-aabbccddeeff" ||
		response.AcceptedEntries != 1 || response.AcceptedBuckets != 1 ||
		response.HistoryRowsTouched != 1 || response.Duplicate {
		t.Fatalf("response = %#v", response)
	}
	if repo.ingestRequest.RequestID != response.RequestID {
		t.Fatalf("normalized request id = %q", repo.ingestRequest.RequestID)
	}
	entry := repo.ingestRequest.Entries[0]
	if entry.ItemKey != "T4_BAG" || entry.ObservedAt.Location() != time.UTC {
		t.Fatalf("normalized entry = %#v", entry)
	}
	if entry.History[0].Timestamp.Location() != time.UTC {
		t.Fatalf("bucket timestamp was not normalized to UTC: %v", entry.History[0].Timestamp)
	}
	if entry.History[0].AverageUnitPrice != nil {
		t.Fatalf("zero average price = %#v, want nil", entry.History[0].AverageUnitPrice)
	}
}

func TestIngestHistoryMapsIdempotencyErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		repo error
		want error
	}{
		{name: "processing", repo: repository.ErrRequestAlreadyProcessing, want: ErrIngestRequestAlreadyProcessing},
		{name: "payload conflict", repo: repository.ErrRequestPayloadConflict, want: ErrIngestRequestPayloadConflict},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repo := &historyRepository{ingestErr: test.repo}
			svc := NewMarketService(repo)
			_, err := svc.IngestHistory(context.Background(), validHistoryIngestRequest())
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestIngestHistoryRejectsInvalidBuckets(t *testing.T) {
	t.Parallel()

	negativePrice := int64(-1)
	tests := []struct {
		name   string
		mutate func(*domain.IngestHistoryRequest)
	}{
		{name: "missing request id", mutate: func(req *domain.IngestHistoryRequest) { req.RequestID = " " }},
		{name: "invalid request id", mutate: func(req *domain.IngestHistoryRequest) { req.RequestID = "not-a-uuid" }},
		{name: "invalid quality", mutate: func(req *domain.IngestHistoryRequest) { req.Entries[0].Quality = 6 }},
		{name: "negative count", mutate: func(req *domain.IngestHistoryRequest) { req.Entries[0].History[0].ItemCount = -1 }},
		{name: "negative average", mutate: func(req *domain.IngestHistoryRequest) { req.Entries[0].History[0].AverageUnitPrice = &negativePrice }},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			req := validHistoryIngestRequest()
			test.mutate(&req)
			_, err := NewMarketService(&historyRepository{}).IngestHistory(context.Background(), req)
			if !errors.Is(err, ErrInvalidHistoryIngestRequest) {
				t.Fatalf("error = %v, want ErrInvalidHistoryIngestRequest", err)
			}
		})
	}
}

func TestQueryMarketHistoryResolvesMarketKeysAndSortsPublicResponse(t *testing.T) {
	t.Parallel()

	pointTime := time.Date(2026, time.June, 25, 0, 0, 0, 0, time.FixedZone("source", -3*60*60))
	price := int64(4500)
	repo := &historyRepository{histories: []domain.MarketHistorySeries{
		{
			Server: domain.ServerWest, LocationID: 4002, ItemKey: "T4_BAG", Quality: 1,
			History: []domain.MarketHistoryPoint{{Timestamp: pointTime, ItemCount: 10, AverageUnitPrice: &price}},
		},
		{
			Server: domain.ServerWest, LocationID: 5003, ItemKey: "T4_BAG", Quality: 1,
			History: nil,
		},
	}}
	svc := NewMarketService(repo)
	requestedAt := time.Date(2026, time.June, 26, 15, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return requestedAt }

	response, err := svc.QueryMarketHistory(context.Background(), domain.HistoryQueryRequest{
		Server:     domain.ServerWest,
		MarketKeys: []string{" brecilien ", "fort_sterling", "brecilien"},
		Entries: []domain.HistoryQueryEntry{
			{ItemKey: " T4_BAG ", Quality: 1},
			{ItemKey: "T4_BAG", Quality: 1},
		},
		RangeStart: "2026-06-01",
		RangeEnd:   "2026-06-25",
	})
	if err != nil {
		t.Fatalf("QueryMarketHistory: %v", err)
	}

	if !reflect.DeepEqual(repo.lookup.LocationIDs, []int16{5003, 4002}) {
		t.Fatalf("lookup locations = %#v", repo.lookup.LocationIDs)
	}
	if len(repo.lookup.Entries) != 1 || repo.lookup.Entries[0].ItemKey != "T4_BAG" {
		t.Fatalf("lookup entries = %#v", repo.lookup.Entries)
	}
	if repo.lookup.RangeStart.Format(utcDateLayout) != "2026-06-01" ||
		repo.lookup.RangeEndExclusive.Format(utcDateLayout) != "2026-06-26" {
		t.Fatalf("lookup range = %v to %v", repo.lookup.RangeStart, repo.lookup.RangeEndExclusive)
	}
	if response.RequestedAt != requestedAt || response.RangeStart != "2026-06-01" || response.RangeEnd != "2026-06-25" {
		t.Fatalf("response metadata = %#v", response)
	}
	if response.Count != 2 || response.BucketCount != 1 {
		t.Fatalf("response counts = %#v", response)
	}
	if response.Data[0].MarketKey != "brecilien" || response.Data[1].MarketKey != "fort_sterling" {
		t.Fatalf("response order = %#v", response.Data)
	}
	if response.Data[0].History == nil {
		t.Fatal("nil history was not normalized to an empty array")
	}
	if response.Data[1].History[0].Timestamp.Location() != time.UTC {
		t.Fatalf("history timestamp = %v, want UTC", response.Data[1].History[0].Timestamp)
	}
}

func TestQueryMarketHistoryRejectsInvalidContract(t *testing.T) {
	t.Parallel()

	tests := []domain.HistoryQueryRequest{
		{Server: domain.ServerWest, MarketKeys: []string{"unknown"}, Entries: []domain.HistoryQueryEntry{{ItemKey: "T4_BAG", Quality: 1}}, RangeStart: "2026-06-01", RangeEnd: "2026-06-25"},
		{Server: domain.ServerWest, MarketKeys: []string{"black_market"}, Entries: []domain.HistoryQueryEntry{{ItemKey: "T4_BAG", Quality: 1}}, RangeStart: "2026-06-01", RangeEnd: "2026-06-25"},
		{Server: domain.ServerWest, MarketKeys: []string{"martlock"}, Entries: []domain.HistoryQueryEntry{{ItemKey: "T4_BAG", Quality: 6}}, RangeStart: "2026-06-01", RangeEnd: "2026-06-25"},
		{Server: domain.ServerWest, MarketKeys: []string{"martlock"}, Entries: []domain.HistoryQueryEntry{{ItemKey: "T4_BAG", Quality: 1}}, RangeStart: "2026/06/01", RangeEnd: "2026-06-25"},
		{Server: domain.ServerWest, MarketKeys: []string{"martlock"}, Entries: []domain.HistoryQueryEntry{{ItemKey: "T4_BAG", Quality: 1}}, RangeStart: "2026-06-25", RangeEnd: "2026-06-01"},
	}

	for index, req := range tests {
		_, err := NewMarketService(&historyRepository{}).QueryMarketHistory(context.Background(), req)
		if !errors.Is(err, ErrInvalidHistoryQuery) {
			t.Fatalf("case %d error = %v, want ErrInvalidHistoryQuery", index, err)
		}
	}
}

func TestQueryMarketHistoryReturnsNonNilEmptyData(t *testing.T) {
	t.Parallel()

	repo := &historyRepository{histories: nil}
	response, err := NewMarketService(repo).QueryMarketHistory(context.Background(), domain.HistoryQueryRequest{
		Server:     domain.ServerWest,
		MarketKeys: []string{"martlock"},
		Entries:    []domain.HistoryQueryEntry{{ItemKey: "T4_BAG", Quality: 1}},
		RangeStart: "2026-06-01",
		RangeEnd:   "2026-06-25",
	})
	if err != nil {
		t.Fatalf("QueryMarketHistory: %v", err)
	}
	if response.Data == nil || len(response.Data) != 0 || response.Count != 0 || response.BucketCount != 0 {
		t.Fatalf("response = %#v", response)
	}
}

func validHistoryIngestRequest() domain.IngestHistoryRequest {
	price := int64(4500)
	return domain.IngestHistoryRequest{
		RequestID: "00112233-4455-6677-8899-aabbccddeeff",
		Server:    domain.ServerWest,
		Entries: []domain.HistoryIngest{{
			ObservedAt: time.Date(2026, time.June, 26, 10, 0, 0, 0, time.UTC),
			LocationID: 4002,
			ItemKey:    "T4_BAG",
			Quality:    1,
			History: []domain.HistoryBucketIngest{{
				Timestamp:        time.Date(2026, time.June, 25, 0, 0, 0, 0, time.UTC),
				ItemCount:        12,
				AverageUnitPrice: &price,
			}},
		}},
	}
}
