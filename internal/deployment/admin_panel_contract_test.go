package deployment

import (
	"strings"
	"testing"
)

func TestSecureAdminPanelMigrationContract(t *testing.T) {
	t.Parallel()
	migration := strings.ToLower(readProjectFile(t, "migrations", "0012_secure_admin_panel.sql"))
	for _, expected := range []string{
		"create table if not exists app_admins",
		"references app_users(id) on delete restrict",
		"constraint app_admins_user_unique unique (user_id)",
		"check (char_length(created_by) between 3 and 200)",
		"check (char_length(reason) between 3 and 500)",
		"set version = greatest(version, 12)",
	} {
		requireContains(t, migration, expected)
	}
}
