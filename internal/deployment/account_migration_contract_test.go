package deployment

import (
	"strings"
	"testing"
)

func TestAccountIntegrityMigrationIsRepeatable(t *testing.T) {
	migration := strings.ToLower(readProjectFile(t, "migrations", "0008_account_integrity_constraints.sql"))
	constraints := []string{
		"app_users_auth_subject_length",
		"app_users_display_name_length",
		"app_users_email_length",
		"billing_webhook_events_error_length",
		"billing_webhook_events_event_id_length",
		"billing_webhook_events_provider_length",
		"billing_webhook_events_status_allowed",
		"billing_webhook_events_type_length",
		"plan_entitlements_key_format",
		"plans_display_name_length",
		"subscriptions_period_order",
		"subscriptions_provider_length",
		"subscriptions_status_allowed",
		"user_entitlement_overrides_key_format",
		"user_entitlement_overrides_reason_length",
	}

	for _, constraint := range constraints {
		requireContains(t, migration, "drop constraint if exists "+constraint)
		requireContains(t, migration, "add constraint "+constraint)
	}

	if strings.Contains(migration, "do $$") {
		t.Fatal("account integrity migration must remain compatible with the managed Neon migration parser")
	}
}
