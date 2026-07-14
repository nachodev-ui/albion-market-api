package playerprofile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGameInfoProviderSearchAndEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			_, _ = w.Write([]byte(`{"players":[{"Id":"p1","Name":"Hero","GuildName":"Guild","Avatar":"AVATAR_01","AvatarRing":"RING_01","KillFame":100,"DeathFame":25,"FameRatio":4}]}`))
		case "/players/p1":
			_, _ = w.Write([]byte(`{"Id":"p1","Name":"Hero","GuildName":"Guild","Avatar":"AVATAR_01","AvatarRing":"RING_01","KillFame":100,"DeathFame":25,"FameRatio":4}`))
		case "/players/p1/kills":
			_, _ = w.Write([]byte(`[{"EventId":2,"TimeStamp":"2026-07-14T00:00:00Z","Killer":{"Id":"p1","Name":"Hero","AverageItemPower":1200,"Equipment":{"MainHand":{"Type":"T6_MAIN_SWORD"},"Head":{"Type":"T6_HEAD_PLATE_SET1"},"Armor":{"Type":"T6_ARMOR_PLATE_SET1"},"Shoes":{"Type":"T6_SHOES_PLATE_SET1"},"Food":{"Type":"T7_MEAL_OMELETTE"}}},"Victim":{"Id":"p2","Name":"Enemy","AverageItemPower":1100,"Equipment":{"MainHand":{"Type":"T5_MAIN_AXE"},"Cape":{"Type":"T5_CAPE"},"Potion":{"Type":"T6_POTION_HEAL"}}},"TotalVictimKillFame":500,"numberOfParticipants":1,"groupMemberCount":1}]`))
		case "/players/p1/deaths":
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	original := serverBaseURLs[ServerAmericas]
	serverBaseURLs[ServerAmericas] = server.URL
	defer func() { serverBaseURLs[ServerAmericas] = original }()

	provider, err := NewGameInfoProvider(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	results, err := provider.Search(context.Background(), ServerAmericas, "Hero")
	if err != nil || len(results) != 1 || results[0].PlayerID != "p1" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	if results[0].Avatar == nil || *results[0].Avatar != "AVATAR_01" {
		t.Fatalf("avatar=%v", results[0].Avatar)
	}
	player, err := provider.Player(context.Background(), ServerAmericas, "p1")
	if err != nil || player.PlayerName != "Hero" {
		t.Fatalf("player=%+v err=%v", player, err)
	}
	events, err := provider.Events(context.Background(), ServerAmericas, "p1", 20)
	if err != nil || len(events) != 1 || events[0].Result != "kill" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	event := events[0]
	if event.PlayerEquipment.MainHand == nil || *event.PlayerEquipment.MainHand != "T6_MAIN_SWORD" {
		t.Fatalf("player equipment=%+v", event.PlayerEquipment)
	}
	if event.PlayerEquipment.Food == nil || *event.PlayerEquipment.Food != "T7_MEAL_OMELETTE" {
		t.Fatalf("player food=%+v", event.PlayerEquipment.Food)
	}
	if event.OpponentEquipment.MainHand == nil || *event.OpponentEquipment.MainHand != "T5_MAIN_AXE" {
		t.Fatalf("opponent equipment=%+v", event.OpponentEquipment)
	}
	if event.WeaponType == nil || *event.WeaponType != "T6_MAIN_SWORD" {
		t.Fatalf("weapon type=%v", event.WeaponType)
	}
}
