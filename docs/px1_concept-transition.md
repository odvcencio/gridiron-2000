# Concept transition guide

Gridiron does not log into, scrape, or automatically import another provider.
The commissioner creates the new league record deliberately and verifies the
result with the runtime pages. The mapping below is a vocabulary aid, not an
ETL promise.

## Common platform vocabulary

| Incoming concept (Sleeper, ESPN, Yahoo, or another platform) | Gridiron concept | Verification / next step |
| --- | --- | --- |
| Account, login, invited email | Identity and admission | Sign in, inspect the admission state, then confirm the team seat. Never put an email in public help content. |
| League, club, franchise | League and team seat | Recreate public identity and stable team IDs in config; verify `/team` and `/scoring`. |
| Roster and starting lineup | Roster and lineup | Recreate slot shape, then set the effective lineup at `/team`; kickoff locks are runtime-owned. |
| Draft rankings, queue, watch list | Big Board | Rebuild the current account's ordered board at `/board`; AUTO uses the current board contract. |
| Free agency, waiver wire, add/drop | Players, free agents, waivers | Read the current acquisition capability, priority/FAAB mode, units, and processing state at `/players`. |
| FAAB budget / bid | Non-currency FAAB units | Enter the displayed units; do not treat them as money or assume another provider's budget transfers. |
| Trade offer, veto, review | Trade and review workflow | Recreate policy, then verify involved seats and current review/deadline state at `/trades`. |
| Matchup, points, stat correction | Matchup and scoring ledger | Use `/scoring` and `/matchups`; provisional values are not final and corrections remain runtime-owned. |
| Commissioner/admin | Commissioner capability and `/admin` | Confirm the operator is acting in the owning league; HQ cards do not grant cross-league writes. |

## Manual cutover checklist

1. Agree on a cutover snapshot and what history, keepers, or credits are
   actually being recorded.
2. Configure identity policy, teams, roster, scoring, draft, waivers, trades,
   and timezone in the new league. Validate configuration before startup.
3. Invite managers through the new league's admission process. Ask each
   manager to verify the intended seat; an old provider session proves nothing.
4. Rebuild each manager's Big Board and the commissioner-approved draft order.
5. Record exceptions in a commissioner note or announcement rather than
   hiding them in an import.

No mapping silently copies roster history, picks, waiver priority, FAAB units,
trades, standings, private provider data, credentials, or disputed corrections.
