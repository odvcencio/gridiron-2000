package scoring

// ScoringRuleRow structurally mirrors internal/league's ScoringRuleRow (the
// loader's actual type) so ScoringRow's named "rule" prop proves at the
// strict-call boundary: the file renderer requires a named struct-typed
// attribute's runtime value to carry the exact type name declared here
// (requireStrictStructValue), by name only, so the two declarations may
// live in different packages as long as they agree on that name and on
// every field this component reads.
type ScoringRuleRow struct {
	Key       string
	Label     string
	Points    string
	IsDefault bool
}

type ScoringRowProps struct {
	Rule      ScoringRuleRow
	Editable  bool
	SetAction string
	CSRF      string
}

component ScoringRow(props: ScoringRowProps) {
	return <div class="scoring-row">
		<div class="scoring-row__label">
			<strong>{props.Rule.Label}</strong>
			<If cond={props.Rule.IsDefault == false}>
				<span class="position-chip">CUSTOM</span>
			</If>
		</div>
		<If cond={props.Editable == false}>
			<b class="mono">{props.Rule.Points}</b>
		</If>
		<If cond={props.Editable}>
			<form class="scoring-row__form" method="post" action={props.SetAction} data-gosx-managed="true">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="key" value={props.Rule.Key}></input>
				<input type="number" name="points" value={props.Rule.Points} class="scoring-input" step="any" min="-25" max="25" inputmode="decimal"></input>
				<button class="board-button" type="submit">Set</button>
			</form>
		</If>
	</div>
}

