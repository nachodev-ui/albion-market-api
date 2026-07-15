package blackmarket

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/marketcatalog"
)

func normalizeUniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := marketcatalog.NormalizeKey(value)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func normalizeInt16Filter(values, defaults []int16, minimum, maximum int16, field string) ([]int16, error) {
	if len(values) == 0 {
		return append([]int16(nil), defaults...), nil
	}
	result := make([]int16, 0, len(values))
	seen := make(map[int16]struct{}, len(values))
	for _, value := range values {
		if value < minimum || value > maximum {
			return nil, invalid(fmt.Sprintf("%s contains an unsupported value", field))
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil, invalid(fmt.Sprintf("%s cannot be empty", field))
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func normalizeOpportunityCategories(values []string) ([]string, bool, bool, bool, bool, error) {
	if len(values) == 0 {
		values = defaultOpportunityCategories
	}
	categories := normalizeUniqueStrings(values)
	if len(categories) == 0 || len(categories) > 4 {
		return nil, false, false, false, false, invalid("categories must contain between 1 and 4 values")
	}
	var weapon, armor, offhand, accessory bool
	for _, category := range categories {
		switch category {
		case "weapon":
			weapon = true
		case "armor":
			armor = true
		case "offhand":
			offhand = true
		case "accessory":
			accessory = true
		default:
			return nil, false, false, false, false, invalid("categories contains an unsupported value")
		}
	}
	sort.Strings(categories)
	return categories, weapon, armor, offhand, accessory, nil
}

func opportunityTierAndEnchantment(itemIdentifier string) (int16, int16) {
	var tier int16
	if len(itemIdentifier) >= 2 && itemIdentifier[0] == 'T' && itemIdentifier[1] >= '0' && itemIdentifier[1] <= '9' {
		tier = int16(itemIdentifier[1] - '0')
	}
	var enchantment int16
	if index := strings.LastIndexByte(itemIdentifier, '@'); index >= 0 && index == len(itemIdentifier)-2 {
		value := itemIdentifier[index+1]
		if value >= '0' && value <= '4' {
			enchantment = int16(value - '0')
		}
	}
	return tier, enchantment
}

func opportunityCategory(itemIdentifier string) string {
	upper := strings.ToUpper(itemIdentifier)
	separator := strings.IndexByte(upper, '_')
	if separator < 0 || separator+1 >= len(upper) {
		return ""
	}
	body := upper[separator+1:]
	switch {
	case strings.HasPrefix(body, "MAIN_") || strings.HasPrefix(body, "2H_"):
		return "weapon"
	case strings.HasPrefix(body, "HEAD_") || strings.HasPrefix(body, "ARMOR_") || strings.HasPrefix(body, "SHOES_"):
		return "armor"
	case strings.HasPrefix(body, "OFF_"):
		return "offhand"
	case strings.HasPrefix(body, "CAPE_") || strings.HasPrefix(body, "BAG"):
		return "accessory"
	default:
		return ""
	}
}

func ageMinutes(now, observedAt time.Time) int {
	if observedAt.After(now) {
		return 0
	}
	return int(now.Sub(observedAt).Minutes())
}

func buildOpportunityCompetition(now time.Time, row opportunityRow) OpportunityCompetition {
	competition := OpportunityCompetition{}
	if row.caerleonPurchaseUnitPrice == nil || row.caerleonPurchaseQuality == nil || row.caerleonPurchasePriceDate == nil {
		return competition
	}
	age := ageMinutes(now, *row.caerleonPurchasePriceDate)
	competition.Available = true
	competition.PurchaseUnitPrice = row.caerleonPurchaseUnitPrice
	competition.PurchaseQuality = row.caerleonPurchaseQuality
	competition.PurchasePriceDate = row.caerleonPurchasePriceDate
	competition.AgeMinutes = &age
	competition.Profit = row.caerleonProfit
	competition.CanFillProfitably = row.caerleonProfit != nil && *row.caerleonProfit > 0
	return competition
}

func opportunityRisk(
	marketKey string,
	purchaseAge int,
	blackMarketAge int,
	maximumCityAge int,
	maximumBlackMarketAge int,
	blackMarketSellPrice *int64,
	blackMarketBuyPrice int64,
	competition OpportunityCompetition,
) (string, []string) {
	reasons := make([]string, 0, 4)
	riskScore := 0
	if purchaseAge*4 >= maximumCityAge*3 {
		riskScore += 2
		reasons = append(reasons, "El precio de compra está cerca del límite máximo de antigüedad.")
	} else if purchaseAge*2 >= maximumCityAge {
		riskScore++
		reasons = append(reasons, "El precio de compra ya consumió más de la mitad de su ventana de frescura.")
	}
	if blackMarketAge*4 >= maximumBlackMarketAge*3 {
		riskScore += 2
		reasons = append(reasons, "La orden del Black Market está cerca del límite máximo de antigüedad.")
	} else if blackMarketAge*2 >= maximumBlackMarketAge {
		riskScore++
		reasons = append(reasons, "La orden del Black Market ya consumió más de la mitad de su ventana de frescura.")
	}
	if marketKey != "caerleon" && competition.CanFillProfitably {
		riskScore += 2
		reasons = append(reasons, "La misma orden puede completarse rentablemente desde el mercado de Caerleon.")
	}
	if blackMarketSellPrice != nil && *blackMarketSellPrice > 0 && *blackMarketSellPrice <= blackMarketBuyPrice {
		riskScore += 2
		reasons = append(reasons, "Las órdenes observadas del Black Market están cruzadas o desactualizadas.")
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "Ambas capturas están dentro de la ventana fresca y no se detectó competencia inmediata desde Caerleon.")
	}
	switch {
	case riskScore >= 4:
		return "high", reasons
	case riskScore >= 2:
		return "medium", reasons
	default:
		return "low", reasons
	}
}

func opportunityWarnings(
	coverage OpportunityCoverage,
	returned int,
	totalMatching int64,
	request normalizedOpportunitiesRequest,
	now time.Time,
) []string {
	warnings := make([]string, 0, 5)
	if coverage.BlackMarketRows == 0 {
		warnings = append(warnings, "No hay órdenes de compra del Black Market almacenadas para este servidor.")
	}
	if coverage.SourceMarketRows == 0 {
		warnings = append(warnings, "No hay precios de venta almacenados para los mercados de compra seleccionados.")
	}
	if coverage.LatestBlackMarketAt != nil && ageMinutes(now, *coverage.LatestBlackMarketAt) > request.maximumBlackMarketAgeMinutes {
		warnings = append(warnings, "La captura más reciente del Black Market queda fuera del límite de antigüedad seleccionado.")
	}
	if coverage.LatestSourceMarketAt != nil && ageMinutes(now, *coverage.LatestSourceMarketAt) > request.maximumCityAgeMinutes {
		warnings = append(warnings, "La captura más reciente de las ciudades queda fuera del límite de antigüedad seleccionado.")
	}
	if returned == 0 && coverage.BlackMarketRows > 0 && coverage.SourceMarketRows > 0 {
		warnings = append(warnings, "No se encontraron cruces rentables y frescos con los filtros actuales.")
	}
	if int64(request.offset+returned) < totalMatching {
		warnings = append(warnings, "Hay más oportunidades disponibles; avanza a la siguiente página para continuar.")
	}
	return warnings
}

func opportunityID(itemIdentifier, marketKey string, purchaseQuality, blackMarketQuality int16) string {
	return fmt.Sprintf("%s:%s:q%d:bmq%d", itemIdentifier, marketKey, purchaseQuality, blackMarketQuality)
}
