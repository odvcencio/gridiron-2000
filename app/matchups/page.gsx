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

// ScoreTeamProps is the fields this body reads directly (ID, Manager,
// Score) plus TeamMark's required fields (Tone, Abbreviation, Name,
// HasAvatarImage, AvatarImageURL). ScoreTeam used to declare "props any"
// and bare-spread that flattened value into strict TeamMark (the
// matchups-page twin of the StandingRow bug fixed on app/page.gsx: a
// non-strict component's spread call site always flattens its source to
// map[string]any first, and a map can never satisfy a strict spread
// boundary afterward). A strict `component` declaration keeps props a
// genuine struct; TeamMark is still called with explicit attributes,
// not a spread, because a spread into a strict callee from inside
// another strict component's body is not provable at transpile time —
// only a legacy (non-strict) caller's spread is proven, at the file
// renderer's own top-level call sites (see MatchupCard below and
// app/page.gsx's StandingRow). MatchupCardProps below reuses this exact
// type for its Away/Home fields, so page.server.go's converter needs no
// sibling type of this name at all.
type ScoreTeamProps struct {
	ID             string
	Name           string
	Manager        string
	Score          string
	Tone           string
	Abbreviation   string
	HasAvatarImage bool
	AvatarImageURL string
}

component ScoreTeam(props: ScoreTeamProps) {
	return <div class="score-team">
		<TeamMark
			Tone={props.Tone}
			Abbreviation={props.Abbreviation}
			Name={props.Name}
			HasAvatarImage={props.HasAvatarImage}
			AvatarImageURL={props.AvatarImageURL}
		></TeamMark>
		<div class="score-team__name">
			<strong>{props.Name}</strong>
			<small>{props.Manager}</small>
		</div>
		<b class="score score--large" data-score-team={props.ID} data-gosx-live-bind={"scores." + props.ID} data-gosx-live-flash-class="score-flash">{props.Score}</b>
	</div>
}

// MatchupCardProps nests Away/Home as ScoreTeamProps-typed fields, the
// natural shape for one matchup's two sides. A prior gosx checker
// version proved a nested struct-typed field inside a spread by exact
// type identity, which conflicted with the rule that a strict
// component's prop types live in its own .gsx file: page.server.go's
// converter could never declare a type identical to a .gsx-local one,
// so this struct used to flatten Away/Home into scalar fields
// (AwayID, AwayName, ...) purely to dodge the conflict. gosx v0.48.0
// proves a nested struct field structurally instead (gosx#230), so
// page.server.go's MatchupCardData is free to nest its own Away/Home
// struct type — matching this one field for field, not by name — and
// the flattening serves no purpose anymore.
//
// It is not named MatchupCard (matching this strict component's own
// name) because a strict `component` compiles to a package-level Go
// declaration named after the component; page.server.go's converter
// type is named MatchupCardData instead, precisely to leave this
// component free to keep the name MatchupCard, matching the template
// tag Page() already calls it by. ScoreTeam below is still called with
// explicit attributes, not a spread, because a spread into a strict
// callee from inside another strict component's body is not provable
// at transpile time — only a legacy (non-strict) caller's spread is
// proven, at the file renderer's own top-level call sites (Page()
// below spreads into MatchupCard itself the same way).
type MatchupCardProps struct {
	ID     string
	Status string
	Clock  string
	Away   ScoreTeamProps
	Home   ScoreTeamProps
}

component MatchupCard(props: MatchupCardProps) {
	return <article class="matchup-card" data-live-matchup={props.ID}>
		<header>
			<span>
				<span class="live-dot" aria-hidden="true"></span>
				<b data-matchup-status data-gosx-live-bind={"matchupStatus." + props.ID}>{props.Status}</b>
			</span>
			<span class="mono" data-matchup-clock data-gosx-live-bind={"matchupClock." + props.ID}>{props.Clock}</span>
		</header>
		<ScoreTeam
			ID={props.Away.ID}
			Name={props.Away.Name}
			Manager={props.Away.Manager}
			Score={props.Away.Score}
			Tone={props.Away.Tone}
			Abbreviation={props.Away.Abbreviation}
			HasAvatarImage={props.Away.HasAvatarImage}
			AvatarImageURL={props.Away.AvatarImageURL}
		></ScoreTeam>
		<div class="matchup-rule">
			<span>VS</span>
		</div>
		<ScoreTeam
			ID={props.Home.ID}
			Name={props.Home.Name}
			Manager={props.Home.Manager}
			Score={props.Home.Score}
			Tone={props.Home.Tone}
			Abbreviation={props.Home.Abbreviation}
			HasAvatarImage={props.Home.HasAvatarImage}
			AvatarImageURL={props.Home.AvatarImageURL}
		></ScoreTeam>
	</article>
}

func Page() Node {
	// data-gosx-live-signal (gosx#228) restores the "Sync now" control
	// deleted during the v0.47 live-bind migration (the region primitive
	// had no manual trigger yet): a click on the button below writes the
	// $matchupsSync shared signal through data-gosx-set — no managed
	// action or WASM engine needed, data-gosx-set never depended on one —
	// and this live region's own subscription to that same signal name
	// forces one immediate /api/live/week fetch, deliberately bypassing
	// the hidden-tab pause (a discrete, user-caused trigger, not a
	// background poll) and never overlapping an already-in-flight fetch
	// (the fetch path's own inFlight latch, shared with the 1m interval
	// trigger, is what keeps a fast double click from firing twice).
	return <main class="page matchups-page" id="main-content" data-live-root data-gosx-revalidate-interval="4s" data-gosx-revalidate-src="/api/league/version" data-gosx-live-src="/api/live/week" data-gosx-live-interval="1m" data-gosx-live-signal="$matchupsSync">
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
				<button class="button button--primary button--compact" type="button" data-live-refresh data-gosx-set="$matchupsSync">Sync now</button>
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
