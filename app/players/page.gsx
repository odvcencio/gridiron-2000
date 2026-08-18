package players

func Page() Node {
	return <main class="page board-page" id="main-content" data-gosx-revalidate-interval="4s" data-gosx-revalidate-src="/api/league/version">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					PLAYER POOL
				</span>
				<h1>
					THE WIRE
					<br></br>
					OPENS HERE.
				</h1>
				<p>
					Every pool player, rostered or free. Sign a free agent, or drop one of your own — the transaction feed records every move.
				</p>
			</div>
			<div class="draft-clock-panel">
				<span>Your roster spots</span>
				<strong class="mono">
					{data.roster_size}
					/
					{data.roster_cap}
				</strong>
				<div class="draft-clock-meta">
					<a href="/activity" data-gosx-link>Transaction feed →</a>
					<a href="/team" data-gosx-link>Team terminal →</a>
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
			<If cond={data.can_edit == false}>
				<p class="demo-message">
					<strong>SIGN IN REQUIRED:</strong>
					use League access to sign or drop players for your seat.
				</p>
			</If>
			<If cond={data.free_agency_open == false}>
				<p class="demo-message">
					<strong>FREE AGENCY OPENS AFTER THE DRAFT:</strong>
					every undrafted player becomes a free agent the moment the draft completes.
				</p>
			</If>
			<If cond={data.pool_live == false}>
				<p class="demo-message">
					<strong>OFFLINE POOL:</strong>
					player data is approximate until the live pool syncs.
				</p>
			</If>
		</div>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">01 // PLAYER DATABASE</span>
					<h2>Browse the pool</h2>
				</div>
			</div>
			<div class="position-filters" aria-label="Filter the player pool by position">
				<Each of={data.positions} as="tab">
					<a href={tab.href} data-gosx-link class="filter-button" aria-current={tab.active}>{tab.label}</a>
				</Each>
			</div>
			<form method="get" action="/players" class="pool-search-bar">
				<label class="mono" for="players-search">QUERY //</label>
				<input id="players-search" type="search" name="q" value={data.query} placeholder="Search player or team" autocomplete="off"></input>
				<If cond={data.pos != ""}>
					<input type="hidden" name="pos" value={data.pos}></input>
				</If>
				<button class="filter-button" type="submit">Search</button>
			</form>
			<If cond={data.players_empty}>
				<div class="empty-tape">
					<strong>NO PLAYERS MATCH</strong>
					<p>
						Try a different position filter or clear your search.
					</p>
				</div>
			</If>
			<div class="pool-list pool-list--tall">
				<Each of={data.players} as="player">
					<article class="pool-row pool-row--status" data-player-position={player.position}>
						<span class="pool-rank mono">{player.rank}</span>
						<div class="pool-player" tabindex="0">
							<If cond={player.has_headshot}>
								<img class="player-headshot" src={player.headshot} alt="" loading="lazy" />
							</If>
							<div class="pool-player__text">
								<strong>{player.name}</strong>
								<If cond={player.has_draft_capital}>
									<span class="badge-rookie">{player.draft_capital}</span>
								</If>
								<small>{player.detail}</small>
							</div>
						</div>
						<span class="position-chip">{player.position}</span>
						<b class="mono">{player.projection}</b>
						<If cond={player.rostered}>
							<span class="position-chip position-chip--locked">{player.owner_abbr}</span>
						</If>
						<If cond={player.on_waivers}>
							<span class="position-chip">ON WAIVERS · {player.waiver_resolves}</span>
						</If>
						<If cond={player.free_agent}>
							<span class="position-chip">FREE AGENT</span>
						</If>
						<div class="board-controls">
							<If cond={player.can_add}>
								<form method="post" action={actionPath("player-add")} data-gosx-managed="true" class="lineup-slot__form">
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
									<input type="hidden" name="player_id" value={player.id}></input>
									<input type="hidden" name="pos" value={data.pos}></input>
									<input type="hidden" name="q" value={data.query}></input>
									<If cond={player.needs_drop}>
										<select name="drop_id" aria-label={"Choose a player to drop for " + player.name}>
											<option value="">Choose a player to drop</option>
											<Each of={data.drop_options} as="opt">
												<option value={opt.id}>{opt.label}</option>
											</Each>
										</select>
									</If>
									<button class="draft-button" type="submit">Add</button>
								</form>
							</If>
							<If cond={player.can_claim}>
								<form method="post" action={actionPath("claim-file")} data-gosx-managed="true" class="lineup-slot__form">
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
									<input type="hidden" name="player_id" value={player.id}></input>
									<input type="hidden" name="pos" value={data.pos}></input>
									<input type="hidden" name="q" value={data.query}></input>
									<If cond={player.needs_drop}>
										<select name="drop_id" aria-label={"Choose a player to drop for " + player.name}>
											<option value="">Choose a player to drop</option>
											<Each of={data.drop_options} as="opt">
												<option value={opt.id}>{opt.label}</option>
											</Each>
										</select>
									</If>
									<If cond={data.waivers_faab}>
										<input type="number" name="bid" min="0" max={data.my_faab_remaining} placeholder="Bid $" aria-label={"Bid for " + player.name}></input>
									</If>
									<button class="draft-button" type="submit">Claim</button>
								</form>
							</If>
							<If cond={player.claimed_by_me}>
								<span class="position-chip">CLAIM FILED</span>
							</If>
							<If cond={player.can_add == false && player.can_claim == false && player.rostered == false && player.claimed_by_me == false}>
								<button class="draft-button" type="button" disabled="disabled">Add</button>
							</If>
							<If cond={player.mine}>
								<form method="post" action={actionPath("player-drop")} data-gosx-managed="true">
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
									<input type="hidden" name="player_id" value={player.id}></input>
									<button class="board-button board-button--cut" type="submit" aria-label={"Drop " + player.name}>Drop</button>
								</form>
							</If>
						</div>
					</article>
				</Each>
			</div>
		</section>
		<section class="player-pool" id="waivers">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">02 // WAIVER DESK</span>
					<h2>My claims</h2>
				</div>
				<If cond={data.waivers_faab == false}>
					<span class="mono">Priority {data.my_waiver_position} of {data.waiver_team_count}</span>
				</If>
				<If cond={data.waivers_faab}>
					<span class="mono">Budget ${data.my_faab_remaining}</span>
				</If>
			</div>
			<If cond={data.my_claims_empty}>
				<div class="empty-tape">
					<strong>NO OPEN CLAIMS</strong>
					<p>
						File a claim from an ON WAIVERS row above; it resolves once waivers clear.
					</p>
				</div>
			</If>
			<div class="pool-list">
				<Each of={data.my_claims} as="claim">
					<article class="pool-row pool-row--claim">
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
						<span class="position-chip">{claim.add_position}</span>
						<If cond={claim.faab}>
							<b class="mono">${claim.bid}</b>
						</If>
						<If cond={claim.faab == false}>
							<b class="mono">Pos {claim.priority}</b>
						</If>
						<div class="board-controls">
							<form method="post" action={actionPath("claim-cancel")} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
								<input type="hidden" name="claim_id" value={claim.id}></input>
								<button class="board-button board-button--cut" type="submit" aria-label={"Cancel claim for " + claim.add_name}>Cancel</button>
							</form>
						</div>
					</article>
				</Each>
			</div>
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
						<span>{slot.abbr}</span>
					</li>
				</Each>
			</ol>
		</section>
	</main>
}
