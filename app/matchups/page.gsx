package matchups

type TeamMarkProps struct {
	Tone           string
	Abbreviation   string
	Name           string
	HasAvatarImage bool
	AvatarImageURL string
}

func TeamMark(props TeamMarkProps) Node {
	return <span class={"team-mark team-mark--large tone-" + props.Tone} aria-hidden="true">
		<If cond={props.HasAvatarImage}>
			<img class="avatar-mark__photo" src={props.AvatarImageURL} alt={props.Name} loading="lazy" />
		</If>
		<If cond={props.HasAvatarImage == false}>
			{props.Abbreviation}
		</If>
	</span>
}

// WeekBrowserProps carries the season-schedule week-paging state
// MatchupsData already computes. The rendered nav keeps the exact
// pickem-weeknav class the page carried before Task 11b, with no
// additional class alongside it: mobile_touch_contract_test.go hard-codes
// both the literal <nav class="pickem-weeknav" ...> opening tag and the
// .site-frame #main-content .pickem-weeknav > a[data-gosx-link] selector,
// so an extra class token here would break the exact-string match. The
// existing .pickem-weeknav rule (flex, wrap, gap) already gives this nav
// the layout it needs; .pickem-weeknav .board-button narrows only the
// button height.
type WeekBrowserProps struct {
	HasPrevious  bool
	PreviousHref string
	Options      []map[string]any
	HasNext      bool
	NextHref     string
	IsCurrent    bool
	CurrentHref  string
}

func WeekBrowser(props WeekBrowserProps) Node {
	return <nav class="pickem-weeknav" aria-label="Matchup week navigation">
		<If cond={props.HasPrevious}>
			<a href={props.PreviousHref} data-gosx-link class="board-button" rel="prev">◀</a>
		</If>
		<form method="get" action="/matchups" class="lineup-week-form">
			<label class="visually-hidden" for="matchups-week-select">Select matchup week</label>
			<select id="matchups-week-select" name="week" class="board-button" aria-label="Select matchup week">
				<Each of={props.Options} as="wk">
					<option value={wk.value} selected={wk.selected}>{wk.label}</option>
				</Each>
			</select>
			<button class="visually-hidden" type="submit">Go</button>
		</form>
		<If cond={props.HasNext}>
			<a href={props.NextHref} data-gosx-link class="board-button" rel="next">▶</a>
		</If>
		<If cond={props.IsCurrent == false}>
			<a href={props.CurrentHref} data-gosx-link class="access-link">Back to current week</a>
		</If>
	</nav>
}

// StarterCell is one lineup slot's single side, shared by FeaturedMatchup's
// and Scorebug's matchup-pairs lists. props.Right marks the "theirs" cell
// of a pair; page.gsx composes the six-column slot-row layout from the
// plain data-right attribute in public/styles.css (round-2 review of
// commit 133d1d7, finding 5), rather than a precomputed class string.
//
// The name renders twice — starter-cell__name-full (the live-bound
// PlayerName) and starter-cell__name-short (the server-abbreviated
// PlayerNameShort, page.server.go's mobileShortName) — with CSS toggling
// which one is visible per breakpoint (item 4, round-2 fidelity pass):
// PlayerNameShort is never itself live-bound (a starter's identity does
// not change mid-game the way its stat line does), so only the full
// variant carries data-gosx-live-bind, keeping a live update's textContent
// patch scoped to the span it actually targets.
func StarterCell(props StarterCellData) Node {
	return <div class="starter-cell" data-right={props.Right}>
		<details class="matchup-ledger">
			<summary class="starter-cell__name">
				<strong>
					<span class="starter-cell__name-full" data-gosx-live-bind={"starterPlayerName." + props.LiveKey}>{props.PlayerName}</span>
					<span class="starter-cell__name-short">{props.PlayerNameShort}</span>
				</strong>
				<small><span data-gosx-live-bind={"starterPosition." + props.LiveKey}>{props.Position}</span> · <span data-gosx-live-bind={"starterNFLTeam." + props.LiveKey}>{props.NFLTeam}</span> · <span class="starter-cell__state-text" data-gosx-live-bind={"starterGameState." + props.LiveKey}>{props.GameState}</span></small>
			</summary>
			<div class="matchup-ledger__body">
				<p class="matchup-ledger__hint">Configured starters only. Bench, reserve, and IR are excluded.</p>
				<span data-gosx-live-bind={"starterProvenance." + props.LiveKey}>{props.Provenance}</span> · <span data-gosx-live-bind={"starterJoinState." + props.LiveKey}>{props.JoinState}</span> · <span data-gosx-live-bind={"starterSource." + props.LiveKey}>{props.Source}</span>
				<small class="matchup-ledger__detail" data-gosx-live-bind={"starterDetail." + props.LiveKey}>{props.Detail}</small>
			</div>
		</details>
		<span class={"state starter-cell__state " + props.StateClass} data-gosx-live-bind={"starterGameState." + props.LiveKey}>{props.GameState}</span>
		<b class="pts starter-cell__pts" data-gosx-live-bind={"starterPoints." + props.LiveKey} data-gosx-live-flash-class="score-flash">{props.Points}</b>
	</div>
}

