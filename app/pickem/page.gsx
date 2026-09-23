package pickem

// PickemConsensusView structurally mirrors internal/league's
// PickemConsensusView (the loader's actual type): the same-file schema
// rule requires a strict component's props, and every struct its body
// reaches, to be declared in this .gsx file, so this needs only to share
// shape and field types with the loader's converter, not identity.
type PickemConsensusView struct {
	HasPicks     bool
	Total        int
	AwayPct      int
	HomePct      int
	AwayBarStyle string
	HomeBarStyle string
}

// ConsensusBarProps is flat, not a nested Consensus struct: a strict
// component's props field, when passed to another strict component as a
// named attribute (not a spread), can only be rendered if it resolves to
// a string, bool, integer, or floating-point builtin (gosx check) — a
// struct-typed value is not renderable at that boundary. PickemRow (the
// caller) is itself strict, so its call here passes each scalar leaf by
// name instead of the whole Consensus value.
type ConsensusBarProps struct {
	AwayBarStyle string
	HomeBarStyle string
	AwayPct      int
	HomePct      int
	Away         string
	Home         string
}

// ConsensusBar renders the league's locked-game pick split as a two-color
// bar plus the exact percentages — server-computed inline widths, no JS.
// Callers must only mount this once a game is locked (see
// pickemConsensus's doc comment: no split ships before lock).
func ConsensusBar(props ConsensusBarProps) Node {
	return <div class="consensus" aria-label="League pick split">
		<div class="consensus-bar">
			<div class="consensus-bar__fill consensus-bar__fill--away" style={props.AwayBarStyle}></div>
			<div class="consensus-bar__fill consensus-bar__fill--home" style={props.HomeBarStyle}></div>
		</div>
		<div class="consensus-legend mono">
			<TextBlock as="span" font="600 13px IBM Plex Mono" lineHeight={18} maxLines={1} overflow="ellipsis">
				{props.Away}
				{props.AwayPct}%
			</TextBlock>
			<TextBlock as="span" font="600 13px IBM Plex Mono" lineHeight={18} maxLines={1} overflow="ellipsis">
				{props.Home}
				{props.HomePct}%
			</TextBlock>
		</div>
	</div>
}

// PickemGameRow structurally mirrors internal/league's PickemGameRow.
type PickemGameRow struct {
	ID             string
	Week           int
	Label          string
	KickoffDisplay string
	Away           string
	Home           string
	Pick           string
	PickedAway     bool
	PickedHome     bool
	Picked         bool
	Locked         bool
	Final          bool
	Winner         string
	Correct        bool
	Wrong          bool
	MissedLoss     bool
	Void           bool
	MarketUnavailable bool
	Outcome        string
	ResultLabel    string
	AwayLine       string
	HomeLine       string
	SpreadState    string
	SpreadAsOf     string
	SpreadLock     string
	SpreadSource   string
	ScoreDisplay   string
	Consensus      PickemConsensusView
}

// PickemGamePickView structurally mirrors internal/league's
// PickemGamePickView: one entrant's recorded call on this game, with its
// grade. Correct/Wrong are precomputed bools rather than template-side
// comparisons for the same reason PickedAway/PickedHome are (see
// PickemGameRow above): a strict component's attribute and condition
// expressions do not support "==" against a string.
type PickemGamePickView struct {
	Name       string
	PickLabel  string
	HasPick    bool
	Outcome    string
	StateLabel string
	Correct    bool
	Wrong      bool
	IsViewer   bool
}

type PickemRowProps struct {
	Game   PickemGameRow
	Action string
	CSRF   string
	// LeaguePicks is the row's permanent record, filled once the game
	// locks: every entrant's call, named. It sits beside Game rather than
	// inside it so this strict component iterates a slice its own props
	// name directly.
	LeaguePicks    []PickemGamePickView
	HasLeaguePicks bool
	LeaguePickLead string
}

