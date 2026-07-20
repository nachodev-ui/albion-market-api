package saveddata

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestValidatePresetInput(t *testing.T) {
	t.Parallel()

	valid := PresetInput{
		Name:    "Crafteo Bridgewatch",
		Payload: json.RawMessage(`{"cityId":"bridgewatch","useFocus":true}`),
	}
	if err := validatePresetInput(valid); err != nil {
		t.Fatalf("validatePresetInput(valid) error = %v", err)
	}

	for name, input := range map[string]PresetInput{
		"empty name":    {Name: " ", Payload: valid.Payload},
		"array payload": {Name: valid.Name, Payload: json.RawMessage(`[]`)},
		"invalid json":  {Name: valid.Name, Payload: json.RawMessage(`{"cityId"`)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := validatePresetInput(input); !errors.Is(err, ErrInvalid) {
				t.Fatalf("validatePresetInput() error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestValidateCalculationInput(t *testing.T) {
	t.Parallel()

	name := "T8 Espada"
	valid := CalculationInput{
		Name:     &name,
		Kind:     "craft",
		Snapshot: json.RawMessage(`{"item":{"id":"T8_MAIN_SWORD"},"result":{"profit":1000}}`),
	}
	if err := validateCalculationInput(valid); err != nil {
		t.Fatalf("validateCalculationInput(valid) error = %v", err)
	}

	invalidKind := valid
	invalidKind.Kind = "Craft Calculation"
	if err := validateCalculationInput(invalidKind); !errors.Is(err, ErrInvalid) {
		t.Fatalf("validateCalculationInput(invalid kind) error = %v, want ErrInvalid", err)
	}

	invalidSnapshot := valid
	invalidSnapshot.Snapshot = json.RawMessage(`"summary"`)
	if err := validateCalculationInput(invalidSnapshot); !errors.Is(err, ErrInvalid) {
		t.Fatalf("validateCalculationInput(invalid snapshot) error = %v, want ErrInvalid", err)
	}
}

func TestEntitlementLimit(t *testing.T) {
	t.Parallel()

	entitlements := map[string]any{
		PresetLimitEntitlement:      float64(100),
		CalculationLimitEntitlement: json.Number("250"),
	}
	if got := entitlementLimit(entitlements, PresetLimitEntitlement, 3); got != 100 {
		t.Fatalf("preset limit = %d, want 100", got)
	}
	if got := entitlementLimit(entitlements, CalculationLimitEntitlement, 20); got != 250 {
		t.Fatalf("calculation limit = %d, want 250", got)
	}
	if got := entitlementLimit(entitlements, "missing", 7); got != 7 {
		t.Fatalf("fallback limit = %d, want 7", got)
	}
}
