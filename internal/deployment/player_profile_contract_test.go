package deployment

import (
	"strings"
	"testing"
)

func TestAlbionPlayerProfileMigrationContract(t *testing.T) {
	t.Parallel()
	migration := strings.ToLower(readProjectFile(t, "migrations", "0013_albion_player_profiles.sql"))
	for _, expected := range []string{
		"create table if not exists albion_player_profiles",
		"references app_users(id) on delete cascade",
		"constraint albion_player_profiles_user_unique unique (user_id)",
		"create table if not exists albion_player_events",
		"references albion_player_profiles(id) on delete cascade",
		"constraint albion_player_events_unique unique (profile_id, event_id, result)",
		"set version = greatest(version, 13)",
	} {
		requireContains(t, migration, expected)
	}
}

func TestAlbionPlayerProfileRoutesAreExplicit(t *testing.T) {
	t.Parallel()
	router := readProjectFile(t, "internal", "server", "router.go")
	for _, expected := range []string{
		`/api/v1/albion/players/search`,
		`/api/v1/me/albion-profile`,
		`/api/v1/me/albion-profile/refresh`,
	} {
		requireContains(t, router, expected)
	}
}