func PickemRow(props PickemRowProps) Node {
	return <article class="pickem-row" id={"game-" + props.Game.ID} data-game-id={props.Game.ID} data-picked={props.Game.Picked}>
		<small class="mono">{props.Game.KickoffDisplay}</small>
		<TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={1} overflow="ellipsis" text={props.Game.Label} />
		{/* J3 F26: the row used to give five lines (state, lock time, both
		    spread numbers — already repeated on the pick buttons below —
		    an as-of stamp, and a source line) to provenance and an
		    auto-sized track to the pick itself, the smallest thing on the
		    action page. Provenance is now one compact state-and-lock line
		    plus a closed-by-default Details disclosure for the as-of/
		    source attribution; .pickem-buttons .filter-button (styles.css,
		    "comb — birch") grows the pick buttons to read as the row's
		    primary control. */}
		<div class="pickem-market pickem-market--compact" data-state={props.Game.SpreadState}>
			<span class="pickem-market__state mono">
				<b>{props.Game.SpreadState}</b>
				{props.Game.SpreadLock}
			</span>
			<details class="pickem-market__provenance">
				<summary class="mono">Line details</summary>
				<div class="pickem-market__line mono">
					<strong>{props.Game.AwayLine}</strong>
					<span>/</span>
					<strong>{props.Game.HomeLine}</strong>
				</div>
				<small class="mono">
					{props.Game.SpreadAsOf}
					{props.Game.SpreadSource}
				</small>
			</details>
		</div>
		<div class="pickem-buttons">
			<If cond={props.Game.MarketUnavailable}>
				<button class="filter-button" type="button" disabled="disabled" aria-disabled="true" aria-pressed={props.Game.PickedAway}>{props.Game.AwayLine}<If cond={props.Game.PickedAway}><span class="pickem-your-pick"> ✓ YOUR PICK</span></If></button>
				<button class="filter-button" type="button" disabled="disabled" aria-disabled="true" aria-pressed={props.Game.PickedHome}>{props.Game.HomeLine}<If cond={props.Game.PickedHome}><span class="pickem-your-pick"> ✓ YOUR PICK</span></If></button>
			</If>
			<If cond={props.Game.MarketUnavailable == false}>
			<If cond={props.Game.Locked == false}>
				<form method="post" action={props.Action} data-gosx-managed="true" data-gosx-action-signal="$pickem.state.refresh">
					<input type="hidden" name="csrf_token" value={props.CSRF}></input>
					<input type="hidden" name="game_id" value={props.Game.ID}></input>
					<input type="hidden" name="week" value={props.Game.Week}></input>
					<input type="hidden" name="team" value={props.Game.Away}></input>
					<button class="filter-button" type="submit" aria-pressed={props.Game.PickedAway}>{props.Game.AwayLine}<If cond={props.Game.PickedAway}><span class="pickem-your-pick"> ✓ YOUR PICK</span></If></button>
				</form>
			</If>
			<If cond={props.Game.Locked}>
				<button class="filter-button" type="button" disabled="disabled" aria-pressed={props.Game.PickedAway}>{props.Game.AwayLine}<If cond={props.Game.PickedAway}><span class="pickem-your-pick"> ✓ YOUR PICK</span></If></button>
			</If>
			<If cond={props.Game.Locked == false}>
				<form method="post" action={props.Action} data-gosx-managed="true" data-gosx-action-signal="$pickem.state.refresh">
					<input type="hidden" name="csrf_token" value={props.CSRF}></input>
					<input type="hidden" name="game_id" value={props.Game.ID}></input>
					<input type="hidden" name="week" value={props.Game.Week}></input>
					<input type="hidden" name="team" value={props.Game.Home}></input>
					<button class="filter-button" type="submit" aria-pressed={props.Game.PickedHome}>{props.Game.HomeLine}<If cond={props.Game.PickedHome}><span class="pickem-your-pick"> ✓ YOUR PICK</span></If></button>
				</form>
			</If>
			<If cond={props.Game.Locked}>
				<button class="filter-button" type="button" disabled="disabled" aria-pressed={props.Game.PickedHome}>{props.Game.HomeLine}<If cond={props.Game.PickedHome}><span class="pickem-your-pick"> ✓ YOUR PICK</span></If></button>
			</If>
			</If>
		</div>
		<div class="pickem-status">
			<If cond={props.Game.Final}>
				<b class="mono">{props.Game.ScoreDisplay}</b>
				<span class="mono">{props.Game.ResultLabel}</span>
				<If cond={props.Game.Correct}>
					<b class="pickem-hit">✓</b>
				</If>
				<If cond={props.Game.Wrong}>
					<b class="pickem-miss">✗</b>
				</If>
			</If>
			<If cond={props.Game.Final == false}>
				<b class="mono">{props.Game.ResultLabel}</b>
			</If>
		</div>
		<If cond={props.Game.Locked}>
			<If cond={props.Game.Consensus.HasPicks}>
				<ConsensusBar
					AwayBarStyle={props.Game.Consensus.AwayBarStyle}
					HomeBarStyle={props.Game.Consensus.HomeBarStyle}
					AwayPct={props.Game.Consensus.AwayPct}
					HomePct={props.Game.Consensus.HomePct}
					Away={props.Game.Away}
					Home={props.Game.Home}
				></ConsensusBar>
			</If>
			{/* The league record. The consensus bar above says how many
			    took each side; this says who took it, and how it graded.
			    Both ship only after kickoff, and both stay for good --
			    this is what keeps a settled week's sheet a record rather
			    than an expired form. */}
			<If cond={props.HasLeaguePicks}>
				<div class="pickem-ledger">
					<span class="pickem-ledger__lead mono">{props.LeaguePickLead}</span>
					<ul class="pickem-ledger__list">
						<Each of={props.LeaguePicks} as="entry">
							{/* The name clamps in CSS rather than through
							    <TextBlock> (the clamp this page uses for the
							    matchup label and the consensus legend). This
							    list is the one place that multiplies: one
							    label per entrant per locked game, and every
							    one re-ships on the live region's 4s poll.
							    Measured on this league's own week-1 slate
							    (six entrants, fifteen locked games), the
							    fragment grows from 51 KB to 77 KB with the
							    ledger. A TextBlock label carries about ten
							    measurement attributes and renders near 490
							    bytes against this span's 33, which would add
							    roughly 40 KB more to every poll for a clamp
							    one line of CSS already gives
							    (.pickem-ledger__who). */}
							<li class="pickem-ledger__entry" data-outcome={entry.Outcome} data-viewer={entry.IsViewer}>
								<span class="pickem-ledger__who">{entry.Name}</span>
								<b class="mono pickem-ledger__call">{entry.PickLabel}</b>
								<span class="mono pickem-ledger__state">{entry.StateLabel}</span>
							</li>
						</Each>
					</ul>
				</div>
			</If>
		</If>
	</article>
}

