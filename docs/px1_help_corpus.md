# Gridiron help corpus v0.1

The public help center is the executable, server-rendered projection of the
versioned corpus in [`app/help/content.go`](../app/help/content.go). Open
[`/help`](../help) for deterministic search, role checklists, migration
concepts, glossary terms, state recovery, and links to the owning product
surface. Every result has a stable topic route at `/help/{topic_id}`.

## Contract

- `CorpusVersion` is the content version; `VerifiedSourceSHA` identifies the
  source snapshot used when the corpus was reviewed.
- `Search` normalizes Unicode letters/numbers, case, punctuation, apostrophes,
  and hyphens before ranking. Exact topic IDs/titles win, followed by aliases,
  synonyms, keywords, and body text. Ties resolve by category order, title,
  and ID, so the same query always produces the same order.
- A topic records actor, prerequisites, supported mode/phase, states, runtime
  deadline source, steps, privacy boundary, consequence, reversibility,
  result, failure, recovery, owning route, and an example.
- Topic routes accept optional `state` and `field` query context. They render
  a recovery panel or field/action explanation from the same topic metadata;
  they never copy the current mutable value into documentation.
- The public projection contains no email address, invitation, manager name,
  credential, provider session, or other personal data.

## Stable topic inventory

| Topic ID | Starting question | Owning route |
| --- | --- | --- |
| `getting-started` | What is this league room and what should I do first? | `/` |
| `identity-admission-and-membership` | Why can I sign in but not claim a seat? | `/join` |
| `teams-team-seats-and-rosters` | Which team seat and roster am I operating? | `/team` |
| `roles-primary-co-manager-and-commissioner` | What can each role see or change? | `/team` |
| `draft-order-readiness-and-clock` | Is the draft ready, open, or on the clock? | `/draft` |
| `big-board-and-autopick` | Which account's board does AUTO consume? | `/board` |
| `lineups-locks-matchups-and-scoring` | Why is this lineup locked or provisional? | `/team` |
| `players-free-agents-waivers-and-faab` | Why did my add or claim not process? | `/players` |
| `trades-review-and-processing` | Where is the trade and what happens next? | `/trades` |
| `pickem` | How do game locks and Pick'em results work? | `/pickem` |
| `preseason-blitz` | What is the bounded preseason contest? | `/blitz` |
| `activity-and-commissioner-notes` | Where is the evidence for a change? | `/activity` |
| `data-state-and-freshness` | Is this value live, stale, degraded, or unavailable? | `/scoring` |
| `commissioner-operations` | How do I start, pause, correct, or close safely? | `/admin` |
| `concept-transition` | What maps from another fantasy platform? | `/help/concept-transition` |
| `glossary` | What does a Gridiron term mean? | `/help/glossary` |

The last two entries are help topics rather than mutations. They keep
orientation stable while the first fourteen point to the runtime-owned
surface that can answer or perform the action.

## Role and phase projections

`ChecklistFor` builds the index from the same topic metadata for primary
manager, co-manager, seatless viewer, and commissioner projections. A
co-manager is scoped to the associated seat and account; the corpus never
promises a shared Big Board unless the runtime and product contract say so.

Each checklist item is filtered by the current runtime mode and normalized
phase. Dates, lock boundaries, waiver windows, capabilities, and source
freshness are deliberately not copied into this document or hard-coded in a
help page. The current runtime page remains authoritative when a guide and a
live state differ.

## Recovery contract

State guidance keeps route, query, filters, team/week, form, and focus context
when safe. It distinguishes `loading`, `empty`, `no-results`, `pending`,
`saved`, `locked`, `disabled`, `stale`, `degraded`, `offline`, `unavailable`,
`failed`, `permission-denied`, and `not-applicable`. A failed mutation is not
described as saved; retry text tells the user when not to replay a request.

The index intentionally links to [`docs/season-operations.md`](season-operations.md)
for the detailed commissioner restart and correction runbooks. The help page
is the short path; the handbook is the evidence-rich operating path.