// FeaturedMatchup is the summary-first "my matchup" card (A6): score,
// projection, win-probability bar, then every configured starter slot by
// slot, mine and theirs side by side. IsViewer selects the labels ("Your
// team"/"Opponent" versus "Featured"/"Versus" when the week has no seated
// viewer matchup to show).
//
// The win-probability bar's fill width (the <i style={"width: " +
// props.WinProbWidth}>...</i> below) is set once, from WinProbWidth, at
// full render only: gosx's live-bind only ever patches an element's text,
// never a style attribute, so a poll can never move the fill in place.
// The percentage text right beside it (winProb.<id>) is the live-bound
// half of that same number — it keeps ticking every poll even though the
// bar it sits next to does not, until the next full render redraws both
// together.
func FeaturedMatchup(props FeaturedMatchupData) Node {
	return <section class="my-matchup card" data-live-matchup={props.ID}>
		<header class="my-matchup__summary">
			<div class="my-matchup__team">
				<TeamMark {...props.Mine}></TeamMark>
				<div>
					<span class="section-index"><If cond={props.IsViewer}>Your team</If><If cond={props.IsViewer == false}>Featured</If></span>
					<strong class="display">{props.Mine.Name}</strong>
					<small class="muted matchup-team-line"><span class="matchup-team-line__manager">{props.Mine.Manager}</span><span class="matchup-team-line__meta"> · {props.Mine.Record} · proj <span data-gosx-live-bind={"projected." + props.Mine.ID}>{props.Mine.Projected}</span></span></small>
				</div>
			</div>
			<div class="my-matchup__score">
				<div class="my-matchup__totals">
					<b class="score score--large mono" data-score-team={props.Mine.ID} data-gosx-live-bind={"scores." + props.Mine.ID} data-gosx-live-flash-class="score-flash">{props.Mine.Score}</b>
					<span class="muted">–</span>
					<b class="score score--large mono" data-score-team={props.Theirs.ID} data-gosx-live-bind={"scores." + props.Theirs.ID} data-gosx-live-flash-class="score-flash">{props.Theirs.Score}</b>
				</div>
				<div class="bar"><i style={"width: " + props.WinProbWidth}></i></div>
				<small class="mono muted"><span data-gosx-live-bind={"winProb." + props.Mine.ID}>{props.WinProb}</span> to win · <span data-gosx-live-bind={"stillToPlay." + props.ID}>{props.StillToPlay}</span> of <span data-gosx-live-bind={"stillToPlayTotal." + props.ID}>{props.StillToPlayTotal}</span> starters still to play</small>
				<span class={"state-chip " + props.StateClass}><span class="live-dot live-dot--bound" aria-hidden="true" data-gosx-live-bind={"matchupIndicator." + props.ID}>{props.LiveIndicator}</span><span data-gosx-live-bind={"matchupLiveState." + props.ID}>{props.LiveState}</span></span>
			</div>
			<div class="my-matchup__team my-matchup__team--opponent">
				<div>
					<span class="section-index muted"><If cond={props.IsViewer}>Opponent</If><If cond={props.IsViewer == false}>Versus</If></span>
					<strong class="display">{props.Theirs.Name}</strong>
					<small class="muted matchup-team-line"><span class="matchup-team-line__manager">{props.Theirs.Manager}</span><span class="matchup-team-line__meta"> · {props.Theirs.Record} · proj <span data-gosx-live-bind={"projected." + props.Theirs.ID}>{props.Theirs.Projected}</span></span></small>
				</div>
				<TeamMark {...props.Theirs}></TeamMark>
			</div>
		</header>
		<div class="slot-row slot-row--head slot-row--head-desktop">
			<span class="section-index">Starter</span><span class="section-index">Game</span><span class="section-index">Pts</span><span class="section-index">Pts</span><span class="section-index">Game</span><span class="section-index right">Starter</span>
		</div>
		<div class="slot-row slot-row--head slot-row--head-mobile">
			<span class="section-index">You</span><span class="section-index">Pts</span><span class="section-index">Pts</span><span class="section-index right">Opponent</span>
		</div>
		<ul class="matchup-pairs">
			<Each of={props.Pairs} as="pair">
				<li class="matchup-pair slot-row">
					<StarterCell {...pair.Mine}></StarterCell>
					<StarterCell {...pair.Theirs}></StarterCell>
				</li>
			</Each>
		</ul>
		<footer class="my-matchup__foot">
			<span class="mono muted">Points update as plays land · tap a starter for the box score</span>
			<If cond={props.IsViewer && props.HasNextWeek}>
				<a class="board-button" href={props.NextLineupHref} data-gosx-link>Set lineup for Week {props.NextWeek} →</a>
			</If>
		</footer>
	</section>
}

