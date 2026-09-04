package help

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// CorpusVersion is intentionally independent of league season/configuration.
// Mutable dates, rules, permissions, and source freshness are projected from
// runtime state by the route loaders.
const CorpusVersion = "0.1"

// VerifiedSourceSHA is the source tree against which this corpus was reviewed.
const VerifiedSourceSHA = "6561962a80b40874ea78dca950f8d41b03be7540"

// ShortSHA returns git's own common 7-character abbreviated form. /help and
// /help/<topic> used to print VerifiedSourceSHA's full 40 characters
// straight into an anonymous visitor's masthead and corpus-receipt prose
// (item 11, 2026-09-02 audit) — far more than anyone reads at a glance, and
// wide enough to overflow the masthead's own narrow console card. The full
// value stays reachable through the caller's own title attribute.
func ShortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

type Category struct {
	ID    string
	Title string
	Order int
}

type Topic struct {
	ID, Title, Summary, Category                                            string
	Audiences, IdentityStates, AdmissionStates, TeamAssociations, TeamRoles []string
	CommissionerCapability, Modes, Phases, RequiredCapabilities, DataStates []string
	Keywords, Synonyms, FlavorAliases                                       []string
	Actor, Prerequisites, Supported, States, Deadline                       string
	Steps                                                                   []string
	Privacy, Consequence, Reversibility, Result, Failure, Recovery          string
	RuntimeSource, Example, ActionRoute                                     string
	SourceRefs                                                              []string
	IntroducedVersion, LastVerifiedSHA                                      string
}

var categories = []Category{
	{ID: "getting-started", Title: "Get started", Order: 1},
	{ID: "draft", Title: "Draft and Big Board", Order: 2},
	{ID: "team", Title: "Team, roster, and lineup", Order: 3},
	{ID: "players", Title: "Players, free agents, and waivers", Order: 4},
	{ID: "trades", Title: "Trades", Order: 5},
	{ID: "scoring", Title: "Matchups and scoring", Order: 6},
	{ID: "pickem", Title: "Pick'em and Blitz", Order: 7},
	{ID: "league", Title: "League, activity, and data state", Order: 8},
	{ID: "commissioner", Title: "Commissioner operations", Order: 9},
	{ID: "glossary", Title: "Glossary and concept transition", Order: 10},
}

func Categories() []Category { return append([]Category(nil), categories...) }

func baseTopic(category, id, title, summary string, keywords, synonyms, aliases []string) Topic {
	return Topic{
		ID: id, Title: title, Summary: summary, Category: category,
		Audiences:        []string{"admitted member", "primary manager", "co-manager", "seatless member", "commissioner"},
		IdentityStates:   []string{"anonymous", "authenticated"},
		AdmissionStates:  []string{"not-admitted", "pending", "admitted", "denied"},
		TeamAssociations: []string{"none", "associated"}, TeamRoles: []string{"none", "primary", "co-manager"},
		CommissionerCapability: []string{"absent", "present"}, Modes: []string{"dynasty", "redraft", "configured"},
		Phases:               []string{"pre-draft", "draft", "preseason", "regular-season", "post-season", "complete", "unknown"},
		RequiredCapabilities: []string{"help"},
		DataStates:           []string{"loading", "empty", "no-results", "pending", "saved", "locked", "disabled", "stale", "degraded", "offline", "unavailable", "failed", "permission-denied", "not-applicable"},
		Keywords:             keywords, Synonyms: synonyms, FlavorAliases: aliases,
		SourceRefs:        []string{"spec.gridiron.manager-onboarding.v0.1", "plan.gridiron.v0.1.product-clarity-foundation"},
		IntroducedVersion: CorpusVersion, LastVerifiedSHA: VerifiedSourceSHA,
		Actor:         "The viewer or authorized operator for this task.",
		Prerequisites: "The current identity, admission, role, capability, and persisted state are shown by the owning page.",
		Supported:     "The configured mode and phase decide applicability; unsupported work is labeled not-applicable.",
		States:        "The owning page names the applicable loading, saved, locked, stale, degraded, unavailable, and recovery state.",
		Deadline:      "The exact runtime deadline and league-local timezone are shown on the owning page; this corpus contains no mutable date.",
		Steps:         []string{"Read the current state and its reason.", "Use the linked owning action.", "Reread the persisted result before acting again."},
		Privacy:       "Public help contains no member names, invitation addresses, picks, boards, or private transactions.",
		Consequence:   "The owning page names the affected team/object and the durable consequence before a mutation.",
		Reversibility: "The owning page states whether the action can be edited, canceled, undone, or requires commissioner correction.",
		Result:        "A local result names the saved, pending, locked, final, or failed state.",
		Failure:       "Validation, stale, authorization, and source failures make no unreported partial change.",
		Recovery:      "Keep the safe route/query/form context, reread current state, and use the bounded retry or escalation path.",
		RuntimeSource: "Validated league configuration, persisted state, authorization, processors, and source freshness.",
		Example:       "When runtime makes a capability unavailable, help explains why and points to the valid next route.",
		ActionRoute:   "/",
	}
}

func details(t *Topic, actor, prereq, supported, states, deadline string, steps []string, privacy, consequence, reversible, result, failure, recovery, source, example, route string) {
	t.Actor, t.Prerequisites, t.Supported, t.States, t.Deadline = actor, prereq, supported, states, deadline
	t.Steps, t.Privacy, t.Consequence, t.Reversibility = steps, privacy, consequence, reversible
	t.Result, t.Failure, t.Recovery, t.RuntimeSource, t.Example, t.ActionRoute = result, failure, recovery, source, example, route
}

