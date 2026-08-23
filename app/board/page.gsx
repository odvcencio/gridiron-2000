package board

func BoardRow(props any) Node {
	return <article class="board-row" data-picked={props.player.picked} data-gosx-reorder-item={props.player.id}>
		<span class="board-row__handle" data-gosx-reorder-handle aria-label={"Reorder " + props.player.name}>⠿</span>
		<span class="pool-rank mono">{props.player.board_rank}</span>
		<div class="pool-player pool-player--photo stat-tip" tabindex="0">
			<If cond={props.player.has_headshot}>
				<img class="player-headshot" src={props.player.headshot} alt="" loading="lazy" />
			</If>
			<div class="pool-player__text">
				<strong>{props.player.name}</strong>
				<If cond={props.player.has_draft_capital}>
					<span class="badge-rookie">{props.player.draft_capital}</span>
				</If>
				<small>{props.player.detail}</small>
				<If cond={props.player.has_opponent}>
					<small class="mono">
						{props.player.opponent}
						<If cond={props.player.has_matchup}>
							·
							<span class="matchup-chip" data-matchup-tier={props.player.matchup_tier}>{props.player.matchup_chip}</span>
						</If>
					</small>
				</If>
			</div>
			<div class="stat-tip__panel" role="tooltip" aria-hidden="true">
				<div class="stat-tip__head">
					<strong>{props.player.name}</strong>
					<span class="mono">{props.player.jersey}</span>
					<span class="mono stat-tip__team">{props.player.nfl_team}</span>
				</div>
				<If cond={props.player.has_breakdown}>
					<div class="stat-tip__rows">
						<Each of={props.player.breakdown} as="row">
							<div class="stat-tip__row" data-scored={row.scored}>
								<span>{row.label}</span>
								<span class="mono">{row.calc}</span>
								<b class="mono">{row.points}</b>
							</div>
						</Each>
						<div class="stat-tip__total">
							<span>League scoring</span>
							<b class="mono">{props.player.breakdown_total}</b>
						</div>
					</div>
				</If>
				<If cond={props.player.has_breakdown == false}>
					<p class="stat-tip__empty">No projection detail for this position.</p>
				</If>
				<If cond={props.player.has_matchup}>
					<p class="stat-tip__hist mono">{props.player.matchup_detail}</p>
				</If>
				<If cond={props.player.has_hist}>
					<p class="stat-tip__hist mono">{props.player.hist}</p>
				</If>
			</div>
		</div>
		<span class="position-chip">{props.player.position}</span>
		<b class="mono">{props.player.projection}</b>
		<div class="board-controls">
			<form method="post" action={props.RemoveAction} data-gosx-managed="true">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="player_id" value={props.player.id}></input>
				<button class="board-button board-button--cut" type="submit" aria-label={"Remove " + props.player.name}>✕</button>
			</form>
		</div>
	</article>
}

func Page() Node {
	return <main class="page board-page" id="main-content" data-gosx-revalidate-interval="4s" data-gosx-revalidate-src="/api/league/version">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					TEAM BIG BOARD
				</span>
				<h1>
					RANK IT
					<br></br>
					YOUR WAY.
				</h1>
				<p>
					Private to this team seat and shared by its primary and co-manager. The draft room and autopick use this exact order when your team is on the clock.
				</p>
			</div>
			<div class="draft-clock-panel">
				<span>Players on your board</span>
				<strong class="mono">{data.board_count}</strong>
				<div class="draft-clock-meta">
					<a href="/draft" data-gosx-link>Draft room →</a>
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
			<If cond={data.has_board_error}>
				<p class="error-message">{data.board_error}</p>
			</If>
			<If cond={data.can_edit == false}>
				<p class="demo-message">
					<strong>SIGN IN REQUIRED:</strong>
					use League access to build a board tied to your seat.
				</p>
			</If>
			<If cond={data.pool_status.has_notice}>
				<p class="demo-message">
					<strong>{data.pool_status.label}:</strong>
					{data.pool_status.detail}
					<If cond={data.pool_status.has_last_success}>
						<br></br>
						<span class="mono">LAST SUCCESS · {data.pool_status.last_success} · {data.pool_status.last_success_relative}</span>
					</If>
				</p>
			</If>
			<If cond={data.has_matchup_source}>
				<p class="demo-message">
					<strong>MATCHUP RANKS:</strong>
					ranked from the {data.matchup_source_label}. A higher "-toughest" number is a softer matchup; a lower one is tougher.
				</p>
			</If>
		</div>
		<div class="board-workspace">
			<section class="player-pool">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">01 // YOUR BOARD</span>
						<h2>Ranked queue</h2>
					</div>
					<If cond={data.board_count > 0}>
						<form method="post" action={actionPath("board-clear")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<button class="filter-button" type="submit">Clear board</button>
						</form>
					</If>
				</div>
				<If cond={data.board_count == 0}>
					<div class="empty-tape">
						<strong>NO PLAYERS RANKED</strong>
						<p>
							Add players from the pool on the right. Your order is saved to your seat.
						</p>
					</div>
				</If>
				<div
					class="pool-list"
					data-gosx-reorder
					data-gosx-reorder-action={"POST " + actionPath("board-move-to")}
					data-gosx-csrf-token={csrf.token}
				>
					<Each of={data.board} as="entry">
						<BoardRow
							player={entry}
							RemoveAction={actionPath("board-remove")}
							CSRF={csrf.token}
						 />
					</Each>
				</div>
			</section>
			<section class="player-pool">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">02 // PLAYER POOL</span>
						<h2>Add to board</h2>
					</div>
				</div>
				<div class="pool-search-bar">
					<label class="mono" for="board-search">SEARCH //</label>
					<input
						id="board-search"
						type="search"
						data-gosx-filter="board-pool-rows"
						placeholder="Search player, team, or position"
						autocomplete="off"
					 />
				</div>
				<div class="pool-list pool-list--tall" id="board-pool-rows">
					<Each of={data.available} as="player">
						<article class="pool-row" data-player-position={player.position} data-gosx-filter-text={player.search}>
							<span class="pool-rank mono">{player.rank}</span>
							<div class="pool-player pool-player--photo stat-tip" tabindex="0">
								<If cond={player.has_headshot}>
									<img class="player-headshot" src={player.headshot} alt="" loading="lazy" />
								</If>
								<div class="pool-player__text">
									<strong>{player.name}</strong>
									<If cond={player.has_draft_capital}>
										<span class="badge-rookie">{player.draft_capital}</span>
									</If>
									<small>{player.detail}</small>
									<If cond={player.has_opponent}>
										<small class="mono">
											{player.opponent}
											<If cond={player.has_matchup}>
												·
												<span class="matchup-chip" data-matchup-tier={player.matchup_tier}>{player.matchup_chip}</span>
											</If>
										</small>
									</If>
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
									<If cond={player.has_matchup}>
										<p class="stat-tip__hist mono">{player.matchup_detail}</p>
									</If>
									<If cond={player.has_hist}>
										<p class="stat-tip__hist mono">{player.hist}</p>
									</If>
								</div>
							</div>
							<span class="position-chip">{player.position}</span>
							<b class="mono">{player.projection}</b>
							<form method="post" action={actionPath("board-add")} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name="player_id" value={player.id}></input>
								<If cond={data.can_edit}>
									<button class="draft-button" type="submit">Add</button>
								</If>
								<If cond={data.can_edit == false}>
									<button class="draft-button" type="button" disabled="disabled">Locked</button>
								</If>
							</form>
						</article>
					</Each>
				</div>
			</section>
		</div>
	</main>
}
