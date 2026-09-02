package players

func Page() Node {
	return <main class="page board-page" id="main-content" data-gosx-revalidate-interval="4s" data-gosx-revalidate-src="/api/league/version">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="signal-mark" aria-hidden="true"></span>
					PLAYER POOL
				</span>
				<h1>PLAYER POOL.</h1>
				<p>
					Every pool player, rostered or free. Sign a free agent, claim a waiver, or drop one of your own — the transaction feed records every move.
				</p>
			</div>
			<div class="draft-clock-panel">
				<span>Draftable roster capacity</span>
				<If cond={data.pool_unavailable}>
					<strong class="mono">DATA PAUSED</strong>
				</If>
				<If cond={data.pool_unavailable == false && data.can_edit}>
					<strong class="mono">
						{data.roster_size}
						/
						{data.roster_cap}
					</strong>
					<div class="roster-capacity-breakdown" aria-label="Roster capacity breakdown">
						<span>
							<b>GENERAL</b>
							<strong class="mono">{data.roster_general_size} / {data.roster_general_cap}</strong>
						</span>
						<span>
							<b>RESERVE</b>
							<strong class="mono">{data.roster_reserve_size} / {data.roster_reserve_cap}</strong>
						</span>
						<span>
							<b>IR · OUTSIDE CAP</b>
							<strong class="mono">{data.roster_ir_size} / {data.roster_ir_cap}</strong>
						</span>
					</div>
					<p class="roster-capacity-note">Reserve counts toward draftable capacity. IR is owned roster space outside that cap.</p>
				</If>
				<If cond={data.pool_unavailable == false && data.can_edit == false}>
					<strong class="mono">NO TEAM</strong>
				</If>
				<div class="draft-clock-meta">
					<a href="/activity" data-gosx-link>Transaction feed →</a>
					<If cond={data.pool_unavailable == false && data.can_edit}>
						<a href="/team" data-gosx-link>Team terminal →</a>
					</If>
					<If cond={data.pool_unavailable == false && data.can_edit == false}>
						<If cond={data.public_entry.can_claim || data.public_entry.action_href != "/join"}>
							<a href={data.public_entry.action_href} data-gosx-link>
								{data.public_entry.action_label}
							</a>
						</If>
					</If>
					<If cond={data.public_entry.is_commissioner}>
						<a href={data.public_entry.commissioner_href} data-gosx-link>{data.public_entry.commissioner_label}</a>
					</If>
				</div>
			</div>
		</section>
		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_notice}>
				<p class="flash-message">{data.notice}</p>
			</If>
			<If cond={data.has_players_error}>
				<p class="error-message">{data.players_error}</p>
			</If>
			<If cond={data.pool_unavailable == false && data.can_edit == false}>
				<p class="demo-message">
					<strong>{data.public_entry.state_label}:</strong>
					{data.public_entry.detail}
					<If cond={data.public_entry.can_claim || data.public_entry.action_href != "/join"}>
						<a href={data.public_entry.action_href} data-gosx-link>
							{data.public_entry.action_label}
						</a>
					</If>
					<If cond={data.public_entry.is_commissioner}>
						<a href={data.public_entry.commissioner_href} data-gosx-link>{data.public_entry.commissioner_label}</a>
					</If>
				</p>
			</If>
			<If cond={data.pool_unavailable == false && data.free_agency_open == false}>
				<p class="demo-message">
					<strong>FREE AGENCY OPENS AFTER THE DRAFT:</strong>
					every undrafted player becomes a free agent the moment the draft completes.
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
		<div
			id="players-pool-region"
			data-gosx-region
			data-gosx-region-url={data.pool_fragment_url}
			data-gosx-region-interval={data.pool_fragment_interval}
			data-gosx-region-signal="$players.state.refresh"
			aria-label="Authoritative player pool"
		>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">01 // PLAYER LIST</span>
					<h2>Browse the pool</h2>
				</div>
			</div>
			<div class="position-filters" aria-label="Filter the player pool by position">
				<Each of={data.positions} as="tab">
					<a href={tab.href} data-gosx-link class="filter-button" aria-current={tab.active}>{tab.label}</a>
				</Each>
			</div>
			<form method="get" action="/players" class="pool-search-bar">
				<label class="mono" for="players-search">SEARCH //</label>
				<input id="players-search" type="search" name="q" value={data.query} placeholder="Search player or team" autocomplete="off"></input>
				<If cond={data.pos != ""}>
					<input type="hidden" name="pos" value={data.pos}></input>
				</If>
				<button class="filter-button" type="submit">Search</button>
			</form>
			<p class="scoring-note" aria-live="polite">
				<If cond={data.pool_total > 0}>
					Showing {data.pool_page_start}–{data.pool_page_end} of {data.pool_total} players · page {data.pool_page} of {data.pool_pages}
				</If>
				<If cond={data.pool_total == 0 && data.pool_unavailable == false}>No players match this search.</If>
			</p>
			<If cond={data.players_empty && data.pool_unavailable == false}>
				<div class="empty-tape">
					<strong>NO PLAYERS MATCH</strong>
					<p>
						Try a different position filter or clear your search.
					</p>
				</div>
			</If>
			<If cond={data.pool_unavailable}>
				<div class="empty-tape">
					<strong>PLAYER DATA UNAVAILABLE</strong>
					<p>
						The authoritative player list is temporarily unavailable. Browsing and roster/waiver actions resume after the source recovers.
					</p>
				</div>
			</If>
			<div class="pool-list pool-list--tall">
				<Each of={data.players} as="player">
					<article class="pool-row pool-row--status" data-player-position={player.position}>
						<span class="pool-rank mono">
							{player.rank}
							<If cond={player.has_house_rank}>
								<small class="house-rank">{player.house_rank}</small>
							</If>
						</span>
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
									<span class="mono">{player.position}</span>
									<span class="mono stat-tip__team">{player.nfl_team}</span>
								</div>
								<div class="stat-tip__rows">
									<div class="stat-tip__row">
										<span>Projection</span>
										<b class="mono">{player.projection}</b>
									</div>
									<If cond={player.rostered}>
										<div class="stat-tip__row">
											<span>Availability</span>
											<b class="mono" title={player.owner_name}>ROSTERED · {player.owner_abbr}</b>
										</div>
									</If>
									<If cond={player.on_waivers}>
										<div class="stat-tip__row">
											<span>Availability</span>
											<span class="mono">ON WAIVERS</span>
											<b class="mono">{player.waiver_resolves}</b>
										</div>
									</If>
									<If cond={player.free_agent}>
										<div class="stat-tip__row">
											<span>Availability</span>
											<b class="mono">FREE AGENT</b>
										</div>
									</If>
									<If cond={player.claimed_by_me}>
										<p class="stat-tip__hist mono">Claim filed for this player.</p>
									</If>
									<If cond={player.needs_drop && player.can_add}>
										<p class="stat-tip__hist mono">Adding requires a drop from your full roster.</p>
									</If>
								</div>
								<If cond={player.has_opponent}>
									<p class="stat-tip__hist mono">{player.opponent}</p>
									<If cond={player.has_matchup}>
										<p class="stat-tip__hist mono">{player.matchup_detail}</p>
									</If>
								</If>
								<If cond={player.has_hist}>
									<p class="stat-tip__hist mono">{player.hist}</p>
									<p class="stat-tip__hist-note">{player.hist_label}</p>
								</If>
							</div>
						</details>
						<span class="position-chip">{player.position}</span>
						<b class="mono">{player.projection}</b>
						<If cond={player.rostered}>
							<span class="position-chip position-chip--locked" title={player.owner_name}>{player.owner_abbr}</span>
						</If>
						<If cond={player.on_waivers}>
							<span class="position-chip">ON WAIVERS · {player.waiver_resolves}</span>
						</If>
						<If cond={player.free_agent}>
							<span class="position-chip">FREE AGENT</span>
						</If>
						<If cond={data.can_edit}>
						<div class="board-controls">
							<If cond={player.can_add}>
							<form method="post" action={actionPath("player-add")} data-gosx-managed="true" data-gosx-action-signal="$players.state.refresh" class="lineup-slot__form">
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
									<input type="hidden" name="player_id" value={player.id}></input>
									<input type="hidden" name="pos" value={data.pos}></input>
									<input type="hidden" name="q" value={data.query}></input>
									<input type="hidden" name="page" value={data.pool_page}></input>
									<If cond={player.needs_drop}>
										<select name="drop_id" aria-label={"Choose a player to drop for " + player.name}>
											<option value="">Choose a player to drop</option>
											<Each of={data.drop_options} as="opt">
												<option value={opt.id}>{opt.label}</option>
											</Each>
										</select>
									</If>
									<If cond={player.needs_drop == false}>
										<button class="draft-button" type="submit">SIGN</button>
									</If>
									<If cond={player.needs_drop}>
										<details class="action-confirmation">
											<summary>Add and drop a player</summary>
											<p>Adding {player.name} will immediately replace the player you select above. The drop is recorded and cannot be undone from this screen.</p>
											<label>
												<input type="checkbox" name="confirmation" value="add-drop-player" required="required"></input>
												I understand this replaces a rostered player.
											</label>
											<button class="draft-button" type="submit">Confirm add and drop</button>
										</details>
									</If>
								</form>
							</If>
							<If cond={player.can_claim}>
								<form method="post" action={actionPath("claim-file") + "#waivers"} data-gosx-managed="true" data-gosx-action-signal="$players.state.refresh" class="lineup-slot__form">
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
									<input type="hidden" name="player_id" value={player.id}></input>
									<input type="hidden" name="pos" value={data.pos}></input>
									<input type="hidden" name="q" value={data.query}></input>
									<input type="hidden" name="page" value={data.pool_page}></input>
									<If cond={player.needs_drop}>
										<select name="drop_id" aria-label={"Choose a player to drop for " + player.name}>
											<option value="">Choose a player to drop</option>
											<Each of={data.drop_options} as="opt">
												<option value={opt.id}>{opt.label}</option>
											</Each>
										</select>
									</If>
									<If cond={data.waivers_faab}>
										<input type="number" name="bid" min="0" max={data.my_faab_remaining} placeholder="Bid FAAB" aria-label={"Bid for " + player.name}></input>
									</If>
									<button class="draft-button" type="submit">CLAIM</button>
								</form>
							</If>
							<If cond={player.claimed_by_me}>
								<span class="position-chip">CLAIM FILED</span>
							</If>
							<If cond={player.can_add == false && player.can_claim == false && player.rostered == false && player.claimed_by_me == false}>
								<span class="control-locked">
									<button class="draft-button" type="button" disabled="disabled" title={data.add_locked_reason}>SIGN</button>
									<If cond={data.add_locked_reason != ""}>
										<small class="control-locked__reason">{data.add_locked_reason}</small>
									</If>
								</span>
							</If>
							<If cond={player.can_drop}>
								<form method="post" action={actionPath("player-drop")} data-gosx-managed="true" data-gosx-action-signal="$players.state.refresh">
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
									<input type="hidden" name="player_id" value={player.id}></input>
									<input type="hidden" name="pos" value={data.pos}></input>
									<input type="hidden" name="q" value={data.query}></input>
									<input type="hidden" name="page" value={data.pool_page}></input>
									<details class="action-confirmation">
										<summary>{"Drop " + player.name}</summary>
										<p>Dropping this player removes them from your roster and starts the waiver process. This roster change cannot be undone from this screen.</p>
										<label>
											<input type="checkbox" name="confirmation" value="drop-player" required="required"></input>
											I understand this player will leave my roster.
										</label>
										<button class="board-button board-button--cut" type="submit" aria-label={"Confirm drop " + player.name}>Confirm drop</button>
									</details>
								</form>
							</If>
							<If cond={player.drop_locked}>
								<small class="position-chip position-chip--locked" role="status">{player.drop_lock_reason}</small>
							</If>
						</div>
						</If>
					</article>
				</Each>
			</div>
			<nav class="pool-pagination" aria-label="Player pool pages">
				<If cond={data.pool_has_previous}>
					<a class="filter-button" href={data.pool_previous_href} data-gosx-link rel="prev">← Previous</a>
				</If>
				<span class="mono">Page {data.pool_page} / {data.pool_pages}</span>
				<If cond={data.pool_has_next}>
					<a class="filter-button" href={data.pool_next_href} data-gosx-link rel="next">Next →</a>
				</If>
			</nav>
		</section>
		</div>
		<div
			id="waivers"
			data-gosx-region
			data-gosx-region-url={data.waiver_fragment_url}
			data-gosx-region-interval={data.waiver_fragment_interval}
			data-gosx-region-signal="$players.state.refresh"
			aria-label="Authoritative waiver desk"
		>
		<section class="player-pool" id="waivers-content">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">02 // WAIVER DESK</span>
					<If cond={data.pool_unavailable}>
						<h2>Player data unavailable</h2>
					</If>
					<If cond={data.pool_unavailable == false && data.can_edit}>
						<h2>My claims</h2>
					</If>
					<If cond={data.pool_unavailable == false && data.can_edit == false}>
						<h2>Waiver access</h2>
					</If>
				</div>
				<If cond={data.can_edit}>
					<If cond={data.waivers_faab == false}>
						<span class="mono">Team waiver position {data.my_waiver_position} of {data.waiver_team_count}</span>
					</If>
					<If cond={data.waivers_faab}>
						<span class="mono">Budget {data.my_faab_remaining} <abbr title="Free Agent Acquisition Budget">FAAB</abbr></span>
					</If>
				</If>
			</div>
			<If cond={data.pool_unavailable}>
				<div class="empty-tape">
					<strong>WAIVER ACTIONS PAUSED</strong>
					<p>Claims and roster changes are blocked until the authoritative player list is available again.</p>
				</div>
			</If>
			<If cond={data.pool_unavailable == false && data.can_edit == false}>
				<div class="empty-tape">
					<strong>{data.public_entry.state_label}</strong>
					<p>
						{data.public_entry.detail}
					</p>
					<If cond={data.public_entry.can_claim || data.public_entry.action_href != "/join"}>
						<a class="draft-button" href={data.public_entry.action_href} data-gosx-link>
							{data.public_entry.action_label}
						</a>
					</If>
				</div>
			</If>
			<If cond={data.can_edit}>
				<p class="scoring-note waiver-desk-explainer">
					Your numbered claim order is private to this team and controls which of your requests runs first. Team waiver position is the separate public league tiebreaker.
					<If cond={data.waivers_faab}>
						Higher FAAB bids run first; tied bids use public team waiver position, then this private order for claims from the same team.
					</If>
				</p>
				<If cond={data.my_claims_empty}>
					<div class="empty-tape">
						<strong>NO OPEN CLAIMS</strong>
						<p>
							File a claim from an ON WAIVERS row above; it resolves once waivers clear.
						</p>
					</div>
				</If>
				<div class="pool-list waiver-claim-list">
					<Each of={data.my_claims} as="claim">
						<article class="waiver-claim-row">
							<div class="pool-player">
								<div class="pool-player__text">
									<strong>{claim.add_name}</strong>
									<small>
										<If cond={claim.has_drop}>
											drops {claim.drop_label} ·
										</If>
										filed {claim.filed_at}
									</small>
								</div>
							</div>
							<div class="waiver-claim-meta">
								<span class="position-chip">{claim.add_position}</span>
								<b class="mono">Claim order {claim.priority} of {claim.claim_count}</b>
								<span class="mono">Team waiver position {claim.waiver_position} of {claim.waiver_team_count}</span>
								<If cond={claim.faab}>
									<span class="mono">Bid {claim.bid} FAAB</span>
								</If>
								<If cond={claim.resolution_state == "scheduled"}>
									<span class="mono">RESOLVES {claim.resolution_at} ({claim.resolution_relative})</span>
								</If>
								<If cond={claim.resolution_state == "overdue"}>
									<span class="position-chip position-chip--warn">RESOLUTION OVERDUE</span>
									<small>{claim.resolution_at} ({claim.resolution_relative})</small>
								</If>
								<If cond={claim.resolution_state == "degraded"}>
									<span class="position-chip position-chip--warn">RESOLUTION DEGRADED</span>
									<small>{claim.resolution_label}</small>
								</If>
								<If cond={claim.resolution_state == "unknown"}>
									<span class="position-chip position-chip--warn">RESOLUTION UNKNOWN</span>
									<small>{claim.resolution_label}</small>
								</If>
								<If cond={claim.resolution_state == "deferred"}>
									<span class="position-chip position-chip--warn">CLAIM DEFERRED</span>
									<small>{claim.resolution_label}</small>
								</If>
							</div>
							<div class="waiver-claim-actions" aria-label={"Filing-order controls for " + claim.add_name}>
								<If cond={claim.can_move_up}>
									<form method="post" action={actionPath("claim-move") + "#waivers"} data-gosx-managed="true" data-gosx-action-signal="$players.state.refresh">
										<input type="hidden" name="csrf_token" value={csrf.token}></input>
										<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
										<input type="hidden" name="claim_id" value={claim.id}></input>
										<input type="hidden" name="direction" value="up"></input>
										<input type="hidden" name="pos" value={data.pos}></input>
										<input type="hidden" name="q" value={data.query}></input>
										<input type="hidden" name="page" value={data.pool_page}></input>
										<button class="board-button" type="submit" aria-label={"Move claim for " + claim.add_name + " up one position"}>Move up</button>
									</form>
								</If>
								<If cond={claim.can_move_down}>
									<form method="post" action={actionPath("claim-move") + "#waivers"} data-gosx-managed="true" data-gosx-action-signal="$players.state.refresh">
										<input type="hidden" name="csrf_token" value={csrf.token}></input>
										<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
										<input type="hidden" name="claim_id" value={claim.id}></input>
										<input type="hidden" name="direction" value="down"></input>
										<input type="hidden" name="pos" value={data.pos}></input>
										<input type="hidden" name="q" value={data.query}></input>
										<input type="hidden" name="page" value={data.pool_page}></input>
										<button class="board-button" type="submit" aria-label={"Move claim for " + claim.add_name + " down one position"}>Move down</button>
									</form>
								</If>
								<form method="post" action={actionPath("claim-cancel") + "#waivers"} data-gosx-managed="true" data-gosx-action-signal="$players.state.refresh">
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
									<input type="hidden" name="claim_id" value={claim.id}></input>
									<input type="hidden" name="pos" value={data.pos}></input>
									<input type="hidden" name="q" value={data.query}></input>
									<input type="hidden" name="page" value={data.pool_page}></input>
									<button class="board-button board-button--cut" type="submit" aria-label={"Cancel claim for " + claim.add_name}>Cancel</button>
								</form>
							</div>
						</article>
					</Each>
				</div>
			</If>
			<div class="pool-toolbar">
				<div>
					<span class="section-index">03 // WAIVER ORDER</span>
					<h2>This week's claim order</h2>
				</div>
			</div>
			<ol class="waiver-order-strip">
				<Each of={data.waiver_order} as="slot">
					<li aria-current={slot.mine}>
						<span class="mono">{slot.position}</span>
						<span title={slot.name}>{slot.abbr}</span>
					</li>
				</Each>
			</ol>
			<If cond={data.can_edit}>
				<div class="pool-toolbar waiver-receipts-heading">
					<div>
						<span class="section-index">PRIVATE RECEIPTS</span>
						<h2>Recent waiver outcomes</h2>
					</div>
					<span class="mono">THIS TEAM ONLY</span>
				</div>
				<p class="scoring-note">Receipts persist independently of email settings. The newest 20 outcomes for this franchise appear here.</p>
				<If cond={data.my_receipts_empty}>
					<div class="empty-tape">
						<strong>NO WAIVER RECEIPTS YET</strong>
						<p>Won, beaten, and failed claims will appear after the waiver processor resolves them.</p>
					</div>
				</If>
				<div class="waiver-receipt-list">
					<Each of={data.my_waiver_receipts} as="receipt">
						<article class="waiver-receipt-row">
							<div class="waiver-receipt-outcome">
								<strong class="mono">{receipt.outcome}</strong>
								<span>Week {receipt.week} · {receipt.resolved_at}</span>
							</div>
							<div class="waiver-receipt-player">
								<strong>{receipt.add_name} <span class="position-chip">{receipt.add_position}</span></strong>
								<If cond={receipt.has_drop}>
									<span>Drops {receipt.drop_label}</span>
								</If>
								<span>{receipt.reason}</span>
								<If cond={receipt.has_winner}>
									<span>Winner: {receipt.winner_name} ({receipt.winner_abbr})</span>
								</If>
							</div>
							<div class="waiver-receipt-order mono">
								<span>Filed order {receipt.submitted_order}</span>
								<span>Team position {receipt.waiver_position} of {receipt.waiver_team_count}</span>
								<If cond={receipt.faab}>
									<span>Bid {receipt.bid} FAAB</span>
								</If>
							</div>
						</article>
					</Each>
				</div>
			</If>
			<If cond={data.is_commissioner}>
				<div class="pool-toolbar waiver-receipts-heading">
					<div>
						<span class="section-index">COMMISSIONER RECEIPTS</span>
						<h2>All teams' waiver outcomes</h2>
					</div>
					<span class="mono">COMMISSIONER ONLY</span>
				</div>
				<p class="scoring-note">The newest 50 outcomes across every team appear here — oversight only; a commissioner cannot alter a resolved claim from this view.</p>
				<If cond={data.commissioner_receipts_empty}>
					<div class="empty-tape">
						<strong>NO WAIVER RECEIPTS YET</strong>
						<p>Won, beaten, and failed claims will appear after the waiver processor resolves them.</p>
					</div>
				</If>
				<div class="waiver-receipt-list">
					<Each of={data.commissioner_waiver_receipts} as="receipt">
						<article class="waiver-receipt-row">
							<div class="waiver-receipt-outcome">
								<strong class="mono">{receipt.outcome}</strong>
								<span>{receipt.team_abbr} · Week {receipt.week} · {receipt.resolved_at}</span>
							</div>
							<div class="waiver-receipt-player">
								<strong>{receipt.add_name} <span class="position-chip">{receipt.add_position}</span></strong>
								<If cond={receipt.has_drop}>
									<span>Drops {receipt.drop_label}</span>
								</If>
								<span>{receipt.reason}</span>
								<If cond={receipt.has_winner}>
									<span>Winner: {receipt.winner_name} ({receipt.winner_abbr})</span>
								</If>
								<If cond={receipt.has_winning_bid}>
									<span>Winning bid: {receipt.winning_bid} FAAB</span>
								</If>
							</div>
							<div class="waiver-receipt-order mono">
								<span>Filed order {receipt.submitted_order}</span>
								<span>Team position {receipt.waiver_position} of {receipt.waiver_team_count}</span>
								<If cond={receipt.faab}>
									<span>Bid {receipt.bid} FAAB</span>
								</If>
							</div>
						</article>
					</Each>
				</div>
			</If>
		</section>
		</div>
		<p class="scoring-note lineup-sync-note" role="status" aria-live="polite">
			Player pool and waiver state refresh automatically within 4 seconds after a manager change.
			If a refresh fails, use
			<button type="button" class="board-button" data-gosx-set="$players.state.refresh" data-gosx-set-value="manual">Refresh player and waiver state now</button>.
		</p>
	</main>
}

