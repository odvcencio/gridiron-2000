# Gridiron glossary

The interactive glossary at [`/help`](../help) is generated from the same
versioned corpus as the topic pages. This projection is useful for operators,
support, and migration notes; if a term's current state differs, the runtime
page wins.

| Term | Meaning | Related topic |
| --- | --- | --- |
| identity | The authenticated person asking for access; it is not automatically a team seat. | `identity-admission-and-membership` |
| admission | The league policy and invitation state that decides whether an identity may enter. | `identity-admission-and-membership` |
| primary manager | The main manager associated with a team seat. | `roles-primary-co-manager-and-commissioner` |
| co-manager | An optional second manager scoped to an associated team seat and configured permissions. | `roles-primary-co-manager-and-commissioner` |
| commissioner capability | Authority for league operations; it does not substitute for membership or a team seat. | `roles-primary-co-manager-and-commissioner` |
| team seat | The durable league association that owns a roster, lineup, and team-scoped actions. | `teams-team-seats-and-rosters` |
| roster | Players assigned to configured slots/zones. | `teams-team-seats-and-rosters` |
| lineup lock | The kickoff/state boundary that prevents ordinary slot edits. | `lineups-locks-matchups-and-scoring` |
| Big Board | The current account's ordered draft targets used by AUTO first. | `big-board-and-autopick` |
| autopick / AUTO | Selection authority that uses the current account's available board, then the authoritative fallback shown by Draft. | `big-board-and-autopick` |
| free agent | An eligible unrostered player available when the active rules permit immediate acquisition. | `players-free-agents-waivers-and-faab` |
| waiver | A delayed acquisition process for a player not immediately addable. | `players-free-agents-waivers-and-faab` |
| FAAB units | Non-currency units used by a configured claim processor; the runtime owns budget and processing. | `players-free-agents-waivers-and-faab` |
| trade review | The configured period and authority evaluating an accepted trade. | `trades-review-and-processing` |
| Pick'em | An independent against-the-spread game with per-game locks and W-L-P results. | `pickem` |
| Preseason Blitz | A bounded preseason side contest with its own slate and locks. | `preseason-blitz` |
| Signal Wire | Provisional mixed-source signals that never mutate fantasy scores. | `data-state-and-freshness` |
| live | A fresh successful source or active workflow state; read its adjacent context. | `data-state-and-freshness` |
| loading | A read or action is still in progress; do not duplicate a mutation. | `data-state-and-freshness` |
| empty | A valid collection contains zero items. | `data-state-and-freshness` |
| no-results | The current query/filter matches nothing; the collection may still contain records. | `data-state-and-freshness` |
| pending | Work awaits a deadline, review, or processor. | `data-state-and-freshness` |
| stale | A last-good snapshot is older than its freshness window. | `data-state-and-freshness` |
| degraded | The latest source attempt failed, but a labeled last-good value remains. | `data-state-and-freshness` |
| unavailable | There is no usable current or last-good value for the requested capability. | `data-state-and-freshness` |
| runtime source | The executable config/state/auth/processor/feed that owns a displayed value. | `data-state-and-freshness` |
| normalized phase | The canonical lifecycle token used to choose role/phase help. | `getting-started` |
| feature capability | Whether a workflow is supported, unsupported, or temporarily unavailable at runtime. | `getting-started` |
