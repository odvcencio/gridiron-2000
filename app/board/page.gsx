package board

type BoardRowProps struct {
	Player            map[string]any
	MoveAction        string
	RemoveAction      string
	CSRF              string
	ReturnTargetField string
	ReturnTarget      string
	Position          string
	Query             string
	Page              any
	CanMoveUp         bool
	CanMoveDown       bool
}

func BoardRow(props BoardRowProps) Node {
	return <article class="board-row" data-picked={props.Player.picked} data-gosx-reorder-item={props.Player.id}>
		<span class="board-row__handle" data-gosx-reorder-handle aria-label={"Reorder " + props.Player.name}>⠿</span>
		<span class="pool-rank mono">{props.Player.board_rank}</span>
		<details class="stat-tip">
			<summary class="pool-player pool-player--photo stat-tip__summary">
			<If cond={props.Player.has_headshot}>
				<img class="player-headshot" src={props.Player.headshot} alt="" loading="lazy" />
			</If>
			<span class="pool-player__text">
				<strong>{props.Player.name}</strong>
				<If cond={props.Player.has_draft_capital}>
					<span class="badge-rookie">{props.Player.draft_capital}</span>
				</If>
				<small>{props.Player.detail}</small>
				<If cond={props.Player.has_opponent}>
					<small class="mono">
						{props.Player.opponent}
						<If cond={props.Player.has_matchup}>
							·
							<span class="matchup-chip" data-matchup-tier={props.Player.matchup_tier}>{props.Player.matchup_chip}</span>
						</If>
					</small>
				</If>
			</span>
			</summary>
			<div class="stat-tip__panel">
				<div class="stat-tip__head">
					<strong>{props.Player.name}</strong>
					<span class="mono">{props.Player.jersey}</span>
					<span class="mono stat-tip__team">{props.Player.nfl_team}</span>
				</div>
				<If cond={props.Player.has_breakdown}>
					<div class="stat-tip__rows">
						<Each of={props.Player.breakdown} as="row">
							<div class="stat-tip__row" data-scored={row.scored}>
								<span>{row.label}</span>
								<span class="mono">{row.calc}</span>
								<b class="mono">{row.points}</b>
							</div>
						</Each>
						<div class="stat-tip__total">
							<span>League scoring</span>
							<b class="mono">{props.Player.breakdown_total}</b>
						</div>
					</div>
				</If>
				<If cond={props.Player.has_breakdown == false}>
					<p class="stat-tip__empty">No projection detail for this position.</p>
				</If>
				<If cond={props.Player.has_matchup}>
					<p class="stat-tip__hist mono">{props.Player.matchup_detail}</p>
				</If>
				<If cond={props.Player.has_hist}>
					<p class="stat-tip__hist mono">{props.Player.hist}</p>
					<p class="stat-tip__hist-note">{props.Player.hist_label}</p>
				</If>
			</div>
		</details>
		<span class="position-chip">{props.Player.position}</span>
		<b class="mono">{props.Player.projection}</b>
		<div class="board-controls">
			<form method="post" action={props.MoveAction} data-gosx-managed="true">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="player_id" value={props.Player.id}></input>
				<input type="hidden" name="direction" value="up"></input>
				<input type="hidden" name="pos" value={props.Position}></input>
				<input type="hidden" name="q" value={props.Query}></input>
				<input type="hidden" name="page" value={props.Page}></input>
				<input type="hidden" name={props.ReturnTargetField} value={props.ReturnTarget}></input>
				<If cond={props.CanMoveUp}>
					<button class="board-button board-button--move" type="submit" aria-label={"Move " + props.Player.name + " up"}>↑ <span class="visually-hidden">Move up</span></button>
				</If>
				<If cond={props.CanMoveUp == false}>
					<button class="board-button board-button--move" type="button" disabled="disabled" aria-label={props.Player.name + " is already first"}>↑ <span class="visually-hidden">Already first</span></button>
				</If>
			</form>
			<form method="post" action={props.MoveAction} data-gosx-managed="true">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="player_id" value={props.Player.id}></input>
				<input type="hidden" name="direction" value="down"></input>
				<input type="hidden" name="pos" value={props.Position}></input>
				<input type="hidden" name="q" value={props.Query}></input>
				<input type="hidden" name="page" value={props.Page}></input>
				<input type="hidden" name={props.ReturnTargetField} value={props.ReturnTarget}></input>
				<If cond={props.CanMoveDown}>
					<button class="board-button board-button--move" type="submit" aria-label={"Move " + props.Player.name + " down"}>↓ <span class="visually-hidden">Move down</span></button>
				</If>
				<If cond={props.CanMoveDown == false}>
					<button class="board-button board-button--move" type="button" disabled="disabled" aria-label={props.Player.name + " is already last"}>↓ <span class="visually-hidden">Already last</span></button>
				</If>
			</form>
			<form method="post" action={props.RemoveAction} data-gosx-managed="true">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="player_id" value={props.Player.id}></input>
				<input type="hidden" name="pos" value={props.Position}></input>
				<input type="hidden" name="q" value={props.Query}></input>
				<input type="hidden" name="page" value={props.Page}></input>
				<input type="hidden" name={props.ReturnTargetField} value={props.ReturnTarget}></input>
				<button class="board-button board-button--cut" type="submit" aria-label={"Remove " + props.Player.name}>✕</button>
			</form>
		</div>
	</article>
}

