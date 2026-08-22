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
// app/page.gsx's StandingRow).
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

// MatchupCardProps mirrors page.server.go's MatchupCardData converter
// type field for field — flat (AwayID, AwayName, ... HomeID, HomeName,
// ...), not nested Away/Home sub-structs, on purpose: Page() below is a
// legacy (non-strict) caller, so it must reach this strict component
// through exactly one {...matchup} spread (a legacy caller cannot pass
// named attributes to a strict callee — proven only at the file
// renderer's boundary). That boundary proves the spread source
// structurally covers MatchupCardProps at the top level, but nested
// struct-typed fields inside the source are checked by exact type
// identity, not structural coverage; keeping every field a plain scalar
// here sidesteps that entirely, and it is what lets this props struct's
// fields (all strings and one bool) resolve without needing any
// sibling-file struct type declared "beside the component" (gosx's
// strict-component check requires that for any struct type a strict
// component's body reaches through field access).
//
// It is not named MatchupCard (matching this strict component's own
// name) because a strict `component` compiles to a package-level Go
// declaration named after the component; page.server.go's converter
// type is named MatchupCardData instead, precisely to leave this
// component free to keep the name MatchupCard, matching the template
// tag Page() already calls it by. ScoreTeam below is still called with
// explicit attributes, not a spread, for the same "not provable from a
// strict body" reason TeamMark is above.
type MatchupCardProps struct {
	ID                 string
	State              string
	ShowLiveIndicator  bool
	LiveIndicator      string
	Status             string
	Clock              string
	AwayID             string
	AwayName           string
	AwayManager        string
	AwayScore          string
	AwayTone           string
	AwayAbbreviation   string
	AwayHasAvatarImage bool
	AwayAvatarImageURL string
	HomeID             string
	HomeName           string
	HomeManager        string
	HomeScore          string
	HomeTone           string
	HomeAbbreviation   string
	HomeHasAvatarImage bool
	HomeAvatarImageURL string
}

component MatchupCard(props: MatchupCardProps) {
	return <article class="matchup-card" data-live-matchup={props.ID}>
		<header>
			<span>
				<span class="live-dot live-dot--bound" aria-hidden="true" data-gosx-live-bind={"matchupIndicator." + props.ID}>{props.LiveIndicator}</span>
				<b data-matchup-status data-gosx-live-bind={"matchupStatus." + props.ID}>{props.Status}</b>
			</span>
			<span class="mono" data-matchup-clock data-gosx-live-bind={"matchupClock." + props.ID}>{props.Clock}</span>
		</header>
		<ScoreTeam
			ID={props.AwayID}
			Name={props.AwayName}
			Manager={props.AwayManager}
			Score={props.AwayScore}
			Tone={props.AwayTone}
			Abbreviation={props.AwayAbbreviation}
			HasAvatarImage={props.AwayHasAvatarImage}
			AvatarImageURL={props.AwayAvatarImageURL}
		></ScoreTeam>
		<div class="matchup-rule">
			<span>VS</span>
		</div>
		<ScoreTeam
			ID={props.HomeID}
			Name={props.HomeName}
			Manager={props.HomeManager}
			Score={props.HomeScore}
			Tone={props.HomeTone}
			Abbreviation={props.HomeAbbreviation}
			HasAvatarImage={props.HomeHasAvatarImage}
			AvatarImageURL={props.HomeAvatarImageURL}
		></ScoreTeam>
	</article>
}

