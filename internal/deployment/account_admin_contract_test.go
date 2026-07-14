package deployment

import (
	"strings"
	"testing"
)

func TestAccountAdminMigrationIsAuditedAndAppendOnly(t *testing.T) {
	t.Parallel()
	migration := strings.ToLower(readProjectFile(t, "migrations", "0011_account_admin_audit.sql"))
	for _, expected := range []string{
		"create table if not exists account_admin_audit_events",
		"check (action in ('grant_pro', 'revoke_pro'))",
		"before_state jsonb not null",
		"after_state jsonb not null",
		"create or replace function reject_account_admin_audit_mutation()",
		"before update or delete on account_admin_audit_events",
		"set version = greatest(version, 11)",
	} {
		requireContains(t, migration, expected)
	}
}

func TestAccountAdminBinaryIsBuiltIntoProductionImage(t *testing.T) {
	t.Parallel()
	dockerfile := readProjectFile(t, "Dockerfile")
	for _, expected := range []string{
		"go build -trimpath -buildvcs=false -ldflags='-s -w' \\",
		"-o /out-account-admin ./cmd/account-admin",
		"COPY --from=builder /out-account-admin /usr/local/bin/account-admin",
	} {
		requireContains(t, dockerfile, expected)
	}
}
