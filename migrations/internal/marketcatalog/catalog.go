package marketcatalog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
)

const SchemaVersion = 1

type record struct {
	definition domain.MarketDefinition
	locationID *int16
}

type Catalog struct {
	records    []record
	byKey      map[string]record
	byLocation map[int16]string
}

func NewDefault() *Catalog {
	location := func(value int16) *int16 { return &value }

	records := []record{
		{
			definition: domain.MarketDefinition{
				Key:     "bridgewatch",
				Name:    "Bridgewatch",
				Type:    domain.MarketTypeRegular,
				Enabled: true,
			},
			locationID: location(2004),
		},
		{
			definition: domain.MarketDefinition{
				Key:     "martlock",
				Name:    "Martlock",
				Type:    domain.MarketTypeRegular,
				Enabled: true,
			},
			locationID: location(3008),
		},
		{
			definition: domain.MarketDefinition{
				Key:     "lymhurst",
				Name:    "Lymhurst",
				Type:    domain.MarketTypeRegular,
				Enabled: true,
			},
			locationID: location(1002),
		},
		{
			definition: domain.MarketDefinition{
				Key:     "fort_sterling",
				Name:    "Fort Sterling",
				Type:    domain.MarketTypeRegular,
				Enabled: true,
			},
			locationID: location(4002),
		},
		{
			definition: domain.MarketDefinition{
				Key:     "thetford",
				Name:    "Thetford",
				Type:    domain.MarketTypeRegular,
				Enabled: true,
			},
			locationID: location(7),
		},
		{
			definition: domain.MarketDefinition{
				Key:     "caerleon",
				Name:    "Caerleon",
				Type:    domain.MarketTypeRegular,
				Enabled: true,
			},
			locationID: location(3005),
		},
		{
			definition: domain.MarketDefinition{
				Key:     "brecilien",
				Name:    "Brecilien",
				Type:    domain.MarketTypeRegular,
				Enabled: true,
			},
			locationID: location(5003),
		},
		{
			definition: domain.MarketDefinition{
				Key:     "black_market",
				Name:    "Black Market",
				Type:    domain.MarketTypeBlackMarket,
				Enabled: false,
			},
			locationID: nil,
		},
	}

	catalog := &Catalog{
		records:    records,
		byKey:      make(map[string]record, len(records)),
		byLocation: make(map[int16]string, len(records)),
	}
	for _, item := range records {
		catalog.byKey[item.definition.Key] = item
		if item.locationID != nil {
			catalog.byLocation[*item.locationID] = item.definition.Key
		}
	}
	return catalog
}

func (c *Catalog) List(includeDisabled bool) []domain.MarketDefinition {
	if c == nil {
		return nil
	}

	markets := make([]domain.MarketDefinition, 0, len(c.records))
	for _, item := range c.records {
		if !includeDisabled && !item.definition.Enabled {
			continue
		}
		markets = append(markets, item.definition)
	}
	sort.SliceStable(markets, func(i, j int) bool {
		return markets[i].Name < markets[j].Name
	})
	return markets
}

// ResolveEnabled converts public market keys into the internal Albion market
// location IDs used by PostgreSQL. Callers outside the service layer never
// need to know these IDs.
func (c *Catalog) ResolveEnabled(keys []string) ([]int16, map[int16]string, error) {
	if c == nil {
		return nil, nil, fmt.Errorf("market catalog is not configured")
	}

	locationIDs := make([]int16, 0, len(keys))
	keysByLocation := make(map[int16]string, len(keys))
	seen := make(map[string]struct{}, len(keys))

	for _, rawKey := range keys {
		key := NormalizeKey(rawKey)
		if key == "" {
			return nil, nil, fmt.Errorf("marketKey cannot be empty")
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		item, exists := c.byKey[key]
		if !exists {
			return nil, nil, fmt.Errorf("unsupported marketKey %q", key)
		}
		if !item.definition.Enabled || item.locationID == nil {
			return nil, nil, fmt.Errorf("marketKey %q is disabled", key)
		}

		locationIDs = append(locationIDs, *item.locationID)
		keysByLocation[*item.locationID] = key
	}

	return locationIDs, keysByLocation, nil
}

func (c *Catalog) KeyForLocation(locationID int16) (string, bool) {
	if c == nil {
		return "", false
	}
	key, ok := c.byLocation[locationID]
	return key, ok
}

func NormalizeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
