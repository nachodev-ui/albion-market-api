package blackmarket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/marketcatalog"
)

const (
	EntitlementKey       = "black_market.analytics"
	blackMarketLocation  = int16(3003)
	defaultHistoryDays   = 28
	maximumHistoryDays   = 90
	defaultSalesTaxRate  = 0.04
	maximumRequestBody   = int64(32 * 1024)
	freshPriceWindow     = 24 * time.Hour
)

type AnalysisRequest struct {
	Server                domain.Server `json:"server"`
	PurchaseMarketKey     string        `json:"purchaseMarketKey"`
	ItemIdentifier        string        `json:"itemIdentifier"`
	Quality               int16         `json:"quality"`
	Quantity              int64         `json:"quantity"`
	SaleUnitPriceOverride *int64        `json:"saleUnitPriceOverride"`
	SalesTaxRate          *float64      `json:"salesTaxRate"`
	TransportCost         int64         `json:"transportCost"`
	HistoryDays           int           `json:"historyDays"`
}

type PriceSnapshot struct {
	MarketKey    string     `json:"marketKey"`
	UnitPrice    *int64     `json:"unitPrice"`
	PriceDate    *time.Time `json:"priceDate"`
	UpdatedAt    *time.Time `json:"updatedAt"`
	Freshness    string     `json:"freshness"`
}

type HistoryPoint struct {
	Timestamp        time.Time `json:"timestamp"`
	ItemCount        int64     `json:"itemCount"`
	AverageUnitPrice *int64    `json:"averageUnitPrice"`
}

type HistorySummary struct {
	RangeDays        int            `json:"rangeDays"`
	BucketCount      int            `json:"bucketCount"`
	SoldUnits        int64          `json:"soldUnits"`
	WeightedAverage *int64         `json:"weightedAverageUnitPrice"`
	LowestAverage   *int64         `json:"lowestAverageUnitPrice"`
	HighestAverage  *int64         `json:"highestAverageUnitPrice"`
	LastObservedAt  *time.Time     `json:"lastObservedAt"`
	Points          []HistoryPoint `json:"points"`
}

type Economics struct {
	Ready              bool    `json:"ready"`
	Quantity           int64   `json:"quantity"`
	PurchaseUnitPrice  *int64  `json:"purchaseUnitPrice"`
	SaleUnitPrice      *int64  `json:"saleUnitPrice"`
	SalePriceSource    string  `json:"salePriceSource"`
	PurchaseCost       *int64  `json:"purchaseCost"`
	GrossRevenue       *int64  `json:"grossRevenue"`
	SalesTax           *int64  `json:"salesTax"`
	TransportCost      int64   `json:"transportCost"`
	NetRevenue         *int64  `json:"netRevenue"`
	Profit              *int64  `json:"profit"`
	ProfitPerUnit       *int64  `json:"profitPerUnit"`
	MarginPercent       *float64 `json:"marginPercent"`
	ReturnOnCostPercent *float64 `json:"returnOnCostPercent"`
	BreakEvenUnitPrice  *int64  `json:"breakEvenUnitPrice"`
}

type AnalysisResponse struct {
	RequestedAt       time.Time      `json:"requestedAt"`
	Server            domain.Server  `json:"server"`
	ItemIdentifier    string         `json:"itemIdentifier"`
	Quality           int16          `json:"quality"`
	PurchaseMarketKey string         `json:"purchaseMarketKey"`
	BlackMarketKey    string         `json:"blackMarketKey"`
	Purchase          PriceSnapshot  `json:"purchase"`
	BlackMarket       PriceSnapshot  `json:"blackMarket"`
	History           HistorySummary `json:"history"`
	Economics         Economics      `json:"economics"`
	Warnings          []string       `json:"warnings"`
}

type Service struct {
	db      *pgxpool.Pool
	catalog *marketcatalog.Catalog
	now     func() time.Time
}

func NewService(db *pgxpool.Pool, catalog *marketcatalog.Catalog) *Service {
	if catalog == nil {
		catalog = marketcatalog.NewDefault()
	}
	return &Service{db: db, catalog: catalog, now: time.Now}
}

