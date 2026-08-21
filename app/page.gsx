package app

type TeamMarkProps struct {
	Tone           string
	Abbreviation   string
	Name           string
	HasAvatarImage bool
	AvatarImageURL string
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"team-mark tone-" + props.Tone} aria-hidden="true">
		<If cond={props.HasAvatarImage}>
			<img class="avatar-mark__photo" src={props.AvatarImageURL} alt={props.Name} loading="lazy" />
		</If>
		<If cond={props.HasAvatarImage == false}>
			{props.Abbreviation}
		</If>
	</span>
}

func MiniMatchup(props any) Node {
	return <article class="mini-matchup" data-live-matchup={props.ID}>
		<div class="mini-matchup__meta">
			<span data-matchup-status>{props.Status}</span>
			<span class="mono" data-matchup-clock>{props.Clock}</span>
		</div>
		<div class="mini-team">
			<TeamMark {...props.Away}></TeamMark>
			<div>
				<strong>{props.Away.Name}</strong>
				<small>{props.Away.Manager}</small>
			</div>
			<b class="score" data-score-team={props.Away.ID}>{props.Away.Score}</b>
		</div>
		<div class="mini-team">
			<TeamMark {...props.Home}></TeamMark>
			<div>
				<strong>{props.Home.Name}</strong>
				<small>{props.Home.Manager}</small>
			</div>
			<b class="score" data-score-team={props.Home.ID}>{props.Home.Score}</b>
		</div>
	</article>
}

// StandingRowProps is the typed spread source for StandingRow (fix,
// registration wave): StandingRow used to declare "props any" and
// re-spread the whole, unmodified props value into strict TeamMark. That
// works for MiniMatchup's TeamMark calls (below) because those spread a
// nested struct *field* (props.Away/props.Home), but spreading "props"
// itself, whole, is different: calling any non-strict ("props any")
// component with a {...} spread first flattens the source struct into a
// map[string]any (route.spreadProps, so the component body can do
// dynamic field access) before "props" is ever bound — and a map can
// never satisfy a strict component's spread boundary afterward
// (strictSpreadProps rejects anything that is not reflect.Struct). This
// was unreachable in this repo's test suite (which never renders the
// .gsx template, only asserts DashboardData's map shape) and in demo
// mode (Viewer never reports signed_in for an anonymous demo visitor),
// so nothing had exercised this render path until the registration
// wave's manual browser verification hit a hard render error here. A
// strict `component` declaration with an explicit prop type sidesteps
// the flattening entirely: StandingRowProps must structurally cover
// StandingTeamCard (page.server.go) since dashboardDivisions' output is
// exactly that shape.
type StandingRowProps struct {
	Rank           string
	Name           string
	Manager        string
	Record         string
	PointsFor      string
	Streak         string
	Tone           string
	Abbreviation   string
	HasAvatarImage bool
	AvatarImageURL string
}

