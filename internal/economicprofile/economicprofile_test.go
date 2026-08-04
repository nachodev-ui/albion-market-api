package economicprofile

import (
	"errors"
	"testing"
)

func validInput() Input {
	return Input{
		Server:            "americas",
		PremiumActive:     true,
		DailyFocusBalance: 10000,
		HomeCity:          "bridgewatch",
		GuildHasIsland:    true,
		SalesTaxRate:      4,
		TransportCost:     25000,
		Specializations: []Specialization{
			{
				BranchKey:           "bags",
				BranchName:          "Bolsas",
				Level:               75,
				FocusCostEfficiency: 6300,
			},
		},
	}
}

func TestNormalizeInput(t *testing.T) {
	input := validInput()
	input.Server = " Americas "
	input.HomeCity = " Bridgewatch "
	input.Specializations[0].BranchKey = " Bags "

	normalized, err := normalizeInput(input)
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if normalized.Server != "americas" || normalized.HomeCity != "bridgewatch" {
		t.Fatalf("unexpected normalized location: %#v", normalized)
	}
	if normalized.Specializations[0].BranchKey != "bags" {
		t.Fatalf("unexpected normalized branch: %#v", normalized.Specializations[0])
	}
}

func TestNormalizeInputRejectsInvalidRangesAndDuplicates(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Input)
	}{
		{name: "server", mutate: func(input *Input) { input.Server = "invalid" }},
		{name: "city", mutate: func(input *Input) { input.HomeCity = "invalid" }},
		{name: "focus", mutate: func(input *Input) { input.DailyFocusBalance = -1 }},
		{name: "tax", mutate: func(input *Input) { input.SalesTaxRate = 101 }},
		{name: "transport", mutate: func(input *Input) { input.TransportCost = -1 }},
		{name: "level", mutate: func(input *Input) { input.Specializations[0].Level = 101 }},
		{name: "efficiency", mutate: func(input *Input) { input.Specializations[0].FocusCostEfficiency = -1 }},
		{name: "duplicate", mutate: func(input *Input) {
			input.Specializations = append(input.Specializations, input.Specializations[0])
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validInput()
			test.mutate(&input)
			_, err := normalizeInput(input)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("normalizeInput() error = %v, want ErrInvalid", err)
			}
		})
	}
}
