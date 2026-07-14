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
			_, _ = w.Write([]byte(`{"players":[{"Id":"p1","Name":"Hero","GuildName":"Guild","KillFame":100,"DeathFame":25,"FameRatio":4}]}`))
		case "/players/p1":
			_, _ = w.Write([]byte(`{"Id":"p1","Name":"Hero","GuildName":"Guild","KillFame":100,"DeathFame":25,"FameRatio":4}`))
		case "/players/p1/kills":
			_, _ = w.Write([]byte(`[{"EventId":2,"TimeStamp":"2026-07-14T00:00:00Z","Killer":{"Id":"p1","Name":"Hero","AverageItemPower":1200,"Equipment":{"MainHand":{"Type":"T6_MAIN_SWORD"}}},"Victim":{"Id":"p2","Name":"Enemy"},"TotalVictimKillFame":500,"numberOfParticipants":1,"groupMemberCount":1}]`))
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
	player, err := provider.Player(context.Background(), ServerAmericas, "p1")
	if err != nil || player.PlayerName != "Hero" {
		t.Fatalf("player=%+v err=%v", player, err)
	}
	events, err := provider.Events(context.Background(), ServerAmericas, "p1", 20)
	if err != nil || len(events) != 1 || events[0].Result != "kill" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}
