package app

func TeamMark(props any) Node {
	return <span class={"team-mark tone-" + props.tone} aria-hidden="true">
		<If cond={props.has_avatar_image}>
			<img class="avatar-mark__photo" src={props.avatar_image_url} alt={props.name} loading="lazy" />
		</If>
		<If cond={props.has_avatar_image == false}>
			{props.abbreviation}
		</If>
	</span>
}

func MiniMatchup(props any) Node {
	return <article class="mini-matchup" data-live-matchup={props.id}>
		<div class="mini-matchup__meta">
			<span data-matchup-status>{props.status}</span>
			<span class="mono" data-matchup-clock>{props.clock}</span>
		</div>
		<div class="mini-team">
			<TeamMark {...props.away}></TeamMark>
			<div>
				<strong>{props.away.name}</strong>
				<small>{props.away.manager}</small>
			</div>
			<b class="score" data-score-team={props.away.id}>{props.away.score}</b>
		</div>
		<div class="mini-team">
			<TeamMark {...props.home}></TeamMark>
			<div>
				<strong>{props.home.name}</strong>
				<small>{props.home.manager}</small>
			</div>
			<b class="score" data-score-team={props.home.id}>{props.home.score}</b>
		</div>
	</article>
}

func StandingRow(props any) Node {
	return <div class="standing-row">
		<span class="rank mono">{props.rank}</span>
		<TeamMark {...props}></TeamMark>
		<div class="standing-team">
			<strong>{props.name}</strong>
			<small>{props.manager}</small>
		</div>
		<span class="record mono">{props.record}</span>
		<span class="points mono">{props.points_for}</span>
		<span class="streak mono">{props.streak}</span>
	</div>
}