type Handler struct {
	service      *Service
	maxBodyBytes int64
}

func NewHandler(service *Service, maxBodyBytes ...int64) *Handler {
	limit := maximumRequestBody
	if len(maxBodyBytes) > 0 && maxBodyBytes[0] > 0 {
		limit = maxBodyBytes[0]
	}
	return &Handler{service: service, maxBodyBytes: limit}
}

func (h *Handler) Analyze(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if h == nil || h.service == nil || h.service.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "black market analytics unavailable"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request AnalysisRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	response, err := h.service.Analyze(r.Context(), request)
	if err != nil {
		var validationError *ValidationError
		if errors.As(err, &validationError) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, response)
}

type ValidationError struct{ message string }

func (e *ValidationError) Error() string { return e.message }

func invalid(message string) error { return &ValidationError{message: message} }

func (s *Service) Analyze(ctx context.Context, request AnalysisRequest) (AnalysisResponse, error) {
	request.ItemIdentifier = strings.ToUpper(strings.TrimSpace(request.ItemIdentifier))
	request.PurchaseMarketKey = marketcatalog.NormalizeKey(request.PurchaseMarketKey)
	if request.Server != domain.ServerWest && request.Server != domain.ServerEast && request.Server != domain.ServerEurope {
		return AnalysisResponse{}, invalid("server must be west, east or europe")
	}
	if request.ItemIdentifier == "" || len(request.ItemIdentifier) > 160 {
		return AnalysisResponse{}, invalid("itemIdentifier is required")
	}
	if request.Quality < 1 || request.Quality > 5 {
		return AnalysisResponse{}, invalid("quality must be between 1 and 5")
	}
	if request.Quantity < 1 || request.Quantity > 1_000_000 {
		return AnalysisResponse{}, invalid("quantity must be between 1 and 1000000")
	}
	if request.TransportCost < 0 {
		return AnalysisResponse{}, invalid("transportCost cannot be negative")
	}
	if request.SaleUnitPriceOverride != nil && *request.SaleUnitPriceOverride <= 0 {
		return AnalysisResponse{}, invalid("saleUnitPriceOverride must be positive")
	}

	historyDays := request.HistoryDays
	if historyDays == 0 {
		historyDays = defaultHistoryDays
	}
	if historyDays < 1 || historyDays > maximumHistoryDays {
		return AnalysisResponse{}, invalid("historyDays must be between 1 and 90")
	}
	taxRate := defaultSalesTaxRate
	if request.SalesTaxRate != nil {
		taxRate = *request.SalesTaxRate
	}
	if math.IsNaN(taxRate) || math.IsInf(taxRate, 0) || taxRate < 0 || taxRate >= 1 {
		return AnalysisResponse{}, invalid("salesTaxRate must be at least 0 and below 1")
	}

	locationIDs, _, err := s.catalog.ResolveEnabled([]string{request.PurchaseMarketKey})
	if err != nil || len(locationIDs) != 1 {
		return AnalysisResponse{}, invalid("purchaseMarketKey must reference an enabled regular market")
	}
	serverID, err := serverID(request.Server)
	if err != nil {
		return AnalysisResponse{}, err
	}

	purchase, blackMarket, err := s.loadPrices(ctx, serverID, locationIDs[0], request)
	if err != nil {
		return AnalysisResponse{}, err
	}
	history, err := s.loadHistory(ctx, serverID, request, historyDays)
	if err != nil {
		return AnalysisResponse{}, err
	}

	salePrice := blackMarket.UnitPrice
	saleSource := "black-market-buy-order"
	if request.SaleUnitPriceOverride != nil {
		override := *request.SaleUnitPriceOverride
		salePrice = &override
		saleSource = "manual"
	}
	if salePrice == nil {
		saleSource = "missing"
	}

	economics := calculateEconomics(request.Quantity, purchase.UnitPrice, salePrice, saleSource, taxRate, request.TransportCost)
	warnings := make([]string, 0, 4)
	if purchase.UnitPrice == nil {
		warnings = append(warnings, "No existe un precio de venta vigente en el mercado de compra seleccionado.")
	}
	if blackMarket.UnitPrice == nil {
		warnings = append(warnings, "No existe una orden de compra vigente del Black Market en la base central; puedes ingresar un precio esperado manual.")
	}
	if purchase.Freshness == "stale" || blackMarket.Freshness == "stale" {
		warnings = append(warnings, "Uno o más precios tienen más de 24 horas y deben verificarse dentro del juego antes de transportar.")
	}
	if history.BucketCount == 0 {
		warnings = append(warnings, "Todavía no hay historial central del Black Market para este objeto y calidad.")
	}

	return AnalysisResponse{
		RequestedAt:       s.now().UTC(),
		Server:            request.Server,
		ItemIdentifier:    request.ItemIdentifier,
		Quality:           request.Quality,
		PurchaseMarketKey: request.PurchaseMarketKey,
		BlackMarketKey:    "black_market",
		Purchase:          purchase,
		BlackMarket:       blackMarket,
		History:           history,
		Economics:         economics,
		Warnings:          warnings,
	}, nil
}

