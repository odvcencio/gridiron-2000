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

type MiniMatchupProps struct {
	ID                string
	ShowLiveIndicator bool
	Status            string
	Clock             string
	Away              MatchupTeamCard
	Home              MatchupTeamCard
}

func MiniMatchup(props MiniMatchupProps) Node {
	return <article class="mini-matchup" data-live-matchup={props.ID}>
		<div class="mini-matchup__meta">
			<span>
				<If cond={props.ShowLiveIndicator}>
					<span class="live-dot" aria-hidden="true"></span>
				</If>
				<span data-matchup-status>{props.Status}</span>
			</span>
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

type ActionCenterActionCard struct {
	ID            string
	Priority      string
	PriorityLabel string
	Label         string
	Detail        string
	Href          string
	DueAt         string
	HasDueAt      bool
	DueLabel      string
	Urgent        bool
	Primary       bool
	NativeNavigation bool
}

type ActionCenterPanelProps struct {
	Stage               string
	StageLabel          string
	Heading             string
	Summary             string
	HasActions          bool
	ActionCount         int
	Actions             []ActionCenterActionCard
	HasCommissioner     bool
	CommissionerActions []ActionCenterActionCard
}

component ActionCenterTask(props: ActionCenterActionCard) {
	return <a
		href={props.Href}
		data-gosx-link
		class={"home-action-center__task home-action-center__task--" + props.Priority}
		data-action-center-task={props.ID}
		data-action-center-priority={props.Priority}
	>
		<span class="home-action-center__task-top">
			<span class="signal-label">{props.PriorityLabel}</span>
			<If cond={props.Primary}>
				<span class="home-action-center__task-marker">PRIMARY</span>
			</If>
			<If cond={props.Urgent}>
				<span class="home-action-center__task-marker home-action-center__task-marker--urgent">URGENT</span>
			</If>
			<If cond={props.HasDueAt}>
				<time class="mono home-action-center__due" dateTime={props.DueAt}>{props.DueLabel}</time>
			</If>
		</span>
		<strong>{props.Label}</strong>
		<span class="home-action-center__task-detail">{props.Detail}</span>
		<span class="home-action-center__task-arrow" aria-hidden="true">→</span>
	</a>
}

component ActionCenterNativeTask(props: ActionCenterActionCard) {
	return <a
		href={props.Href}
		class={"home-action-center__task home-action-center__task--" + props.Priority}
		data-action-center-task={props.ID}
		data-action-center-priority={props.Priority}
	>
		<span class="home-action-center__task-top">
			<span class="signal-label">{props.PriorityLabel}</span>
			<If cond={props.Primary}>
				<span class="home-action-center__task-marker">PRIMARY</span>
			</If>
			<If cond={props.Urgent}>
				<span class="home-action-center__task-marker home-action-center__task-marker--urgent">URGENT</span>
			</If>
			<If cond={props.HasDueAt}>
				<time class="mono home-action-center__due" dateTime={props.DueAt}>{props.DueLabel}</time>
			</If>
		</span>
		<strong>{props.Label}</strong>
		<span class="home-action-center__task-detail">{props.Detail}</span>
		<span class="home-action-center__task-arrow" aria-hidden="true">→</span>
	</a>
}

component ActionCenterPanel(props: ActionCenterPanelProps) {
	return <section class="home-action-center" data-action-center-stage={props.Stage} aria-labelledby="home-action-center-heading">
		<header class="home-action-center__header">
			<div>
				<span class="section-index">00 // {props.StageLabel}</span>
				<h1 id="home-action-center-heading">{props.Heading}</h1>
				<p>{props.Summary}</p>
			</div>
			<span class="home-action-center__status mono">ACTION CENTER</span>
		</header>
		<div class="home-action-center__body">
		<If cond={props.HasActions}>
			<div class="home-action-center__tasks" data-action-center-tasks>
				<Each of={props.Actions} as="task">
					<If cond={task.NativeNavigation == false}>
					<ActionCenterTask
						ID={task.ID}
						Priority={task.Priority}
						PriorityLabel={task.PriorityLabel}
						Label={task.Label}
						Detail={task.Detail}
						Href={task.Href}
						DueAt={task.DueAt}
						HasDueAt={task.HasDueAt}
						DueLabel={task.DueLabel}
						Urgent={task.Urgent}
						Primary={task.Primary}
						NativeNavigation={task.NativeNavigation}
					></ActionCenterTask>
					</If>
					<If cond={task.NativeNavigation}>
						<ActionCenterNativeTask
							ID={task.ID}
							Priority={task.Priority}
							PriorityLabel={task.PriorityLabel}
							Label={task.Label}
							Detail={task.Detail}
							Href={task.Href}
							DueAt={task.DueAt}
							HasDueAt={task.HasDueAt}
							DueLabel={task.DueLabel}
							Urgent={task.Urgent}
							Primary={task.Primary}
							NativeNavigation={task.NativeNavigation}
						></ActionCenterNativeTask>
					</If>
				</Each>
			</div>
		</If>
		<If cond={props.HasCommissioner}>
			<aside class="home-action-center__commissioner" data-action-center-commissioner>
				<div>
					<span class="signal-label">COMMISSIONER OVERLAY</span>
					<strong>League controls</strong>
				</div>
				<div class="home-action-center__commissioner-links">
					<Each of={props.CommissionerActions} as="task">
						<If cond={task.NativeNavigation == false}>
						<ActionCenterTask
							ID={task.ID}
							Priority={task.Priority}
							PriorityLabel={task.PriorityLabel}
							Label={task.Label}
							Detail={task.Detail}
							Href={task.Href}
							DueAt={task.DueAt}
							HasDueAt={task.HasDueAt}
							DueLabel={task.DueLabel}
							Urgent={task.Urgent}
							Primary={task.Primary}
							NativeNavigation={task.NativeNavigation}
						></ActionCenterTask>
						</If>
						<If cond={task.NativeNavigation}>
							<ActionCenterNativeTask
								ID={task.ID}
								Priority={task.Priority}
								PriorityLabel={task.PriorityLabel}
								Label={task.Label}
								Detail={task.Detail}
								Href={task.Href}
								DueAt={task.DueAt}
								HasDueAt={task.HasDueAt}
								DueLabel={task.DueLabel}
								Urgent={task.Urgent}
								Primary={task.Primary}
								NativeNavigation={task.NativeNavigation}
							></ActionCenterNativeTask>
						</If>
					</Each>
				</div>
			</aside>
		</If>
		</div>
	</section>
}

func Page() Node {
	return <main class="page home-page" id="main-content" data-gosx-revalidate-interval="4s" data-gosx-revalidate-src="/api/league/version">
		<If cond={data.viewer.signed_in == false}>
			<section class="hero-command">
				<div class="hero-command__copy">
					<div class="signal-label">
						<span class="signal-mark" aria-hidden="true"></span>
						LEAGUE SYSTEM // PRIVATE
					</div>
					<p class="hero-kicker">
						{data.league.hero_kicker}
					</p>
					<h1>
						{data.league.name}
						<br></br>
						<span>{data.public_entry.headline}</span>
					</h1>
					<p class="hero-deck">
						A private <strong>{data.league.format_blurb}</strong> for <strong>{data.league.seat_count_word}</strong> managers: lineups, waivers, and a commissioner-published league record.
					</p>
					<p class="entry-status">
						<strong>{data.public_entry.state_label}</strong>
						·
						{data.public_entry.membership_label}
					</p>
					<p class="entry-note">{data.public_entry.detail}</p>
					<p class="entry-policy-detail">{data.public_entry.membership_detail}</p>
					<div class="hero-actions">
						<a href="/login" data-gosx-link class="button button--primary">
							Sign in with Google
							<span aria-hidden="true">→</span>
						</a>
						<a href="/guide" data-gosx-link class="button button--ghost">Read the manager guide</a>
					</div>
					<If cond={data.viewer.demo}>
						<p class="demo-message">
							<strong>REHEARSAL MODE:</strong>
							explore every page without signing in.
						</p>
					</If>
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
			<ActionCenterPanel {...data.action_center}></ActionCenterPanel>
		</If>
		<If cond={data.viewer.signed_in && data.has_seat}>
		<section class="score-command" data-live-root>
			<header class="section-heading section-heading--split">
				<div>
					<span class="section-index">01 // MATCHUP PREVIEW</span>
					<h2>League simulator</h2>
				</div>
				<div class="sync-state" role="status" aria-live="polite">
					<If cond={data.live.show_live_indicator}>
						<span class="live-dot" aria-hidden="true"></span>
					</If>
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
						Matchups appear in NFL week
						{data.season_start_week}.
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
					<h2>{data.standings_title}</h2>
					<p>{data.standings_note}</p>
				</header>
				<If cond={data.standings_available}>
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
				</If>
				<If cond={data.standings_available == false}>
					<div class="empty-tape">
						<strong>{data.standings_empty_title}</strong>
						<p>{data.standings_note}</p>
					</div>
				</If>
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
						Scheduled time is the meeting point, not an auto-start. The commissioner randomizes draft order about one hour before the room opens. Draft order locks when the commissioner starts the draft.
					</p>
					<a href="/draft" data-gosx-link>Review draft protocol</a>
				</div>
			</aside>
		</div>
		</If>
	</main>
}