// PlayerPoolRegion is the bounded fragment swapped by the player-pool region.
// The full page keeps the richer initial render above as the native fallback;
// this component carries the same authoritative rows, filters, action fields,
// and pagination state for later convergence without replacing the shell.
func PlayerPoolRegion() Node {
	return <section class="player-pool">
		<div class="pool-toolbar">
			<div>
				<span class="section-index">01 // PLAYER LIST</span>
				<h2>Browse the pool</h2>
			</div>
		</div>
		<div class="position-filters" aria-label="Filter the player pool by position">
			<Each of={data.positions} as="tab">
				<a href={tab.href} data-gosx-link class="filter-button" aria-current={tab.active}>{tab.label}</a>
			</Each>
		</div>
		<form method="get" action="/players" class="pool-search-bar">
			<label class="mono" for="players-sync-search">SEARCH //</label>
			<input id="players-sync-search" type="search" name="q" value={data.query} placeholder="Search player or team" autocomplete="off"></input>
			<If cond={data.pos != ""}>
				<input type="hidden" name="pos" value={data.pos}></input>
			</If>
			<If cond={data.pool_page > 1}>
				<input type="hidden" name="page" value={data.pool_page}></input>
			</If>
			<button class="filter-button" type="submit">Search</button>
		</form>
		<p class="scoring-note" aria-live="polite">
			<If cond={data.pool_total > 0}>Showing {data.pool_page_start}–{data.pool_page_end} of {data.pool_total} players · page {data.pool_page} of {data.pool_pages}</If>
			<If cond={data.pool_total == 0 && data.pool_unavailable == false}>No players match this search.</If>
		</p>
		<If cond={data.pool_unavailable}>
			<div class="empty-tape"><strong>PLAYER DATA UNAVAILABLE</strong><p>The authoritative player list is temporarily unavailable.</p></div>
		</If>
		<If cond={data.players_empty && data.pool_unavailable == false}>
			<div class="empty-tape"><strong>NO PLAYERS MATCH</strong><p>Try a different position filter or clear your search.</p></div>
		</If>
		<div class="pool-list pool-list--tall">
			<Each of={data.players} as="player">
				<article class="pool-row pool-row--status" data-player-position={player.position}>
					<span class="pool-rank mono">{player.rank}<If cond={player.has_house_rank}><small class="house-rank">{player.house_rank}</small></If></span>
					<details class="stat-tip">
						<summary class="pool-player pool-player--photo stat-tip__summary">
							<If cond={player.has_headshot}><img class="player-headshot" src={player.headshot} alt="" loading="lazy" /></If>
							<span class="pool-player__text"><strong>{player.name}</strong><If cond={player.has_draft_capital}><span class="badge-rookie">{player.draft_capital}</span></If><small>{player.detail}</small><If cond={player.has_opponent}><small class="mono">{player.opponent}<If cond={player.has_matchup}> · <span class="matchup-chip" data-matchup-tier={player.matchup_tier}>{player.matchup_chip}</span></If></small></If></span>
						</summary>
						<div class="stat-tip__panel">
							<div class="stat-tip__head"><strong>{player.name}</strong><span class="mono">{player.position}</span><span class="mono stat-tip__team">{player.nfl_team}</span></div>
							<div class="stat-tip__rows"><div class="stat-tip__row"><span>Projection</span><b class="mono">{player.projection}</b></div><If cond={player.rostered}><div class="stat-tip__row"><span>Availability</span><b class="mono" title={player.owner_name}>ROSTERED · {player.owner_abbr}</b></div></If><If cond={player.on_waivers}><div class="stat-tip__row"><span>Availability</span><span class="mono">ON WAIVERS</span><b class="mono">{player.waiver_resolves}</b></div></If><If cond={player.free_agent}><div class="stat-tip__row"><span>Availability</span><b class="mono">FREE AGENT</b></div></If><If cond={player.claimed_by_me}><p class="stat-tip__hist mono">Claim filed for this player.</p></If><If cond={player.needs_drop && player.can_add}><p class="stat-tip__hist mono">Adding requires a drop from your full roster.</p></If></div>
							<If cond={player.has_opponent}><p class="stat-tip__hist mono">{player.opponent}</p><If cond={player.has_matchup}><p class="stat-tip__hist mono">{player.matchup_detail}</p></If></If><If cond={player.has_hist}><p class="stat-tip__hist mono">{player.hist}</p><p class="stat-tip__hist-note">{player.hist_label}</p></If>
						</div>
					</details>
					<span class="position-chip">{player.position}</span>
					<b class="mono">{player.projection}</b>
					<If cond={player.rostered}><span class="position-chip position-chip--locked" title={player.owner_name}>{player.owner_abbr}</span></If>
					<If cond={player.on_waivers}><span class="position-chip">ON WAIVERS · {player.waiver_resolves}</span></If>
					<If cond={player.free_agent}><span class="position-chip">FREE AGENT</span></If>
					<If cond={data.can_edit}>
						<div class="board-controls">
							<If cond={player.can_add}>
								<form method="post" action={actionPath("player-add")} data-gosx-managed="true" data-gosx-action-signal="$players.state.refresh" class="lineup-slot__form">
									<input type="hidden" name="csrf_token" value={csrf.token}></input><input type="hidden" name="team_id" value={data.viewer.team_id}></input><input type="hidden" name="player_id" value={player.id}></input><input type="hidden" name="pos" value={data.pos}></input><input type="hidden" name="q" value={data.query}></input><input type="hidden" name="page" value={data.pool_page}></input>
									<If cond={player.needs_drop}>
										<select name="drop_id" aria-label={"Choose a player to drop for " + player.name}><option value="">Choose a player to drop</option><Each of={data.drop_options} as="opt"><option value={opt.id}>{opt.label}</option></Each></select>
										<label><input type="checkbox" name="confirmation" value="add-drop-player" required="required"></input>I understand this replaces a rostered player.</label>
										<button class="draft-button" type="submit">Confirm add and drop</button>
									</If>
									<If cond={player.needs_drop == false}><button class="draft-button" type="submit">SIGN</button></If>
								</form>
							</If>
							<If cond={player.can_claim}>
								<form method="post" action={actionPath("claim-file") + "#waivers"} data-gosx-managed="true" data-gosx-action-signal="$players.state.refresh" class="lineup-slot__form">
									<input type="hidden" name="csrf_token" value={csrf.token}></input><input type="hidden" name="team_id" value={data.viewer.team_id}></input><input type="hidden" name="player_id" value={player.id}></input><input type="hidden" name="pos" value={data.pos}></input><input type="hidden" name="q" value={data.query}></input><input type="hidden" name="page" value={data.pool_page}></input>
									<If cond={data.waivers_faab}><input type="number" name="bid" min="0" max={data.my_faab_remaining} placeholder="Bid FAAB" aria-label={"Bid for " + player.name}></input></If><button class="draft-button" type="submit">CLAIM</button>
								</form>
							</If>
							<If cond={player.claimed_by_me}><span class="position-chip">CLAIM FILED</span></If>
							<If cond={player.can_add == false && player.can_claim == false && player.rostered == false && player.claimed_by_me == false}><span class="control-locked"><button class="draft-button" type="button" disabled="disabled" title={data.add_locked_reason}>SIGN</button><If cond={data.add_locked_reason != ""}><small class="control-locked__reason">{data.add_locked_reason}</small></If></span></If>
							<If cond={player.can_drop}>
								<form method="post" action={actionPath("player-drop")} data-gosx-managed="true" data-gosx-action-signal="$players.state.refresh">
									<input type="hidden" name="csrf_token" value={csrf.token}></input><input type="hidden" name="team_id" value={data.viewer.team_id}></input><input type="hidden" name="player_id" value={player.id}></input><input type="hidden" name="pos" value={data.pos}></input><input type="hidden" name="q" value={data.query}></input><input type="hidden" name="page" value={data.pool_page}></input><label><input type="checkbox" name="confirmation" value="drop-player" required="required"></input>I understand this player will leave my roster.</label><button class="board-button board-button--cut" type="submit">Confirm drop</button>
								</form>
							</If>
							<If cond={player.drop_locked}><small class="position-chip position-chip--locked" role="status">{player.drop_lock_reason}</small></If>
						</div>
					</If>
				</article>
			</Each>
		</div>
		<nav class="pool-pagination" aria-label="Player pool pages">
			<If cond={data.pool_has_previous}><a class="filter-button" href={data.pool_previous_href} data-gosx-link rel="prev">← Previous</a></If>
			<span class="mono">Page {data.pool_page} / {data.pool_pages}</span>
			<If cond={data.pool_has_next}><a class="filter-button" href={data.pool_next_href} data-gosx-link rel="next">Next →</a></If>
		</nav>
	</section>
}

