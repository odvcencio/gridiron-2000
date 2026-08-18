package matchups

type TeamMarkProps struct {
	Tone           string
	Abbreviation   string
	Name           string
	HasAvatarImage bool
	AvatarImageURL string
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"team-mark team-mark--large tone-" + props.Tone} aria-hidden="true">
		<If cond={props.HasAvatarImage}>
			<img class="avatar-mark__photo" src={props.AvatarImageURL} alt={props.Name} loading="lazy" />
		</If>
		<If cond={props.HasAvatarImage == false}>
			{props.Abbreviation}
		</If>
	</span>
}

func ScoreTeam(props any) Node {
	return <div class="score-team">
		<TeamMark {...props}></TeamMark>
		<div class="score-team__name">
			<strong>{props.Name}</strong>
			<small>{props.Manager}</small>
		</div>
		<b class="score score--large" data-score-team={props.ID} data-gosx-live-bind={"scores." + props.ID} data-gosx-live-flash-class="score-flash">{props.Score}</b>
	</div>
}

func MatchupCard(props any) Node {
	return <article class="matchup-card" data-live-matchup={props.ID}>
		<header>
			<span>
				<span class="live-dot" aria-hidden="true"></span>
				<b data-matchup-status data-gosx-live-bind={"matchupStatus." + props.ID}>{props.Status}</b>
			</span>
			<span class="mono" data-matchup-clock data-gosx-live-bind={"matchupClock." + props.ID}>{props.Clock}</span>
		</header>
		<ScoreTeam {...props.Away}></ScoreTeam>
		<div class="matchup-rule">
			<span>VS</span>
		</div>
		<ScoreTeam {...props.Home}></ScoreTeam>
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
	return <main class="page matchups-page" id="main-content" data-live-root data-gosx-revalidate-interval="4s" data-gosx-revalidate-src="/api/league/version" data-gosx-live-src="/api/live/week" data-gosx-live-interval="1m">
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
					<strong class="mono" data-live-updated data-gosx-live-bind="liveUpdated">{data.live.last_updated}</strong>
				</div>
				<div>
					<span>Refresh</span>
					<strong class="mono">60 SEC</strong>
				</div>
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
						<span data-live-status data-gosx-live-bind="liveStatus">Feed connected</span>
					</div>
				</header>
				<If cond={data.matchups_empty}>
					<div class="empty-tape">
						<strong>NO MATCHUPS YET</strong>
						<p>
							{data.league.season_open_line}
						</p>
					</div>
				</If>
				<div class="matchup-grid">
					<Each of={data.matchups} as="matchup">
						<MatchupCard {...matchup}></MatchupCard>
					</Each>
				</div>
			</section>
			<aside class="leader-rail">
				<header>
					<span class="section-index">PLAYER TAPE</span>
					<b>PROJECTION LEADERS</b>
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
