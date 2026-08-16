package team

func RosterRow(props any) Node {
	return <div class="roster-row">
		<div class="position-chip">{props.position}</div>
		<div class="player-identity">
			<If cond={props.has_headshot}>
				<img class="player-avatar player-avatar--photo" src={props.headshot} alt="" loading="lazy" />
			</If>
			<If cond={props.has_headshot == false}>
				<span class="player-avatar" aria-hidden="true">{props.nfl_team}</span>
			</If>
			<div>
				<strong>{props.name}</strong>
				<small>
					{props.nfl_team}
					·
					{props.opponent}
				</small>
			</div>
		</div>
		<div class="game-state">
			<span class="status-pin" aria-hidden="true"></span>
			{props.status}
		</div>
		<div class="player-number mono">
			<small>PROJ</small>
			{props.projection}
		</div>
		<div class="player-number mono">
			<small>PTS</small>
			{props.points}
		</div>
		<button class="row-menu" type="button" aria-label={"Roster options for " + props.name}>•••</button>
	</div>
}

func Page() Node {
	return <main class="page team-page" id="main-content">
		<section class="team-hero tone-lime">
			<div class="team-hero__identity">
				<span class="team-monogram">{data.team.abbreviation}</span>
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
