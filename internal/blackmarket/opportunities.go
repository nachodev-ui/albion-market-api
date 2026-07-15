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

	"github.com/nachodev-ui/albion-market-api/internal/domain"
)

func (h *Handler) Opportunities(w http.ResponseWriter, r *http.Request) {
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
	var request OpportunitiesRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	response, err := h.service.Opportunities(r.Context(), request)
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

func (s *Service) Opportunities(ctx context.Context, request OpportunitiesRequest) (OpportunitiesResponse, error) {
	normalized, err := s.normalizeOpportunitiesRequest(request)
	if err != nil {
		return OpportunitiesResponse{}, err
	}

	now := s.now().UTC()
	rows, totalMatching, err := s.loadOpportunities(ctx, normalized, now)
	if err != nil {
		return OpportunitiesResponse{}, err
	}
	coverage, err := s.loadOpportunityCoverage(ctx, normalized)
	if err != nil {
		return OpportunitiesResponse{}, err
	}

	data := make([]Opportunity, 0, len(rows))
	for _, row := range rows {
		marketKey, exists := normalized.marketKeysByLocation[row.locationID]
		if !exists {
			continue
		}
		category := opportunityCategory(row.itemIdentifier)
		if category == "" {
			continue
		}
		tier, enchantment := opportunityTierAndEnchantment(row.itemIdentifier)
		purchaseAge := ageMinutes(now, row.purchasePriceDate)
		blackMarketAge := ageMinutes(now, row.blackMarketBuyPriceDate)
		competition := buildOpportunityCompetition(now, row)
		risk, reasons := opportunityRisk(
			marketKey,
			purchaseAge,
			blackMarketAge,
			normalized.maximumCityAgeMinutes,
			normalized.maximumBlackMarketAgeMinutes,
			row.blackMarketSellUnitPrice,
			row.blackMarketBuyUnitPrice,
			competition,
		)

		var orderDifference *int64
		if row.blackMarketSellUnitPrice != nil && *row.blackMarketSellUnitPrice > 0 {
			value := *row.blackMarketSellUnitPrice - row.blackMarketBuyUnitPrice
			orderDifference = &value
		}
		data = append(data, Opportunity{
			ID:                         opportunityID(row.itemIdentifier, marketKey, row.purchaseQuality, row.blackMarketQuality),
			ItemIdentifier:             row.itemIdentifier,
			Tier:                       tier,
			Enchantment:                enchantment,
			Category:                   category,
			PurchaseMarketKey:          marketKey,
			PurchaseQuality:            row.purchaseQuality,
			PurchaseUnitPrice:          row.purchaseUnitPrice,
			PurchasePriceDate:          row.purchasePriceDate,
			PurchaseAgeMinutes:         purchaseAge,
			BlackMarketQuality:         row.blackMarketQuality,
			BlackMarketBuyUnitPrice:    row.blackMarketBuyUnitPrice,
			BlackMarketBuyPriceDate:    row.blackMarketBuyPriceDate,
			BlackMarketAgeMinutes:      blackMarketAge,
			BlackMarketSellUnitPrice:   row.blackMarketSellUnitPrice,
			BlackMarketSellPriceDate:   row.blackMarketSellPriceDate,
			BlackMarketOrderDifference: orderDifference,
			EstimatedSalesTax:          row.estimatedSalesTax,
			TransportCostPerUnit:       normalized.transportCostPerUnit,
			NetUnitRevenue:             row.netUnitRevenue,
			Profit:                     row.profit,
			MarginPercent:              row.marginPercent,
			ReturnOnCostPercent:        row.returnOnCostPercent,
			BreakEvenUnitPrice:         row.breakEvenUnitPrice,
			CaerleonCompetition:        competition,
			Risk:                       risk,
			RiskReasons:                reasons,
		})
	}

	warnings := opportunityWarnings(coverage, len(data), totalMatching, normalized, now)
	return OpportunitiesResponse{
		RequestedAt:   now,
		Server:        normalized.server,
		TotalMatching: totalMatching,
		Returned:      len(data),
		Limit:         normalized.limit,
		Offset:        normalized.offset,
		Sort:          normalized.sort,
		Coverage:      coverage,
		Data:          data,
		Warnings:      warnings,
	}, nil
}

func (s *Service) normalizeOpportunitiesRequest(request OpportunitiesRequest) (normalizedOpportunitiesRequest, error) {
	if request.Server != domain.ServerWest && request.Server != domain.ServerEast && request.Server != domain.ServerEurope {
		return normalizedOpportunitiesRequest{}, invalid("server must be west, east or europe")
	}
	serverID, err := serverID(request.Server)
	if err != nil {
		return normalizedOpportunitiesRequest{}, err
	}

	marketKeys := request.PurchaseMarketKeys
	if len(marketKeys) == 0 {
		marketKeys = append([]string(nil), defaultOpportunityMarkets...)
	}
	marketKeys = normalizeUniqueStrings(marketKeys)
	if len(marketKeys) == 0 || len(marketKeys) > 7 {
		return normalizedOpportunitiesRequest{}, invalid("purchaseMarketKeys must contain between 1 and 7 markets")
	}
	locationIDs, keysByLocation, err := s.catalog.ResolveEnabled(marketKeys)
	if err != nil {
		return normalizedOpportunitiesRequest{}, invalid(err.Error())
	}
	resolvedKeys := make([]string, 0, len(locationIDs))
	for _, locationID := range locationIDs {
		resolvedKeys = append(resolvedKeys, keysByLocation[locationID])
	}

	tiers, err := normalizeInt16Filter(request.Tiers, defaultOpportunityTiers, 4, 8, "tiers")
	if err != nil {
		return normalizedOpportunitiesRequest{}, err
	}
	enchantments, err := normalizeInt16Filter(request.Enchantments, defaultOpportunityEnchantments, 0, 4, "enchantments")
	if err != nil {
		return normalizedOpportunitiesRequest{}, err
	}
	qualities, err := normalizeInt16Filter(request.Qualities, defaultOpportunityQualities, 1, 5, "qualities")
	if err != nil {
		return normalizedOpportunitiesRequest{}, err
	}
	categories, includeWeapon, includeArmor, includeOffhand, includeAccessory, err := normalizeOpportunityCategories(request.Categories)
	if err != nil {
		return normalizedOpportunitiesRequest{}, err
	}

	if request.MinimumProfit < 0 || request.MinimumProfit > maximumOpportunityMinimumProfit {
		return normalizedOpportunitiesRequest{}, invalid("minimumProfit must be between 0 and 1000000000000")
	}
	if math.IsNaN(request.MinimumReturnOnCostPercent) || math.IsInf(request.MinimumReturnOnCostPercent, 0) || request.MinimumReturnOnCostPercent < 0 || request.MinimumReturnOnCostPercent > maximumOpportunityReturnOnCostPercent {
		return normalizedOpportunitiesRequest{}, invalid("minimumReturnOnCostPercent must be between 0 and 100000")
	}

	maximumCityAge := request.MaximumCityAgeMinutes
	if maximumCityAge == 0 {
		maximumCityAge = defaultMaximumCityAgeMinutes
	}
	maximumBlackMarketAge := request.MaximumBlackMarketAgeMinutes
	if maximumBlackMarketAge == 0 {
		maximumBlackMarketAge = defaultMaximumBlackMarketAgeMinutes
	}
	if maximumCityAge < 1 || maximumCityAge > maximumOpportunityAgeMinutes {
		return normalizedOpportunitiesRequest{}, invalid("maximumCityAgeMinutes must be between 1 and 10080")
	}
	if maximumBlackMarketAge < 1 || maximumBlackMarketAge > maximumOpportunityAgeMinutes {
		return normalizedOpportunitiesRequest{}, invalid("maximumBlackMarketAgeMinutes must be between 1 and 10080")
	}

	taxRate := defaultSalesTaxRate
	if request.SalesTaxRate != nil {
		taxRate = *request.SalesTaxRate
	}
	if math.IsNaN(taxRate) || math.IsInf(taxRate, 0) || taxRate < 0 || taxRate >= 1 {
		return normalizedOpportunitiesRequest{}, invalid("salesTaxRate must be at least 0 and below 1")
	}
	if request.TransportCostPerUnit < 0 || request.TransportCostPerUnit > maximumOpportunityTransportCost {
		return normalizedOpportunitiesRequest{}, invalid("transportCostPerUnit must be between 0 and 1000000000000")
	}

	sortKey := strings.ToLower(strings.TrimSpace(request.Sort))
	if sortKey == "" {
		sortKey = "profit"
	}
	if sortKey != "profit" && sortKey != "roi" && sortKey != "freshness" {
		return normalizedOpportunitiesRequest{}, invalid("sort must be profit, roi or freshness")
	}
	limit := request.Limit
	if limit == 0 {
		limit = defaultOpportunityLimit
	}
	if limit < 1 || limit > maximumOpportunityLimit {
		return normalizedOpportunitiesRequest{}, invalid("limit must be between 1 and 500")
	}
	if request.Offset < 0 || request.Offset > maximumOpportunityOffset {
		return normalizedOpportunitiesRequest{}, invalid("offset must be between 0 and 10000")
	}

	return normalizedOpportunitiesRequest{
		server:                       request.Server,
		serverID:                     serverID,
		purchaseMarketKeys:           resolvedKeys,
		purchaseLocationIDs:          locationIDs,
		marketKeysByLocation:         keysByLocation,
		tiers:                        tiers,
		enchantments:                 enchantments,
		qualities:                    qualities,
		categories:                   categories,
		minimumProfit:                request.MinimumProfit,
		minimumReturnOnCostPercent:   request.MinimumReturnOnCostPercent,
		maximumCityAgeMinutes:        maximumCityAge,
		maximumBlackMarketAgeMinutes: maximumBlackMarketAge,
		salesTaxRate:                 taxRate,
		transportCostPerUnit:         request.TransportCostPerUnit,
		sort:                         sortKey,
		limit:                        limit,
		offset:                       request.Offset,
		includeWeapon:                includeWeapon,
		includeArmor:                 includeArmor,
		includeOffhand:               includeOffhand,
		includeAccessory:             includeAccessory,
	}, nil
}

func (s *Service) loadOpportunities(ctx context.Context, request normalizedOpportunitiesRequest, now time.Time) ([]opportunityRow, int64, error) {
	orderClause := "profit desc, return_on_cost_percent desc, black_market_buy_price_date desc"
	switch request.sort {
	case "roi":
		orderClause = "return_on_cost_percent desc, profit desc, black_market_buy_price_date desc"
	case "freshness":
		orderClause = "greatest(purchase_price_date, black_market_buy_price_date) desc, profit desc"
	}

	query := fmt.Sprintf(`
		with black_market as (
			select item_key, quality, buy_price_max, buy_price_max_at, sell_price_min, sell_price_min_at
			from current_market_prices
			where server = $1
			  and location_id = $2
			  and buy_price_max > 0
			  and buy_price_max_at is not null
			  and buy_price_max_at >= $3
			  and case when item_key ~ '^T[4-8]_' then substring(item_key from 2 for 1)::smallint else 0 end = any($5::smallint[])
			  and case when item_key ~ '@[0-4]$' then right(item_key, 1)::smallint else 0 end = any($6::smallint[])
			  and (($8 and item_key ~ '^T[4-8]_(MAIN|2H)_') or ($9 and item_key ~ '^T[4-8]_(HEAD|ARMOR|SHOES)_') or ($10 and item_key ~ '^T[4-8]_OFF_') or ($11 and item_key ~ '^T[4-8]_(CAPE|BAG)_'))
		),
		source_market as (
			select location_id, item_key, quality, sell_price_min, sell_price_min_at
			from current_market_prices
			where server = $1
			  and location_id = any($4::smallint[])
			  and sell_price_min > 0
			  and sell_price_min_at is not null
			  and sell_price_min_at >= $12
			  and case when item_key ~ '^T[4-8]_' then substring(item_key from 2 for 1)::smallint else 0 end = any($5::smallint[])
			  and case when item_key ~ '@[0-4]$' then right(item_key, 1)::smallint else 0 end = any($6::smallint[])
			  and quality = any($7::smallint[])
			  and (($8 and item_key ~ '^T[4-8]_(MAIN|2H)_') or ($9 and item_key ~ '^T[4-8]_(HEAD|ARMOR|SHOES)_') or ($10 and item_key ~ '^T[4-8]_OFF_') or ($11 and item_key ~ '^T[4-8]_(CAPE|BAG)_'))
		),
		candidate as (
			select
				s.location_id,
				s.item_key,
				s.quality as purchase_quality,
				s.sell_price_min as purchase_unit_price,
				s.sell_price_min_at as purchase_price_date,
				bm.quality as black_market_quality,
				bm.buy_price_max as black_market_buy_unit_price,
				bm.buy_price_max_at as black_market_buy_price_date,
				bm.sell_price_min as black_market_sell_unit_price,
				bm.sell_price_min_at as black_market_sell_price_date,
				round(bm.buy_price_max::double precision * $13::double precision)::bigint as estimated_sales_tax,
				bm.buy_price_max - round(bm.buy_price_max::double precision * $13::double precision)::bigint as net_unit_revenue,
				bm.buy_price_max - round(bm.buy_price_max::double precision * $13::double precision)::bigint - s.sell_price_min - $14::bigint as profit,
				case when bm.buy_price_max > 0 then ((bm.buy_price_max - round(bm.buy_price_max::double precision * $13::double precision)::bigint - s.sell_price_min - $14::bigint)::double precision / bm.buy_price_max::double precision) * 100 else 0 end as margin_percent,
				case when s.sell_price_min + $14::bigint > 0 then ((bm.buy_price_max - round(bm.buy_price_max::double precision * $13::double precision)::bigint - s.sell_price_min - $14::bigint)::double precision / (s.sell_price_min + $14::bigint)::double precision) * 100 else 0 end as return_on_cost_percent,
				ceil((s.sell_price_min + $14::bigint)::double precision / (1 - $13::double precision))::bigint as break_even_unit_price,
				ca.sell_price_min as caerleon_purchase_unit_price,
				ca.quality as caerleon_purchase_quality,
				ca.sell_price_min_at as caerleon_purchase_price_date,
				case when ca.sell_price_min is not null then bm.buy_price_max - round(bm.buy_price_max::double precision * $13::double precision)::bigint - ca.sell_price_min - $14::bigint else null end as caerleon_profit
			from source_market s
			join black_market bm on bm.item_key = s.item_key and bm.quality <= s.quality
			left join lateral (
				select quality, sell_price_min, sell_price_min_at
				from current_market_prices ca
				where ca.server = $1
				  and ca.location_id = $15
				  and ca.item_key = s.item_key
				  and ca.quality >= bm.quality
				  and ca.sell_price_min > 0
				  and ca.sell_price_min_at is not null
				  and ca.sell_price_min_at >= $16
				order by ca.sell_price_min asc, ca.quality asc
				limit 1
			) ca on true
		),
		eligible as (
			select * from candidate
			where profit >= $17 and return_on_cost_percent >= $18
		),
		ranked as (
			select eligible.*, row_number() over (partition by location_id, item_key order by profit desc, purchase_quality asc, black_market_quality desc) as item_rank
			from eligible
		),
		filtered as (
			select * from ranked where item_rank = 1
		)
		select
			count(*) over() as total_matching,
			location_id,
			item_key,
			purchase_quality,
			purchase_unit_price,
			purchase_price_date,
			black_market_quality,
			black_market_buy_unit_price,
			black_market_buy_price_date,
			black_market_sell_unit_price,
			black_market_sell_price_date,
			estimated_sales_tax,
			net_unit_revenue,
			profit,
			margin_percent::double precision,
			return_on_cost_percent::double precision,
			break_even_unit_price,
			caerleon_purchase_unit_price,
			caerleon_purchase_quality,
			caerleon_purchase_price_date,
			caerleon_profit
		from filtered
		order by %s
		limit $19 offset $20
	`, orderClause)

	blackMarketCutoff := now.Add(-time.Duration(request.maximumBlackMarketAgeMinutes) * time.Minute)
	cityCutoff := now.Add(-time.Duration(request.maximumCityAgeMinutes) * time.Minute)
	caerleonCutoff := now.Add(-7 * 24 * time.Hour)
	rows, err := s.db.Query(
		ctx,
		query,
		request.serverID,
		blackMarketLocation,
		blackMarketCutoff,
		request.purchaseLocationIDs,
		request.tiers,
		request.enchantments,
		request.qualities,
		request.includeWeapon,
		request.includeArmor,
		request.includeOffhand,
		request.includeAccessory,
		cityCutoff,
		request.salesTaxRate,
		request.transportCostPerUnit,
		int16(3005),
		caerleonCutoff,
		request.minimumProfit,
		request.minimumReturnOnCostPercent,
		request.limit,
		request.offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("query Black Market opportunities: %w", err)
	}
	defer rows.Close()

	result := make([]opportunityRow, 0, request.limit)
	var totalMatching int64
	for rows.Next() {
		var row opportunityRow
		if err := rows.Scan(
			&row.totalMatching,
			&row.locationID,
			&row.itemIdentifier,
			&row.purchaseQuality,
			&row.purchaseUnitPrice,
			&row.purchasePriceDate,
			&row.blackMarketQuality,
			&row.blackMarketBuyUnitPrice,
			&row.blackMarketBuyPriceDate,
			&row.blackMarketSellUnitPrice,
			&row.blackMarketSellPriceDate,
			&row.estimatedSalesTax,
			&row.netUnitRevenue,
			&row.profit,
			&row.marginPercent,
			&row.returnOnCostPercent,
			&row.breakEvenUnitPrice,
			&row.caerleonPurchaseUnitPrice,
			&row.caerleonPurchaseQuality,
			&row.caerleonPurchasePriceDate,
			&row.caerleonProfit,
		); err != nil {
			return nil, 0, fmt.Errorf("scan Black Market opportunity: %w", err)
		}
		totalMatching = row.totalMatching
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate Black Market opportunities: %w", err)
	}
	return result, totalMatching, nil
}

func (s *Service) loadOpportunityCoverage(ctx context.Context, request normalizedOpportunitiesRequest) (OpportunityCoverage, error) {
	const query = `
		select
			count(*) filter (where location_id = $2 and buy_price_max > 0 and buy_price_max_at is not null),
			count(*) filter (where location_id = any($3::smallint[]) and sell_price_min > 0 and sell_price_min_at is not null),
			max(buy_price_max_at) filter (where location_id = $2 and buy_price_max > 0),
			max(sell_price_min_at) filter (where location_id = any($3::smallint[]) and sell_price_min > 0)
		from current_market_prices
		where server = $1 and (location_id = $2 or location_id = any($3::smallint[]))
	`
	var coverage OpportunityCoverage
	if err := s.db.QueryRow(ctx, query, request.serverID, blackMarketLocation, request.purchaseLocationIDs).Scan(
		&coverage.BlackMarketRows,
		&coverage.SourceMarketRows,
		&coverage.LatestBlackMarketAt,
		&coverage.LatestSourceMarketAt,
	); err != nil {
		return OpportunityCoverage{}, fmt.Errorf("query Black Market coverage: %w", err)
	}
	coverage.SelectedMarketKeys = append([]string(nil), request.purchaseMarketKeys...)
	return coverage, nil
}