func (s *Service) loadPrices(ctx context.Context, server int16, purchaseLocation int16, request AnalysisRequest) (PriceSnapshot, PriceSnapshot, error) {
	const query = `
		select location_id, sell_price_min, sell_price_min_at, buy_price_max, buy_price_max_at, updated_at
		from current_market_prices
		where server = $1
		  and location_id = any($2::smallint[])
		  and item_key = $3
		  and quality = $4
	`
	rows, err := s.db.Query(ctx, query, server, []int16{purchaseLocation, blackMarketLocation}, request.ItemIdentifier, request.Quality)
	if err != nil {
		return PriceSnapshot{}, PriceSnapshot{}, fmt.Errorf("query Black Market prices: %w", err)
	}
	defer rows.Close()

	purchase := PriceSnapshot{MarketKey: request.PurchaseMarketKey, Freshness: "missing"}
	blackMarket := PriceSnapshot{MarketKey: "black_market", Freshness: "missing"}
	for rows.Next() {
		var locationID int16
		var sellPrice, buyPrice *int64
		var sellAt, buyAt *time.Time
		var updatedAt time.Time
		if err := rows.Scan(&locationID, &sellPrice, &sellAt, &buyPrice, &buyAt, &updatedAt); err != nil {
			return PriceSnapshot{}, PriceSnapshot{}, fmt.Errorf("scan Black Market prices: %w", err)
		}
		if locationID == purchaseLocation {
			purchase.UnitPrice = positive(sellPrice)
			purchase.PriceDate = sellAt
			purchase.UpdatedAt = &updatedAt
			purchase.Freshness = freshness(s.now(), sellAt)
		}
		if locationID == blackMarketLocation {
			blackMarket.UnitPrice = positive(buyPrice)
			blackMarket.PriceDate = buyAt
			blackMarket.UpdatedAt = &updatedAt
			blackMarket.Freshness = freshness(s.now(), buyAt)
		}
	}
	if err := rows.Err(); err != nil {
		return PriceSnapshot{}, PriceSnapshot{}, fmt.Errorf("iterate Black Market prices: %w", err)
	}
	return purchase, blackMarket, nil
}

