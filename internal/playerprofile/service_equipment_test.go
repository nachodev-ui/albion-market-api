package playerprofile

import (
	"testing"
	"time"
)

func stringPointer(value string) *string { return &value }

func TestSnapshotNeedsEquipmentRefresh(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		snapshot Snapshot
		want     bool
	}{
		{
			name: "legacy weapon only cache",
			snapshot: Snapshot{Events: []Event{
				{
					WeaponType:      stringPointer("T6_MAIN_SWORD"),
					PlayerEquipment: Equipment{MainHand: stringPointer("T6_MAIN_SWORD")},
				},
				{
					WeaponType:      stringPointer("T6_MAIN_SWORD"),
					PlayerEquipment: Equipment{MainHand: stringPointer("T6_MAIN_SWORD")},
				},
			}},
			want: true,
		},
		{
			name: "enriched player equipment",
			snapshot: Snapshot{Events: []Event{
				{
					PlayerEquipment: Equipment{
						MainHand: stringPointer("T6_MAIN_SWORD"),
						Armor:    stringPointer("T6_ARMOR_PLATE_SET1"),
					},
				},
			}},
			want: false,
		},
		{
			name: "enriched opponent equipment",
			snapshot: Snapshot{Events: []Event{
				{
					PlayerEquipment:   Equipment{MainHand: stringPointer("T6_MAIN_SWORD")},
					OpponentEquipment: Equipment{MainHand: stringPointer("T6_MAIN_AXE")},
				},
			}},
			want: false,
		},
		{
			name:     "no events",
			snapshot: Snapshot{},
			want:     false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := snapshotNeedsEquipmentRefresh(test.snapshot); got != test.want {
				t.Fatalf("snapshotNeedsEquipmentRefresh() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRefreshWindowElapsed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 14, 20, 0, 0, 0, time.UTC)
	cooldown := 5 * time.Minute

	if !refreshWindowElapsed(Profile{}, now, cooldown) {
		t.Fatal("profile without a refresh attempt should be eligible")
	}

	recent := now.Add(-2 * time.Minute)
	if refreshWindowElapsed(Profile{LastRefreshAttempt: &recent}, now, cooldown) {
		t.Fatal("profile inside cooldown should not be eligible")
	}

	old := now.Add(-10 * time.Minute)
	if !refreshWindowElapsed(Profile{LastRefreshAttempt: &old}, now, cooldown) {
		t.Fatal("profile outside cooldown should be eligible")
	}
}