// Scorebug is one compact "around the league" matchup card: a summary a
// manager can expand to see both sides' starters, one slot per row, the
// same shape FeaturedMatchup renders for the viewer's own matchup. Its
// .mini team rows are copied from MiniMatchup (app/page.gsx:32-58) and
// then diverge (a state-chip instead of a bare live-dot, a projection
// line), so no shared component is extracted.
func Scorebug(props ScorebugData) Node {
	return <details class="scorebug card" data-live-matchup={props.ID}>
		<summary class="scorebug__summary">
			<div class="scorebug__meta">
				<span class={"state-chip " + props.StateClass}><span class="live-dot live-dot--bound" aria-hidden="true" data-gosx-live-bind={"matchupIndicator." + props.ID}>{props.LiveIndicator}</span><span data-gosx-live-bind={"matchupLiveState." + props.ID}>{props.LiveState}</span></span>
				<span class="mono muted">proj <span data-gosx-live-bind={"projected." + props.Away.ID}>{props.ProjectedAway}</span> – <span data-gosx-live-bind={"projected." + props.Home.ID}>{props.ProjectedHome}</span></span>
			</div>
			<div class="mini">
				<TeamMark {...props.Away}></TeamMark>
				<div><strong>{props.Away.Name}</strong><small>{props.Away.Manager}</small></div>
				<b class="pts score" data-score-team={props.Away.ID} data-gosx-live-bind={"scores." + props.Away.ID} data-gosx-live-flash-class="score-flash">{props.Away.Score}</b>
			</div>
			<div class="mini">
				<TeamMark {...props.Home}></TeamMark>
				<div><strong>{props.Home.Name}</strong><small>{props.Home.Manager}</small></div>
				<b class="pts score" data-score-team={props.Home.ID} data-gosx-live-bind={"scores." + props.Home.ID} data-gosx-live-flash-class="score-flash">{props.Home.Score}</b>
			</div>
			<span class="visually-hidden" data-gosx-live-bind={"matchupStatus." + props.ID}>{props.Status}</span>
			<span class="visually-hidden" data-gosx-live-bind={"matchupClock." + props.ID}>{props.Clock}</span>
		</summary>
		<ul class="matchup-pairs">
			<Each of={props.Pairs} as="pair">
				<li class="matchup-pair slot-row">
					<StarterCell {...pair.Mine}></StarterCell>
					<StarterCell {...pair.Theirs}></StarterCell>
				</li>
			</Each>
		</ul>
	</details>
}