func Page() Node {
	return <main class="page matchups-page" id="main-content" data-live-root data-gosx-live-src="/api/live/week" data-gosx-live-interval={data.live_interval}>
		<header class="page-masthead">
			<div>
				<span class="signal-label">
					<span class="live-dot live-dot--bound" aria-hidden="true" data-gosx-live-bind="liveIndicator">{data.live.live_indicator}</span>
					<span data-gosx-live-bind="status">{data.live.status}</span>
				</span>
				<p class="page-kicker" data-gosx-live-bind="weekLabel">{data.live.week_label}</p>
				<h1>
					<span data-gosx-live-bind="headlineTop">{data.live.headline_top}</span>
					<br></br>
					<span data-gosx-live-bind="headlineBottom">{data.live.headline_bottom}</span>
				</h1>
			</div>
			<div class="masthead-console">
				<div>
					<span>Source</span>
					<strong data-gosx-live-bind="sourceLabel">{data.live.source_label}</strong>
				</div>
				<div>
					<span>Last update</span>
					<strong class="mono" data-live-updated data-gosx-live-bind="liveUpdated">{data.live.last_updated}</strong>
				</div>
				<div>
					<span>Updates</span>
					<strong class="mono" data-gosx-live-bind="refreshLabel">{data.live.refresh_label}</strong>
				</div>
			</div>
		</header>
		<section class="matchup-week-controls" aria-label="Season matchup week">
			<div class="matchup-week-controls__copy">
				<span class="section-index">SEASON SCHEDULE // WEEK {data.week}</span>
				<strong>
					<If cond={data.is_current_week}>CURRENT WEEK</If>
					<If cond={data.is_current_week == false}>WEEK {data.week} VIEW</If>
				</strong>
				<p>
					<If cond={data.is_current_week}>Current-week updates stay on the current week. Browse another published week below.</If>
					<If cond={data.is_current_week == false}>This is a published schedule view. Live polling stays on the current week.</If>
				</p>
			</div>
			<If cond={data.has_weeks}>
				<nav class="pickem-weeknav" aria-label="Matchup week navigation">
					<If cond={data.has_previous_week}>
						<a href={data.previous_week_href} data-gosx-link class="board-button" rel="prev">← Previous</a>
					</If>
					<form method="get" action="/matchups" class="lineup-week-form">
						<label class="sr-only" for="matchups-week-select">Select matchup week</label>
						<select id="matchups-week-select" name="week" aria-label="Select matchup week">
							<Each of={data.week_options} as="wk">
								<option value={wk.value} selected={wk.selected}>{wk.label}</option>
							</Each>
						</select>
						<button class="board-button" type="submit">Go</button>
					</form>
					<If cond={data.has_next_week}>
						<a href={data.next_week_href} data-gosx-link class="board-button" rel="next">Next →</a>
					</If>
					<If cond={data.is_current_week == false}>
						<a href={data.current_week_href} data-gosx-link class="access-link">Back to current week</a>
					</If>
				</nav>
			</If>
			<If cond={data.has_week_notice}>
				<p class="matchup-week-notice" role="status">{data.week_notice}</p>
			</If>
		</section>
		<div class="matchup-layout">
			<section class="matchup-stage">
				<header class="section-heading section-heading--split">
					<div>
						<span class="section-index">SCORENET // CHANNEL 01</span>
						<h2>All league matchups</h2>
					</div>
					<div class="sync-state" role="status" aria-live="polite">
						<span class="live-dot live-dot--bound" aria-hidden="true" data-gosx-live-bind="liveIndicator">{data.live.live_indicator}</span>
						<span data-live-status data-gosx-live-bind="liveStatus">{data.live.sync_label}</span>
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
				<section class="next-matchup-panel" aria-label="Your next matchup">
					<header>
						<span class="section-index">YOUR NEXT MATCHUP</span>
						<b>{data.next_matchup.week_label}</b>
					</header>
					<If cond={data.next_matchup.has_seat}>
						<If cond={data.next_matchup.has_matchup}>
							<a href={data.next_matchup.href} data-gosx-link class="next-matchup-panel__link">
								<strong>{data.next_matchup.team_name} <span>{data.next_matchup.location_label}</span> {data.next_matchup.opponent_name}</strong>
								<small>Manager: {data.next_matchup.opponent_manager}</small>
							</a>
						</If>
						<If cond={data.next_matchup.is_bye}>
							<p class="next-matchup-panel__message">{data.next_matchup.message}</p>
						</If>
						<If cond={data.next_matchup.has_matchup == false}>
							<If cond={data.next_matchup.is_bye == false}>
								<p class="next-matchup-panel__message">{data.next_matchup.message}</p>
							</If>
						</If>
					</If>
					<If cond={data.next_matchup.has_seat == false}>
						<p class="next-matchup-panel__message">{data.next_matchup.message}</p>
						<a href="/join" class="access-link">Claim a franchise</a>
					</If>
				</section>
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
					<span data-gosx-live-bind="noteTitle">{data.live.note_title}</span>
					<p data-gosx-live-bind="noteBody">
						{data.live.note_body}
					</p>
				</div>
			</aside>
		</div>
	</main>
}
