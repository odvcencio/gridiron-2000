package matchups

func TeamMark(props any) Node {
	return <span class={"team-mark team-mark--large tone-" + props.tone} aria-hidden="true">{props.abbreviation}</span>
}

func ScoreTeam(props any) Node {
	return <div class="score-team">
		<TeamMark {...props}></TeamMark>
		<div class="score-team__name">
			<strong>{props.name}</strong>
			<small>{props.manager}</small>
		</div>
		<b class="score score--large" data-score-team={props.id}>{props.score}</b>
	</div>
}

func MatchupCard(props any) Node {
	return <article class="matchup-card" data-live-matchup={props.id}>
		<header>
			<span>
				<span class="live-dot" aria-hidden="true"></span>
				<b data-matchup-status>{props.status}</b>
			</span>
			<span class="mono" data-matchup-clock>{props.clock}</span>
		</header>
		<ScoreTeam {...props.away}></ScoreTeam>
		<div class="matchup-rule">
			<span>VS</span>
		</div>
		<ScoreTeam {...props.home}></ScoreTeam>
		<footer>
			<span>Win probability</span>
			<div class="probability-track" aria-hidden="true">
				<i></i>
			</div>
			<strong class="mono">52 / 48</strong>
		</footer>
	</article>
}

func Page() Node {
	return <main class="page matchups-page" id="main-content" data-live-root>
		<header class="page-masthead">
			<div>
				<span class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					{data.live.status}
				</span>
				<p class="page-kicker">{data.live.week_label}</p>
				<h1>
					LIVE
					<br></br>
					SIGNAL.
				</h1>
			</div>
			<div class="masthead-console">
				<div>
					<span>Provider</span>
					<strong>{data.live.source_label}</strong>
				</div>
				<div>
					<span>Last packet</span>
					<strong class="mono" data-live-updated>{data.live.last_updated}</strong>
				</div>
				<div>
					<span>Refresh</span>
					<strong class="mono">60 SEC</strong>
				</div>
				<button class="button button--primary button--compact" type="button" data-live-refresh>Sync now</button>
			</div>
		</header>
		<div class="matchup-layout">
			<section class="matchup-stage">
				<header class="section-heading section-heading--split">
					<div>
						<span class="section-index">SCORENET // CHANNEL 01</span>
						<h2>All league matchups</h2>
					</div>
					<div class="sync-state" role="status" aria-live="polite">
						<span class="live-dot" aria-hidden="true"></span>
						<span data-live-status>Feed connected</span>
					</div>
				</header>
				<div class="matchup-grid">
					<Each of={data.matchups} as="matchup">
						<MatchupCard {...matchup}></MatchupCard>
					</Each>
				</div>
			</section>
			<aside class="leader-rail">
				<header>
					<span class="section-index">PLAYER TAPE</span>
					<b>TOP SIGNALS</b>
				</header>
				<div class="leader-list">
					<Each of={data.leaders} as="leader">
						<div class="leader-row">
							<span class="mono">{leader.rank}</span>
							<div>
								<strong>{leader.name}</strong>
								<small>{leader.position}</small>
							</div>
							<b class="mono">{leader.points}</b>
							<em class="mono">{leader.trend}</em>
						</div>
					</Each>
				</div>
				<div class="data-note">
					<span>How live works</span>
					<p>
						The browser asks this GoSX server for one fresh snapshot per minute. The server deduplicates requests for 45 seconds before contacting the configured league provider.
					</p>
				</div>
			</aside>
		</div>
	</main>
}