// PickemLeaderboardEntry structurally mirrors internal/league's
// PickemLeaderboardEntry, and is LeaderboardRow's own props type directly
// (no wrapper field): a page-level spread proves a root value structurally
// regardless of its declared type's name, so data.leaderboard's entries
// need only share this shape.
type PickemLeaderboardEntry struct {
	Rank    string
	Name    string
	Team    string
	Correct int
	Total   int
	Wins    int
	Losses  int
}

component LeaderboardRow(props: PickemLeaderboardEntry) {
	return <div class="rank-row">
		<span class="pool-rank mono">{props.Rank}</span>
		<div class="pool-player">
			<strong>{props.Name}</strong>
			<span class="position-chip">{props.Team}</span>
		</div>
		<b class="mono">
			{props.Wins}
			-
			{props.Losses}
		</b>
	</div>
}

func Page() Node {
	return <main class="page pickem-page" id="main-content" data-gosx-revalidate-interval="4s" data-gosx-revalidate-src="/api/league/version">
		<div
			id="pickem-live-region"
			data-gosx-region
			data-gosx-region-url={data.pickem_fragment_url}
			data-gosx-region-interval={data.pickem_fragment_interval}
			data-gosx-region-signal="$pickem.state.refresh"
			aria-label="Authoritative Pick'em slate and scoring"
		>
			<PickemLiveRegion></PickemLiveRegion>
		</div>
		<p class="scoring-note pickem-live-note" role="status" aria-live="polite">
			Pick'em state refreshes automatically while games are live and settles to a slower check once the displayed slate is final.
			If a refresh fails, use
			<button type="button" class="board-button" data-gosx-set="$pickem.state.refresh" data-gosx-set-value="manual">Refresh Pick'em now</button>.
		</p>
	</main>
}

