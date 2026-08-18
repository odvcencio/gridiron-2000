package team

type BreakdownRow struct {
	Scored bool
	Label  string
	Calc   string
	Points string
}

type RosterRowProps struct {
	Position       string
	HasHeadshot    bool
	Headshot       string
	Name           string
	NFLTeam        string
	Opponent       string
	HasOpponent    bool
	HasMatchup     bool
	MatchupTier    string
	MatchupChip    string
	MatchupDetail  string
	Jersey         string
	HasBreakdown   bool
	Breakdown      []BreakdownRow
	BreakdownTotal string
	HasHist        bool
	Hist           string
	Status         string
	Projection     string
	Points         string
}

// BadgeCellProps is one cell of the team-badge picker grid: a free
// motif's cell is a small submit form (csrf_token, team_id, motif,
// redirect_to), a taken-by-another-team cell is a dimmed, non-clickable
// preview labeled with that team's abbreviation, and this team's own
// claim is a highlighted preview. Free/Mine/TakenByOther are precomputed,
// mutually exclusive booleans (not "Claimed && ..." expressions) because a
// strict <If> cond must be a plain bool props field, or a bool props
// field compared with "== false" — see page.server.go's badgeGridProps
// doc comment for how the three are derived. CSRF/TeamID/RedirectTo
// repeat on every cell (rather than living once on the grid container)
// because a strict component call accepts exactly one spread attribute
// and no other attributes.
type BadgeCellProps struct {
	Slug          string
	Name          string
	Free          bool
	Mine          bool
	TakenByOther  bool
	ClaimedByAbbr string
	CSRF          string
	TeamID        string
	RedirectTo    string
}

component BadgeCell(props: BadgeCellProps) {
	return <div class="badge-option-wrap">
		<If cond={props.Free}>
			<form method="post" action="/avatar/badge" data-gosx-managed="false" class="badge-option-form">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="team_id" value={props.TeamID}></input>
				<input type="hidden" name="motif" value={props.Slug}></input>
				<input type="hidden" name="redirect_to" value={props.RedirectTo}></input>
				<button type="submit" class="badge-option" title={props.Name}>
					<span class="badge-option__art" style={"mask-image:url(/avatars/motifs/" + props.Slug + ".png);-webkit-mask-image:url(/avatars/motifs/" + props.Slug + ".png);"} aria-hidden="true"></span>
					<small>{props.Name}</small>
				</button>
			</form>
		</If>
		<If cond={props.Mine}>
			<div class="badge-option badge-option--mine" title={props.Name}>
				<span class="badge-option__art" style={"mask-image:url(/avatars/motifs/" + props.Slug + ".png);-webkit-mask-image:url(/avatars/motifs/" + props.Slug + ".png);"} aria-hidden="true"></span>
				<small>{props.Name}</small>
			</div>
		</If>
		<If cond={props.TakenByOther}>
			<div class="badge-option badge-option--taken" title={props.Name} aria-disabled="true">
				<span class="badge-option__art" style={"mask-image:url(/avatars/motifs/" + props.Slug + ".png);-webkit-mask-image:url(/avatars/motifs/" + props.Slug + ".png);"} aria-hidden="true"></span>
				<small>{props.ClaimedByAbbr}</small>
			</div>
		</If>
	</div>
}

component RosterRow(props: RosterRowProps) {
	return <div class="roster-row">
		<div class="position-chip">{props.Position}</div>
		<div class="player-identity stat-tip" tabindex="0">
			<If cond={props.HasHeadshot}>
				<img class="player-avatar player-avatar--photo" src={props.Headshot} alt="" loading="lazy" />
			</If>
			<If cond={props.HasHeadshot == false}>
				<span class="player-avatar" aria-hidden="true">{props.NFLTeam}</span>
			</If>
			<div>
				<strong>{props.Name}</strong>
				<small>
					{props.NFLTeam}
					<If cond={props.HasOpponent}>
						·
						{props.Opponent}
						<If cond={props.HasMatchup}>
							·
							<span class="matchup-chip" data-matchup-tier={props.MatchupTier}>{props.MatchupChip}</span>
						</If>
					</If>
				</small>
			</div>
			<div class="stat-tip__panel" role="tooltip" aria-hidden="true">
				<div class="stat-tip__head">
					<strong>{props.Name}</strong>
					<span class="mono">{props.Jersey}</span>
					<span class="mono stat-tip__team">{props.NFLTeam}</span>
				</div>
				<If cond={props.HasBreakdown}>
					<div class="stat-tip__rows">
						<Each of={props.Breakdown} as="row">
							<div class="stat-tip__row" data-scored={row.Scored}>
								<span>{row.Label}</span>
								<span class="mono">{row.Calc}</span>
								<b class="mono">{row.Points}</b>
							</div>
						</Each>
						<div class="stat-tip__total">
							<span>League scoring</span>
							<b class="mono">{props.BreakdownTotal}</b>
						</div>
					</div>
				</If>
				<If cond={props.HasBreakdown == false}>
					<p class="stat-tip__empty">No projection detail for this position.</p>
				</If>
				<If cond={props.HasMatchup}>
					<p class="stat-tip__hist mono">{props.MatchupDetail}</p>
				</If>
				<If cond={props.HasHist}>
					<p class="stat-tip__hist mono">{props.Hist}</p>
				</If>
			</div>
		</div>
		<div class="game-state">
			<span class="status-pin" aria-hidden="true"></span>
			{props.Status}
		</div>
		<div class="player-number mono">
			<small>PROJ</small>
			{props.Projection}
		</div>
		<div class="player-number mono">
			<small>PTS</small>
			{props.Points}
		</div>
	</div>
}

