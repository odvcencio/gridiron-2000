package scoring

func ScoringRow(props any) Node {
	return <div class="scoring-row">
		<div class="scoring-row__label">
			<strong>{props.rule.label}</strong>
			<If cond={props.rule.is_default == false}>
				<span class="position-chip">CUSTOM</span>
			</If>
		</div>
		<If cond={props.Editable == false}>
			<b class="mono">{props.rule.points}</b>
		</If>
		<If cond={props.Editable}>
			<form class="scoring-row__form" method="post" action={props.SetAction} data-gosx-managed="true">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="key" value={props.rule.key}></input>
				<input type="text" name="points" value={props.rule.points} class="scoring-input"></input>
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
					<span class="live-dot" aria-hidden="true"></span>
					RULES &amp; SCORING //
					{data.league_mode}
				</span>
				<h1>
					HOW THE
					<br></br>
					LEAGUE RUNS.
				</h1>
				<p>
					Roster shape, scoring values, the draft, the season, and where transactions stand today — every number below is live, not a static description.
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
					<span class="section-index">01 // ROSTER</span>
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
		<Each of={data.groups} as="group">
			<section class="player-pool">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">
							02 // SCORING //
							{group.name}
						</span>
						<h2>{group.name}</h2>
					</div>
				</div>
				<If cond={group.note != ""}>
					<p class="scoring-note">{group.note}</p>
				</If>
				<div class="pool-list">
					<Each of={group.rules} as="rule">
						<ScoringRow
							rule={rule}
							Editable={data.editable}
							SetAction={actionPath("scoring-set")}
							CSRF={csrf.token}
						 />
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
					<span class="section-index">03 // DRAFT</span>
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
					<span class="section-index">04 // SEASON</span>
					<h2>Schedule and playoffs</h2>
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
		</section>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">05 // WAIVERS &amp; TRANSACTIONS</span>
					<h2>Current state</h2>
				</div>
			</div>
			<p class="scoring-note">{data.waivers_rules.note}</p>
		</section>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">06 // PRESEASON BLITZ</span>
					<h2>A side contest before the real thing</h2>
				</div>
			</div>
			<p class="scoring-note">
				Pick five preseason players and race the field on total production, no roster spot required. Scores live during the preseason window only.
			</p>
			<a href="/blitz" data-gosx-link class="button button--compact">Open Preseason Blitz →</a>
		</section>
	</main>
}