func (s *Service) loadHistory(ctx context.Context, server int16, request AnalysisRequest, historyDays int) (HistorySummary, error) {
	from := s.now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -(historyDays - 1))
	const query = `
		select bucket_at, item_count, average_unit_price, observed_at
		from market_history_buckets
		where server = $1
		  and location_id = $2
		  and item_key = $3
		  and quality = $4
		  and bucket_at >= $5
		order by bucket_at
	`
	rows, err := s.db.Query(ctx, query, server, blackMarketLocation, request.ItemIdentifier, request.Quality, from)
	if err != nil {
		return HistorySummary{}, fmt.Errorf("query Black Market history: %w", err)
	}
	defer rows.Close()

	summary := HistorySummary{RangeDays: historyDays, Points: []HistoryPoint{}}
	var weightedTotal int64
	var weightedUnits int64
	for rows.Next() {
		var point HistoryPoint
		var observedAt time.Time
		if err := rows.Scan(&point.Timestamp, &point.ItemCount, &point.AverageUnitPrice, &observedAt); err != nil {
			return HistorySummary{}, fmt.Errorf("scan Black Market history: %w", err)
		}
		summary.Points = append(summary.Points, point)
		summary.BucketCount++
		summary.SoldUnits += point.ItemCount
		if summary.LastObservedAt == nil || observedAt.After(*summary.LastObservedAt) {
			observed := observedAt
			summary.LastObservedAt = &observed
		}
		if point.AverageUnitPrice != nil && *point.AverageUnitPrice > 0 {
			price := *point.AverageUnitPrice
			if summary.LowestAverage == nil || price < *summary.LowestAverage {
				value := price
				summary.LowestAverage = &value
			}
			if summary.HighestAverage == nil || price > *summary.HighestAverage {
				value := price
				summary.HighestAverage = &value
			}
			if point.ItemCount > 0 && price <= math.MaxInt64/point.ItemCount {
				weightedTotal += price * point.ItemCount
				weightedUnits += point.ItemCount
			}
		}
	}
	if err := rows.Err(); err != nil {
		return HistorySummary{}, fmt.Errorf("iterate Black Market history: %w", err)
	}
	if weightedUnits > 0 {
		value := weightedTotal / weightedUnits
		summary.WeightedAverage = &value
	}
	return summary, nil
}

func calculateEconomics(quantity int64, purchaseUnitPrice, saleUnitPrice *int64, saleSource string, taxRate float64, transportCost int64) Economics {
	result := Economics{
		Quantity:          quantity,
		PurchaseUnitPrice: purchaseUnitPrice,
		SaleUnitPrice:     saleUnitPrice,
		SalePriceSource:   saleSource,
		TransportCost:     transportCost,
	}
	if purchaseUnitPrice == nil || saleUnitPrice == nil {
		return result
	}
	purchaseCost, ok := multiply(*purchaseUnitPrice, quantity)
	if !ok {
		return result
	}
	grossRevenue, ok := multiply(*saleUnitPrice, quantity)
	if !ok {
		return result
	}
	tax := int64(math.Round(float64(grossRevenue) * taxRate))
	netRevenue := grossRevenue - tax - transportCost
	profit := netRevenue - purchaseCost
	profitPerUnit := profit / quantity
	margin := 0.0
	if grossRevenue > 0 {
		margin = float64(profit) / float64(grossRevenue) * 100
	}
	roi := 0.0
	if purchaseCost+transportCost > 0 {
		roi = float64(profit) / float64(purchaseCost+transportCost) * 100
	}
	breakEven := int64(math.Ceil(float64(purchaseCost+transportCost) / float64(quantity) / (1 - taxRate)))
	result.Ready = true
	result.PurchaseCost = &purchaseCost
	result.GrossRevenue = &grossRevenue
	result.SalesTax = &tax
	result.NetRevenue = &netRevenue
	result.Profit = &profit
	result.ProfitPerUnit = &profitPerUnit
	result.MarginPercent = &margin
	result.ReturnOnCostPercent = &roi
	result.BreakEvenUnitPrice = &breakEven
	return result
}

func multiply(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || (left > 0 && right > math.MaxInt64/left) {
		return 0, false
	}
	return left * right, true
}

func positive(value *int64) *int64 {
	if value == nil || *value <= 0 {
		return nil
	}
	copy := *value
	return &copy
}

func freshness(now time.Time, at *time.Time) string {
	if at == nil || at.IsZero() {
		return "missing"
	}
	if now.UTC().Sub(at.UTC()) <= freshPriceWindow {
		return "fresh"
	}
	return "stale"
}

func serverID(server domain.Server) (int16, error) {
	switch server {
	case domain.ServerWest:
		return 1, nil
	case domain.ServerEast:
		return 2, nil
	case domain.ServerEurope:
		return 3, nil
	default:
		return 0, fmt.Errorf("invalid server")
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

var _ = pgx.ErrNoRows
