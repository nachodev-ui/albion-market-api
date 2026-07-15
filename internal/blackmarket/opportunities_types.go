package blackmarket

import (
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
)

const (
	defaultOpportunityLimit               = 100
	maximumOpportunityLimit               = 500
	maximumOpportunityOffset              = 10_000
	defaultMaximumCityAgeMinutes          = 30
	defaultMaximumBlackMarketAgeMinutes   = 20
	maximumOpportunityAgeMinutes          = 10_080
	maximumOpportunityTransportCost       = int64(1_000_000_000_000)
	maximumOpportunityMinimumProfit       = int64(1_000_000_000_000)
	maximumOpportunityReturnOnCostPercent = 100_000.0
)

var defaultOpportunityMarkets = []string{
	"bridgewatch",
	"martlock",
	"lymhurst",
	"fort_sterling",
	"thetford",
	"caerleon",
}

var defaultOpportunityTiers = []int16{4, 5, 6, 7, 8}
var defaultOpportunityEnchantments = []int16{0, 1, 2, 3, 4}
var defaultOpportunityQualities = []int16{1, 2, 3, 4, 5}
var defaultOpportunityCategories = []string{"weapon", "armor", "offhand", "accessory"}

type OpportunitiesRequest struct {
	Server                       domain.Server `json:"server"`
	PurchaseMarketKeys           []string      `json:"purchaseMarketKeys"`
	Tiers                        []int16       `json:"tiers"`
	Enchantments                 []int16       `json:"enchantments"`
	Qualities                    []int16       `json:"qualities"`
	Categories                   []string      `json:"categories"`
	MinimumProfit                int64         `json:"minimumProfit"`
	MinimumReturnOnCostPercent   float64       `json:"minimumReturnOnCostPercent"`
	MaximumCityAgeMinutes        int           `json:"maximumCityAgeMinutes"`
	MaximumBlackMarketAgeMinutes int           `json:"maximumBlackMarketAgeMinutes"`
	SalesTaxRate                 *float64      `json:"salesTaxRate"`
	TransportCostPerUnit         int64         `json:"transportCostPerUnit"`
	Sort                         string        `json:"sort"`
	Limit                        int           `json:"limit"`
	Offset                       int           `json:"offset"`
}

type OpportunityCompetition struct {
	Available         bool       `json:"available"`
	PurchaseUnitPrice *int64     `json:"purchaseUnitPrice"`
	PurchaseQuality   *int16     `json:"purchaseQuality"`
	PurchasePriceDate *time.Time `json:"purchasePriceDate"`
	AgeMinutes        *int       `json:"ageMinutes"`
	Profit            *int64     `json:"profit"`
	CanFillProfitably bool       `json:"canFillProfitably"`
}

type Opportunity struct {
	ID                         string                 `json:"id"`
	ItemIdentifier             string                 `json:"itemIdentifier"`
	Tier                       int16                  `json:"tier"`
	Enchantment                int16                  `json:"enchantment"`
	Category                   string                 `json:"category"`
	PurchaseMarketKey          string                 `json:"purchaseMarketKey"`
	PurchaseQuality            int16                  `json:"purchaseQuality"`
	PurchaseUnitPrice          int64                  `json:"purchaseUnitPrice"`
	PurchasePriceDate          time.Time              `json:"purchasePriceDate"`
	PurchaseAgeMinutes         int                    `json:"purchaseAgeMinutes"`
	BlackMarketQuality         int16                  `json:"blackMarketQuality"`
	BlackMarketBuyUnitPrice    int64                  `json:"blackMarketBuyUnitPrice"`
	BlackMarketBuyPriceDate    time.Time              `json:"blackMarketBuyPriceDate"`
	BlackMarketAgeMinutes      int                    `json:"blackMarketAgeMinutes"`
	BlackMarketSellUnitPrice   *int64                 `json:"blackMarketSellUnitPrice"`
	BlackMarketSellPriceDate   *time.Time             `json:"blackMarketSellPriceDate"`
	BlackMarketOrderDifference *int64                 `json:"blackMarketOrderDifference"`
	EstimatedSalesTax          int64                  `json:"estimatedSalesTax"`
	TransportCostPerUnit       int64                  `json:"transportCostPerUnit"`
	NetUnitRevenue             int64                  `json:"netUnitRevenue"`
	Profit                     int64                  `json:"profit"`
	MarginPercent              float64                `json:"marginPercent"`
	ReturnOnCostPercent        float64                `json:"returnOnCostPercent"`
	BreakEvenUnitPrice         int64                  `json:"breakEvenUnitPrice"`
	CaerleonCompetition        OpportunityCompetition `json:"caerleonCompetition"`
	Risk                       string                 `json:"risk"`
	RiskReasons                []string               `json:"riskReasons"`
}

type OpportunityCoverage struct {
	BlackMarketRows      int64      `json:"blackMarketRows"`
	SourceMarketRows     int64      `json:"sourceMarketRows"`
	LatestBlackMarketAt  *time.Time `json:"latestBlackMarketAt"`
	LatestSourceMarketAt *time.Time `json:"latestSourceMarketAt"`
	SelectedMarketKeys   []string   `json:"selectedMarketKeys"`
}

type OpportunitiesResponse struct {
	RequestedAt   time.Time           `json:"requestedAt"`
	Server        domain.Server       `json:"server"`
	TotalMatching int64               `json:"totalMatching"`
	Returned      int                 `json:"returned"`
	Limit         int                 `json:"limit"`
	Offset        int                 `json:"offset"`
	Sort          string              `json:"sort"`
	Coverage      OpportunityCoverage `json:"coverage"`
	Data          []Opportunity       `json:"data"`
	Warnings      []string            `json:"warnings"`
}

type normalizedOpportunitiesRequest struct {
	server                       domain.Server
	serverID                     int16
	purchaseMarketKeys           []string
	purchaseLocationIDs          []int16
	marketKeysByLocation         map[int16]string
	tiers                        []int16
	enchantments                 []int16
	qualities                    []int16
	categories                   []string
	minimumProfit                int64
	minimumReturnOnCostPercent   float64
	maximumCityAgeMinutes        int
	maximumBlackMarketAgeMinutes int
	salesTaxRate                 float64
	transportCostPerUnit         int64
	sort                         string
	limit                        int
	offset                       int
	includeWeapon                bool
	includeArmor                 bool
	includeOffhand               bool
	includeAccessory             bool
}

type opportunityRow struct {
	totalMatching             int64
	locationID                int16
	itemIdentifier            string
	purchaseQuality           int16
	purchaseUnitPrice         int64
	purchasePriceDate         time.Time
	blackMarketQuality        int16
	blackMarketBuyUnitPrice   int64
	blackMarketBuyPriceDate   time.Time
	blackMarketSellUnitPrice  *int64
	blackMarketSellPriceDate  *time.Time
	estimatedSalesTax         int64
	netUnitRevenue            int64
	profit                    int64
	marginPercent             float64
	returnOnCostPercent       float64
	breakEvenUnitPrice        int64
	caerleonPurchaseUnitPrice *int64
	caerleonPurchaseQuality   *int16
	caerleonPurchasePriceDate *time.Time
	caerleonProfit            *int64
}