func buildCorpus() []Topic {
	var out []Topic
	add := func(t Topic) { out = append(out, t) }

	t := baseTopic("getting-started", "getting-started", "Get started in Gridiron", "Find the active league, understand access, and complete the first useful task.", []string{"start", "first session", "orientation", "new manager", "setup"}, []string{"start here", "how do I begin", "new to gridiron"}, []string{"manager guide", "play the room"})
	details(&t, "Any person entering a league room.", "No prerequisite for this explanation; actions use the admission and team predicates shown by the current page.", "All configured modes and phases; checklist items are omitted when a capability is not applicable.", "Anonymous, authenticated, pending, admitted, seatless, and associated states are independent.", "Use the runtime league clock and timezone for the next material deadline.", []string{"Confirm league name, mode, phase, timezone, and next deadline.", "Confirm admission and team association.", "Open rules, then use the linked action for the current task."}, "Public help has no member PII.", "Sign-in identifies a person; it does not import an old provider account or grant a team seat.", "Reading is reversible; each game action states its own boundary.", "The next action is named with a route and local explanation.", "If a page cannot load, keep the current route and use its state-specific recovery.", "Start from the active league home or stable help topic; never guess a deadline from a stale screenshot.", "Runtime league configuration, membership, authorization, phase, and source status.", "An admitted seatless member can read rules and activity without an invented claim button.", "/")
	add(t)

	t = baseTopic("getting-started", "identity-admission-and-membership", "Identity, admission, and membership", "Separate person, sign-in, league admission, and team association.", []string{"sign in", "login", "invite", "allowlist", "domain", "seatless", "pending", "access"}, []string{"who can join", "league access", "account access", "membership"}, []string{"owner login", "franchise access"})
	details(&t, "A person requesting access or a commissioner reviewing admission.", "Use the intended identity. Admission is evaluated before team association.", "Anonymous/authenticated states; pending appears only when an implemented admission capability exists.", "Not-admitted, pending, admitted seatless, associated, and denied are distinct.", "Configured admission/expiry policy is rendered in league-local time.", []string{"Sign in with the intended account.", "Read the rendered admission reason.", "Claim or confirm a team only when eligible.", "Ask the commissioner to correct a wrong invite or assignment."}, "Authentication proves the requester; public help does not disclose admitted identities.", "A signed-in person is not automatically a member, and an admitted member may remain seatless.", "Wrong association follows the audited commissioner path; aliases may not bypass policy.", "The page reports persisted admission/association and the valid next action.", "Rejected or malformed auth returns safely with no membership mutation.", "Use the displayed access route or /guide#identity; do not reauthenticate when the issue is admission.", "OAuth identity, membership policy, aliases, membership rows, and team associations.", "An unadmitted identity sees its reason and request route, not a misleading claim form.", "/login")
	add(t)

	t = baseTopic("getting-started", "teams-team-seats-and-rosters", "Teams, team seats, and rosters", "A team is the durable football object; a team seat is the responsibility shared by its managers.", []string{"team", "team seat", "roster", "association", "franchise", "club", "claim"}, []string{"my team", "choose a team", "team status", "franchise"}, []string{"club", "roster owner"})
	details(&t, "An admitted member viewing a team or an authorized manager changing its roster.", "Admission is required for team data; a persisted association is required for roster mutation.", "All modes; roster zones and limits are selected from configured league state.", "Seatless, primary, co-manager, claimed, open, full, locked, and unavailable are distinct.", "Roster/lineup deadlines derive from each player's kickoff and league timezone.", []string{"Confirm team display identity and seat role.", "Read roster slots and capacity.", "Use Team for lineup/roster and Players for acquisition.", "Refresh after a commissioner correction."}, "Team-scoped details follow the league visibility policy.", "A roster belongs to a team, not one identity; a seatless member cannot mutate it.", "Unlocked lineup changes can be revised; kickoff and final boundaries are authoritative.", "The result names team, slot, player, and saved/locked state.", "Capacity, authorization, lock, and stale failures make no partial change.", "Return to /team, reread week/locks, and use commissioner help for a wrong association.", "Roster config, teams/memberships/rosters, schedule kickoff, and lineup processor.", "A seatless member may read rules but is not told to edit a roster they do not own.", "/team")
	add(t)

	t = baseTopic("getting-started", "roles-primary-co-manager-and-commissioner", "Roles: primary manager, co-manager, and commissioner", "Roles are separate authorization axes; commissioner capability is an overlay.", []string{"role", "primary", "co-manager", "commissioner", "owner", "authorization", "permission"}, []string{"what can I do", "manager permissions", "commissioner access"}, []string{"owner", "franchise owner", "commish"})
	details(&t, "Any authenticated person asking what an action means or who may perform it.", "Viewer state and action authorization are persisted and evaluated together.", "Primary/co-manager apply to a team association; commissioner composes with either role or seatless membership.", "Absent, primary, co-manager, seatless commissioner, and pending co-manager are not interchangeable.", "Role/invitation expiry uses configured runtime policy.", []string{"Read the role label.", "Use the local disabled reason.", "Keep seat responsibility and commissioner authority separate.", "Ask for an audited correction instead of sharing credentials."}, "Role projections avoid unauthorized identity/invitation details.", "A commissioner may oversee a league without owning a roster; a co-manager is not the primary manager.", "Role changes use explicit confirmation and audit.", "The interface exposes allowed action, disabled reason, or valid request route.", "Permission-denied preserves state and protected details.", "Follow the help link/return target; never retry a forbidden mutation as another role.", "Persisted membership/role, commissioner authorization, co-manager capability, and action policy.", "A seatless commissioner receives seatless base help plus commissioner overlay.", "/help/roles-primary-co-manager-and-commissioner")
	add(t)

	t = baseTopic("draft", "draft-order-readiness-and-clock", "Draft order, readiness, and clock", "The scheduled meeting is not an automatic start; the commissioner intentionally opens pick one.", []string{"draft", "order", "readiness", "clock", "start", "pause", "resume", "pick", "schedule"}, []string{"draft time", "draft timer", "on the clock", "when does the draft start"}, []string{"draft room", "war room"})
	details(&t, "Managers prepare their team; the commissioner controls order, start, pause, and recovery.", "Admission, association, order, pool capacity, and readiness are shown in Draft.", "Dynasty/redraft; all dates, rounds, clock, and timezone values are runtime-owned.", "Unscheduled, scheduled, readiness-open, ready, not-ready, order-incomplete, open, on-clock, paused, in-progress, complete, unavailable.", "Render exact configured meeting/start/pick time, timezone, and relative text.", []string{"Review seats, attendance, boards, pool health, order, and schedule.", "Managers mark ready/not ready and choose AUTO.", "Commissioner enters the runtime-required confirmation to start.", "Read exclusive NOT RUNNING, PAUSED, or RUNNING before controls.", "After refresh/restart reread persisted pick/deadline."}, "Boards and manager controls are team-scoped; commissioner readouts avoid unnecessary identity detail.", "Clock expiry/AUTO uses board first then authoritative best available; force advances the current pick.", "Readiness is reversible before its gate; force/undo use explicit server-validated current-pick controls.", "Tape names player, pick, next actor, and AUTO/forced outcome.", "Stale, double, pool-insufficient, and unauthorized requests consume no player.", "Refresh and follow commissioner recovery; never edit SQLite.", "Draft config/lifecycle/order/picks, player pool, presence/readiness, commissioner service.", "An absent manager's AUTO uses its available board before best available.", "/draft")
	add(t)

	t = baseTopic("draft", "big-board-and-autopick", "Big Board and autopick", "Rank private draft targets and understand exactly which account's order AUTO consumes.", []string{"big board", "board", "queue", "draft queue", "rank", "autopick", "auto pick", "adp"}, []string{"draft queue", "player queue", "my rankings", "what will autopick choose"}, []string{"draft board", "war board"})
	details(&t, "The primary/co-manager associated with a team seat; commissioner access is separate.", "A persisted association and available pool are required to edit a board.", "Dynasty/redraft; current per-account ownership is runtime truth.", "Loading, empty, no-results, saved, player-picked, disabled, permission-denied, stale, unavailable.", "The draft clock and player kickoff locks are runtime deadlines.", []string{"Search/browse the pool.", "Add, remove, and reorder targets for the current account.", "Keep backups because picked players leave availability.", "Read Draft's AUTO explanation before leaving the clock unattended."}, "The board is private to account/team policy; do not imply another manager edits it.", "AUTO selects the current account's first available board target, then authoritative best available.", "Board edits are reversible while available; a pick follows commissioner correction rules.", "Saved ordering and search/filter context remain visible.", "Unavailable player, stale ordering, permission, and pool failures make no partial reorder.", "Reread after another client change; use Draft to verify authoritative pick.", "Per-account board persistence, player availability, draft pick state, autopick selector.", "A co-manager queue is not described as shared until owner decision/migration/implementation gates pass.", "/board")
	add(t)

	t = baseTopic("draft", "practice-draft", "Practice draft", "Take a few picks on the clock in a copy of the draft room; nothing you do there is saved.", []string{"practice", "practice draft", "mock", "mock draft", "simulation", "rehearse", "bots", "try the draft"}, []string{"can I try the draft room", "mock draft", "test the draft", "draft simulator"}, []string{"scrimmage", "walkthrough"})
	details(&t, "A seated primary or co-manager, before the real draft starts.", "A team seat, and a real draft that has not started; a seatless commissioner cannot practice.", "Every league mode; the practice uses the league's real pool, draft order, roster shape, and pick clock.", "Available, unavailable (no seat, real draft live, draft complete), open, on-clock, complete.", "The practice pick clock is the league's real pick clock; a practice ends three rounds after its start round.", []string{"Open Draft and choose Practice the draft room, or go to /draft/practice.", "Choose a start round: early, middle, late, or specialists.", "Pick when you are on the clock; the other seats pick after a short pause.", "Leave at any time, or start again from any round."}, "A practice is private to your own sign-in; no one else sees it.", "Nothing is saved: no pick, readiness, autopick, or board change reaches the league.", "Leave or restart at any time; the real draft is untouched.", "The room says PRACTICE and that picks do not count; the real draft's start time stays visible.", "A practice cannot start while the real draft is live or complete, or without a seat.", "Return to Draft; the real room is exactly as you left it.", "Draft config, draft order, player pool, Big Boards, pick clock: an in-memory copy only.", "A manager practices the late rounds twice to see how kickers and defenses come off the board.", "/draft/practice")
	add(t)

	t = baseTopic("team", "lineups-locks-matchups-and-scoring", "Lineups, locks, matchups, and scoring", "Set the effective lineup before kickoff and read AWAITING RELEASE versus final scoring honestly.", []string{"lineup", "starter", "bench", "lock", "kickoff", "matchup", "score", "scoring", "stat correction"}, []string{"when do lineups lock", "lineup deadline", "game lock", "fantasy points"}, []string{"starting lineup", "scorebug"})
	details(&t, "Admitted viewers read matchups; an authorized manager changes only unlocked slots.", "Team association and current week/slot lock state control mutations.", "Configured regular-season or applicable phase; unavailable phases are explained.", "Upcoming, lineup-editable, lineup-locked, live-provisional, source-stale, degraded, final, corrected, bye, unavailable.", "Each player's kickoff and league timezone govern that slot.", []string{"Open Team and select current week.", "Fill valid starters and review source state.", "Save before kickoff.", "Use Matchups for provisional/final labels.", "Reread after source change."}, "Roster visibility follows league policy; projections are not private authority.", "Locked slots cannot be changed normally; closed weeks pin effective starters/results.", "Unlocked edits can be revised; locked/final outcomes use commissioner correction.", "Local result names saved slot/player state and preserves week/team context.", "Cross-week, lock, invalid-slot, stale, and source failures reject the mutation.", "Return to Team, inspect lock reason/last-success age, retry only when available.", "Roster config, schedule, lineup persistence, stats ledger, matchup/correction state.", "A late stat correction can change provisional scoring without unlocking a lineup.", "/team")
	add(t)

	t = baseTopic("players", "players-free-agents-waivers-and-faab", "Players, free agents, waivers, and FAAB", "Acquire an eligible player using the displayed rule and preserve filter context.", []string{"players", "free agent", "waiver", "claim", "FAAB", "budget", "priority", "add", "drop", "acquisition"}, []string{"waiver budget", "bid units", "why can't I add a player", "how do waivers work"}, []string{"player pool", "wire pickup"})
	details(&t, "An authorized manager with roster capacity and an eligible player action.", "Team association, capacity/position eligibility, acquisition capability, and priority/FAAB units.", "The current league waiver mode; unsupported/source-unavailable modes are labeled.", "Free-agent-available, waiver-locked, claim-draft, claim-submitted, pending-run, processing, awarded, lost, canceled, roster-ineligible, insufficient-FAAB, priority-unavailable, source-unavailable.", "Exact waiver clear/process time, timezone, and relative text come from runtime.", []string{"Read pool freshness and eligibility.", "Add a true free agent or file a claim.", "Enter non-currency FAAB units when configured.", "Review drop, priority, and submitted state.", "Cancel/reorder only while permitted."}, "Claims/bids follow configured visibility; never expose another team's private bid.", "A claim is revalidated at processing; capacity, ownership, eligibility, and units may change.", "Edit/cancel only before the displayed boundary; awarded movement follows runtime finality.", "Submitted/awarded/lost/canceled reason and player/filter context are shown.", "Validation, stale, insufficient units, capacity, source, and permission failures spend nothing.", "Return to Players with waiver filter; reread state/last success before retry/cancel.", "Acquisition config, pool, roster, claim queue, processor clock, source freshness.", "Two claims can be submitted before processing; the configured priority or units decide the recorded result.", "/players")
	add(t)

	t = baseTopic("trades", "trades-review-and-processing", "Trades, review, and processing", "Compose, respond to, and track a trade without guessing its boundary.", []string{"trade", "offer", "counter", "accept", "decline", "review", "veto", "process", "expire"}, []string{"trade veto", "trade deadline", "trade review", "send a trade"}, []string{"deal desk", "trade desk"})
	details(&t, "Authorized managers of involved teams; commissioner review is an orthogonal capability.", "Claimed teams, eligible assets, and open trade capability.", "Configured trade deadline, review, veto, and processing policy.", "Composing, proposed, countered, pending-response, accepted-pending-review, review-open, approved, rejected, canceled, vetoed, processed, expired, failed, unavailable.", "Proposal expiry/review/processing/deadline values are runtime-derived in league-local time.", []string{"Select counterparty/assets.", "Review named teams and consequence.", "Respond/counter/withdraw while permitted.", "Read review/veto/processing result in activity."}, "Terms are visible to involved teams and configured reviewers only.", "Accepted terms may enter review and then move named assets.", "Proposal/response/review reversibility ends at runtime boundary; processed movement is not undone by refresh.", "Result names trade state, teams/assets, and activity receipt.", "Stale offer, lock, deadline, authorization, and processing failures leave rosters unchanged.", "Return to Trades with form context; reread offer and never replay stale submission.", "Trade config/state, rosters, locks, memberships, processor, activity.", "Accepted may remain review-open until the configured boundary; acceptance is not processing.", "/trades")
	add(t)

	t = baseTopic("pickem", "pickem", "Pick'em", "Choose against the displayed market line with per-game locks and an independent W-L-P record.", []string{"pickem", "pick'em", "spread", "ATS", "line", "push", "missed", "kickoff", "picks"}, []string{"where did my pick go", "against the spread", "pick sheet", "football picks"}, []string{"picks", "confidence picks"})
	details(&t, "An admitted member with Pick'em capability for the current week/slate.", "Admission and the current slate; a fantasy team seat is not required.", "Current configured slate/source; unavailable lines become void/unavailable, not straight-up guesses.", "Not-open, open-unpicked, partially-picked, submitted, edited, locked, scored, corrected, missed, not-applicable, unavailable.", "Each game's kickoff and line-freeze rule are runtime deadlines in league-local time.", []string{"Read the market spread.", "Select a side.", "Save/submit per form semantics.", "Review per-game lock/result; later games remain open.", "Read W-L-P separately from fantasy scoring."}, "Pick visibility follows configured pre/post-lock policy.", "After week entry, a missed kickoff is a loss; a push is neutral and a void has no result.", "Edits stop at each game's runtime lock; scored/corrected results persist.", "Each game reports saved, locked, missed, push, void, or scored state.", "Stale slate, unavailable line, validation, and authorization preserve unaffected picks.", "Return to Pick'em with week context, reread market/locks, retry open games only.", "Pick'em config, market/slate, schedule/kickoffs, entries, result processor.", "Thursday can lock while Sunday games remain open; entering the week does not lock the sheet.", "/pickem")
	add(t)

	t = baseTopic("pickem", "preseason-blitz", "Preseason Blitz", "A bounded preseason side contest with its own slate, entries, locks, scoring, and archive state.", []string{"blitz", "preseason", "contest", "slate", "entry", "leaderboard"}, []string{"preseason contest", "blitz lineup", "side contest"}, []string{"Blitz", "preseason DFS"})
	details(&t, "An admitted member while the configured preseason Blitz capability/slate applies.", "Admission, supported slate, and preseason phase.", "Only the bounded configured contest; not a generalized year-round salary-cap DFS product.", "Not-open, open, in-progress, submitted, edited, locked, scored, corrected, closed, not-applicable, unavailable.", "Slate open/lock/result times and timezone are runtime-owned.", []string{"Open the current slate.", "Build the configured entry within limits.", "Save/submit before each player's kickoff lock.", "Review live/final/archive state."}, "Entry/leaderboard visibility follows contest policy and does not grant a roster seat.", "An individual player kickoff can lock one choice while others remain open.", "Edits stop at each lock; closed contests remain archives.", "Slate reports saved, locked, scored, corrected, closed, or unavailable.", "Source/validation failure keeps entry context and does not claim a final score.", "Return to Blitz with slate query, inspect last-success age, retry an open choice.", "Blitz config/slate, kickoff schedule, score source, entry, leaderboard.", "A closed preseason slate is archive content, not an active regular-season obligation.", "/blitz")
	add(t)

	t = baseTopic("league", "activity-and-commissioner-notes", "Activity and Commissioner Notes", "Distinguish manager actions, processor results, commissioner rulings, and notification receipts.", []string{"activity", "audit", "history", "receipt", "commissioner note", "event", "announcement"}, []string{"what changed", "transaction history", "action history", "audit log"}, []string{"signal tape", "event tape"})
	details(&t, "Admitted viewers read permitted activity; commissioners add notes under configured policy.", "Owning league instance and activity visibility policy.", "All phases; events may be pending, saved, failed, corrected, or final.", "Loading, empty, saved, pending, failed, permission-denied, stale, unavailable.", "Events show runtime timestamps and league-local timezone.", []string{"Open Activity after consequential actions.", "Identify actor class, object, outcome, and receipt.", "Treat notes as context, not replacement for runtime.", "Follow owning route for current action."}, "Activity filters private team data by viewer authorization.", "A receipt records persistence; a queued notification is not delivery proof.", "History is append-oriented; corrections create explicit follow-up events.", "Entries identify saved/pending/failed/corrected/final state and route.", "Partial reads label unavailable pages and preserve unaffected entries.", "Refresh owning route and compare latest receipt; escalate suspected mismatch.", "Persisted activity/audit events, processor receipts, notes, notification receipts.", "A waiver award and commissioner correction are separate actor-class events.", "/activity")
	add(t)

	t = baseTopic("league", "data-state-and-freshness", "Data state and freshness", "Read LIVE, CACHED, STALE, DEGRADED, OFFLINE, UNAVAILABLE, and AWAITING RELEASE without treating unknown as zero.", []string{"data", "live", "freshness", "cache", "cached", "stale", "degraded", "offline", "unavailable", "source", "last success"}, []string{"live data", "is the data current", "source status", "feed health"}, []string{"signal status", "feed status"})
	details(&t, "Any viewer deciding whether source-backed data or an action is safe.", "The owning surface's source status and last-success projection.", "All modes/phases; source state is distinct from process liveness.", "Loading, live, cached, stale, degraded, offline, awaiting-release, unavailable, failed, not-applicable.", "Freshness windows and source timestamps are runtime values.", []string{"Read state next to the value.", "Check source and last-success age.", "Continue only when remaining capability is explicitly usable.", "Retry/wait through the bounded recovery action."}, "Health/help projections are PII-free.", "Cached/stale can be useful but are not live; unavailable cannot authorize a fabricated fact.", "Reading a snapshot is reversible; refresh and game mutations have separate semantics.", "The page says why, remaining capability, and next action.", "Failed refresh retains last-good data and never replaces unknown with zero.", "Use local retry/manual refresh and escalate persistent source failure.", "Provider/relay status, cache timestamps, processors, route loaders.", "A cached pool with enough capacity can remain labeled usable; unavailable cannot be a zero-player list.", "/")
	add(t)

	t = baseTopic("commissioner", "commissioner-operations", "Commissioner operations", "Run a league intentionally: admission, setup, draft control, weekly processing, playoff truth, recovery, and isolated HQ.", []string{"commissioner", "admin", "setup", "invite", "start", "pause", "force", "undo", "close week", "HQ", "recovery", "operator", "playoff", "playoffs", "postseason", "bracket", "seed", "bye", "tie-break", "ledger", "correction"}, []string{"commissioner checklist", "commissioner handbook", "league admin", "how do I run the league", "playoff correction", "bracket preview"}, []string{"admin console", "commissioner HQ", "control room", "postseason board"})
	details(&t, "A commissioner with configured capability operating the owning league instance.", "Commissioner authorization, current league identity, and a read of state before every mutation.", "Dynasty/redraft; HQ is bounded read-only portfolio and links to owning league for writes. Published playoff truth is member-visible; preview is commissioner-only.", "Ready, not-ready, scheduled, running, paused, stale, degraded, unavailable, failed, final, preview-only, published, waiting-for-ledger, corrected, recovery-required.", "Draft, waiver, trade, lineup, close, playoff, and notification deadlines come from runtime configuration.", []string{"Verify identity, seats, order, schedule, readiness, source health, and postseason provenance.", "Use typed confirmations for destructive/force/publish/correction actions.", "Read persisted result, bracket truth, and activity receipt.", "Advance playoffs only from complete final authoritative ledgers; never enter scores in a browser form.", "After restart/stale response, reconcile before retry. Earlier-round correction requires a fresh preview.", "Use HQ only to find attention; mutate in owning league."}, "HQ summaries and public bracket projections are PII-free and do not merge sessions or expose addresses.", "Start/force/undo/close/reset/release/publish/advance/correction can change durable state and name affected objects.", "Only documented action is reversible; final/reset/publish/correction controls require confirmation/audit. A retry of the same publication is idempotent.", "Commissioner sees receipt, current persisted bracket revision, source provenance, and next operation.", "Stale/double/unauthorized/partial/degraded actions fail closed; peer failure is isolated.", "Follow handbook normal/restart/source recovery; wait for final ledgers, retry bounded actions, or rebuild a fresh preview. Never edit SQLite.", "League state/config, postseason truth/provenance, final scoring ledger, authorization, processors, health, activity, peer summaries.", "A scheduled meeting remains stopped until the commissioner intentionally confirms start; a bye advances without a fabricated opponent score.", "/admin")
	add(t)

	t = baseTopic("glossary", "concept-transition", "Concept transition from another fantasy platform", "Translate football questions into Gridiron's concepts without promising identical provider behavior.", []string{"migration", "transition", "import", "provider", "ESPN", "Yahoo", "Sleeper", "translation", "concept"}, []string{"moving leagues", "switching platforms", "what changed from my old league"}, []string{"migration guide", "provider mapping"})
	details(&t, "A manager or commissioner moving an existing workflow into a new Gridiron room.", "Agree on cutover and verify every new runtime rule before inviting managers.", "Brand-agnostic mappings for dynasty/redraft; no automatic account, roster, or history import.", "Mapping-reviewed, runtime-confirmed, not-applicable, unavailable, manual-exception.", "Old-provider timing is reference only; new deadlines come from runtime.", []string{"Freeze/reference old rules.", "Map identity, seats, rosters, lineups, acquisition, trades, scoring, privacy.", "Configure new league.", "Verify each identity/seat.", "Record exceptions as notes."}, "Do not paste passwords, private exports, or member addresses into public help/source.", "An alias such as owner or waiver budget does not imply identical permissions, deadlines, visibility, or scoring.", "Mappings are reviewable; new actions use their own correction path.", "Mapping names canonical term, alias, difference, source, privacy, consequence, next action.", "Missing/conflicting old data becomes an explicit manual exception.", "Return to concept map and /scoring or /admin; record a commissioner ruling.", "New league config/state; old artifacts are reference input only.", "Waiver budget maps to non-currency FAAB units whose use follows runtime rules.", "/guide#migration")
	add(t)

	t = baseTopic("glossary", "glossary", "Gridiron glossary", "Canonical product terms come first; incoming aliases remain searchable but do not override runtime meaning.", []string{"definitions", "terms", "vocabulary", "what does this mean", "aliases"}, []string{"dictionary", "term lookup", "what is a team seat"}, []string{"signal dictionary", "broadcast glossary"})
	details(&t, "Any viewer looking up a product or football term.", "None.", "All modes/phases; linked examples and values remain live.", "Available, not-applicable, unavailable when capability is not configured.", "Glossary has no deadline; linked actions use owning runtime route.", []string{"Search canonical term or alias.", "Read definition and consequence.", "Follow topic/action route.", "Use current league page for mutable values."}, "Definitions contain no private identity, membership, or transaction data.", "Canonical nouns prevent aliases from silently changing behavior.", "Reading is reversible; linked actions state their own boundary.", "One canonical definition and related topic are returned.", "Unknown terms receive search recovery rather than a guessed definition.", "Try canonical noun, normalize punctuation, or open concept transition.", "Versioned corpus plus owning runtime route for mutable examples.", "Owner resolves to primary manager; commissioner remains an independent overlay.", "/help")
	add(t)

	return out
}

