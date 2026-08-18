package pickem

// ConsensusBar renders the league's locked-game pick split as a two-color
// bar plus the exact percentages — server-computed inline widths, no JS.
// Callers must only mount this once a game is locked (see
// pickemConsensus's doc comment: no split ships before lock).
func ConsensusBar(props any) Node {
	return <div class="consensus" aria-label="League pick split">
		<div class="consensus-bar">
			<div class="consensus-bar__fill consensus-bar__fill--away" style={props.consensus.away_bar_style}></div>
			<div class="consensus-bar__fill consensus-bar__fill--home" style={props.consensus.home_bar_style}></div>
		</div>
		<div class="consensus-legend mono">
			<span>
				{props.away}
				{props.consensus.away_pct}%
			</span>
			<span>
				{props.home}
				{props.consensus.home_pct}%
			</span>
		</div>
	</div>
}

func PickemRow(props any) Node {
	return <article class="pickem-row" data-picked={props.game.picked}>
		<small class="mono">{props.game.kickoff_display}</small>
		<strong>{props.game.label}</strong>
		<div class="pickem-buttons">
			<If cond={props.game.locked == false}>
				<form method="post" action={props.Action} data-gosx-managed="true">
					<input type="hidden" name="csrf_token" value={props.CSRF}></input>
					<input type="hidden" name="game_id" value={props.game.id}></input>
					<input type="hidden" name="team" value={props.game.away}></input>
					<button class="filter-button" type="submit" aria-pressed={props.game.pick == props.game.away}>{props.game.away}</button>
				</form>
			</If>
			<If cond={props.game.locked}>
				<button class="filter-button" type="button" disabled="disabled" aria-pressed={props.game.pick == props.game.away}>{props.game.away}</button>
			</If>
			<If cond={props.game.locked == false}>
				<form method="post" action={props.Action} data-gosx-managed="true">
					<input type="hidden" name="csrf_token" value={props.CSRF}></input>
					<input type="hidden" name="game_id" value={props.game.id}></input>
					<input type="hidden" name="team" value={props.game.home}></input>
					<button class="filter-button" type="submit" aria-pressed={props.game.pick == props.game.home}>{props.game.home}</button>
				</form>
			</If>
			<If cond={props.game.locked}>
				<button class="filter-button" type="button" disabled="disabled" aria-pressed={props.game.pick == props.game.home}>{props.game.home}</button>
			</If>
		</div>
		<div class="pickem-status">
			<If cond={props.game.final}>
				<b class="mono">{props.game.score_display}</b>
				<span class="mono">{props.game.winner}</span>
				<If cond={props.game.correct}>
					<b class="pickem-hit">✓</b>
				</If>
				<If cond={props.game.wrong}>
					<b class="pickem-miss">✗</b>
				</If>
			</If>
			<If cond={props.game.locked}>
				<If cond={props.game.final == false}>
					<b class="mono">LOCKED</b>
				</If>
			</If>
		</div>
		<If cond={props.game.locked}>
			<If cond={props.game.consensus.has_picks}>
				<ConsensusBar consensus={props.game.consensus} away={props.game.away} home={props.game.home}></ConsensusBar>
			</If>
		</If>
	</article>
}

func LeaderboardRow(props any) Node {
	return <div class="rank-row">
		<span class="pool-rank mono">{props.entry.rank}</span>
		<div class="pool-player">
			<strong>{props.entry.name}</strong>
			<span class="position-chip">{props.entry.team}</span>
		</div>
		<b class="mono">
			{props.entry.correct}
			/
			{props.entry.total}
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
					One pick per game. Locks at kickoff. Bragging rights compound weekly.
					No fantasy team required — pick'em is its own game here.
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
					{data.record.week_correct}
					/
					{data.record.week_total}
				</strong>
			</div>
			<div class="pickem-record__stat">
				<span class="section-index">SEASON</span>
				<strong class="mono">
					{data.record.season_correct}
					/
					{data.record.season_total}
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
							The schedule syncs from the open nflverse mirror once the season is set. Check back soon.
						</p>
					</div>
				</If>
			</If>
			<div class="pool-list">
				<Each of={data.games} as="game">
					<PickemRow
						game={game}
						Action={actionPath("pickem-set")}
						CSRF={csrf.token}
					 />
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
						<LeaderboardRow entry={entry}></LeaderboardRow>
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
						<LeaderboardRow entry={entry}></LeaderboardRow>
					</Each>
				</div>
			</section>
		</div>
	</main>
}
