from pathlib import Path

p = Path("cmd/api/main.go")
s = p.read_text()
s = s.replace(
    '"github.com/nachodev-ui/albion-market-api/internal/observability"\n',
    '"github.com/nachodev-ui/albion-market-api/internal/observability"\n\t"github.com/nachodev-ui/albion-market-api/internal/playerprofile"\n',
    1,
)
s = s.replace(
    '\taccountHandler := accounts.NewHandler(accountService)\n',
    '\taccountHandler := accounts.NewHandler(accountService)\n'
    '\tprofileRepository, err := playerprofile.NewPostgresRepository(dbpool)\n'
    '\tif err != nil { logger.Error("player_profile.repository_configure_failed", observability.F("error", err)); return }\n'
    '\tprofileProvider, err := playerprofile.NewGameInfoProvider(12 * time.Second)\n'
    '\tif err != nil { logger.Error("player_profile.provider_configure_failed", observability.F("error", err)); return }\n'
    '\tprofileService, err := playerprofile.NewService(profileRepository, profileProvider, accountService, 5*time.Minute, 50)\n'
    '\tif err != nil { logger.Error("player_profile.service_configure_failed", observability.F("error", err)); return }\n'
    '\tprofileHandler := playerprofile.NewHandler(profileService)\n',
    1,
)
s = s.replace(
    '\t\t\tBillingHandler: billingHandler,\n\t\t\tAuthenticator:  accountAuthenticator,',
    '\t\t\tBillingHandler:       billingHandler,\n'
    '\t\t\tPlayerProfileHandler: profileHandler,\n'
    '\t\t\tAuthenticator:        accountAuthenticator,',
    1,
)
p.write_text(s)

p = Path("internal/observability/readiness.go")
s = p.read_text().replace("const ExpectedSchemaVersion = 12", "const ExpectedSchemaVersion = 13")
s = s.replace(
    '\t"public.app_schema_state",\n',
    '\t"public.app_schema_state",\n\t"public.albion_player_events",\n\t"public.albion_player_profiles",\n',
    1,
)
p.write_text(s)

p = Path("internal/observability/readiness_test.go")
s = p.read_text().replace("ExpectedSchemaVersion != 12", "ExpectedSchemaVersion != 13").replace("want 12", "want 13")
p.write_text(s)

p = Path("openapi/openapi.yaml")
s = p.read_text()
block = '''  /api/v1/albion/players/search:
    get:
      operationId: searchAlbionPlayers
      responses:
        '200':
          description: Public player search results.
  /api/v1/me/albion-profile:
    get:
      operationId: getMyAlbionProfile
      responses:
        '200':
          description: Linked player profile.
    put:
      operationId: linkMyAlbionProfile
      responses:
        '200':
          description: Player profile linked.
    delete:
      operationId: unlinkMyAlbionProfile
      responses:
        '204':
          description: Player profile removed.
  /api/v1/me/albion-profile/refresh:
    post:
      operationId: refreshMyAlbionProfile
      responses:
        '200':
          description: Player profile refreshed.

'''
if "  /api/v1/albion/players/search:\n" not in s:
    s = s.replace("  /api/v1/billing/checkout:\n", block + "  /api/v1/billing/checkout:\n", 1)
p.write_text(s)
