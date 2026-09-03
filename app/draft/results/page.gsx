package results

// Page is /draft/results (wave 7, item 4): a dedicated, shareable results
// view reusing Service.DraftHistory's own Teams/Board/Picks — by team
// (the viewer's own team, or "?team=CODE", leading; every other team
// after it in draft order), by round (Round 1 first, a plain list), and
// the grid (the same board /draft's own "Draft grid" segment shows,
// reusing its exact CSS class names — public/styles.css's .board-grid
// family — with every cell already filled, since this page only ever
// shows the grid once the draft is complete). "Draft results" is the
// page's own plain h1 — no slogan under it, matching /activity's
// masthead pattern but without the marketing line.
func Page() Node {
	return <main class="page results-page" id="main-content">
		<header class="draft-masthead">
			<div class="draft-masthead__copy">
				<h1>Draft results</h1>
			</div>
			<div class="draft-clock-panel">
				<span>Drafted</span>
				<strong class="mono">{data.long_date}</strong>
				<div class="draft-clock-meta">
					<If cond={data.published}>
						<span class="mono">{data.time} · {data.timezone}</span>
					</If>
					<span class="mono">{data.rounds} rounds · {data.team_count} teams</span>
					<a href={data.ledger_href}>Download the ledger (CSV) →</a>
				</div>
			</div>
		</header>
		<If cond={data.complete == false}>
			<div class="empty-tape">
				<strong>DRAFT NOT COMPLETE</strong>
				<p>Results appear here once every pick is locked. <a href="/draft" data-gosx-link>Open the draft room →</a></p>
			</div>
		</If>
		<If cond={data.complete}>
			<If cond={data.team_not_found}>
				<p class="error-message" role="status">No team is coded {data.team_not_found_code}. Showing your own team instead.</p>
			</If>
			<nav class="segment results-segment" aria-label="Draft results views">
				<a class="segment__option" href={data.teams_href} data-gosx-link aria-current={data.show_teams}>By team</a>
				<a class="segment__option" href={data.rounds_href} data-gosx-link aria-current={data.show_rounds}>By round</a>
				<a class="segment__option" href={data.grid_href} data-gosx-link aria-current={data.show_grid}>Draft grid</a>
			</nav>
			<If cond={data.show_teams}>
				<div class="results-teams" aria-label="Draft results by team">
					<Each of={data.teams} as="team">
						<article class="results-team-card" id={"team-" + team.id} data-mine={team.mine}>
							<header class="results-team-card__head">
								<span class={"team-mark tone-" + team.tone}>
									<If cond={team.has_avatar_image}>
										<img class="avatar-mark__photo" src={team.avatar_image_url} alt={team.name} loading="lazy" />
									</If>
									<If cond={team.has_avatar_image == false}>{team.abbreviation}</If>
								</span>
								<div class="results-team-card__name">
									<strong>{team.name}<If cond={team.mine}> · you</If></strong>
									<small>{team.manager}</small>
								</div>
							</header>
							<ol class="results-team-card__picks">
								<Each of={team.picks} as="pick">
									<li>
										<span class="mono results-pick__label">{pick.label}</span>
										<span class="results-pick__player">{pick.player_name}</span>
										<span class={"pos pos-" + pick.position}>{pick.position}</span>
										<small class="muted">{pick.nfl_team}</small>
										<If cond={pick.has_value}><span class="mono results-pick__value">{pick.value_label}</span></If>
									</li>
								</Each>
								<If cond={team.pick_count == 0}>
									<li class="results-team-card__empty">No picks</li>
								</If>
							</ol>
						</article>
					</Each>
				</div>
				<nav class="results-team-dots" aria-label="Jump to a team">
					<Each of={data.teams} as="team">
						<a class="results-team-dot" href={"#team-" + team.id} aria-label={"Jump to " + team.name} title={team.name}></a>
					</Each>
				</nav>
			</If>
			<If cond={data.show_rounds}>
				<div class="results-rounds">
					<Each of={data.team_rounds} as="round">
						<section class="results-round">
							<h2 class="results-round__head">ROUND {round.round}</h2>
							<ol class="results-round__picks">
								<Each of={round.picks} as="pick">
									<li data-mine={pick.mine}>
										<span class="mono results-pick__label">{pick.label}</span>
										<span class={"team-mark tone-" + pick.team_tone}>
											<If cond={pick.has_avatar_image}>
												<img class="avatar-mark__photo" src={pick.avatar_image_url} alt={pick.team_name} loading="lazy" />
											</If>
											<If cond={pick.has_avatar_image == false}>{pick.team_abbr}</If>
										</span>
										<span class="results-pick__player">{pick.player_name}<small> · {pick.team_name}</small></span>
										<span class={"pos pos-" + pick.position}>{pick.position}</span>
										<small class="muted">{pick.nfl_team}</small>
									</li>
								</Each>
							</ol>
						</section>
					</Each>
				</div>
			</If>
			<If cond={data.show_grid}>
				<If cond={data.board.has_mine}>
					<a class="board-jump" href={"#results-board-team-" + data.board.mine_id}>↓ Jump to my picks</a>
				</If>
				<div class="draft-history__view--board results-board-scroll">
					<div class="board-grid" style={"--board-columns:" + data.board.column_count}>
						<div class="board-grid__corner"></div>
						<Each of={data.board.columns} as="column">
							<div class="board-grid__team" id={"results-board-team-" + column.id} data-mine={column.mine}>
								<span class={"team-mark tone-" + column.tone}>
									<If cond={column.has_avatar_image}>
										<img class="avatar-mark__photo" src={column.avatar_image_url} alt={column.name} loading="lazy" />
									</If>
									<If cond={column.has_avatar_image == false}>{column.abbreviation}</If>
								</span>
								<span class="board-grid__name">{column.name}<If cond={column.mine}> · you</If></span>
							</div>
						</Each>
						<Each of={data.board.rows} as="row">
							<div class="board-grid__round">
								<span class="idx">ROUND {row.round}</span>
								<span class="mono muted">{row.direction}</span>
							</div>
							<Each of={row.cells} as="cell">
								<div class={"board-cell c-" + cell.position} data-mine={cell.mine}>
									<strong>{cell.player_name}</strong>
									<small>{cell.label} · {cell.position} · {cell.nfl_team}<If cond={cell.is_auto}> · AUTO</If></small>
								</div>
							</Each>
						</Each>
					</div>
				</div>
			</If>
		</If>
	</main>
}