// Page's avatar-upload form posts to /avatar/upload as a plain, unmanaged
// (data-gosx-managed="false") full-page submission. TODO(gosx#187): once
// ctx.Files lands upstream, revisit whether it can move to the managed
// path — see avatar_handlers.go in the repo root for why it does not today.
func Page() Node {
	return <main class="page team-page" id="main-content">
		<If cond={data.has_seat == false}>
			<section class="no-franchise tone-lime">
				<div class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					MANAGER TERMINAL // NO FRANCHISE
				</div>
				<h1>NO TEAM YET.</h1>
				<If cond={data.fantasy_card.league_full == false}>
					<p>
						{data.fantasy_card.open_seats}
						seat(s) are still open. Claim one and build your roster.
					</p>
					<a href="/join" data-gosx-link class="button button--primary">Claim a team →</a>
				</If>
				<If cond={data.fantasy_card.league_full}>
					<p>Every manager seat is claimed. Pick'em is always open — no seat required.</p>
				</If>
				<a href="/pickem" data-gosx-link class="button button--ghost">Open Pick'em HQ →</a>
			</section>
		</If>
		<If cond={data.has_seat}>
		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_notice}>
				<p class="flash-message">{data.notice}</p>
			</If>
			<If cond={data.has_avatar_error}>
				<p class="error-message">{data.avatar_error}</p>
			</If>
			<If cond={data.has_lineup_error}>
				<p class="error-message">{data.lineup_error}</p>
			</If>
			<If cond={data.has_rename_error}>
				<p class="error-message">{data.rename_error}</p>
			</If>
			<If cond={data.has_co_error}>
				<p class="error-message">{data.co_error}</p>
			</If>
			<If cond={data.has_matchup_source}>
				<p class="demo-message">
					<strong>MATCHUP RANKS:</strong>
					ranked from the {data.matchup_source_label}. A higher "-toughest" number is a softer matchup; a lower one is tougher.
				</p>
			</If>
		</div>
		<section class={"team-hero tone-" + data.team.tone}>
			<div class="team-hero__identity">
				<span class="team-monogram">
					<If cond={data.team.has_avatar_image}>
						<img class="avatar-mark__photo" src={data.team.avatar_image_url} alt={data.team.name} loading="lazy" />
					</If>
					<If cond={data.team.has_avatar_image == false}>
						{data.team.abbreviation}
					</If>
				</span>
				<div>
					<span class="section-index">
						MANAGER TERMINAL //
						{data.viewer.initials}
					</span>
					<h1>{data.team.name}</h1>
					<small class="mono">
						{data.team.division}
						DIVISION
					</small>
					<If cond={data.team.claimed && data.co_manager.has_co}>
						<p>
							Operated by
							{data.team.manager}
							· with
							{data.co_manager.co_name}
						</p>
					</If>
					<If cond={data.team.claimed && data.co_manager.has_co == false}>
						<p>
							Operated by
							{data.team.manager}
						</p>
					</If>
					<If cond={data.team.claimed == false}>
						<p>
							Awaiting a manager — sign in to claim this seat.
						</p>
					</If>
					<If cond={data.co_manager.has_pending}>
						<p class="status-card__line">
							Co-manager invite pending:
							{data.co_manager.pending_email}
						</p>
					</If>
					<If cond={data.co_manager.can_invite}>
						<form method="post" action={actionPath("co-invite")} data-gosx-managed="true" class="co-manager-form">
							<input type="hidden" name="team_id" value={data.team.id}></input>
							<input type="email" name="email" placeholder="co-manager@example.com" autocomplete="off" required="required"></input>
							<button class="button button--compact" type="submit">Invite co-manager</button>
						</form>
					</If>
					<If cond={data.co_manager.can_detach}>
						<form method="post" action={actionPath("co-detach")} data-gosx-managed="true" class="co-manager-form">
							<input type="hidden" name="team_id" value={data.team.id}></input>
							<button class="button button--compact button--ghost" type="submit">Detach co-manager</button>
						</form>
					</If>
					<form method="post" action={actionPath("team-rename")} data-gosx-managed="true" class="team-rename-form">
						<input type="hidden" name="team_id" value={data.team.id}></input>
						<input type="text" name="name" value={data.team.name} maxlength="40" aria-label="Team name"></input>
						<button class="button button--compact" type="submit">Rename</button>
					</form>
					<form method="post" action="/avatar/upload" enctype="multipart/form-data" data-gosx-managed="false" class="avatar-upload-form">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<input type="hidden" name="team_id" value={data.team.id}></input>
						<input type="hidden" name="redirect_to" value="/team"></input>
						<input type="file" name="avatar" accept="image/png,image/jpeg" required="required"></input>
						<button class="button button--compact" type="submit">Upload team avatar</button>
					</form>
					<h2 class="badge-picker-title">Team badge</h2>
					<div class="badge-picker" style={"--badge-tone: " + data.badge_tone_hex + ";"}>
						<Each of={data.badge_grid} as="badge">
							<BadgeCell {...badge}></BadgeCell>
						</Each>
					</div>
					<If cond={data.has_badge_claim}>
						<form method="post" action="/avatar/badge" data-gosx-managed="false" class="badge-release-form">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<input type="hidden" name="team_id" value={data.team.id}></input>
							<input type="hidden" name="motif" value=""></input>
							<input type="hidden" name="action" value="release"></input>
							<input type="hidden" name="redirect_to" value="/team"></input>
							<button class="button button--compact" type="submit">Use default badge</button>
						</form>
					</If>
				</div>
			</div>
			<div class="team-hero__record">
				<span>Season</span>
				<strong class="mono">{data.team.record}</strong>
				<small>
					{data.team.points_for}
					PF ·
					{data.team.streak}
				</small>
			</div>
		</section>
		<div class="team-command-strip">
			<div>
				<span>Projected</span>
				<strong class="mono">{data.projected}</strong>
			</div>
			<div>
				<span>Starters</span>
				<strong class="mono">
					{data.starters_filled}
					/
					{data.starters_total}
				</strong>
			</div>
			<div>
				<span>Division</span>
				<strong class="mono">{data.team.division}</strong>
			</div>
			<div>
				<span>League</span>
				<strong class="mono">{data.league_mode}</strong>
			</div>
			<a href="/matchups" data-gosx-link class="button button--primary button--compact">View matchup</a>
		</div>
		<div class="team-layout">
			<section class="roster-panel">
				<header class="section-heading section-heading--split">
					<div>
						<span class="section-index">01 // STARTING LINEUP</span>
						<h2>
							Week
							{data.week}
							lineup
						</h2>
					</div>
					<span class="lineup-lock">
						<span class="status-pin" aria-hidden="true"></span>
						<b class="mono">{data.starters_filled}</b>
						/
						{data.starters_total}
						STARTERS
					</span>
				</header>
				<div class="roster-shape" aria-label="League roster shape">
					<Each of={data.roster_shape} as="slot">
						<span class="roster-shape__slot mono" title={slot.eligible}>{slot.label}</span>
					</Each>
					<p class="roster-shape__summary mono">{data.shape_summary}</p>
				</div>
				<div class="lineup-toolbar">
					<form method="get" action="/team" class="lineup-week-form">
						<select name="week" aria-label="Select week">
							<Each of={data.week_options} as="wk">
								<option value={wk.value} selected={wk.selected}>{wk.label}</option>
							</Each>
						</select>
						<button class="board-button" type="submit">Go</button>
					</form>
					<form method="post" action={actionPath("lineup-auto")} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<input type="hidden" name="team_id" value={data.team.id}></input>
						<input type="hidden" name="week" value={data.week}></input>
						<button class="button button--compact" type="submit">Set best lineup</button>
					</form>
				</div>
				<If cond={data.drafted == false}>
					<div class="empty-tape">
						<strong>NO ROSTER YET</strong>
						<p>
							This roster fills as draft picks are made. Rank your targets on the Big Board first.
						</p>
						<a href="/board" data-gosx-link>Open your board →</a>
					</div>
				</If>
				<If cond={data.drafted}>
					<div class="lineup-slot-list">
						<Each of={data.starters} as="slot">
							<div class="lineup-slot">
								<div class="lineup-slot__id mono">{slot.slot_id}</div>
								<If cond={slot.has_player}>
									<div class="player-identity stat-tip" tabindex="0">
										<If cond={slot.has_headshot}>
											<img class="player-avatar player-avatar--photo" src={slot.headshot} alt="" loading="lazy" />
										</If>
										<If cond={slot.has_headshot == false}>
											<span class="player-avatar" aria-hidden="true">{slot.nfl_team}</span>
										</If>
										<div>
											<strong>{slot.name}</strong>
											<small>
												{slot.position}
												·
												{slot.nfl_team}
												<If cond={slot.has_opponent}>
													·
													{slot.opponent}
													<If cond={slot.has_matchup}>
														·
														<span class="matchup-chip" data-matchup-tier={slot.matchup_tier}>{slot.matchup_chip}</span>
													</If>
												</If>
											</small>
										</div>
										<div class="stat-tip__panel" role="tooltip" aria-hidden="true">
											<div class="stat-tip__head">
												<strong>{slot.name}</strong>
												<span class="mono">{slot.jersey}</span>
												<span class="mono stat-tip__team">{slot.nfl_team}</span>
											</div>
											<If cond={slot.has_breakdown}>
												<div class="stat-tip__rows">
													<Each of={slot.breakdown} as="row">
														<div class="stat-tip__row" data-scored={row.scored}>
															<span>{row.label}</span>
															<span class="mono">{row.calc}</span>
															<b class="mono">{row.points}</b>
														</div>
													</Each>
													<div class="stat-tip__total">
														<span>League scoring</span>
														<b class="mono">{slot.breakdown_total}</b>
													</div>
												</div>
											</If>
											<If cond={slot.has_breakdown == false}>
												<p class="stat-tip__empty">No projection detail for this position.</p>
											</If>
											<If cond={slot.has_matchup}>
												<p class="stat-tip__hist mono">{slot.matchup_detail}</p>
											</If>
										</div>
									</div>
								</If>
								<If cond={slot.has_player == false}>
									<div class="slot-empty mono">EMPTY</div>
								</If>
								<div class="lineup-slot__chips">
									<If cond={slot.auto_filled}>
										<span class="position-chip" title="Filled automatically by SET BEST LINEUP">AUTO</span>
									</If>
									<If cond={slot.has_warning}>
										<span class="position-chip position-chip--warn">{slot.warning_label}</span>
									</If>
									<If cond={slot.locked}>
										<span class="position-chip position-chip--locked">{slot.lock_label}</span>
									</If>
								</div>
								<If cond={slot.locked == false}>
									<form method="post" action={actionPath("lineup-set")} data-gosx-managed="true" class="lineup-slot__form">
										<input type="hidden" name="csrf_token" value={csrf.token}></input>
										<input type="hidden" name="team_id" value={data.team.id}></input>
										<input type="hidden" name="week" value={data.week}></input>
										<input type="hidden" name="slot" value={slot.slot_id}></input>
										<select name="player_id" aria-label={"Assign a player to " + slot.slot_id}>
											<Each of={slot.options} as="opt">
												<option value={opt.id} selected={opt.selected}>{opt.label}</option>
											</Each>
										</select>
										<button class="board-button" type="submit">Set</button>
									</form>
								</If>
							</div>
						</Each>
					</div>
					<h3 class="lineup-bench-title">Bench</h3>
					<If cond={data.bench_empty}>
						<p class="stat-tip__empty">No bench players.</p>
					</If>
					<If cond={data.bench_empty == false}>
						<div class="roster-labels mono" aria-hidden="true">
							<span>POS</span>
							<span>PLAYER</span>
							<span>GAME</span>
							<span>PROJ</span>
							<span>PTS</span>
						</div>
						<div class="roster-list">
							<Each of={data.bench} as="player">
								<RosterRow {...player}></RosterRow>
							</Each>
						</div>
						<If cond={data.has_reserve}>
							<h3 class="lineup-bench-title">
								Reserve
								<small class="mono">{data.reserve_capacity}</small>
							</h3>
							<If cond={data.reserve_occupants_empty}>
								<p class="stat-tip__empty">No one is on reserve.</p>
							</If>
							<If cond={data.reserve_occupants_empty == false}>
								<div class="roster-list">
									<Each of={data.reserve_occupants} as="occ">
										<div class="roster-row">
											<div class="position-chip">{occ.position}</div>
											<div>
												<strong>{occ.name}</strong>
												<small>
													{occ.nfl_team}
													<If cond={occ.has_opponent}>
														·
														{occ.opponent}
														<If cond={occ.has_matchup}>
															·
															<span class="matchup-chip" data-matchup-tier={occ.matchup_tier}>{occ.matchup_chip}</span>
														</If>
													</If>
												</small>
											</div>
											<form method="post" action={actionPath("reserve-activate")} data-gosx-managed="true">
												<input type="hidden" name="csrf_token" value={csrf.token}></input>
												<input type="hidden" name="team_id" value={data.team.id}></input>
												<input type="hidden" name="player_id" value={occ.id}></input>
												<button class="button button--compact" type="submit">Activate</button>
											</form>
										</div>
									</Each>
								</div>
							</If>
							<If cond={data.reserve_place_empty == false}>
								<form method="post" action={actionPath("reserve-place")} data-gosx-managed="true" class="lineup-toolbar">
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="team_id" value={data.team.id}></input>
									<select name="player_id" aria-label="Place a player on reserve">
										<Each of={data.reserve_place_options} as="opt">
											<option value={opt.id}>{opt.label}</option>
										</Each>
									</select>
									<button class="board-button" type="submit">Place on reserve</button>
								</form>
							</If>
						</If>
						<If cond={data.has_ir}>
							<h3 class="lineup-bench-title">
								IR
								<small class="mono">{data.ir_capacity}</small>
							</h3>
							<If cond={data.ir_occupants_empty}>
								<p class="stat-tip__empty">No one is on IR.</p>
							</If>
							<If cond={data.ir_occupants_empty == false}>
								<div class="roster-list">
									<Each of={data.ir_occupants} as="occ">
										<div class="roster-row">
											<div class="position-chip">{occ.position}</div>
											<div>
												<strong>{occ.name}</strong>
												<small>
													{occ.nfl_team}
													<If cond={occ.has_opponent}>
														·
														{occ.opponent}
														<If cond={occ.has_matchup}>
															·
															<span class="matchup-chip" data-matchup-tier={occ.matchup_tier}>{occ.matchup_chip}</span>
														</If>
													</If>
												</small>
												<If cond={occ.healed}>
													<p class="error-message">
														Off the injury report — activate before
														{occ.deadline_label}
														or the platform auto-cuts him.
													</p>
												</If>
											</div>
											<form method="post" action={actionPath("ir-activate")} data-gosx-managed="true">
												<input type="hidden" name="csrf_token" value={csrf.token}></input>
												<input type="hidden" name="team_id" value={data.team.id}></input>
												<input type="hidden" name="player_id" value={occ.id}></input>
												<select name="drop_id" aria-label="Optional drop to make room">
													<option value="">— no drop —</option>
													<Each of={data.ir_drop_options} as="opt">
														<option value={opt.id}>{opt.label}</option>
													</Each>
												</select>
												<button class="button button--compact" type="submit">Activate</button>
											</form>
										</div>
									</Each>
								</div>
							</If>
							<If cond={data.ir_place_empty == false}>
								<form method="post" action={actionPath("ir-place")} data-gosx-managed="true" class="lineup-toolbar">
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="team_id" value={data.team.id}></input>
									<select name="player_id" aria-label="Place a player on IR">
										<Each of={data.ir_place_options} as="opt">
											<option value={opt.id}>{opt.label}</option>
										</Each>
									</select>
									<button class="board-button" type="submit">Place on IR</button>
								</form>
							</If>
						</If>
					</If>
				</If>
			</section>
			<aside class="scout-panel">
				<header>
					<span class="section-index">02 // WAIVER RADAR</span>
					<h2>Signal watch</h2>
				</header>
				<div class="scout-list">
					<Each of={data.scouting} as="player">
						<article class="scout-row">
							<span class="position-chip">{player.position}</span>
							<div>
								<strong>{player.name}</strong>
								<small>
									{player.team}
									·
									{player.signal}
								</small>
							</div>
							<b class="mono">{player.status}</b>
						</article>
					</Each>
				</div>
				<div class="scout-callout">
					<span>Roster note</span>
					<p>
						The radar lists the best players still undrafted, straight from the live pool.
					</p>
					<a href="/draft" data-gosx-link>Scout draft pool →</a>
					<If cond={data.is_commissioner}>
						<a href="/admin" data-gosx-link>Commissioner console →</a>
					</If>
				</div>
			</aside>
		</div>
		</If>
	</main>
}
