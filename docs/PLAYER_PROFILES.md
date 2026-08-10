# Albion player profiles

The player-profile API links one public Albion Online character to an authenticated application account. PvP activity is served from our own PostgreSQL event store; opening or refreshing a linked profile does **not** call Albion GameInfo or MurderLedger.

- Character ownership is not verified.
- Public search supports Americas, Europe, and Asia.
- Profile reads, linking, refresh, and unlinking require an Auth0 access token with `read:account`.
- Linking performs one identity lookup so the selected player ID can be validated.
- After linking, identity refresh happens asynchronously in a background worker.
- PvP events are ingested continuously for all three regions from the global GameInfo event feed.
- Events are idempotent by `(server, event_id)` and retain both players' visible equipment and IP.
- GameInfo polling uses retries, adaptive polling, persisted failure state and a circuit breaker.
- While the circuit is open, MurderLedger is used only as a background reconciliation source for linked players. It is never a dependency of an HTTP profile request.
- `identityRefreshedAt` and `activityRefreshedAt` are independent freshness signals. `activitySource` reports the source that last refreshed the regional activity store.
- Legacy `albion_player_events` rows are migrated into the global event store by migration 19.

## Request path

A normal profile read is deliberately local-only:

```text
browser
  -> GET /api/v1/me/albion-profile
  -> albion_player_profiles
  -> albion_pvp_events
  -> derived summary
```

The request path never waits for GameInfo.

## Background ingestion

The API process runs one ingestion loop per Albion region:

```text
GameInfo /events
  -> retry
  -> circuit breaker
  -> albion_pvp_events

circuit open
  -> MurderLedger player reconciliation
  -> albion_pvp_events
```

Normal polling runs every 30 seconds. When more than one page is required to catch up, the next poll is shortened to 10 seconds. Each page contains at most 50 events and up to 20 pages are traversed, matching the approximately 1,000-event rolling upstream window. Keeping the worker running continuously is therefore operationally important.

## Identity refresh

Character identity is intentionally decoupled from combat activity. A separate worker refreshes stale linked identities in the background. It updates name, guild, alliance, avatar and lifetime PvP fame without changing the freshness of the PvP event store.

A GameInfo identity failure can therefore produce this valid state:

```text
identityRefreshedAt: older timestamp
lastRefreshStatus: error
activityRefreshedAt: recent timestamp
activitySource: gameinfo | murderledger
```

The UI should continue showing locally stored combat data in that state.

## HTTP routes

- `GET /api/v1/albion/players/search`
- `GET /api/v1/me/albion-profile`
- `PUT /api/v1/me/albion-profile/link`
- `DELETE /api/v1/me/albion-profile/unlink`
- `POST /api/v1/me/albion-profile/refresh`

`POST /refresh` now reloads the newest database snapshot and has no upstream cooldown. It exists for client compatibility and for users who explicitly want to reload the local snapshot.

Each returned event exposes `playerEquipment` and `opponentEquipment`. The equipment object may contain `mainHand`, `offHand`, `head`, `armor`, `shoes`, `bag`, `cape`, `mount`, `potion`, and `food` item identifiers when the source supplied them.

## Derived statistics

The API derives the linked player's recent statistics from stored events, rather than trusting a provider-specific aggregate. This supports victories, defeats, K/D, fame gained/lost, weapon usage, win rate by weapon, opponent history, IP comparisons, builds and time-window trend queries. The indexes introduced by migration 19 are arranged around player ID, weapon and occurrence time so weekly/monthly analysis can be added without changing the ingestion format.