component StandingRow(props: StandingRowProps) {
	return <div class="standing-row">
		<span class="rank mono">{props.Rank}</span>
		<TeamMark
			Tone={props.Tone}
			Abbreviation={props.Abbreviation}
			Name={props.Name}
			HasAvatarImage={props.HasAvatarImage}
			AvatarImageURL={props.AvatarImageURL}
		></TeamMark>
		<div class="standing-team">
			<strong>{props.Name}</strong>
			<small>{props.Manager}</small>
		</div>
		<span class="record mono">{props.Record}</span>
		<span class="points mono">{props.PointsFor}</span>
		<span class="streak mono">{props.Streak}</span>
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
						{data.league.hero_kicker}
					</p>
					<h1>
						{data.league.name}
						<br></br>
						<span>CLAIM YOUR SEAT.</span>
					</h1>
					<p class="hero-deck">
						A private <strong>{data.league.format_blurb}</strong> for <strong>{data.league.seat_count_word}</strong> managers: lineups, waivers, and a commissioner-published league record.
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
							explore every page without signing in.
						</p>
					</If>
					<div class="oss-invite">
						<span class="section-index">RUN YOUR OWN</span>
						<p>
							This whole league room is open source: GoSX server, snake draft, {data.league.format_blurb}, personal big boards, a player pool with optional live data, and a commissioner console. One Go binary, your own data, MIT licensed.
						</p>
						<div class="hero-actions">
							<a href="https://github.com/odvcencio/gridiron-2000" rel="noreferrer" class="button button--compact">Get the source ↗</a>
							<a href="https://github.com/odvcencio/gosx" rel="noreferrer" class="button button--compact">Built with GoSX ↗</a>
						</div>
					</div>
				</div>
				<aside class="draft-transmission" aria-labelledby="draft-event-heading-public">
					<div class="transmission-top">
						<span>Incoming transmission</span>
						<span class="mono">DRAFT EVENT</span>
					</div>
					<div class="chrome-disc" aria-hidden="true">
						<span>{data.league.short_code}</span>
					</div>
					<div class="draft-transmission__body">
						<h2 id="draft-event-heading-public">{data.draft.event_label}</h2>
						<time class="event-date">{data.draft.long_date}</time>
						<div class="event-time">
							<strong>{data.draft.time}</strong>
							<span>{data.draft.timezone}</span>
						</div>
						<div class="event-state" role="status">
							<span class="event-state__mark" aria-hidden="true"></span>
							<strong>{data.draft.status_label}</strong>
						</div>
						<p class="event-note">{data.draft.status_note}</p>
					</div>
					<If cond={data.draft.window_reached == false}>
						<div class="countdown-strip">
							<span>Window in</span>
							<b
								class="mono"
								data-gosx-countdown={data.draft.at}
								data-gosx-countdown-format="dhms"
								data-gosx-countdown-then="revalidate"
							>{data.draft.countdown_label}</b>
						</div>
					</If>
					<If cond={data.draft.window_reached}>
						<div class="countdown-strip">
							<span>Window status</span>
							<b>{data.draft.status_label}</b>
						</div>
					</If>
				</aside>
			</section>
		</If>
		<If cond={data.viewer.signed_in}>
			<section class="status-cards">
				<article class="status-card status-card--fantasy">
					<div class="signal-label">
						<span class="live-dot" aria-hidden="true"></span>
						FANTASY
					</div>
					<If cond={data.fantasy_card.has_seat}>
						<div class="status-card__team">
							<span class={"team-mark tone-" + data.fantasy_card.team.tone} aria-hidden="true">
								<If cond={data.fantasy_card.team.has_avatar_image}>
									<img class="avatar-mark__photo" src={data.fantasy_card.team.avatar_image_url} alt={data.fantasy_card.team.name} loading="lazy" />
								</If>
								<If cond={data.fantasy_card.team.has_avatar_image == false}>
									{data.fantasy_card.team.abbreviation}
								</If>
							</span>
							<div>
								<strong>{data.fantasy_card.team.name}</strong>
								<small class="mono">
									{data.fantasy_card.team.record}
									·
									{data.fantasy_card.team.streak}
								</small>
							</div>
						</div>
						<a href="/team" data-gosx-link class="button button--compact">Open your team →</a>
					</If>
					<If cond={data.fantasy_card.has_seat == false && data.fantasy_card.league_full == false}>
						<p class="status-card__line">
							{data.fantasy_card.open_seats}
							seat(s) open — build your roster before the room fills.
						</p>
						<a href="/join" data-gosx-link class="button button--primary">Claim a team →</a>
					</If>
					<If cond={data.fantasy_card.has_seat == false && data.fantasy_card.league_full}>
						<p class="status-card__line">Every manager seat is claimed. Pick'em is always open.</p>
					</If>
				</article>
				<article class="status-card status-card--pickem">
					<div class="signal-label">
						<span class="live-dot" aria-hidden="true"></span>
						PICK'EM
					</div>
					<div class="pickem-home-panel__row">
						<span>This week</span>
						<strong class="mono">
							{data.pickem_home.unpicked_count}
							unpicked
						</strong>
					</div>
					<div class="pickem-home-panel__row">
						<span>Season record</span>
						<If cond={data.pickem_home.has_record}>
							<strong class="mono">
								{data.pickem_home.season_correct}
								/
								{data.pickem_home.season_total}
							</strong>
						</If>
						<If cond={data.pickem_home.has_record == false}>
							<strong class="mono">No picks yet</strong>
						</If>
					</div>
					<div class="pickem-home-panel__row">
						<span>Current streak</span>
						<If cond={data.pickem_home.has_streak}>
							<strong class="mono">
								{data.pickem_home.streak}
								W
							</strong>
						</If>
						<If cond={data.pickem_home.has_streak == false}>
							<strong class="mono">—</strong>
						</If>
					</div>
					<a href="/pickem" data-gosx-link class="button button--compact">Open Pick'em HQ →</a>
				</article>
			</section>
		</If>
		<If cond={data.viewer.signed_in && data.has_seat}>
		<section class="hero-command">
			<div class="hero-command__copy">
				<div class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					LEAGUE SYSTEM // ONLINE
				</div>
				<p class="hero-kicker">
					{data.league.hero_kicker}
					<span class="position-chip">{data.league_mode}</span>
				</p>
				<h1>
					THE SEASON
					<br></br>
					<span>NEVER SLEEPS.</span>
				</h1>
				<p class="hero-deck">
					Your <strong>{data.league.format_blurb}</strong> league control room: lineups, waivers, and a commissioner-published league record.
				</p>
				<div class="hero-actions">
					<a href="/matchups" data-gosx-link class="button button--primary">
						Enter live center
						<span aria-hidden="true">↗</span>
					</a>
					<a href="/draft" data-gosx-link class="button button--ghost">Open draft room</a>
				</div>
			</div>
			<aside class="draft-transmission" aria-labelledby="draft-event-heading-member">
				<div class="transmission-top">
					<span>Incoming transmission</span>
					<span class="mono">DRAFT EVENT</span>
				</div>
				<div class="chrome-disc" aria-hidden="true">
					<span>{data.league.short_code}</span>
				</div>
				<div class="draft-transmission__body">
					<h2 id="draft-event-heading-member">{data.draft.event_label}</h2>
					<time class="event-date">{data.draft.long_date}</time>
					<div class="event-time">
						<strong>{data.draft.time}</strong>
						<span>{data.draft.timezone}</span>
					</div>
					<div class="event-state" role="status">
						<span class="event-state__mark" aria-hidden="true"></span>
						<strong>{data.draft.status_label}</strong>
					</div>
					<p class="event-note">{data.draft.status_note}</p>
				</div>
				<If cond={data.draft.window_reached == false}>
					<div class="countdown-strip">
						<span>Window in</span>
						<b
							class="mono"
							data-gosx-countdown={data.draft.at}
							data-gosx-countdown-format="dhms"
							data-gosx-countdown-then="revalidate"
						>{data.draft.countdown_label}</b>
					</div>
				</If>
				<If cond={data.draft.window_reached}>
					<div class="countdown-strip">
						<span>Window status</span>
						<b>{data.draft.status_label}</b>
					</div>
				</If>
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
					// updates every 60 seconds
				</p>
				<a href="/matchups" data-gosx-link>All matchups →</a>
			</div>
		</section>
		<If cond={data.announcements_empty == false}>
			<section class="score-command">
				<header class="section-heading section-heading--split">
					<div>
						<span class="section-index">00 // ANNOUNCEMENTS</span>
						<h2>From the commissioner</h2>
					</div>
				</header>
				<div class="announcement-list">
					<Each of={data.announcements} as="note">
						<article class="announcement-item">
							<p>{note.body}</p>
							<small class="mono">
								{note.posted_by}
								·
								{note.posted_ago}
								·
								{note.posted_at}
							</small>
						</article>
					</Each>
				</div>
			</section>
		</If>
		<div class="dashboard-split">
			<section class="standings-panel">
				<header class="section-heading">
					<span class="section-index">02 // POWER GRID</span>
					<h2>Final ’{data.league.prior_season_short} table</h2>
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
							<span class="division-heading mono">{division.Name}</span>
							<Each of={division.Teams} as="team">
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