var topicCorpus = buildCorpus()

func cloneTopic(t Topic) Topic {
	t.Audiences = append([]string(nil), t.Audiences...)
	t.IdentityStates = append([]string(nil), t.IdentityStates...)
	t.AdmissionStates = append([]string(nil), t.AdmissionStates...)
	t.TeamAssociations = append([]string(nil), t.TeamAssociations...)
	t.TeamRoles = append([]string(nil), t.TeamRoles...)
	t.CommissionerCapability = append([]string(nil), t.CommissionerCapability...)
	t.Modes = append([]string(nil), t.Modes...)
	t.Phases = append([]string(nil), t.Phases...)
	t.RequiredCapabilities = append([]string(nil), t.RequiredCapabilities...)
	t.DataStates = append([]string(nil), t.DataStates...)
	t.Keywords = append([]string(nil), t.Keywords...)
	t.Synonyms = append([]string(nil), t.Synonyms...)
	t.FlavorAliases = append([]string(nil), t.FlavorAliases...)
	t.Steps = append([]string(nil), t.Steps...)
	t.SourceRefs = append([]string(nil), t.SourceRefs...)
	return t
}

func TopicCorpus() []Topic {
	out := make([]Topic, len(topicCorpus))
	for i, t := range topicCorpus {
		out[i] = cloneTopic(t)
	}
	return out
}

