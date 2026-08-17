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
					·
					{props.Opponent}
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
		<button class="row-menu" type="button" aria-label={"Roster options for " + props.Name}>•••</button>
	</div>
}

// Page's avatar-upload form posts to /avatar/upload as a plain, unmanaged
// (data-gosx-managed="false") full-page submission. TODO(gosx#187): once
// ctx.Files lands upstream, revisit whether it can move to the managed
// path — see avatar_handlers.go in the repo root for why it does not today.
func Page() Node {
	return <main class="page team-page" id="main-content">
		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_notice}>
				<p class="flash-message">{data.notice}</p>
			</If>
			<If cond={data.has_avatar_error}>
				<p class="error-message">{data.avatar_error}</p>
			</If>
		</div>
		<section class="team-hero tone-lime">
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
					<If cond={data.team.claimed}>
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
					{data.starters}
					/ 8
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
						<span class="section-index">01 // STARTING UNIT</span>
						<h2>Dynasty roster</h2>
					</div>
					<span class="lineup-lock">
						<span class="status-pin" aria-hidden="true"></span>
						<b class="mono">{data.starters}</b>
						PLAYERS
					</span>
				</header>
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
					<div class="roster-labels mono" aria-hidden="true">
						<span>POS</span>
						<span>PLAYER</span>
						<span>GAME</span>
						<span>PROJ</span>
						<span>PTS</span>
						<span></span>
					</div>
					<div class="roster-list">
						<Each of={data.roster} as="player">
							<RosterRow {...player}></RosterRow>
						</Each>
					</div>
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
	</main>
}
