package draft

//gosx:island
func DraftQueue(props any) Node {
	filter := signal.New("ALL")
	showAll := func() { filter.Set("ALL") }
	showRB := func() { filter.Set("RB") }
	showWR := func() { filter.Set("WR") }
	showQB := func() { filter.Set("QB") }
	showTE := func() { filter.Set("TE") }
	showK := func() { filter.Set("K") }
	showDST := func() { filter.Set("DST") }
	return <section class="player-pool" data-filter={filter}>
		<div class="pool-toolbar">
			<div>
				<span class="section-index">PLAYER DATABASE</span>
				<h2>Available now</h2>
			</div>
			<div class="position-filters" aria-label="Filter draft pool by position">
				<button type="button" class="filter-button" aria-pressed={filter == "ALL"} onClick={showAll}>All</button>
				<button type="button" class="filter-button" aria-pressed={filter == "RB"} onClick={showRB}>RB</button>
				<button type="button" class="filter-button" aria-pressed={filter == "WR"} onClick={showWR}>WR</button>
				<button type="button" class="filter-button" aria-pressed={filter == "QB"} onClick={showQB}>QB</button>
				<button type="button" class="filter-button" aria-pressed={filter == "TE"} onClick={showTE}>TE</button>
				<button type="button" class="filter-button" aria-pressed={filter == "K"} onClick={showK}>K</button>
				<button type="button" class="filter-button" aria-pressed={filter == "DST"} onClick={showDST}>DST</button>
			</div>
		</div>
		<div class="pool-labels mono" aria-hidden="true">
			<span>RK</span>
			<span>PLAYER</span>
			<span>POS</span>
			<span>PROJ</span>
			<span>ACTION</span>
		</div>
		<div class="pool-list">
			<Each of={props.Players} as="player">
				<article class="pool-row" data-player-position={player.position} data-search={player.search}>
					<span class="pool-rank mono">{player.rank}</span>
					<div class="pool-player">
						<strong>{player.name}</strong>
						<small>{player.detail}</small>
					</div>
					<span class="position-chip">{player.position}</span>
					<b class="mono">{player.projection}</b>
					<form method="post" action={props.Action}>
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<input type="hidden" name="team_id" value={props.TeamID}></input>
						<input type="hidden" name="player_id" value={player.id}></input>
						<If cond={props.CanPick}>
							<button class="draft-button" type="submit">Draft</button>
						</If>
						<If cond={props.CanPick == false}>
							<button class="draft-button" type="button" disabled="disabled">Locked</button>
						</If>
					</form>
				</article>
			</Each>
		</div>
	</section>
}

func DraftTeam(props any) Node {
	return <div class="draft-team" data-on-clock={props.on_clock}>
		<span class={"team-mark tone-" + props.tone}>{props.abbreviation}</span>
		<div>
			<strong>{props.name}</strong>
			<small>{props.manager}</small>
		</div>
		<If cond={props.ready}>
			<b class="ready-state is-ready">Ready</b>
		</If>
		<If cond={props.ready == false}>
			<b class="ready-state">Not ready</b>
		</If>
	</div>
}

func Page() Node {
	return <main class="page draft-page" id="main-content" data-draft-at={data.draft.at}>
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					DRAFT EVENT //
					{data.draft.date}
				</span>
				<p class="page-kicker">{data.draft.long_date}</p>
				<h1>
					BUILD
					<br></br>
					THE FUTURE.
				</h1>
				<p>
					{data.draft.format}
					. The room switches from rehearsal to live mode at
					{data.draft.time}
					.
				</p>
			</div>
			<div class="draft-clock-panel">
				<span>Room opens in</span>
				<strong class="mono" data-draft-countdown>CALCULATING…</strong>
				<div class="draft-clock-meta">
					<span>
						{data.ready_count}
						/
						{data.manager_count}
						ready
					</span>
					<span>
						Pick #
						{data.pick_number}
					</span>
				</div>
				<form method="post" action={actionPath("toggle-ready")}>
					<input type="hidden" name="csrf_token" value={csrf.token}></input>
					<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
					<button class="button button--primary" type="submit">Toggle my ready state</button>
				</form>
			</div>
		</section>
		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_notice}>
				<p class="flash-message">{data.notice}</p>
			</If>
			<If cond={data.has_pick_error}>
				<p class="error-message">
					{data.pick_error}
				</p>
			</If>
			<If cond={data.demo_mode}>
				<p class="demo-message">
					<strong>REHEARSAL MODE:</strong>
					picks are enabled before the room goes live and control the current team on the clock.
				</p>
			</If>
			<If cond={data.pool_live == false}>
				<p class="demo-message">
					<strong>OFFLINE POOL:</strong>
					player ranks are approximate. Set TANK01_API_KEY to sync live ADP and projections.
				</p>
			</If>
		</div>
		<section class="draft-order-strip">
			<header>
				<span class="section-index">
					ROUND
					{data.round}
					// SNAKE ORDER
				</span>
				<span class="mono">
					ON CLOCK:
					{data.on_clock.abbreviation}
				</span>
			</header>
			<div class="draft-team-grid">
				<Each of={data.teams} as="team">
					<DraftTeam {...team}></DraftTeam>
				</Each>
			</div>
		</section>
		<div class="pool-search-bar">
			<label class="mono" for="pool-search">DATABASE QUERY //</label>
			<input
				id="pool-search"
				type="search"
				data-pool-search="true"
				placeholder="Search player, team, or position"
				autocomplete="off"
			 />
			<span class="mono pool-count">
				{data.pool_count}
				PLAYERS
			</span>
		</div>
		<div class="draft-workspace">
			<DraftQueue
				Players={data.available}
				Action={actionPath("make-pick")}
				CSRF={csrf.token}
				TeamID={data.on_clock_id}
				CanPick={data.can_pick}
			 />
			<aside class="pick-tape">
				<If cond={data.board_count > 0}>
					<header>
						<span class="section-index">YOUR BOARD</span>
						<a href="/board" data-gosx-link class="mono">EDIT →</a>
					</header>
					<div class="pick-list board-peek">
						<Each of={data.board} as="target">
							<div class="pick-row">
								<span class="mono">{target.rank}</span>
								<div>
									<strong>{target.name}</strong>
									<small>
										{target.position}
										·
										{target.nfl_team}
									</small>
								</div>
								<b>{target.projection}</b>
							</div>
						</Each>
					</div>
				</If>
				<If cond={data.board_count == 0}>
					<div class="board-peek-empty">
						<a href="/board" data-gosx-link class="mono">BUILD YOUR BOARD →</a>
					</div>
				</If>
				<header>
					<span class="section-index">PICK TAPE</span>
					<b class="mono">LIVE LOG</b>
				</header>
				<If cond={data.picks.length == 0}>
					<div class="empty-tape">
						<strong>NO PICKS YET</strong>
						<p>
							The tape starts moving when the first selection is locked.
						</p>
					</div>
				</If>
				<div class="pick-list">
					<Each of={data.picks} as="pick">
						<div class="pick-row">
							<span class="mono">{pick.number}</span>
							<div>
								<strong>{pick.player.name}</strong>
								<small>
									{pick.player.position}
									·
									{pick.player.nfl_team}
								</small>
							</div>
							<b>{pick.team.abbreviation}</b>
						</div>
					</Each>
				</div>
			</aside>
		</div>
	</main>
}
