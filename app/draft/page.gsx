package draft

//gosx:island
func DraftQueue(props any) Node {
	filter := signal.New("ALL")
	query := signal.New("")
	showAll := func() { filter.Set("ALL") }
	showRB := func() { filter.Set("RB") }
	showWR := func() { filter.Set("WR") }
	showQB := func() { filter.Set("QB") }
	showTE := func() { filter.Set("TE") }
	showK := func() { filter.Set("K") }
	showDST := func() { filter.Set("DST") }
	onQuery := func() { query.Set(value) }
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
		<div class="pool-search-bar">
			<label class="mono" for="pool-search">DATABASE QUERY //</label>
			<input
				id="pool-search"
				type="search"
				placeholder="Search player, team, or position"
				autocomplete="off"
				onInput={onQuery}
			 />
		</div>
		<div class="pool-labels mono" aria-hidden="true">
			<span>RK</span>
			<span>PLAYER</span>
			<span>POS</span>
			<span>PROJ</span>
			<span>ACTION</span>
		</div>
		<div class="pool-list">
			<Each of={props.Players.filter(func(p){ return p.search.contains(query.trim().toLower()) })} as="player">
				<article class="pool-row" data-player-position={player.position} data-search={player.search}>
					<span class="pool-rank mono">{player.rank}</span>
					<div class="pool-player pool-player--photo stat-tip" tabindex="0">
						<If cond={player.has_headshot}>
							<img class="player-headshot" src={player.headshot} alt="" loading="lazy" />
						</If>
						<div class="pool-player__text">
							<strong>{player.name}</strong>
							<small>{player.detail}</small>
						</div>
						<div class="stat-tip__panel" role="tooltip" aria-hidden="true">
							<div class="stat-tip__head">
								<strong>{player.name}</strong>
								<span class="mono">{player.jersey}</span>
								<span class="mono stat-tip__team">{player.nfl_team}</span>
							</div>
							<If cond={player.has_breakdown}>
								<div class="stat-tip__rows">
									<Each of={player.breakdown} as="row">
										<div class="stat-tip__row" data-scored={row.scored}>
											<span>{row.label}</span>
											<span class="mono">{row.calc}</span>
											<b class="mono">{row.points}</b>
										</div>
									</Each>
									<div class="stat-tip__total">
										<span>League scoring</span>
										<b class="mono">{player.breakdown_total}</b>
									</div>
								</div>
							</If>
							<If cond={player.has_breakdown == false}>
								<p class="stat-tip__empty">No projection detail for this position.</p>
							</If>
							<If cond={player.has_hist}>
								<p class="stat-tip__hist mono">{player.hist}</p>
							</If>
						</div>
					</div>
					<span class="position-chip">{player.position}</span>
					<b class="mono">{player.projection}</b>
					<form method="post" action={props.Action} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
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
			<small>
				<span class="presence-dot" data-presence={props.presence}></span>
				{props.manager}
			</small>
			<small class="mono division-tag">{props.division}</small>
		</div>
		<If cond={props.ready}>
			<b class="ready-state is-ready">Ready</b>
		</If>
		<If cond={props.ready == false}>
			<b class="ready-state">Not ready</b>
		</If>
		<If cond={props.autopick}>
			<b class="autopick-badge mono">AUTO</b>
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
				<div class="pick-clock-row">
					<span class="mono">ON THE CLOCK //</span>
					<strong
						class="pick-clock mono"
						data-pick-clock
						data-armed={data.clock.armed}
						data-paused={data.clock.paused}
						data-deadline={data.clock.effective_deadline}
						data-server-now={data.clock.server_now}
					>--:--</strong>
					<span class="mono pick-clock-reason">{data.clock.reason}</span>
				</div>
				<form method="post" action={actionPath("toggle-ready")} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
					<input type="hidden" name="csrf_token" value={csrf.token}></input>
					<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
					<button class="button button--primary" type="submit">Toggle my ready state</button>
				</form>
				<form method="post" action={actionPath("toggle-autopick")} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
					<input type="hidden" name="csrf_token" value={csrf.token}></input>
					<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
					<button class="button autopick-toggle" type="submit">Toggle my autopick</button>
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
			<If cond={data.clock.paused}>
				<p class="demo-message">
					<strong>CLOCK PAUSED:</strong>
					the commissioner paused the pick clock. Picks stay open.
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
				<span class="mono">
					ORDER:
					<If cond={data.order_randomized}>
						<b>RANDOMIZED</b>
					</If>
					<If cond={data.order_randomized == false}>
						<b>DEFAULT</b>
					</If>
				</span>
			</header>
			<div class="draft-team-grid">
				<Each of={data.teams} as="team">
					<DraftTeam {...team}></DraftTeam>
				</Each>
			</div>
		</section>
		<If cond={data.viewer.is_commissioner}>
			<section class="draft-order-strip commissioner-clock-controls">
				<header>
					<span class="section-index">COMMISSIONER // CLOCK</span>
					<span class="mono">{data.clock.reason}</span>
				</header>
				<div class="clock-controls">
					<form method="post" action={actionPath("clock-pause")} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<button class="button button--compact" type="submit">Pause clock</button>
					</form>
					<form method="post" action={actionPath("clock-resume")} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<button class="button button--compact button--primary" type="submit">Resume clock</button>
					</form>
					<form method="post" action={actionPath("clock-extend")} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<input class="scoring-input" type="number" name="seconds" placeholder="30" min="1" max="600"></input>
						<button class="button button--compact" type="submit">Extend pick</button>
					</form>
					<form method="post" action={actionPath("clock-set-duration")} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<input class="scoring-input" type="number" name="seconds" placeholder="90" min="10" max="600"></input>
						<button class="button button--compact" type="submit">Set duration</button>
					</form>
					<form method="post" action={actionPath("clock-force-autopick")} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<button class="button button--compact button--ghost" type="submit">Force autopick</button>
					</form>
				</div>
			</section>
		</If>
		<div class="pool-count-bar">
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
				<If cond={data.picks_empty}>
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
							<div class="pick-tape-meta">
								<If cond={pick.is_auto}>
									<b class="pick-tag pick-tag--auto mono">AUTO</b>
								</If>
								<If cond={pick.is_commissioner}>
									<b class="pick-tag pick-tag--comm mono">COMM</b>
								</If>
								<b class="mono">{pick.team.abbreviation}</b>
							</div>
						</div>
					</Each>
				</div>
			</aside>
		</div>
	</main>
}