func Page() Node {
	return <main class="page scoring-page" id="main-content">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="signal-mark" aria-hidden="true"></span>
					RULES &amp; SCORING //
					{data.league_mode}
				</span>
				<h1>
					HOW THE
					<br></br>
					LEAGUE RUNS.
				</h1>
				<p>
					Every rule below matches exactly how this league is set up right now — roster shape, scoring values, the draft, lineups, waivers, trades, and pick'em. A new manager can read this page start to finish and play with no questions left.
				</p>
			</div>
			<div class="draft-clock-panel">
				<If cond={data.locked == false}>
					<span>Scoring editable until</span>
					<strong class="mono">{data.season_start}</strong>
				</If>
				<If cond={data.locked}>
					<span>Scoring status</span>
					<strong class="mono">LOCKED</strong>
				</If>
				<div class="draft-clock-meta">
					<If cond={data.is_commissioner}>
						<a href="/admin" data-gosx-link>Admin console →</a>
					</If>
				</div>
			</div>
		</section>
		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_notice}>
				<p class="flash-message">{data.notice}</p>
			</If>
			<If cond={data.has_scoring_error}>
				<p class="error-message">{data.scoring_error}</p>
			</If>
			<If cond={data.locked}>
				<p class="demo-message">
					<strong>SEASON LOCK:</strong>
					scoring is frozen for the season.
				</p>
			</If>
		</div>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">01 // LEAGUE</span>
					<h2>League identity</h2>
				</div>
			</div>
			<p class="scoring-note">
				{data.identity_rules.name}
				({data.identity_rules.short_code}) runs a
				{data.identity_rules.mode_label}
				format for the
				{data.identity_rules.season}
				season, across
				{data.identity_rules.team_count}
				teams.
			</p>
			<div class="pool-stats">
				<div class="pool-stat">
					<span>Timezone</span>
					<b class="mono">{data.identity_rules.timezone}</b>
				</div>
				<div class="pool-stat">
					<span>Draft date</span>
					<b class="mono">{data.identity_rules.draft_date}</b>
				</div>
				<div class="pool-stat">
					<span>Season start</span>
					<b class="mono">{data.identity_rules.season_start}</b>
				</div>
			</div>
		</section>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">02 // MEMBERSHIP</span>
					<h2>Who can join</h2>
				</div>
			</div>
			<p class="scoring-note">
				<b class="mono">{data.membership_rules.label}</b>
				— {data.membership_rules.detail}
			</p>
			<div class="pool-stats">
				<div class="pool-stat">
					<span>Seats claimed</span>
					<b class="mono">
						{data.membership_rules.claimed_seats}
						/
						{data.membership_rules.seat_count}
					</b>
				</div>
			</div>
		</section>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">03 // ROSTER</span>
					<h2>Starting lineup and bench</h2>
				</div>
			</div>
			<div class="pool-list">
				<Each of={data.roster_rules.slots} as="slot">
					<div class="scoring-row">
						<div class="scoring-row__label">
							<strong>
								{slot.key}
								·
								{slot.count}
							</strong>
							<span class="position-chip">{slot.note}</span>
						</div>
					</div>
				</Each>
			</div>
			<p class="scoring-note">
				{data.roster_rules.starters}
				starters +
				{data.roster_rules.bench}
				bench =
				{data.roster_rules.total}
				roster spots, which is also the
				{data.roster_rules.rounds}
				-round draft. Draft rounds derive from the roster shape; they are never set independently.
			</p>
		</section>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">04 // DRAFT</span>
					<h2>Startup snake draft</h2>
				</div>
			</div>
			<div class="pool-stats">
				<div class="pool-stat">
					<span>Date</span>
					<b class="mono">
						{data.draft_rules.date}
						·
						{data.draft_rules.time}
					</b>
				</div>
				<div class="pool-stat">
					<span>Timezone</span>
					<b class="mono">{data.draft_rules.timezone}</b>
				</div>
				<div class="pool-stat">
					<span>Format</span>
					<b class="mono">{data.draft_rules.format}</b>
				</div>
				<div class="pool-stat">
					<span>Pick clock</span>
					<b class="mono">
						{data.draft_rules.clock_seconds}
						S
					</b>
				</div>
			</div>
			<p class="scoring-note">
				An away or idle manager's pick fires automatically from their Big Board, or best available by ADP when the board is empty. The commissioner can undo the most recent pick and reopen that slot.
			</p>
		</section>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">05 // LINEUPS &amp; LOCKS</span>
					<h2>Setting your lineup</h2>
				</div>
			</div>
			<p class="scoring-note">
				Each player locks at their own NFL team's kickoff, not one league-wide lock time — a Sunday player can still be swapped Monday morning if their game has not kicked off yet.
			</p>
			<p class="scoring-note">
				An empty or locked-but-suboptimal slot auto-fills from your bench: the highest-projection eligible player who is not on a bye and carries no injury warning, when one exists. A slot flags a warning when it is empty, on a bye week, or when the player's injury note starts with
				{data.lineup_rules.warn_prefixes}
				.
			</p>
			<p class="scoring-note">
				The commissioner can set any team's lineup on a missing manager's behalf; this never locks out the manager's own changes once they return.
			</p>
			<a href="/team" data-gosx-link class="button button--compact">Open your team terminal →</a>
		</section>
		<Each of={data.groups} as="group">
			<section class="player-pool">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">
							06 // SCORING //
							{group.Name}
						</span>
						<h2>{group.Name}</h2>
					</div>
				</div>
				<If cond={group.Note != ""}>
					<p class="scoring-note">{group.Note}</p>
				</If>
				<div class="pool-list">
					<Each of={group.Rules} as="row">
						<ScoringRow {...row}></ScoringRow>
					</Each>
				</div>
			</section>
		</Each>
		<If cond={data.editable}>
			<section class="player-pool admin-danger">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">RESET // SCORING</span>
						<h2>Danger zone</h2>
					</div>
				</div>
				<div class="danger-grid">
					<form method="post" action={actionPath("scoring-reset")} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<strong>Reset scoring</strong>
						<p>Restores every rule to the league defaults. Custom values are lost.</p>
						<input type="text" name="confirm" placeholder="type RESET" autocomplete="off"></input>
						<button class="button" type="submit">Reset scoring</button>
					</form>
				</div>
			</section>
		</If>
		<p class="scoring-note">{data.scoring_note}</p>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">07 // WEEK CLOSE</span>
					<h2>Schedule, playoffs, and closing a week</h2>
				</div>
			</div>
			<div class="pool-stats">
				<div class="pool-stat">
					<span>Current phase</span>
					<b class="mono">{data.season_rules.phase}</b>
				</div>
			</div>
			<If cond={data.season_rules.schedule_generated}>
				<p class="scoring-note">
					{data.season_rules.weeks}
					regular-season weeks, starting NFL week
					{data.season_rules.start_week}
					.
				</p>
			</If>
			<If cond={data.season_rules.schedule_generated == false}>
				<p class="scoring-note">No regular-season schedule has been generated yet.</p>
			</If>
			<If cond={data.season_rules.playoffs_seeded}>
				<p class="scoring-note">
					{data.season_rules.playoff_teams}
					-team playoff bracket, starting NFL week
					{data.season_rules.playoff_start_week}
					, each round
					{data.season_rules.playoff_round_weeks}
					week(s).
				</p>
			</If>
			<If cond={data.season_rules.playoffs_seeded == false}>
				<p class="scoring-note">No playoff bracket has been seeded yet.</p>
			</If>
			<p class="scoring-note">
				Closing a week scores every matchup and marks it final. The same close also pins every starting lineup that scored that week — a later drop, trade, or roster-shape edit can never change a closed week's score.
			</p>
		</section>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">08 // FREE AGENCY</span>
					<h2>Signing free agents</h2>
				</div>
			</div>
			<div class="pool-stats">
				<div class="pool-stat">
					<span>Status</span>
					<If cond={data.free_agency_rules.open}>
						<b class="mono">OPEN</b>
					</If>
					<If cond={data.free_agency_rules.open == false}>
						<b class="mono">NOT YET OPEN</b>
					</If>
				</div>
				<div class="pool-stat">
					<span>Roster cap</span>
					<b class="mono">{data.free_agency_rules.roster_cap}</b>
				</div>
			</div>
			<p class="scoring-note">
				Free agency opens the moment the draft fills every roster spot on every team. Signing a free agent onto a full roster requires naming a drop in the same move — one single step, never a separate add and drop.
			</p>
			<a href="/players" data-gosx-link class="button button--compact">Open the player pool →</a>
		</section>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">09 // WAIVERS</span>
					<h2>Claiming a dropped or locked player</h2>
				</div>
			</div>
			<div class="pool-stats">
				<div class="pool-stat">
					<span>Mode</span>
					<b class="mono">{data.waivers_rules.mode}</b>
				</div>
				<If cond={data.waivers_rules.faab}>
					<div class="pool-stat">
						<span>FAAB budget</span>
						<b class="mono">
							{data.waivers_rules.faab_budget}
							{" "}FAAB
						</b>
					</div>
				</If>
				<If cond={data.waivers_rules.faab == false}>
					<div class="pool-stat">
						<span>Season weight</span>
						<b class="mono">
							{data.waivers_rules.season_weight_pct}
							%
						</b>
					</div>
				</If>
				<If cond={data.waivers_rules.faab == false}>
					<div class="pool-stat">
						<span>This-week weight</span>
						<b class="mono">
							{data.waivers_rules.weekly_weight_pct}
							%
						</b>
					</div>
				</If>
				<div class="pool-stat">
					<span>Clear window</span>
					<b class="mono">
						{data.waivers_rules.clear_days}
						day(s)
					</b>
				</div>
				<div class="pool-stat">
					<span>Claims process</span>
					<b class="mono">{data.waivers_rules.process_display}</b>
				</div>
			</div>
			<If cond={data.waivers_rules.faab == false}>
				<p class="scoring-note">
					Claim priority blends
					{data.waivers_rules.season_weight_pct}
					% season rank with
					{data.waivers_rules.weekly_weight_pct}
					% this week's rank — the worst combined performance claims first. Winning a claim moves that team to the back of the order until the next weekly close. Before NFL week {data.waivers_rules.start_week} closes, the order runs the reverse of round 1 of the draft.
				</p>
			</If>
			<If cond={data.waivers_rules.faab}>
				<p class="scoring-note">
					Each team bids its own FAAB budget on a claim; the highest bid wins, ties broken by the perf-priority order above.
				</p>
			</If>
			<p class="scoring-note">
				A dropped player enters ON WAIVERS: the clear window above must pass before anyone can sign them as a free agent. A rostered player whose game has kicked off is also ON WAIVERS until that game ends and waivers clear.
			</p>
			<a href="/players" data-gosx-link class="button button--compact">Open the waiver desk →</a>
		</section>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">10 // TRADES</span>
					<h2>This league's veto policy</h2>
				</div>
			</div>
			<div class="pool-stats">
				<div class="pool-stat">
					<span>Veto policy</span>
					<b class="mono">{data.trades_rules.veto_mode}</b>
				</div>
				<div class="pool-stat">
					<span>Review window</span>
					<b class="mono">
						{data.trades_rules.review_hours}
						H
					</b>
				</div>
				<div class="pool-stat">
					<span>Offer expiry</span>
					<b class="mono">
						{data.trades_rules.expiry_days}
						day(s)
					</b>
				</div>
				<If cond={data.trades_rules.has_deadline}>
					<div class="pool-stat">
						<span>Trade deadline</span>
						<b class="mono">{data.trades_rules.deadline}</b>
					</div>
				</If>
			</div>
			<If cond={data.trades_rules.is_commissioner}>
				<p class="scoring-note">
					Every accepted trade enters a
					{data.trades_rules.review_hours}
					-hour commissioner review window. The commissioner may approve it early or veto it; otherwise it executes automatically once the window passes.
				</p>
			</If>
			<If cond={data.trades_rules.is_vote}>
				<p class="scoring-note">
					Every accepted trade enters a
					{data.trades_rules.review_hours}
					-hour review window. Any
					{data.trades_rules.veto_threshold}
					of the
					{data.trades_rules.seat_count}
					managers outside the trade can veto it by vote; short of that, it executes automatically once the window passes.
				</p>
			</If>
			<If cond={data.trades_rules.is_both}>
				<p class="scoring-note">
					Every accepted trade enters a
					{data.trades_rules.review_hours}
					-hour review window. The commissioner can approve or veto it, and so can a league vote of
					{data.trades_rules.veto_threshold}
					of the
					{data.trades_rules.seat_count}
					managers outside the trade — whichever resolves it first stands.
				</p>
			</If>
			<If cond={data.trades_rules.is_none}>
				<p class="scoring-note">
					A trade executes the instant the receiving manager accepts it — no review window, no veto.
				</p>
			</If>
			<p class="scoring-note">
				Both managers may counter or withdraw an open offer before it is accepted. An open offer nobody answers expires after
				{data.trades_rules.expiry_days}
				days. The league does not trade draft picks yet.
			</p>
			<a href="/trades" data-gosx-link class="button button--compact">Open the trade desk →</a>
		</section>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">11 // PICK'EM</span>
					<h2>The weekly side game</h2>
				</div>
			</div>
			<p class="scoring-note">
				Every signed-in member may make pick'em picks — a team seat is not required. Pick against the market spread shown on each matchup. The line updates until the weekly Thursday freeze; an earlier game freezes at its own kickoff. A missing frozen line is void, never silently converted to straight-up scoring.
			</p>
			<p class="scoring-note">
				The line freeze does not lock the sheet. Each matchup remains pickable until its own kickoff. Once you make any valid pick in a week, an unpicked game that starts is a loss; later games remain open. A push is neutral, and a missed loss breaks a winning streak. Pick'em has its own W-L-P leaderboard and is not a fantasy-standings tiebreaker.
			</p>
			<a href="/pickem" data-gosx-link class="button button--compact">Make this week's picks →</a>
		</section>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">12 // PRESEASON BLITZ</span>
					<h2>A side contest before the real thing</h2>
				</div>
			</div>
			<p class="scoring-note">
				Pick five preseason players and race the field on total production, no roster spot required. Scores live during the preseason window only.
			</p>
			<a href="/blitz" data-gosx-link class="button button--compact">Open Preseason Blitz →</a>
		</section>
		<p class="scoring-note mono">
			RULES LAST CONFIRMED //
			{data.rules_version.generated_at}
		</p>
	</main>
}