// WaiverDeskRegion keeps private claims/receipts and the league order in one
// server-authoritative swap. Query/filter/page values are carried by the
// fragment URL and every native form, so a failed refresh never resets them.
//
// Its own waiver-desk-explainer paragraph, below, is a hand-duplicated copy
// of Page()'s (item 10, 2026-08-31 post-wave audit): the two drifted apart
// ("controls which requests run first" here vs "controls which of your
// requests runs first" in Page()), so a manager reading the fragment's 4s
// interval refresh saw different wording than the initial page load. Keep
// this paragraph's text byte-for-byte identical to Page()'s own
// waiver-desk-explainer <p> if either ever needs to change.
func WaiverDeskRegion() Node {
	return <section class="player-pool">
		<div class="pool-toolbar"><div><span class="section-index">02 // WAIVER DESK</span><If cond={data.pool_unavailable}><h2>Player data unavailable</h2></If><If cond={data.pool_unavailable == false && data.can_edit}><h2>My claims</h2></If><If cond={data.pool_unavailable == false && data.can_edit == false}><h2>Waiver access</h2></If></div><If cond={data.can_edit}><If cond={data.waivers_faab == false}><span class="mono">Team waiver position {data.my_waiver_position} of {data.waiver_team_count}</span></If><If cond={data.waivers_faab}><span class="mono">Budget {data.my_faab_remaining} FAAB</span></If></If></div>
		<If cond={data.pool_unavailable}><div class="empty-tape"><strong>WAIVER ACTIONS PAUSED</strong><p>Claims and roster changes are blocked until the authoritative player list is available again.</p></div></If>
		<If cond={data.pool_unavailable == false && data.can_edit == false}><div class="empty-tape"><strong>{data.public_entry.state_label}</strong><p>{data.public_entry.detail}</p><If cond={data.public_entry.can_claim || data.public_entry.action_href != "/join"}><a class="draft-button" href={data.public_entry.action_href} data-gosx-link>{data.public_entry.action_label}</a></If></div></If>
		<If cond={data.can_edit}>
			<p class="scoring-note waiver-desk-explainer">Your numbered claim order is private to this team and controls which of your requests runs first. Team waiver position is the separate public league tiebreaker.<If cond={data.waivers_faab}> Higher FAAB bids run first; tied bids use public team waiver position, then this private order for claims from the same team.</If></p>
			<If cond={data.my_claims_empty}><div class="empty-tape"><strong>NO OPEN CLAIMS</strong><p>File a claim from an ON WAIVERS row above; it resolves once waivers clear.</p></div></If>
			<div class="pool-list waiver-claim-list"><Each of={data.my_claims} as="claim"><article class="waiver-claim-row"><div class="pool-player"><div class="pool-player__text"><strong>{claim.add_name}</strong><small><If cond={claim.has_drop}>drops {claim.drop_label} ·</If> filed {claim.filed_at}</small></div></div><div class="waiver-claim-meta"><span class="position-chip">{claim.add_position}</span><b class="mono">Claim order {claim.priority} of {claim.claim_count}</b><span class="mono">Team waiver position {claim.waiver_position} of {claim.waiver_team_count}</span><If cond={claim.faab}><span class="mono">Bid {claim.bid} FAAB</span></If><If cond={claim.resolution_state == "scheduled"}><span class="mono">RESOLVES {claim.resolution_at} ({claim.resolution_relative})</span></If><If cond={claim.resolution_state == "overdue"}><span class="position-chip position-chip--warn">RESOLUTION OVERDUE</span><small>{claim.resolution_at} ({claim.resolution_relative})</small></If><If cond={claim.resolution_state == "degraded"}><span class="position-chip position-chip--warn">RESOLUTION DEGRADED</span><small>{claim.resolution_label}</small></If><If cond={claim.resolution_state == "unknown"}><span class="position-chip position-chip--warn">RESOLUTION UNKNOWN</span><small>{claim.resolution_label}</small></If><If cond={claim.resolution_state == "deferred"}><span class="position-chip position-chip--warn">CLAIM DEFERRED</span><small>{claim.resolution_label}</small></If></div><div class="waiver-claim-actions" aria-label={"Filing-order controls for " + claim.add_name}><If cond={claim.can_move_up}><form method="post" action={actionPath("claim-move") + "#waivers"} data-gosx-managed="true" data-gosx-action-signal="$players.state.refresh"><input type="hidden" name="csrf_token" value={csrf.token}></input><input type="hidden" name="team_id" value={data.viewer.team_id}></input><input type="hidden" name="claim_id" value={claim.id}></input><input type="hidden" name="direction" value="up"></input><input type="hidden" name="pos" value={data.pos}></input><input type="hidden" name="q" value={data.query}></input><input type="hidden" name="page" value={data.pool_page}></input><button class="board-button" type="submit">Move up</button></form></If><If cond={claim.can_move_down}><form method="post" action={actionPath("claim-move") + "#waivers"} data-gosx-managed="true" data-gosx-action-signal="$players.state.refresh"><input type="hidden" name="csrf_token" value={csrf.token}></input><input type="hidden" name="team_id" value={data.viewer.team_id}></input><input type="hidden" name="claim_id" value={claim.id}></input><input type="hidden" name="direction" value="down"></input><input type="hidden" name="pos" value={data.pos}></input><input type="hidden" name="q" value={data.query}></input><input type="hidden" name="page" value={data.pool_page}></input><button class="board-button" type="submit">Move down</button></form></If><form method="post" action={actionPath("claim-cancel") + "#waivers"} data-gosx-managed="true" data-gosx-action-signal="$players.state.refresh"><input type="hidden" name="csrf_token" value={csrf.token}></input><input type="hidden" name="team_id" value={data.viewer.team_id}></input><input type="hidden" name="claim_id" value={claim.id}></input><input type="hidden" name="pos" value={data.pos}></input><input type="hidden" name="q" value={data.query}></input><input type="hidden" name="page" value={data.pool_page}></input><button class="board-button board-button--cut" type="submit">Cancel</button></form></div></article></Each></div>
		</If>
		<div class="pool-toolbar"><div><span class="section-index">03 // WAIVER ORDER</span><h2>This week's claim order</h2></div></div>
		<ol class="waiver-order-strip"><Each of={data.waiver_order} as="slot"><li aria-current={slot.mine}><span class="mono">{slot.position}</span><span title={slot.name}>{slot.abbr}</span></li></Each></ol>
		<If cond={data.can_edit}><div class="pool-toolbar waiver-receipts-heading"><div><span class="section-index">PRIVATE RECEIPTS</span><h2>Recent waiver outcomes</h2></div><span class="mono">THIS TEAM ONLY</span></div><p class="scoring-note">Receipts persist independently of email settings.</p><If cond={data.my_receipts_empty}><div class="empty-tape"><strong>NO WAIVER RECEIPTS YET</strong><p>Won, beaten, and failed claims appear after processing.</p></div></If><div class="waiver-receipt-list"><Each of={data.my_waiver_receipts} as="receipt"><article class="waiver-receipt-row"><div class="waiver-receipt-outcome"><strong class="mono">{receipt.outcome}</strong><span>Week {receipt.week} · {receipt.resolved_at}</span></div><div class="waiver-receipt-player"><strong>{receipt.add_name} <span class="position-chip">{receipt.add_position}</span></strong><If cond={receipt.has_drop}><span>Drops {receipt.drop_label}</span></If><span>{receipt.reason}</span></div><div class="waiver-receipt-order mono"><span>Filed order {receipt.submitted_order}</span><span>Team position {receipt.waiver_position} of {receipt.waiver_team_count}</span></div></article></Each></div></If>
		<If cond={data.is_commissioner}><div class="pool-toolbar waiver-receipts-heading"><div><span class="section-index">COMMISSIONER RECEIPTS</span><h2>All teams' waiver outcomes</h2></div><span class="mono">COMMISSIONER ONLY</span></div><p class="scoring-note">The newest 50 outcomes across every team.</p><If cond={data.commissioner_receipts_empty}><div class="empty-tape"><strong>NO WAIVER RECEIPTS YET</strong><p>Won, beaten, and failed claims appear after processing.</p></div></If><div class="waiver-receipt-list"><Each of={data.commissioner_waiver_receipts} as="receipt"><article class="waiver-receipt-row"><div class="waiver-receipt-outcome"><strong class="mono">{receipt.outcome}</strong><span>{receipt.team_abbr} · Week {receipt.week} · {receipt.resolved_at}</span></div><div class="waiver-receipt-player"><strong>{receipt.add_name} <span class="position-chip">{receipt.add_position}</span></strong><If cond={receipt.has_drop}><span>Drops {receipt.drop_label}</span></If><span>{receipt.reason}</span><If cond={receipt.has_winner}><span>Winner: {receipt.winner_name} ({receipt.winner_abbr})</span></If><If cond={receipt.has_winning_bid}><span>Winning bid: {receipt.winning_bid} FAAB</span></If></div><div class="waiver-receipt-order mono"><span>Filed order {receipt.submitted_order}</span><span>Team position {receipt.waiver_position} of {receipt.waiver_team_count}</span></div></article></Each></div></If>
	</section>
}
