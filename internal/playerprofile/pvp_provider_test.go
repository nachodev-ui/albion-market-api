package playerprofile

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNormalizePvPEventPreservesBothLoadouts(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 8, 9, 22, 0, 0, 0, time.UTC)
	raw := rawEvent{
		EventID:              991,
		TimeStamp:            occurredAt,
		TotalVictimKillFame:  12345,
		NumberOfParticipants: 3,
		GroupMemberCount:     2,
		Killer: rawPlayer{
			ID:               "killer-id",
			Name:             "Killer",
			GuildName:        "Guild A",
			AverageItemPower: 1450.5,
			Equipment: rawEquipment{
				MainHand: &rawEquipmentItem{Type: "T7_MAIN_SWORD"},
				Armor:    &rawEquipmentItem{Type: "T7_ARMOR_PLATE_SET1"},
			},
		},
		Victim: rawPlayer{
			ID:               "victim-id",
			Name:             "Victim",
			GuildName:        "Guild B",
			AverageItemPower: 1398.25,
			Equipment: rawEquipment{
				MainHand: &rawEquipmentItem{Type: "T6_MAIN_AXE"},
			},
		},
	}

	event, ok := normalizePvPEvent(raw, ServerAmericas, "gameinfo")
	if !ok {
		t.Fatal("expected valid event")
	}
	if event.EventID != raw.EventID || event.KillerID != "killer-id" || event.VictimID != "victim-id" {
		t.Fatalf("unexpected normalized identities: %#v", event)
	}
	if event.KillerWeaponType == nil || *event.KillerWeaponType != "T7_MAIN_SWORD" {
		t.Fatalf("unexpected killer weapon: %#v", event.KillerWeaponType)
	}
	if event.VictimWeaponType == nil || *event.VictimWeaponType != "T6_MAIN_AXE" {
		t.Fatalf("unexpected victim weapon: %#v", event.VictimWeaponType)
	}
	if event.TotalVictimKillFame != 12345 || event.Source != "gameinfo" {
		t.Fatalf("unexpected fame/source: %#v", event)
	}
}

func TestDecodeMurderLedgerEventsAcceptsArrayAndWrapper(t *testing.T) {
	t.Parallel()

	raw := rawEvent{
		EventID:   42,
		TimeStamp: time.Date(2026, 8, 9, 22, 0, 0, 0, time.UTC),
		Killer:    rawPlayer{ID: "killer", Name: "Killer"},
		Victim:    rawPlayer{ID: "victim", Name: "Victim"},
	}
	arrayPayload, err := json.Marshal([]rawEvent{raw})
	if err != nil {
		t.Fatal(err)
	}
	arrayEvents, err := decodeMurderLedgerEvents(arrayPayload)
	if err != nil || len(arrayEvents) != 1 || arrayEvents[0].EventID != 42 {
		t.Fatalf("array decode failed: events=%#v err=%v", arrayEvents, err)
	}

	wrapperPayload, err := json.Marshal(map[string]any{"events": []rawEvent{raw}})
	if err != nil {
		t.Fatal(err)
	}
	wrappedEvents, err := decodeMurderLedgerEvents(wrapperPayload)
	if err != nil || len(wrappedEvents) != 1 || wrappedEvents[0].EventID != 42 {
		t.Fatalf("wrapper decode failed: events=%#v err=%v", wrappedEvents, err)
	}
}

func TestNormalizePvPEventRejectsIncompleteEvent(t *testing.T) {
	t.Parallel()

	if _, ok := normalizePvPEvent(rawEvent{EventID: 1}, ServerAmericas, "gameinfo"); ok {
		t.Fatal("expected incomplete event to be rejected")
	}
}
