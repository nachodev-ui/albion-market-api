# Albion player profiles

The player-profile API links one public Albion Online character to an authenticated application account.

- Character ownership is not verified in Hito 5A.1.
- Public search supports Americas, Europe, and Asia.
- Profile reads, linking, refresh, and unlinking require an Auth0 access token with `read:account`.
- Manual refreshes have a five-minute cooldown.
- The last cached profile and activity remain available when the upstream game-information service fails.
- Unlinking removes the profile and cached activity through database cascade rules.
