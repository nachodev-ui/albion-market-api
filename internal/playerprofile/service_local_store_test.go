package playerprofile

import "testing"

func TestResponseFromSnapshotDerivesRecentSummary(t *testing.T) {
	t.Parallel()

	snapshot := Snapshot{
		Profile: Profile{PlayerID: "player"},
		Events: []Event{
			{EventID: 1, Result: "kill", KillFame: 100},
			{EventID: 2, Result: "kill", KillFame: 250},
			{EventID: 3, Result: "death", KillFame: 200},
		},
	}
	response := responseFromSnapshot(snapshot)
	if response.Summary.RecentKills != 2 || response.Summary.RecentDeaths != 1 || response.Summary.RecentFightCount != 3 {
		t.Fatalf("unexpected fight summary: %#v", response.Summary)
	}
	if response.Summary.KillFame != 350 || response.Summary.DeathFame != 200 {
		t.Fatalf("unexpected fame summary: %#v", response.Summary)
	}
	if response.Summary.KDRatio == nil || *response.Summary.KDRatio != 2 {
		t.Fatalf("unexpected K/D: %#v", response.Summary.KDRatio)
	}
	if response.Summary.FameRatio != 1.75 {
		t.Fatalf("unexpected fame ratio: %v", response.Summary.FameRatio)
	}
}

func TestLimitSnapshotEvents(t *testing.T) {
	t.Parallel()

	snapshot := Snapshot{Events: []Event{{EventID: 1}, {EventID: 2}, {EventID: 3}}}
	limited := limitSnapshotEvents(snapshot, 2)
	if len(limited.Events) != 2 || limited.Events[1].EventID != 2 {
		t.Fatalf("unexpected limited snapshot: %#v", limited.Events)
	}
}
