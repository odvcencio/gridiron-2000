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
component ConsensusBar(props: ConsensusBarProps) {
	return <div class="consensus" aria-label="League pick split">
		<div class="consensus-bar">
			<div class="consensus-bar__fill consensus-bar__fill--away" style={props.AwayBarStyle}></div>
			<div class="consensus-bar__fill consensus-bar__fill--home" style={props.HomeBarStyle}></div>
		</div>
		<div class="consensus-legend mono">
			<span>
				{props.Away}
				{props.AwayPct}%
			</span>
			<span>
				{props.Home}
				{props.HomePct}%
			</span>
		</div>
	</div>
}

// PickemGameRow structurally mirrors internal/league's PickemGameRow.
type PickemGameRow struct {
	ID             string
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
	Push           bool
	MissedLoss     bool
	Void           bool
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

type PickemRowProps struct {
	Game   PickemGameRow
	Action string
	CSRF   string
}

component PickemRow(props: PickemRowProps) {
	return <article class="pickem-row" data-picked={props.Game.Picked}>
		<small class="mono">{props.Game.KickoffDisplay}</small>
		<strong>{props.Game.Label}</strong>
		<div class="pickem-market" data-state={props.Game.SpreadState}>
			<div class="pickem-market__head mono">
				<b>{props.Game.SpreadState}</b>
				<span>{props.Game.SpreadLock}</span>
			</div>
			<div class="pickem-market__line mono">
				<strong>{props.Game.AwayLine}</strong>
				<span>/</span>
				<strong>{props.Game.HomeLine}</strong>
			</div>
			<small class="mono">
				{props.Game.SpreadAsOf}
				{props.Game.SpreadSource}
			</small>
		</div>
		<div class="pickem-buttons">
			<If cond={props.Game.Locked == false}>
				<form method="post" action={props.Action} data-gosx-managed="true">
					<input type="hidden" name="csrf_token" value={props.CSRF}></input>
					<input type="hidden" name="game_id" value={props.Game.ID}></input>
					<input type="hidden" name="team" value={props.Game.Away}></input>
					<button class="filter-button" type="submit" aria-pressed={props.Game.PickedAway}>{props.Game.AwayLine}</button>
				</form>
			</If>
			<If cond={props.Game.Locked}>
				<button class="filter-button" type="button" disabled="disabled" aria-pressed={props.Game.PickedAway}>{props.Game.AwayLine}</button>
			</If>
			<If cond={props.Game.Locked == false}>
				<form method="post" action={props.Action} data-gosx-managed="true">
					<input type="hidden" name="csrf_token" value={props.CSRF}></input>
					<input type="hidden" name="game_id" value={props.Game.ID}></input>
					<input type="hidden" name="team" value={props.Game.Home}></input>
					<button class="filter-button" type="submit" aria-pressed={props.Game.PickedHome}>{props.Game.HomeLine}</button>
				</form>
			</If>
			<If cond={props.Game.Locked}>
				<button class="filter-button" type="button" disabled="disabled" aria-pressed={props.Game.PickedHome}>{props.Game.HomeLine}</button>
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
	Pushes  int
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
			-
			{props.Pushes}
		</b>
	</div>
}

func Page() Node {
	return <main class="page pickem-page" id="main-content" data-gosx-revalidate-interval="4s" data-gosx-revalidate-src="/api/league/version">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					PICK 'EM HQ // WEEK
					{data.week}
				</span>
				<h1>
					CALL YOUR
					<br></br>
					SHOTS.
				</h1>
				<p>
					Pick against the frozen market spread. Each game stays open until its own kickoff.
					After you enter a week, a missed kickoff is a loss — later games still stay open.
				</p>
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
					-
					{data.record.week_pushes}
				</strong>
			</div>
			<div class="pickem-record__stat">
				<span class="section-index">SEASON</span>
				<strong class="mono">
					{data.record.season_wins}
					-
					{data.record.season_losses}
					-
					{data.record.season_pushes}
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

		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_notice}>
				<p class="flash-message">{data.notice}</p>
			</If>
			<If cond={data.has_pickem_error}>
				<p class="error-message">{data.pickem_error}</p>
			</If>
			<If cond={data.can_pick == false}>
				<p class="demo-message">
					<strong>SIGN IN REQUIRED:</strong>
					use League access to lock in picks. No fantasy team seat needed.
				</p>
			</If>
		</div>

		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">
						01 // WEEK
						{data.week}
						SLATE
					</span>
					<h2>Weekly slate</h2>
				</div>
			</div>
			<p class="pickem-rule-note">
				<strong>THE LINE FREEZES THURSDAY.</strong>
				The sheet does not. Every matchup accepts picks until that game's kickoff. Once you make any pick this week, a game you miss becomes a loss; games that have not started remain available.
			</p>
			<If cond={data.has_weeks}>
				<div class="pickem-weeknav">
					<If cond={data.has_prev_week}>
						<a href={data.prev_week_href} data-gosx-link class="board-button">← Prev</a>
					</If>
					<form method="get" action="/pickem" class="lineup-week-form">
						<select name="week" aria-label="Select week">
							<Each of={data.week_options} as="wk">
								<option value={wk.value} selected={wk.selected}>{wk.label}</option>
							</Each>
						</select>
						<button class="board-button" type="submit">Go</button>
					</form>
					<If cond={data.has_next_week}>
						<a href={data.next_week_href} data-gosx-link class="board-button">Next →</a>
					</If>
					<If cond={data.is_current_week == false}>
						<a href={data.current_week_href} data-gosx-link class="access-link">Back to current week</a>
					</If>
				</div>
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
						<span class="section-index">02 // SEASON LEADERBOARD</span>
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
			</section>

			<section class="player-pool">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">
							03 // WEEK
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
	</main>
}
