# Albion player profiles

The player-profile API links one public Albion Online character to an authenticated application account.

- Character ownership is not verified in Hito 5A.1.
- Public search supports Americas, Europe, and Asia.
- Profile reads, linking, refresh, and unlinking require an Auth0 access token with `read:account`.
- Manual refreshes have a five-minute cooldown.
- The last cached profile and activity remain available when the upstream game-information service fails.
- Cached combat events include the visible equipment for both the linked player and the opponent.
- Legacy events retain `weaponType`; migration 14 backfills that value into `playerEquipment.mainHand`.
- Unlinking removes the profile and cached activity through database cascade rules.

## HTTP routes

- `GET /api/v1/albion/players/search`
- `GET /api/v1/me/albion-profile`
- `PUT /api/v1/me/albion-profile/link`
- `DELETE /api/v1/me/albion-profile/unlink`
- `POST /api/v1/me/albion-profile/refresh`

Each event returned by the authenticated profile routes exposes `playerEquipment` and `opponentEquipment`. The equipment object may contain `mainHand`, `offHand`, `head`, `armor`, `shoes`, `bag`, `cape`, `mount`, `potion`, and `food` item identifiers when the provider supplied them.