func Page() Node {
	return <main class="page home-page" id="main-content" data-gosx-revalidate-interval="4s" data-gosx-revalidate-src="/api/league/version">
		<If cond={data.viewer.signed_in == false}>
			<section class="hero-command">
				<div class="hero-command__copy">
					<div class="signal-label">
						<span class="live-dot" aria-hidden="true"></span>
						LEAGUE SYSTEM // PRIVATE
					</div>
					<p class="hero-kicker">
						Eight-manager dynasty league · Aqua and Orange divisions
					</p>
					<h1>
						GRIDIRON 2000
						<br></br>
						<span>CLAIM YOUR SEAT.</span>
					</h1>
					<p class="hero-deck">
						A private fantasy command center for lineups, live Sundays, waiver panic, and receipts that survive well past Monday night.
					</p>
					<div class="hero-actions">
						<a href="/login" data-gosx-link class="button button--primary">
							Sign in with Google
							<span aria-hidden="true">→</span>
						</a>
					</div>
					<If cond={data.viewer.demo}>
						<p class="demo-message">
							<strong>REHEARSAL MODE:</strong>
							every page opens without a sign-in in this preview league.
						</p>
					</If>
					<div class="oss-invite">
						<span class="section-index">RUN YOUR OWN</span>
						<p>
							This whole league room is open source: GoSX server, snake draft, dynasty scoring, personal big boards, live Tank01 player pool, commissioner console. One Go binary, your own data, MIT licensed.
						</p>
						<div class="hero-actions">
							<a href="https://github.com/odvcencio/gridiron-2000" rel="noreferrer" class="button button--compact">Get the source ↗</a>
							<a href="https://github.com/odvcencio/gosx" rel="noreferrer" class="button button--compact">Built with GoSX ↗</a>
						</div>
					</div>
				</div>
				<aside class="draft-transmission" data-draft-at={data.draft.at}>
					<div class="transmission-top">
						<span>Incoming transmission</span>
						<span class="mono">EVENT 0815</span>
					</div>
					<div class="chrome-disc" aria-hidden="true">
						<span>G2K</span>
					</div>
					<div class="draft-transmission__body">
						<p>2026 LEAGUE DRAFT</p>
						<strong>{data.draft.date}</strong>
						<span>{data.draft.time}</span>
					</div>
					<div class="countdown-strip">
						<span>Doors in</span>
						<b class="mono" data-draft-countdown>CALCULATING…</b>
					</div>
				</aside>
			</section>
		</If>
		<If cond={data.viewer.signed_in}>
		<section class="hero-command">
			<div class="hero-command__copy">
				<div class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					LEAGUE SYSTEM // ONLINE
				</div>
				<p class="hero-kicker">
					Eight managers. Zero restraint.
					<span class="position-chip">{data.league_mode}</span>
				</p>
				<h1>
					THE SEASON
					<br></br>
					<span>NEVER SLEEPS.</span>
				</h1>
				<p class="hero-deck">
					A private fantasy command center for lineups, live Sundays, waiver panic, and receipts that survive well past Monday night.
				</p>
				<div class="hero-actions">
					<a href="/matchups" data-gosx-link class="button button--primary">
						Enter live center
						<span aria-hidden="true">↗</span>
					</a>
					<a href="/draft" data-gosx-link class="button button--ghost">Open draft room</a>
				</div>
			</div>
			<aside class="draft-transmission" data-draft-at={data.draft.at}>
				<div class="transmission-top">
					<span>Incoming transmission</span>
					<span class="mono">EVENT 0815</span>
				</div>
				<div class="chrome-disc" aria-hidden="true">
					<span>G2K</span>
				</div>
				<div class="draft-transmission__body">
					<p>2026 LEAGUE DRAFT</p>
					<strong>{data.draft.date}</strong>
					<span>{data.draft.time}</span>
				</div>
				<div class="countdown-strip">
					<span>Doors in</span>
					<b class="mono" data-draft-countdown>CALCULATING…</b>
				</div>
			</aside>
		</section>
		<section class="score-command" data-live-root>
			<header class="section-heading section-heading--split">
				<div>
					<span class="section-index">01 // MATCHUP PREVIEW</span>
					<h2>League simulator</h2>
				</div>
				<div class="sync-state" role="status" aria-live="polite">
					<span class="live-dot" aria-hidden="true"></span>
					<span data-live-status>
						{data.live.source_label}
						·
						{data.live.last_updated}
					</span>
				</div>
			</header>
			<If cond={data.featured_empty}>
				<div class="empty-tape">
					<strong>SEASON NOT STARTED</strong>
					<p>
						Matchups appear in Week 1.
					</p>
				</div>
			</If>
			<div class="featured-score-grid">
				<Each of={data.featured} as="matchup">
					<MiniMatchup {...matchup}></MiniMatchup>
				</Each>
			</div>
			<div class="score-ticker" aria-label="Matchup preview status">
				<span>PREVIEW</span>
				<p>
					{data.live.week_label}
					//
					{data.live.status}
					// local fixture refreshes every 60 seconds
				</p>
				<a href="/matchups" data-gosx-link>All matchups →</a>
			</div>
		</section>
		<div class="dashboard-split">
			<section class="standings-panel">
				<header class="section-heading">
					<span class="section-index">02 // POWER GRID</span>
					<h2>Final ’25 table</h2>
					<p>
						Last season’s damage report. Everyone resets to 0–0 after draft night.
					</p>
				</header>
				<div class="standing-labels mono" aria-hidden="true">
					<span>RK</span>
					<span>CLUB</span>
					<span>W–L</span>
					<span>PF</span>
					<span>FORM</span>
				</div>
				<div class="standings-list">
					<Each of={data.divisions} as="division">
						<div class="division-group">
							<span class="division-heading mono">{division.name}</span>
							<Each of={division.teams} as="team">
								<StandingRow {...team}></StandingRow>
							</Each>
						</div>
					</Each>
				</div>
			</section>
			<aside class="activity-panel">
				<div class="activity-panel__header">
					<span class="section-index">03 // WIRE LOG</span>
					<span class="mono">AUTO-SCROLL</span>
				</div>
				<h2>Moves after midnight</h2>
				<div class="activity-feed">
					<If cond={data.transactions_empty}>
						<div class="empty-tape">
							<strong>NO TRANSACTIONS YET</strong>
							<p>
								The wire opens after the draft.
							</p>
						</div>
					</If>
					<Each of={data.transactions} as="move">
						<div class="activity-item">
							<time class="mono">{move.time}</time>
							<p>
								<strong>{move.team}</strong>
								{move.action}
								<b>{move.player}</b>
							</p>
						</div>
					</Each>
				</div>
				<div class="commissioner-note">
					<span>Commissioner’s desk</span>
					<p>
						Draft order locks 24 hours before showtime. Bring a charger. Bring a strategy. Do not bring a five-minute monologue for pick 1.01.
					</p>
					<a href="/draft" data-gosx-link>Review draft protocol</a>
				</div>
			</aside>
		</div>
		</If>
	</main>
}