// Page's status line (below) is one composed sentence for assistive tech
// (AT) — state, source phrase, ledger stamp, games-final count, in the
// mockup's own order: the three raw bookkeeping spans a poll needs
// (checkedAt/liveStatus/refreshLabel) carry no reading-order meaning of
// their own, so they sit outside the role="status" region entirely rather
// than fragmenting it (item 7, round-2 fidelity pass) — aria-live="polite"
// announces the region's own text as it changes, and a value no sighted
// or AT user is meant to read should not be part of that text. LEDGER's
// own sourceLine value is the literal string "Weekly ledger (nflverse)"
// (liveSourceLine, feed.go): the same words the static ledger-stamp span
// always opens with, so that span only renders once the live state has
// moved off LEDGER and the two no longer say the same thing back to back
// (item 2).
func Page() Node {
	return <main class="page matchups-page" id="main-content" data-live-root data-gosx-live-src="/api/live/week" data-gosx-live-interval={data.live_interval} data-gosx-live-on="scores:changed">
		<header class="matchups-masthead">
			<div class="matchups-masthead__title">
				<h1 class="display"><span data-gosx-live-bind="weekLabel">{data.live.week_label}</span> <span class="matchups-masthead__word">MATCHUPS</span></h1>
				<If cond={data.live.slate_line != ""}><p class="matchups-masthead__sub mono" data-gosx-live-bind="slateLine">{data.live.slate_line}</p></If>
				<p class="visually-hidden"><span data-gosx-live-bind="headlineTop">{data.live.headline_top}</span> <span data-gosx-live-bind="headlineBottom">{data.live.headline_bottom}</span> · <span data-gosx-live-bind="status">{data.live.status}</span></p>
			</div>
			<If cond={data.has_weeks}><WeekBrowser HasPrevious={data.has_previous_week} PreviousHref={data.previous_week_href} Options={data.week_options} HasNext={data.has_next_week} NextHref={data.next_week_href} IsCurrent={data.is_current_week} CurrentHref={data.current_week_href}></WeekBrowser></If>
		</header>
		<p class="matchup-status-line" role="status" aria-live="polite">
			<span class="state-chip" data-live-state={data.status_line.live_state}><span class="live-dot live-dot--bound" aria-hidden="true" data-gosx-live-bind="liveIndicator">{data.live.live_indicator}</span><b data-gosx-live-bind="liveState">{data.status_line.live_state}</b></span>
			<span class="mono matchup-status-line__source" data-gosx-live-bind="sourceLine">{data.status_line.source_line}</span>
			<If cond={data.status_line.live_state != "LEDGER"}>
				<span class="mono muted matchup-status-line__ledger">Weekly ledger (nflverse) · <span data-gosx-live-bind="statsUpdatedAt">{data.status_line.stats_updated_at}</span></span>
			</If>
			<span class="mono muted matchup-status-line__games" data-gosx-live-bind="gamesFinal">{data.status_line.games_final}</span>
		</p>
		<span class="visually-hidden" data-gosx-live-bind="checkedAt">{data.status_line.checked_at}</span><span class="visually-hidden" data-gosx-live-bind="liveStatus">{data.live.live_status}</span><span class="visually-hidden" data-gosx-live-bind="refreshLabel">{data.live.refresh_label}</span>
		<If cond={data.has_week_notice}><p class="matchup-week-notice" role="status">{data.week_notice}</p></If>
		<div class="matchup-layout">
			<If cond={data.my_matchup.HasMatchup}><FeaturedMatchup {...data.my_matchup}></FeaturedMatchup></If>
			<If cond={data.matchups_empty}><section class="my-matchup card"><div class="empty-tape"><strong>NO MATCHUPS YET</strong><p>{data.league.season_open_line}</p></div></section></If>
			<aside class="around-league">
				<header class="around-league__head"><span class="section-index">Around the league</span><span class="mono muted">{data.other_count_label}</span></header>
				<div class="matchup-grid"><Each of={data.other_matchups} as="other"><Scorebug {...other}></Scorebug></Each></div>
				<section class="score-command playoff-truth-card card" aria-labelledby="matchups-playoff-truth-heading">
					<header class="section-heading section-heading--split"><div><span class="section-index">POSTSEASON // BRACKET</span><h2 id="matchups-playoff-truth-heading">{data.playoff_truth.headline}</h2></div><span class="position-chip">{data.playoff_truth.status_label}</span></header>
					<p>{data.playoff_truth.detail}</p>
					<If cond={data.playoff_truth.source != ""}><p class="scoring-note mono">SOURCE {data.playoff_truth.source} · {data.playoff_truth.source_state} · FINAL WEEK {data.playoff_truth.final_week}</p></If>
					<If cond={data.playoff_truth.recovery != ""}><p class="demo-message"><strong>RECOVERY:</strong> {data.playoff_truth.recovery}</p></If>
					<If cond={data.playoff_truth.has_matchups}><div class="activity-feed"><Each of={data.playoff_truth.matchups} as="matchup"><div class="activity-item"><p><strong>{matchup.bracket} · ROUND {matchup.round} · WEEK {matchup.week}</strong> {matchup.home_team_name} {matchup.home_score_text} — {matchup.away_team_name} {matchup.away_score_text}</p><small>{matchup.tie_break_explanation}</small></div></Each></div></If>
					<a href="/help/commissioner-operations" data-gosx-link class="access-link">Read postseason and recovery help →</a>
				</section>
				<div class="data-note"><span data-gosx-live-bind="noteTitle">{data.live.note_title}</span><p data-gosx-live-bind="noteBody">{data.live.note_body}</p></div>
			</aside>
		</div>
	</main>
}