func RequiredTopicIDs() []string {
	out := make([]string, 0, len(topicCorpus))
	for _, t := range topicCorpus {
		out = append(out, t.ID)
	}
	return out
}

func FindTopic(id string) (Topic, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, t := range topicCorpus {
		if t.ID == id {
			return cloneTopic(t), true
		}
	}
	return Topic{}, false
}

// NormalizeSearchQuery makes search stable across Unicode punctuation,
// apostrophes, hyphens, case, and whitespace.
func NormalizeSearchQuery(query string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(strings.TrimSpace(query)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			space = false
			continue
		}
		if b.Len() > 0 && !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}
func tokens(s string) []string { return strings.Fields(NormalizeSearchQuery(s)) }
func allTokens(q []string, fields ...string) bool {
	for _, want := range q {
		found := false
		for _, field := range fields {
			for _, got := range tokens(field) {
				if got == want {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func anyTokens(q []string, fields ...string) bool {
	for _, want := range q {
		for _, field := range fields {
			for _, got := range tokens(field) {
				if got == want {
					return true
				}
			}
		}
	}
	return false
}

type SearchResult struct {
	Topic Topic
	Score int
}

func searchScore(t Topic, query string) int {
	q, normalized := tokens(query), NormalizeSearchQuery(query)
	if len(q) == 0 {
		return 0
	}
	if NormalizeSearchQuery(t.ID) == normalized || NormalizeSearchQuery(t.Title) == normalized {
		return 1000
	}
	for _, alias := range append(append([]string{}, t.Synonyms...), t.FlavorAliases...) {
		if NormalizeSearchQuery(alias) == normalized {
			return 1000
		}
	}
	if allTokens(q, t.Title) {
		return 800
	}
	if allTokens(q, t.Synonyms...) || allTokens(q, t.FlavorAliases...) {
		return 760
	}
	if allTokens(q, t.Keywords...) {
		return 620
	}
	if anyTokens(q, t.Title) {
		return 500
	}
	if anyTokens(q, append(append(append([]string{}, t.Synonyms...), t.FlavorAliases...), t.Keywords...)...) {
		return 350
	}
	if anyTokens(q, t.Summary, t.Actor, t.Prerequisites, t.States, t.Example) {
		return 180
	}
	return 0
}
func categoryOrder(id string) int {
	for _, c := range categories {
		if c.ID == id {
			return c.Order
		}
	}
	return 999
}

// Search has no viewer-specific or recency-based ranking. Ties are category,
// normalized title, then topic ID.
func Search(query string) []SearchResult {
	var out []SearchResult
	for _, t := range topicCorpus {
		if score := searchScore(t, query); score > 0 {
			out = append(out, SearchResult{Topic: cloneTopic(t), Score: score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		a, b := out[i].Topic, out[j].Topic
		if categoryOrder(a.Category) != categoryOrder(b.Category) {
			return categoryOrder(a.Category) < categoryOrder(b.Category)
		}
		if NormalizeSearchQuery(a.Title) != NormalizeSearchQuery(b.Title) {
			return NormalizeSearchQuery(a.Title) < NormalizeSearchQuery(b.Title)
		}
		return a.ID < b.ID
	})
	return out
}
func SearchTop(query string) (Topic, bool) {
	r := Search(query)
	if len(r) == 0 {
		return Topic{}, false
	}
	return r[0].Topic, true
}

type ChecklistItem struct {
	ID, Role, Title, Detail, Predicate, ActionRoute string
	Applicable                                      bool
}

var baseChecklist = []ChecklistItem{
	{ID: "league-context", Role: "admitted-member", Title: "Confirm the active league", Detail: "Read configured name, mode, normalized phase, timezone, and next material deadline.", Predicate: "all admitted members", ActionRoute: "/"},
	{ID: "membership", Role: "admitted-member", Title: "Confirm membership and team association", Detail: "Membership may be valid while seatless; the page says what remains available.", Predicate: "all admitted members", ActionRoute: "/help/identity-admission-and-membership"},
	{ID: "rules-state", Role: "admitted-member", Title: "Review rules and data-state vocabulary", Detail: "Learn how LIVE, CACHED, STALE, DEGRADED, OFFLINE, UNAVAILABLE, and AWAITING RELEASE values are labeled.", Predicate: "all admitted members", ActionRoute: "/scoring"},
	{ID: "task-help", Role: "admitted-member", Title: "Keep the route back to the current task", Detail: "Contextual help preserves meaningful week, filter, form, and focus context.", Predicate: "all admitted members", ActionRoute: "/help"},
}
var roleChecklists = map[string][]ChecklistItem{
	"primary":    {{ID: "team-identity", Role: "primary-manager", Title: "Confirm team identity and seat", Detail: "Review display identity, roster shape, co-manager membership, and primary-only actions.", Predicate: "associated primary manager", ActionRoute: "/team"}, {ID: "lineup", Role: "primary-manager", Title: "Learn lineup locks", Detail: "Set unlocked starters before kickoff and read the exact lock reason.", Predicate: "associated primary manager", ActionRoute: "/help/lineups-locks-matchups-and-scoring"}, {ID: "board", Role: "primary-manager", Title: "Build the current Big Board", Detail: "The board is keyed per account; read which account/order AUTO consumes.", Predicate: "draft capability and applicable phase", ActionRoute: "/board"}, {ID: "workflows", Role: "primary-manager", Title: "Review applicable workflows", Detail: "Review players, waivers, trades, Pick'em, and Blitz only when capability/phase applies.", Predicate: "capability and phase predicates", ActionRoute: "/players"}},
	"co-manager": {{ID: "shared-seat", Role: "co-manager", Title: "Confirm the shared team seat", Detail: "Read primary relationship, permissions, private/team-visible data, and detach implications.", Predicate: "admitted co-manager", ActionRoute: "/help/roles-primary-co-manager-and-commissioner"}, {ID: "board-truth", Role: "co-manager", Title: "Read current board ownership truth", Detail: "The current board is per-account; do not infer shared visibility, merged ordering, attribution, or detach migration.", Predicate: "admitted co-manager", ActionRoute: "/board"}, {ID: "co-workflows", Role: "co-manager", Title: "Use the team's applicable workflows", Detail: "Review roster, lineup, acquisition, trade, Pick'em, and Blitz with disabled reasons.", Predicate: "capability and phase predicates", ActionRoute: "/team"}},
	"seatless":   {{ID: "seatless-valid", Role: "seatless-member", Title: "Understand seatless membership", Detail: "Read available league information and activity without being told to mutate a roster.", Predicate: "admitted seatless member", ActionRoute: "/help/teams-team-seats-and-rosters"}, {ID: "seatless-next", Role: "seatless-member", Title: "Use the actual assignment policy", Detail: "Choose/request/await a team only when the capability exists; otherwise contact the commissioner.", Predicate: "admission and assignment capability", ActionRoute: "/"}},
}
var commissionerChecklist = []ChecklistItem{
	{ID: "admission", Role: "commissioner-overlay", Title: "Review admission and assignments", Detail: "Name the league/object, capability, privacy, consequence, reversibility, evidence, and recovery path.", Predicate: "commissioner capability", ActionRoute: "/admin"},
	{ID: "draft-ops", Role: "commissioner-overlay", Title: "Review draft setup and readiness", Detail: "Verify seats, order, board gaps, pool health, and intentional start.", Predicate: "commissioner capability and draft phase", ActionRoute: "/draft"},
	{ID: "weekly-ops", Role: "commissioner-overlay", Title: "Review processing and close gates", Detail: "Use the owning league for waivers, trades, locks, week close, communications, and activity.", Predicate: "commissioner capability and applicable phase", ActionRoute: "/admin"},
	{ID: "playoff-truth", Role: "commissioner-overlay", Title: "Operate playoff truth", Detail: "Preview from final standings, review provenance and tie-break explanations, publish explicitly, advance only from the final ledger, and gate earlier-round corrections behind a fresh preview.", Predicate: "commissioner capability and playoffs phase", ActionRoute: "/admin?section=playoffs#admin-playoffs"},
	{ID: "recovery", Role: "commissioner-overlay", Title: "Keep the recovery sequence", Detail: "After refresh/restart/stale/peer failure, reread persisted state and never edit SQLite.", Predicate: "commissioner capability", ActionRoute: "/help/commissioner-operations"},
}

func ChecklistFor(role, mode, phase string, commissioner bool) []ChecklistItem {
	role, mode, phase = strings.ToLower(strings.TrimSpace(role)), strings.ToLower(strings.TrimSpace(mode)), strings.ToLower(strings.TrimSpace(phase))
	out := append([]ChecklistItem(nil), baseChecklist...)
	if role == "primary-manager" || role == "manager" {
		role = "primary"
	}
	if role == "comanager" {
		role = "co-manager"
	}
	out = append(out, roleChecklists[role]...)
	if commissioner {
		out = append(out, commissionerChecklist...)
	}
	for i := range out {
		out[i].Applicable = true
		if strings.Contains(out[i].Predicate, "draft") && phase != "" && phase != "pre-draft" && phase != "draft" {
			out[i].Applicable = false
		}
		if mode != "" && mode != "dynasty" && mode != "redraft" && mode != "configured" {
			out[i].Applicable = false
		}
	}
	return out
}

type MigrationMapping struct {
	Canonical                                                               string
	IncomingAliases                                                         []string
	Equivalent, Difference, RuntimeSource, Privacy, Consequence, NextAction string
}

var migrationMappings = []MigrationMapping{
	{Canonical: "identity and admission", IncomingAliases: []string{"account", "invite", "league access"}, Equivalent: "Sign-in identifies the person requesting access.", Difference: "Admission and team association are separate; an old login does not transfer.", RuntimeSource: "OAuth identity, membership policy, aliases, membership rows.", Privacy: "Never paste passwords or private address lists into help.", Consequence: "A rejected identity cannot claim a team.", NextAction: "Use the current login result or ask the commissioner to correct admission."},
	{Canonical: "team and team seat", IncomingAliases: []string{"franchise", "club", "owner slot"}, Equivalent: "A durable football team manages the roster.", Difference: "A seat may be shared by primary/co-manager; commissioner capability is orthogonal.", RuntimeSource: "Teams, memberships, role, co-manager configuration.", Privacy: "Team data is not an identity-owned public record.", Consequence: "Seatless members cannot perform roster mutations.", NextAction: "Open Team and read persisted association before claiming."},
	{Canonical: "roster and lineup", IncomingAliases: []string{"fantasy roster", "starters", "bench"}, Equivalent: "Managers select players into configured slots.", Difference: "Each slot locks from kickoff; final weeks pin effective starters.", RuntimeSource: "Roster preset, schedule, lineup state, processor.", Privacy: "Visibility follows league projection.", Consequence: "Locked/final slots reject normal edits.", NextAction: "Use Team and read lock label."},
	{Canonical: "Big Board", IncomingAliases: []string{"draft queue", "draft board", "rankings"}, Equivalent: "A private ordered list can guide AUTO.", Difference: "The current implementation is per-account; do not promise shared co-manager queue.", RuntimeSource: "Per-account board, player pool, autopick selector.", Privacy: "Board order is team/account-scoped.", Consequence: "AUTO uses current account's first available target, then best available.", NextAction: "Build the board and verify Draft copy."},
	{Canonical: "free agent and waiver claim", IncomingAliases: []string{"pickup", "waiver wire", "claim"}, Equivalent: "An eligible manager requests a player.", Difference: "A true free agent may add immediately; others follow displayed process.", RuntimeSource: "Acquisition rules, player state, roster, queue, processor.", Privacy: "Claim/bid visibility is configured.", Consequence: "Capacity/eligibility/units can change before processing.", NextAction: "Use Players and preserve filter."},
	{Canonical: "FAAB units", IncomingAliases: []string{"waiver budget", "bid units", "blind bidding"}, Equivalent: "Numeric claim-allocation units can rank requests.", Difference: "FAAB is not currency; spending, ties, and visibility are league rules.", RuntimeSource: "Waiver config, team balance, claim processor.", Privacy: "Do not expose another team's bid.", Consequence: "Insufficient units fail without roster change.", NextAction: "Read the configured non-currency label in Players."},
	{Canonical: "trade review", IncomingAliases: []string{"trade veto", "league vote", "trade desk"}, Equivalent: "A proposed exchange can require response/review.", Difference: "Deadlines, locks, review, veto, and processing are independent settings.", RuntimeSource: "Trade rules, offer state, rosters, locks, activity.", Privacy: "Terms are visible only to configured audience.", Consequence: "Accepted does not necessarily mean processed.", NextAction: "Use Trades and read offer state."},
	{Canonical: "scoring and matchup", IncomingAliases: []string{"fantasy points", "standings", "score"}, Equivalent: "A schedule-backed matchup reports team results.", Difference: "Provisional/stale/degraded/final/corrected are explicit; unknown is not zero.", RuntimeSource: "Scoring config, schedule, stat ledger, correction state.", Privacy: "Roster visibility follows league.", Consequence: "Final weeks pin effective starters/results.", NextAction: "Read Scoring and Matchups."},
	{Canonical: "playoff bracket and truth", IncomingAliases: []string{"ESPN playoffs", "Yahoo playoff bracket", "Sleeper playoffs", "NFL fantasy playoffs", "postseason bracket"}, Equivalent: "A configured postseason bracket resolves seeds, byes, rounds, and champion.", Difference: "Gridiron requires commissioner preview before publication and advances only from the final authoritative starter ledger; provider dates and tie rules do not transfer automatically.", RuntimeSource: "Postseason config, persisted PlayoffTruth, standings provenance, final scoring ledger, audit.", Privacy: "Public projection is PII-free; commissioner preview and correction controls are authorization-gated.", Consequence: "Earlier-round corrections require a fresh preview so downstream participants never remain stale.", NextAction: "Open Matchups for published truth or the commissioner Playoff Truth panel for preview/advance/recovery."},
}

func MigrationMappings() []MigrationMapping {
	out := make([]MigrationMapping, len(migrationMappings))
	copy(out, migrationMappings)
	for i := range out {
		out[i].IncomingAliases = append([]string(nil), out[i].IncomingAliases...)
	}
	return out
}

type GlossaryEntry struct {
	Term                string
	Aliases             []string
	Definition, TopicID string
}

var glossaryEntries = []GlossaryEntry{
	{Term: "person", Definition: "This is you, the person using the app. The human using Gridiron.", TopicID: "identity-admission-and-membership"}, {Term: "identity", Definition: "This is the signed-in account behind your actions. The authenticated account representing a person.", TopicID: "identity-admission-and-membership"}, {Term: "admission", Definition: "This is whether the league lets your account in. The policy decision granting a signed-in identity league access.", TopicID: "identity-admission-and-membership"}, {Term: "membership", Definition: "This is your saved place in the league, even before you have a team. The persisted league relationship after admission; it may be seatless.", TopicID: "identity-admission-and-membership"},
	{Term: "team", Aliases: []string{"franchise", "club"}, Definition: "This is one manager's fantasy team. The durable football object owning a roster.", TopicID: "teams-team-seats-and-rosters"}, {Term: "team association", Definition: "The link between an admitted identity and a team.", TopicID: "teams-team-seats-and-rosters"}, {Term: "team seat", Aliases: []string{"seat"}, Definition: "This is the job of running one team's roster. Team-scoped responsibility for managing a roster.", TopicID: "teams-team-seats-and-rosters"}, {Term: "primary manager", Aliases: []string{"owner"}, Definition: "The primary role associated with a team seat.", TopicID: "roles-primary-co-manager-and-commissioner"}, {Term: "co-manager", Definition: "An optional second manager sharing a team seat under configured permissions.", TopicID: "roles-primary-co-manager-and-commissioner"}, {Term: "commissioner", Definition: "This is the person who runs the league's settings and rules. A person with an orthogonal league-operations capability.", TopicID: "roles-primary-co-manager-and-commissioner"}, {Term: "commissioner capability", Definition: "Authorization for league operations; it does not substitute for membership/team.", TopicID: "roles-primary-co-manager-and-commissioner"},
	{Term: "roster", Definition: "This is the full list of players on your team. Players assigned to a team's configured slots/zones.", TopicID: "teams-team-seats-and-rosters"}, {Term: "lineup", Definition: "This is the players you start this week. The effective starter/bench arrangement for a week.", TopicID: "lineups-locks-matchups-and-scoring"}, {Term: "lineup slot", Definition: "A configured position or zone holding a player.", TopicID: "lineups-locks-matchups-and-scoring"}, {Term: "lineup lock", Definition: "The kickoff/state boundary preventing normal slot edits.", TopicID: "lineups-locks-matchups-and-scoring"}, {Term: "matchup", Definition: "This is your team's game against another team this week. A schedule-backed comparison of two teams for a week.", TopicID: "lineups-locks-matchups-and-scoring"}, {Term: "schedule", Definition: "Runtime game/week calendar supplying kickoffs and timing.", TopicID: "lineups-locks-matchups-and-scoring"}, {Term: "projection", Definition: "This is a guess at a player's points, not a final score. A non-authoritative estimate, never a final score.", TopicID: "lineups-locks-matchups-and-scoring"}, {Term: "provisional score", Definition: "A score calculated before finalization.", TopicID: "lineups-locks-matchups-and-scoring"}, {Term: "final score", Definition: "A score pinned by finalization/close processing.", TopicID: "lineups-locks-matchups-and-scoring"}, {Term: "stat correction", Definition: "A later authoritative change to a published stat/result.", TopicID: "lineups-locks-matchups-and-scoring"},
	{Term: "playoff truth", Definition: "This is the one official playoff bracket every manager sees. The one persisted bracket projection shared by member and commissioner surfaces.", TopicID: "commissioner-operations"}, {Term: "playoff preview", Definition: "A commissioner-only bracket candidate; preview is not publication.", TopicID: "commissioner-operations"}, {Term: "bye", Definition: "This is a bracket slot that skips a game because no opponent is left. A bracket slot that advances its seeded team without an opponent score.", TopicID: "commissioner-operations"}, {Term: "bracket provenance", Definition: "The final-standings snapshot, source state, capture time, and tie-break order behind a bracket.", TopicID: "commissioner-operations"}, {Term: "authoritative ledger", Definition: "A complete final starter-scoring ledger accepted as the source for weekly advancement.", TopicID: "commissioner-operations"}, {Term: "confirmed correction", Definition: "An audited commissioner correction at an allowed terminal boundary.", TopicID: "commissioner-operations"},
	{Term: "player pool", Definition: "This is every player the league can draft or add. Source-backed players available to the league.", TopicID: "players-free-agents-waivers-and-faab"}, {Term: "player", Definition: "A canonical football participant in the pool.", TopicID: "players-free-agents-waivers-and-faab"}, {Term: "free agent", Definition: "An eligible unrostered player available when rules allow.", TopicID: "players-free-agents-waivers-and-faab"}, {Term: "waiver", Definition: "This is a short wait before you can add a dropped or free player. The delayed acquisition process for an ineligible-immediate player.", TopicID: "players-free-agents-waivers-and-faab"}, {Term: "claim", Definition: "This is your request to add a player who is on waivers. A submitted request for a waiver player.", TopicID: "players-free-agents-waivers-and-faab"}, {Term: "claim priority", Definition: "The runtime ordering rule resolving claims.", TopicID: "players-free-agents-waivers-and-faab"}, {Term: "FAAB units", Aliases: []string{"waiver budget", "bid units"}, Definition: "FAAB stands for Free Agent Acquisition Budget: play money you bid to win a waiver claim. Non-currency units used by a configured claim processor.", TopicID: "players-free-agents-waivers-and-faab"},
	{Term: "Big Board", Aliases: []string{"draft board", "queue"}, Definition: "This is your own private ranked list of players you want. The current account's private ordered draft targets used by AUTO first.", TopicID: "big-board-and-autopick"}, {Term: "Ready", Definition: "A persisted manager signal that never starts the draft.", TopicID: "draft-order-readiness-and-clock"}, {Term: "draft order", Definition: "This is the set order teams pick in. The configured sequence of team picks.", TopicID: "draft-order-readiness-and-clock"}, {Term: "on the clock", Definition: "This is the team that must pick right now. The current persisted team eligible to select.", TopicID: "draft-order-readiness-and-clock"}, {Term: "pick", Definition: "This is one player your team drafted, in order. One persisted draft selection with team/player/order.", TopicID: "draft-order-readiness-and-clock"}, {Term: "autopick", Definition: "This is the app picking for you from your board, or the best player left. AUTO selects current account's first available board target, then best available.", TopicID: "big-board-and-autopick"},
	{Term: "trade", Definition: "This is an offer to swap players with another team. A proposed exchange between team seats.", TopicID: "trades-review-and-processing"}, {Term: "counter", Definition: "A replacement trade proposal responding to an offer.", TopicID: "trades-review-and-processing"}, {Term: "review", Definition: "Configured period/authority evaluating an accepted trade.", TopicID: "trades-review-and-processing"}, {Term: "veto", Definition: "This is the league blocking a trade during review. Configured review outcome rejecting a trade.", TopicID: "trades-review-and-processing"}, {Term: "activity", Definition: "This is the running log of what happened in the league. Persisted event history for manager, processor, commissioner, and correction actions.", TopicID: "activity-and-commissioner-notes"}, {Term: "audit", Definition: "Evidence trail explaining a consequential change.", TopicID: "activity-and-commissioner-notes"}, {Term: "Commissioner Note", Definition: "Operator explanation attached to a decision; it does not replace runtime state.", TopicID: "activity-and-commissioner-notes"}, {Term: "Players", Definition: "Route showing pool, free agents, waivers, and claims.", TopicID: "players-free-agents-waivers-and-faab"}, {Term: "Signal Wire", Definition: "Provisional mixed-source signals that never mutate fantasy scores.", TopicID: "data-state-and-freshness"}, {Term: "Pick'em", Definition: "Independent against-the-spread game with per-game locks and W-L-P.", TopicID: "pickem"}, {Term: "Preseason Blitz", Definition: "Bounded preseason side contest with its own slate and locks.", TopicID: "preseason-blitz"}, {Term: "league mode", Definition: "Configured format label such as dynasty or redraft.", TopicID: "getting-started"}, {Term: "normalized phase", Definition: "Canonical lifecycle token such as pre-draft or regular-season.", TopicID: "getting-started"}, {Term: "feature capability", Definition: "Runtime-supported, unsupported, or temporarily unavailable workflow status.", TopicID: "getting-started"}, {Term: "runtime source", Definition: "Executable config/state/auth/processor/feed owning a value.", TopicID: "data-state-and-freshness"},
	{Term: "live", Definition: "This means the data is fresh and current right now. A fresh successful source or active workflow state; read its adjacent context.", TopicID: "data-state-and-freshness"}, {Term: "loading", Definition: "A read or action is still in progress; do not duplicate a mutation.", TopicID: "data-state-and-freshness"}, {Term: "empty", Definition: "This means there is nothing here yet, and that is normal. A valid collection contains zero items.", TopicID: "data-state-and-freshness"}, {Term: "no-results", Aliases: []string{"no results"}, Definition: "The current query/filter matches nothing; the collection may still contain records.", TopicID: "data-state-and-freshness"}, {Term: "pending", Definition: "Work awaits a deadline, review, or processor.", TopicID: "data-state-and-freshness"}, {Term: "locked", Definition: "This means a rule or deadline stops you from changing it. A time, rule, or final boundary prevents ordinary change.", TopicID: "data-state-and-freshness"}, {Term: "disabled", Definition: "A role, prerequisite, or capability prevents the action.", TopicID: "data-state-and-freshness"}, {Term: "stale", Definition: "This means the data is old and due for a refresh. A last-good snapshot is beyond its freshness window.", TopicID: "data-state-and-freshness"}, {Term: "degraded", Definition: "A source or partial read failed while last-good data remains.", TopicID: "data-state-and-freshness"}, {Term: "offline", Definition: "The source is knowingly offline or a fallback is serving.", TopicID: "data-state-and-freshness"}, {Term: "unavailable", Definition: "No reliable value exists for the requested scope.", TopicID: "data-state-and-freshness"}, {Term: "failed", Definition: "An attempted read or action failed and its effect must be stated.", TopicID: "data-state-and-freshness"}, {Term: "permission-denied", Aliases: []string{"permission denied"}, Definition: "The current identity lacks authorization.", TopicID: "data-state-and-freshness"}, {Term: "not-applicable", Aliases: []string{"not applicable"}, Definition: "Mode, phase, role, or capability makes the topic irrelevant.", TopicID: "data-state-and-freshness"},
	{Term: "ADP", Aliases: []string{"average draft position"}, Definition: "This is where the wider fantasy market usually drafts a player. ADP (average draft position) ranks players by that market consensus, not by this league's own points.", TopicID: "big-board-and-autopick"}, {Term: "snake draft", Aliases: []string{"snake"}, Definition: "This is a draft order that reverses every round, so the last pick in round 1 picks first in round 2. It gives every team one early pick and one late pick per two rounds, instead of the same team always picking last.", TopicID: "draft-order-readiness-and-clock"}, {Term: "FLEX", Definition: "This is a lineup slot that takes a player from more than one position, most often RB, WR, or TE. It gives a manager one extra flexible starter beyond the position-locked slots.", TopicID: "lineups-locks-matchups-and-scoring"}, {Term: "SUPERFLEX", Aliases: []string{"super flex"}, Definition: "This is a FLEX slot that also accepts a QB, not only RB, WR, or TE. A league with a SUPERFLEX slot usually values quarterbacks much higher than a league without one.", TopicID: "lineups-locks-matchups-and-scoring"}, {Term: "PF", Aliases: []string{"points for"}, Definition: "This is the total points your team has scored all season. PF (points for) is the running season total; a single week's matchup score is separate.", TopicID: "lineups-locks-matchups-and-scoring"}, {Term: "IR", Aliases: []string{"injured reserve"}, Definition: "This is a roster spot for an injured player that does not use a normal bench slot. Placing a player on IR (injured reserve) frees a bench spot while that designation still qualifies.", TopicID: "teams-team-seats-and-rosters"},
}

func Glossary() []GlossaryEntry {
	out := make([]GlossaryEntry, len(glossaryEntries))
	copy(out, glossaryEntries)
	for i := range out {
		out[i].Aliases = append([]string(nil), out[i].Aliases...)
	}
	return out
}

type StateGuidance struct{ State, Why, Impact, Remaining, PreservedContext, NextAction, Retry, LastSuccess, TopicID string }

var stateGuidance = map[string]StateGuidance{
	"loading":           {State: "loading", Why: "The read/action is still in progress.", Impact: "The value is not final yet.", Remaining: "Keep the route and prior safe content.", PreservedContext: "Preserve route, query, form, and focus.", NextAction: "Wait for local status.", Retry: "Do not duplicate a mutation while loading."},
	"empty":             {State: "empty", Why: "The valid collection has zero items.", Impact: "Nothing is selectable yet.", Remaining: "Create/discovery and navigation remain available.", PreservedContext: "Keep league/team/week context.", NextAction: "Use the first meaningful create/discovery action.", Retry: "Retry only if source could change."},
	"no-results":        {State: "no-results", Why: "The current filter/query matches nothing.", Impact: "The collection may still contain records.", Remaining: "Clear/reset or change the query.", PreservedContext: "Preserve query, filters, tab, and anchor.", NextAction: "Clear filters or broaden the search.", Retry: "Retry with a new query; do not call collection empty."},
	"pending":           {State: "pending", Why: "Work awaits a deadline, review, or processor.", Impact: "Final consequence has not occurred.", Remaining: "Read submitted state/time and unaffected actions.", PreservedContext: "Preserve submitted form and owning route.", NextAction: "Wait for displayed boundary.", Retry: "Do not resubmit; edit/cancel only when shown."},
	"saved":             {State: "saved", Why: "Persistence succeeded.", Impact: "The named object has the saved effect.", Remaining: "Continue and use undo/edit if offered.", PreservedContext: "Keep week/team/filter/focus on result.", NextAction: "Read exact saved consequence.", Retry: "Refresh to reconcile; do not replay."},
	"locked":            {State: "locked", Why: "A time/rule/final boundary prevents change.", Impact: "The mutation cannot apply now.", Remaining: "Read locked reason and permitted route.", PreservedContext: "Preserve object/week/form/query.", NextAction: "Wait for unlock or use commissioner path.", Retry: "Retry only after boundary changes."},
	"disabled":          {State: "disabled", Why: "Role, prerequisite, or capability prevents action.", Impact: "Submission must not mutate state.", Remaining: "Explanation and prerequisite remain available.", PreservedContext: "Keep route/task context.", NextAction: "Complete prerequisite or ask authorized operator.", Retry: "Do not retry as another identity."},
	"stale":             {State: "stale", Why: "Last-success data is beyond freshness window.", Impact: "Snapshot informs but is not live.", Remaining: "Use unaffected fields and age.", PreservedContext: "Preserve filters/week/object.", NextAction: "Manual refresh or wait for source.", Retry: "Bounded retry; never fabricate fresh value.", LastSuccess: "Show exact runtime last-success time and age."},
	"degraded":          {State: "degraded", Why: "A source/partial read failed while last-good data remains.", Impact: "Some fields are unknown/provisional.", Remaining: "Preserve unaffected content.", PreservedContext: "Preserve task/query/form/object.", NextAction: "Retry affected source or use alternate route.", Retry: "Do not replace unknown with zero.", LastSuccess: "Show last-success time/age when known."},
	"offline":           {State: "offline", Why: "Source/service is knowingly offline or fallback is serving.", Impact: "Live enrichment/mutation may be unavailable.", Remaining: "Use explicitly labeled rehearsal/cache content.", PreservedContext: "Preserve route and pending inputs.", NextAction: "Inspect source and wait for recovery.", Retry: "Retry after source reports available.", LastSuccess: "Show last-success and fallback source."},
	"unavailable":       {State: "unavailable", Why: "No reliable value exists for the scope.", Impact: "The source cannot authorize a fact/action.", Remaining: "Use unaffected routes and escalation.", PreservedContext: "Preserve league/team/week/filter.", NextAction: "Wait, retry, or use alternate route.", Retry: "Bounded retry; never fabricate zero/live.", LastSuccess: "Say when no last-success exists."},
	"failed":            {State: "failed", Why: "An attempted read/action failed.", Impact: "Effect/no-effect is stated.", Remaining: "Safe form/query and unaffected content remain.", PreservedContext: "Preserve return target/query/form/focus.", NextAction: "Read local error and recovery route.", Retry: "Retry from freshly rendered state; never replay stale mutation.", LastSuccess: "Show source last-success for failed reads."},
	"permission-denied": {State: "permission-denied", Why: "Identity lacks authorization.", Impact: "Protected data/mutation is not exposed/applied.", Remaining: "Valid request/help route remains.", PreservedContext: "Preserve only safe same-origin context.", NextAction: "Use admission/role request path.", Retry: "Do not retry as another role or leak details."},
	"not-applicable":    {State: "not-applicable", Why: "Mode, phase, role, or capability makes topic irrelevant.", Impact: "There is no obligation/control.", Remaining: "Read predicate and continue applicable checklist.", PreservedContext: "Preserve route if opened directly.", NextAction: "Return to applicable task/phase.", Retry: "No retry unless capability changes."},
}

func Guidance(topicID, state string) StateGuidance {
	state = strings.ReplaceAll(NormalizeSearchQuery(state), " ", "-")
	g, ok := stateGuidance[state]
	if !ok {
		g = stateGuidance["unavailable"]
		g.State = state
	}
	g.TopicID = topicID
	return g
}

// ContextualFieldHelp keeps field/validation copy tied to the same topic
// metadata as the browse and action projections. It explains ownership and
// the safe next route without copying a mutable value such as a date, amount,
// score, or deadline.
func ContextualFieldHelp(topicID, field string) map[string]string {
	topic, ok := FindTopic(topicID)
	if !ok {
		return map[string]string{
			"field": "field", "label": "Current field",
			"help":           "This field is not available in the current help corpus.",
			"runtime_source": "Current runtime route",
			"next_action":    "/help",
		}
	}
	normalized := strings.ReplaceAll(NormalizeSearchQuery(field), " ", "-")
	label := strings.TrimSpace(field)
	if label == "" {
		label = "current state"
		normalized = "state"
	}
	help := "Read this value from the current owning page; help explains the contract but does not replace runtime truth."
	source := topic.RuntimeSource
	next := topic.ActionRoute
	switch normalized {
	case "deadline", "time", "timezone", "lock":
		help = "The current deadline, lock, and timezone are runtime-owned. Read the exact value and relative text beside the action before submitting."
	case "state", "status", "freshness":
		help = "The state label explains what happened, what remains available, and whether a retry is safe. Unknown is not zero and stale is not live."
	case "role", "actor", "permission":
		help = "The actor and capability predicate decide whether this action is available. Do not retry as another role; use the admission or commissioner path shown by the page."
	case "privacy", "visibility":
		help = "Visibility follows the current league policy and account/seat boundary. Do not paste private identity, board, claim, or transaction data into public help."
	case "action", "next":
		help = "Use the owning action after rereading its current state. A help link explains the next move; it does not authorize a mutation."
	}
	return map[string]string{
		"field": normalized, "label": label, "help": help,
		"runtime_source": source, "next_action": next,
	}
}

func StateNames() []string {
	out := make([]string, 0, len(stateGuidance))
	for key := range stateGuidance {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func ValidateCorpus() error {
	seen := map[string]bool{}
	for _, t := range topicCorpus {
		if t.ID == "" || t.Title == "" || t.Category == "" || seen[t.ID] {
			return fmt.Errorf("invalid or duplicate topic %q", t.ID)
		}
		seen[t.ID] = true
		if t.IntroducedVersion == "" || t.LastVerifiedSHA == "" || len(t.SourceRefs) == 0 || t.ActionRoute == "" || t.RuntimeSource == "" || t.Recovery == "" {
			return fmt.Errorf("topic %q missing metadata/contract", t.ID)
		}
	}
	for _, e := range glossaryEntries {
		if e.Term == "" || e.Definition == "" || e.TopicID == "" {
			return fmt.Errorf("incomplete glossary entry %q", e.Term)
		}
		if _, ok := FindTopic(e.TopicID); !ok {
			return fmt.Errorf("glossary %q references unknown topic %q", e.Term, e.TopicID)
		}
	}
	return nil
}