// Page's can_edit branch (gap-audit item 8's second half) replaces the
// seatless masthead that used to render the exact same "Private to this
// team seat ... PLAYERS ON YOUR BOARD 0" copy a seated manager sees —
// reason nowhere in it, and a zero counter that read as a bug, not a
// state. The seatless branch leads with the reason instead (the same
// public_entry projection team/page.gsx and app/blitz/page.gsx already
// render from — seatless_surface_contract_test.go pins the same four
// fields on this file), and drops the "players on your board" counter
// entirely rather than showing a false floor.
func Page() Node {
	return <main class="page board-page" id="main-content" data-gosx-revalidate-interval="4s" data-gosx-revalidate-src="/api/league/version">
		<If cond={data.can_edit}>
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="signal-mark" aria-hidden="true"></span>
					BIG BOARD
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
		</If>
		<If cond={data.can_edit == false}>
		<section class="no-franchise tone-lime">
			<div class="signal-label">
				<span class="signal-mark" aria-hidden="true"></span>
				BIG BOARD // NO FRANCHISE
			</div>
			<h1>{data.public_entry.state_label}</h1>
			<p>{data.public_entry.detail}</p>
			<a href={data.public_entry.action_href} data-gosx-link class="button button--primary">{data.public_entry.action_label}</a>
		</section>
		</If>
		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_notice}>
				<p class="flash-message">{data.notice}</p>
			</If>
			<If cond={data.has_board_error}>
				<p class="error-message">{data.board_error}</p>
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
						<h2>Big Board</h2>
					</div>
					<If cond={data.board_count > 0}>
						<form method="post" action={actionPath("board-clear")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<input type="hidden" name="pos" value={data.pool_position}></input>
							<input type="hidden" name="q" value={data.pool_query}></input>
							<input type="hidden" name="page" value={data.pool_page}></input>
							<input type="hidden" name={data.board_return_target_field} value={data.board_return_target}></input>
							<button class="filter-button" type="submit">Clear board</button>
						</form>
					</If>
				</div>
				<If cond={data.board_count == 0}>
					<If cond={data.can_edit}>
						<div class="empty-tape">
							<strong>NO PLAYERS RANKED</strong>
							<p>
								Add players from the pool on the right. Your order is saved to your seat.
							</p>
						</div>
					</If>
					<If cond={data.can_edit == false}>
						<div class="empty-tape">
							<strong>BROWSE THE PLAYER POOL</strong>
							<p>
								Player rankings remain visible below. A franchise seat is required before this page can save a private draft order.
							</p>
						</div>
					</If>
				</If>
				<div
					class="pool-list pool-list--reorder-scroll"
					data-gosx-reorder
					data-gosx-reorder-action={"POST " + actionPath("board-move-to")}
					data-gosx-csrf-token={csrf.token}
				>
					<Each of={data.board} as="entry">
						<BoardRow
							player={entry}
							MoveAction={actionPath("board-move")}
							RemoveAction={actionPath("board-remove")}
							CSRF={csrf.token}
							ReturnTargetField={data.board_return_target_field}
							ReturnTarget={data.board_return_target}
							Position={data.pool_position}
							Query={data.pool_query}
							Page={data.pool_page}
							CanMoveUp={entry.board_can_move_up}
							CanMoveDown={entry.board_can_move_down}
						 />
					</Each>
					<p class="reorder-status reorder-status--pending">Saving order…</p>
					<p class="reorder-status reorder-status--error">Reorder failed. The previous order was restored.</p>
				</div>
			</section>
			<section class="player-pool" id="board-pool">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">02 // PLAYER POOL</span>
						<If cond={data.can_edit}>
							<h2>Add to board</h2>
						</If>
						<If cond={data.can_edit == false}>
							<h2>Browse available players</h2>
						</If>
					</div>
				</div>
				<div class="position-filters" aria-label="Filter available players by position">
					<Each of={data.position_filters} as="filter">
						<a href={filter.href} data-gosx-link class="filter-button" aria-current={filter.active}>{filter.label}</a>
					</Each>
				</div>
				<form method="get" action="/board#board-pool" class="pool-search-bar">
					<label class="mono" for="board-search">SEARCH //</label>
					<input
						id="board-search"
						type="search"
						name="q"
						value={data.pool_query}
						placeholder="Search player, team, or position"
						autocomplete="off"
					 />
					<If cond={data.pool_position != ""}>
						<input type="hidden" name="pos" value={data.pool_position}></input>
					</If>
					<button class="filter-button" type="submit">Search</button>
					<If cond={data.has_filters}>
						<a class="filter-button" href={data.clear_filters_href} data-gosx-link>Clear</a>
					</If>
				</form>
				<If cond={data.matching_count > 0}>
					<p class="scoring-note" aria-live="polite">
						Showing {data.pool_page_start}–{data.pool_page_end} of {data.matching_count} matching players · {data.available_count} available overall · page {data.pool_page} of {data.pool_pages}
					</p>
				</If>
				<div class="pool-list pool-list--tall">
					<Each of={data.available} as="player">
						<article class="pool-row" data-player-position={player.position}>
							<span class="pool-rank mono">{player.rank}</span>
							<details class="stat-tip">
								<summary class="pool-player pool-player--photo stat-tip__summary">
								<If cond={player.has_headshot}>
									<img class="player-headshot" src={player.headshot} alt="" loading="lazy" />
								</If>
								<span class="pool-player__text">
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
								</span>
								</summary>
								<div class="stat-tip__panel">
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
										<p class="stat-tip__hist-note">{player.hist_label}</p>
									</If>
								</div>
							</details>
							<span class="position-chip">{player.position}</span>
							<b class="mono">{player.projection}</b>
							<form method="post" action={actionPath("board-add")} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name="player_id" value={player.id}></input>
								<input type="hidden" name="pos" value={data.pool_position}></input>
								<input type="hidden" name="q" value={data.pool_query}></input>
								<input type="hidden" name="page" value={data.pool_page}></input>
								<input type="hidden" name={data.board_return_target_field} value={data.board_return_target}></input>
								<If cond={data.can_edit}>
									<button class="button button--ghost" type="submit">RANK</button>
								</If>
								<If cond={data.can_edit == false}>
									<button class="button button--ghost" type="button" disabled="disabled">Locked</button>
								</If>
							</form>
						</article>
					</Each>
				</div>
				<If cond={data.available_empty}>
					<div class="empty-tape">
						<strong>POOL EMPTY</strong>
						<p>Every player is already on your board or has been picked.</p>
					</div>
				</If>
				<If cond={data.available_empty == false && data.matching_empty}>
					<div class="empty-tape">
						<strong>NO PLAYERS MATCH</strong>
						<p>Try another position or search, or clear the filters to return to the full available pool.</p>
						<a class="filter-button" href={data.clear_filters_href} data-gosx-link>Clear filters</a>
					</div>
				</If>
				<nav class="pool-pagination" aria-label="Big Board player pool pages">
					<If cond={data.pool_has_previous}>
						<a class="filter-button" href={data.pool_previous_href} data-gosx-link rel="prev">← Previous</a>
					</If>
					<Each of={data.pool_page_links} as="link">
						<a class="filter-button" href={link.href} data-gosx-link aria-current={link.current}>{link.label}</a>
					</Each>
					<If cond={data.pool_has_next}>
						<a class="filter-button" href={data.pool_next_href} data-gosx-link rel="next">Next →</a>
					</If>
				</nav>
			</section>
		</div>
	</main>
}
