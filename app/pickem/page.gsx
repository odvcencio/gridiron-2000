package pickem

func PickemRow(props any) Node {
	return <article class="pickem-row">
		<small class="mono">{props.game.kickoff_display}</small>
		<strong>{props.game.label}</strong>
		<div class="pickem-buttons">
			<If cond={props.game.locked == false}>
				<form method="post" action={props.Action} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
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
				<form method="post" action={props.Action} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
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
	</article>
}

func Page() Node {
	return <main class="page pickem-page" id="main-content">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					PICK 'EM // WEEK
					{data.week}
				</span>
				<h1>
					CALL YOUR
					<br></br>
					SHOTS.
				</h1>
				<p>
					One pick per game. Locks at kickoff. Bragging rights compound weekly.
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
					use League access to lock in picks tied to your seat.
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
			<If cond={data.games_empty}>
				<div class="empty-tape">
					<strong>NO GAMES THIS WEEK</strong>
					<p>
						The schedule syncs from the open nflverse mirror.
					</p>
				</div>
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
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">02 // SEASON STANDINGS</span>
					<h2>Standings</h2>
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
					<div class="board-row">
						<span class="pool-rank mono">{entry.rank}</span>
						<div class="pool-player">
							<strong>{entry.name}</strong>
							<span class="position-chip">{entry.team}</span>
						</div>
						<b class="mono">
							{entry.correct}
							/
							{entry.total}
						</b>
					</div>
				</Each>
			</div>
		</section>
	</main>
}