// PickemLiveRegion is the single authoritative render shared by the initial
// page and /pickem/fragment. Keeping the masthead counters, per-game rows,
// lock/result state, records, and leaderboards together means a fragment swap
// never leaves the sheet scoring summary out of sync with a game transition.
func PickemLiveRegion() Node {
	return <div class="pickem-live-region-content">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="signal-mark" aria-hidden="true"></span>
					PICK 'EM HQ // WEEK
					{data.week}
				</span>
				<h1>Pick'em</h1>
			</div>
			<div class="draft-clock-panel">
				<span>Your picks this week</span>
				<strong class="mono">{data.picked_count}</strong>
				<div class="draft-clock-meta">
					<a href="/scoring" data-gosx-link>Scoring rules →</a>
				</div>
			</div>
		</section>

		<section class="pickem-record" aria-label="Your pick'em record">
			<div class="pickem-record__stat">
				<span class="section-index">THIS WEEK</span>
				<strong class="mono">
					{data.record.week_wins}
					-
					{data.record.week_losses}
				</strong>
			</div>
			<div class="pickem-record__stat">
				<span class="section-index">SEASON</span>
				<strong class="mono">
					{data.record.season_wins}
					-
					{data.record.season_losses}
				</strong>
			</div>
			<div class="pickem-record__stat">
				<span class="section-index">STREAK</span>
				<If cond={data.record.has_streak}>
					<strong class="mono">
						{data.record.streak}
						W
					</strong>
				</If>
				<If cond={data.record.has_streak == false}>
					<strong class="mono">—</strong>
				</If>
			</div>
		</section>

		{/* The week's own standing, stated plainly. A sheet that is meant
		    to serve the record permanently has to say which record it is
		    serving: open, in progress, or settled -- and, once settled,
		    who took the week. week_state/week_state_note/week_winner_*
		    come from PickemData (internal/league/pickem.go); the winner
		    is read off the same week leaderboard rendered below, so the
		    two can never disagree. */}
		<section class="pickem-week-record" data-state={data.week_state} aria-label="This week's pick'em standing">
			<span class="section-index">
				WEEK
				{data.week}
				·
				{data.week_state}
			</span>
			<If cond={data.has_week_winner}>
				<p class="pickem-week-record__winner">
					<span class="pickem-week-record__mark" aria-hidden="true">◎</span>
					<span class="section-index">WEEK LEADER</span>
					<TextBlock as="strong" font="700 16px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis" text={data.week_winner_names} />
					<b class="mono">{data.week_winner_record}</b>
				</p>
			</If>
		</section>

		<div class="notice-stack">
			<If cond={data.has_notice}>
				<TextBlock as="p" class="flash-message" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={3} overflow="ellipsis" text={data.notice} />
			</If>
			<If cond={data.has_week_notice}>
				<TextBlock as="p" class="demo-message" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis">
					<strong>WEEK ADJUSTED:</strong>
					{data.week_notice}
				</TextBlock>
			</If>
			<If cond={data.has_pickem_error}>
				<TextBlock as="p" class="error-message" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={3} overflow="ellipsis" text={data.pickem_error} />
			</If>
			{/* No pick controls: say why, from the canonical public-entry
			    projection (internal/league/public_entry.go) — sign-in for a
			    visitor, "membership not recorded" for a signed-in account the
			    league does not know, the invite for a pending co-manager —
			    the same projection /board and /blitz render. */}
			<If cond={data.can_pick == false}>
				<div class="demo-message pickem-entry-state">
					<strong>{data.public_entry.state_label}</strong>
					<TextBlock as="p" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={4} overflow="ellipsis" text={data.public_entry.detail} />
					<a href={data.public_entry.action_href} data-gosx-link class="button button--compact">{data.public_entry.action_label}</a>
				</div>
			</If>
			<If cond={data.viewer.demo}>
				<TextBlock as="p" class="demo-message" font="400 15px Plus Jakarta Sans" lineHeight={22}>
					<strong>REHEARSAL MODE:</strong>
					Public demo.
				</TextBlock>
			</If>
		</div>

		<section class="player-pool" id="pickem-slate">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">
						WEEK
						{data.week}
						SLATE
					</span>
					<h2>Weekly slate</h2>
				</div>
			</div>
			<If cond={data.week_settled == false}>
				<p class="pickem-rule-note"><strong>LINE FREEZES THURSDAY</strong> Picks lock at kickoff · Unpicked games count as losses after entry.</p>
			</If>
			<If cond={data.week_settled}>
				<p class="pickem-rule-note pickem-rule-note--settled"><strong>WEEK SETTLED</strong> · Final scores and picks below.</p>
			</If>
			<If cond={data.has_weeks}>
				<div class="pickem-weeknav">
					<If cond={data.has_prev_week}>
						<a href={data.prev_week_href} data-gosx-link class="board-button" rel="prev">← Prev</a>
					</If>
					<form method="get" action="/pickem" class="lineup-week-form">
						<select name="week" class="board-button" aria-label="Select week">
							<Each of={data.week_options} as="wk">
								<option value={wk.value} selected={wk.selected}>{wk.label}</option>
							</Each>
						</select>
						<button class="board-button" type="submit">Go</button>
					</form>
					<If cond={data.has_next_week}>
						<a href={data.next_week_href} data-gosx-link class="board-button" rel="next">Next →</a>
					</If>
				</div>
				<If cond={data.is_current_week == false}>
					<a href={data.current_week_href} data-gosx-link class="access-link pickem-back-to-current">Back to current week</a>
				</If>
			</If>
			<If cond={data.games_empty}>
				<If cond={data.has_weeks}>
					<div class="empty-tape">
						<strong>NO GAMES THIS WEEK</strong>
						<p>
							Nothing on the slate for week
							{data.week}
							. Pick another week above.
						</p>
					</div>
				</If>
				<If cond={data.has_weeks == false}>
					<div class="empty-tape">
						<strong>PICK 'EM OPENS WITH THE SCHEDULE</strong>
						<p>
							The schedule loads once the season is set. Check back soon.
						</p>
					</div>
				</If>
			</If>
			<div class="pool-list">
				<Each of={data.games} as="row">
					<PickemRow {...row}></PickemRow>
				</Each>
			</div>
		</section>

		<div class="pickem-boards">
			<section class="player-pool">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">SEASON LEADERBOARD</span>
						<h2>Season standings</h2>
					</div>
				</div>
				<If cond={data.leaderboard_empty}>
					<div class="empty-tape">
						<strong>NO RESULTS YET</strong>
						<p>
							Standings appear once picked games go final.
						</p>
					</div>
				</If>
				<div class="pool-list">
					<Each of={data.leaderboard} as="entry">
						<LeaderboardRow {...entry}></LeaderboardRow>
					</Each>
				</div>
				{/* A member with no pick at all has not entered, so the board
				    omits them by design. Naming them here keeps "where is X?"
				    from reading as a missing row (pickemNotEnteredNames,
				    internal/league/pickem.go). */}
				<If cond={data.has_not_entered}>
					<p class="scoring-note pickem-not-entered">
						<strong>Not yet entered:</strong>
						{data.not_entered_names}
					</p>
				</If>
			</section>

			<section class="player-pool">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">
							WEEK
							{data.week}
							LEADERBOARD
						</span>
						<h2>This week's leaderboard</h2>
					</div>
				</div>
				<If cond={data.week_leaderboard_empty}>
					<div class="empty-tape">
						<strong>NO RESULTS YET</strong>
						<p>
							This week's board fills in as picked games go final.
						</p>
					</div>
				</If>
				<div class="pool-list">
					<Each of={data.week_leaderboard} as="entry">
						<LeaderboardRow {...entry}></LeaderboardRow>
					</Each>
				</div>
			</section>
		</div>
	</div>
}
