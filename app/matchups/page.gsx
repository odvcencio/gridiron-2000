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
			<a href={props.PreviousHref} data-gosx-link class="board-button" aria-label="Previous week" rel="prev">◀</a>
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
			<a href={props.NextHref} data-gosx-link class="board-button" aria-label="Next week" rel="next">▶</a>
		</If>
		<If cond={props.IsCurrent == false}>
			<a href={props.CurrentHref} data-gosx-link class="access-link">Back to current week</a>
		</If>
	</nav>
}

// StarterCell is one lineup slot's single side, shared by FeaturedMatchup's
// and Scorebug's matchup-pairs lists. props.Right marks the "theirs" cell
// of a pair; page.gsx composes the nine-column slot-row layout (four per
// side plus the shared slot-label column) from the plain data-right
// attribute in public/styles.css (round-2 review of commit 133d1d7,
// finding 5), rather than a precomputed class string.
//
// The name renders twice — starter-cell__name-full (the live-bound
// PlayerName) and starter-cell__name-short (the server-abbreviated
// PlayerNameShort, page.server.go's mobileShortName) — with CSS toggling
// which one is visible per breakpoint (item 4, round-2 fidelity pass):
// PlayerNameShort is never itself live-bound (a starter's identity does
// not change mid-game the way its stat line does), so only the full
// variant carries data-gosx-live-bind, keeping a live update's textContent
// patch scoped to the span it actually targets.
//
// starter-cell__name-short's own TextBlock carries mode="native" (found
// during the 2026-09-07 matchup redesign's own verification pass): the
// default "bootstrap" runtime sets this element's display to flow-root
// via an inline style the instant it enhances the block, which — being
// an inline style — outranks this file's own .starter-cell__name-short
// { display: none } desktop rule no matter its selector specificity,
// showing BOTH names stacked at every width instead of only the one the
// current breakpoint calls for. native mode still gets the server-
// planned maxLines clamp (the only refinement this short, pre-abbreviated
// label ever needs) without the runtime ever touching its display.
//
// role="cell" (gap-audit item 7, wave 4 — linden) marks each of this
// component's four direct children — .matchup-ledger, .starter-cell__state,
// .starter-cell__proj, .starter-cell__pts — as one ARIA table cell apiece
// (the PROJ cell, A2 of the 2026-09-07 matchup redesign, is the newest of
// the four). That works without any wrapper element because .starter-cell
// itself has display: contents (public/styles.css), which already removes
// it from both the visual box tree and the accessibility tree, promoting
// these four children to be the actual grid items .slot-row lays out — the
// same fact the CSS comment beside .starter-cell documents for layout
// purposes. The enclosing .slot-row (FeaturedMatchup/Scorebug below)
// carries role="row" and this cell count matches its header row's own
// role="columnheader" cells exactly (four per side, plus the shared slot
// label) at every breakpoint: .starter-cell__state is display: none below
// the mobile breakpoint, and the mobile header row narrows in step with
// it (public/styles.css), so an assistive-technology user is never told
// about a header/cell column that is not actually visible.
func StarterCell(props StarterCellData) Node {
	return <div class="starter-cell" data-right={props.Right}>
		<details class="matchup-ledger" role="cell">
			<summary class="starter-cell__name">
				<strong>
					<span class="starter-cell__name-full" data-gosx-live-bind={"starterPlayerName." + props.LiveKey}>{props.PlayerName}</span>
					<TextBlock as="span" class="starter-cell__name-short" font="700 15px Plus Jakarta Sans" lineHeight={18} maxLines={2} overflow="ellipsis" mode="native" text={props.PlayerNameShort} />
				</strong>
				<small><span data-gosx-live-bind={"starterPosition." + props.LiveKey}>{props.Position}</span> · <span data-gosx-live-bind={"starterNFLTeam." + props.LiveKey}>{props.NFLTeam}</span><span class="starter-cell__state-text"> · <span data-gosx-live-bind={"starterGameState." + props.LiveKey}>{props.GameState}</span></span><span class="possession-chip" data-gosx-live-bind={"starterPossession." + props.LiveKey}>{props.Possession}</span></small>
			</summary>
			<div class="matchup-ledger__body">
				<TextBlock as="p" class="matchup-ledger__hint" font="400 13px Plus Jakarta Sans" lineHeight={18} text="Configured starters only. Bench, reserve, and IR are excluded." />
				<span data-gosx-live-bind={"starterProvenanceText." + props.LiveKey}>{props.ProvenanceText}</span><span data-gosx-live-bind={"starterJoinStateText." + props.LiveKey}>{props.JoinStateText}</span><span data-gosx-live-bind={"starterSourceText." + props.LiveKey}>{props.SourceText}</span>
				<small class="matchup-ledger__detail" data-gosx-live-bind={"starterDetail." + props.LiveKey}>{props.Detail}</small>
			</div>
		</details>
		<span class={"state starter-cell__state " + props.StateClass} role="cell" data-gosx-live-bind={"starterGameState." + props.LiveKey}>{props.GameState}</span>
		<span class="proj starter-cell__proj" role="cell" data-gosx-live-bind={"starterProj." + props.LiveKey}>{props.Proj}</span>
		<b class="pts starter-cell__pts" role="cell" data-gosx-live-bind={"starterPoints." + props.LiveKey} data-gosx-live-flash-class="score-flash">{props.Points}</b>
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
					<TextBlock as="strong" class="display" font="400 15px Archivo Black" lineHeight={18} maxLines={2} overflow="ellipsis" text={props.Mine.Name} />
					<small class="muted matchup-team-line"><TextBlock as="span" class="matchup-team-line__manager" font="400 13px Plus Jakarta Sans" lineHeight={18} maxLines={1} overflow="ellipsis" text={props.Mine.Manager} /><span class="matchup-team-line__meta"> · {props.Mine.Record}</span></small>
				</div>
			</div>
			<div class={"my-matchup__score " + props.StateClass}>
				<span class="my-matchup__phase-label mono muted">{props.PhaseLabel}</span>
				<div class="my-matchup__totals">
					<b class="score score--large mono my-matchup__score-value" data-score-team={props.Mine.ID} data-gosx-live-bind={"scores." + props.Mine.ID} data-gosx-live-flash-class="score-flash">{props.Mine.Score}</b>
					<span class="my-matchup__proj-value mono" data-gosx-live-bind={"projected." + props.Mine.ID}>{props.Mine.Projected}</span>
					<span class="muted">–</span>
					<b class="score score--large mono my-matchup__score-value" data-score-team={props.Theirs.ID} data-gosx-live-bind={"scores." + props.Theirs.ID} data-gosx-live-flash-class="score-flash">{props.Theirs.Score}</b>
					<span class="my-matchup__proj-value mono" data-gosx-live-bind={"projected." + props.Theirs.ID}>{props.Theirs.Projected}</span>
				</div>
				<small class="my-matchup__proj-sub mono muted">proj <span data-gosx-live-bind={"projected." + props.Mine.ID}>{props.Mine.Projected}</span> – <span data-gosx-live-bind={"projected." + props.Theirs.ID}>{props.Theirs.Projected}</span></small>
				<div class="bar"><i style={"width: " + props.WinProbWidth} role="meter" aria-valuemin="0" aria-valuemax="100" aria-valuenow={props.WinProbAriaValue} aria-label={props.WinProbAriaLabel}></i></div>
				<small class="mono muted"><span data-gosx-live-bind={"winProb." + props.Mine.ID}>{props.WinProb}</span> to win · <span data-gosx-live-bind={"stillToPlaySentence." + props.ID}>{props.StillToPlaySentence}</span><span class="visually-hidden" data-gosx-live-bind={"stillToPlay." + props.ID}>{props.StillToPlay}</span><span class="visually-hidden" data-gosx-live-bind={"stillToPlayTotal." + props.ID}>{props.StillToPlayTotal}</span></small>
				<span class={"state-chip " + props.StateClass}><span class="live-dot live-dot--bound" aria-hidden="true" data-gosx-live-bind={"matchupIndicator." + props.ID}>{props.LiveIndicator}</span><span data-gosx-live-bind={"matchupLiveState." + props.ID}>{props.LiveState}</span></span>
			</div>
			<div class="my-matchup__team my-matchup__team--opponent">
				<div>
					<span class="section-index muted"><If cond={props.IsViewer}>Opponent</If><If cond={props.IsViewer == false}>Versus</If></span>
					<TextBlock as="strong" class="display" font="400 15px Archivo Black" lineHeight={18} maxLines={2} overflow="ellipsis" text={props.Theirs.Name} />
					<small class="muted matchup-team-line"><TextBlock as="span" class="matchup-team-line__manager" font="400 13px Plus Jakarta Sans" lineHeight={18} maxLines={1} overflow="ellipsis" text={props.Theirs.Manager} /><span class="matchup-team-line__meta"> · {props.Theirs.Record}</span></small>
				</div>
				<TeamMark {...props.Theirs}></TeamMark>
			</div>
		</header>
		<div class="matchup-pairs-table" role="table" aria-label={"Starting lineup comparison: " + props.Mine.Name + " versus " + props.Theirs.Name}>
		<div class="slot-row slot-row--head slot-row--head-desktop" role="row">
			<span class="section-index" role="columnheader" aria-label={props.Mine.Name + " starter"}>Starter</span><span class="section-index" role="columnheader" aria-label={props.Mine.Name + " game"}>Game</span><span class="section-index" role="columnheader" aria-label={props.Mine.Name + " projected points"}>Proj</span><span class="section-index" role="columnheader" aria-label={props.Mine.Name + " points"}>Pts</span><span class="section-index slot-row__slot-head" role="columnheader">Slot</span><span class="section-index" role="columnheader" aria-label={props.Theirs.Name + " points"}>Pts</span><span class="section-index" role="columnheader" aria-label={props.Theirs.Name + " projected points"}>Proj</span><span class="section-index" role="columnheader" aria-label={props.Theirs.Name + " game"}>Game</span><span class="section-index right" role="columnheader" aria-label={props.Theirs.Name + " starter"}>Starter</span>
		</div>
		<div class="slot-row slot-row--head slot-row--head-mobile" role="row">
			<span class="section-index" role="columnheader" aria-label={props.Mine.Name + " starter"}>You</span><span class="section-index right" role="columnheader" aria-label={props.Theirs.Name + " starter"}>Opponent</span>
		</div>
		<ul class="matchup-pairs" role="rowgroup">
			<Each of={props.Pairs} as="pair">
				<li class="matchup-pair slot-row" role="row">
					<StarterCell {...pair.Mine}></StarterCell>
					<span class="slot-row__slot" role="cell">{pair.Slot}</span>
					<StarterCell {...pair.Theirs}></StarterCell>
				</li>
			</Each>
		</ul>
		<div class="matchup-pairs-totals" role="row">
			<span class="matchup-pairs-totals__side" role="cell">PROJ <b data-gosx-live-bind={"projected." + props.Mine.ID}>{props.Mine.Projected}</b> · PTS <b data-gosx-live-bind={"scores." + props.Mine.ID}>{props.Mine.Score}</b></span>
			<span class="matchup-pairs-totals__label" role="cell">Total</span>
			<span class="matchup-pairs-totals__side matchup-pairs-totals__side--right" role="cell">PROJ <b data-gosx-live-bind={"projected." + props.Theirs.ID}>{props.Theirs.Projected}</b> · PTS <b data-gosx-live-bind={"scores." + props.Theirs.ID}>{props.Theirs.Score}</b></span>
		</div>
		</div>
		<details class="matchup-benches">
			<summary>Benches</summary>
			<div class="matchup-benches__body">
				<div class="matchup-benches__side">
					<span class="section-index">{props.Mine.Name}</span>
					<ul class="matchup-benches__list">
						<Each of={props.MineBench} as="player">
							<li><TextBlock as="span" class="matchup-benches__name" font="600 14px Plus Jakarta Sans" lineHeight={18} maxLines={1} overflow="ellipsis" text={player.PlayerName} /><span class="matchup-benches__meta muted">{player.Position} · {player.NFLTeam}</span><span class="matchup-benches__proj mono">PROJ {player.Proj}</span></li>
						</Each>
					</ul>
				</div>
				<div class="matchup-benches__side">
					<span class="section-index">{props.Theirs.Name}</span>
					<ul class="matchup-benches__list">
						<Each of={props.TheirsBench} as="player">
							<li><TextBlock as="span" class="matchup-benches__name" font="600 14px Plus Jakarta Sans" lineHeight={18} maxLines={1} overflow="ellipsis" text={player.PlayerName} /><span class="matchup-benches__meta muted">{player.Position} · {player.NFLTeam}</span><span class="matchup-benches__proj mono">PROJ {player.Proj}</span></li>
						</Each>
					</ul>
				</div>
			</div>
		</details>
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
// line, a record, and — A1 of the 2026-09-07 matchup redesign — the same
// win-probability meter and still-to-play sentence the featured card
// carries), so no shared component is extracted.
//
// The role="table" wrapper and its visually-hidden column-header row
// (gap-audit item 7, wave 4 — linden) match FeaturedMatchup's own fix
// below, minus the visible "Starter/Game/Pts" header text: Scorebug never
// showed a visible header row for this nine-column comparison (unlike
// FeaturedMatchup), and this fix keeps that unchanged for sighted users
// while still giving a screen reader the same qualified column headers.
// Neither side here is the viewer's own team (this card is "around the
// league"), so headers name the actual teams (Away/Home) rather than
// "Your"/"Opponent" — matchupsPageScorebugs (page.server.go) builds
// pair.Mine from the same "away" entry TeamMark renders first above and
// pair.Theirs from "home", the identical left-to-right order this
// summary already uses.
func Scorebug(props ScorebugData) Node {
	return <details class="scorebug card" data-live-matchup={props.ID} data-phase={props.StateClass}>
		<summary class="scorebug__summary">
			<div class="scorebug__meta">
				<span class={"state-chip " + props.StateClass}><span class="live-dot live-dot--bound" aria-hidden="true" data-gosx-live-bind={"matchupIndicator." + props.ID}>{props.LiveIndicator}</span><span data-gosx-live-bind={"matchupLiveState." + props.ID}>{props.LiveState}</span></span>
				<span class="scorebug__phase-label mono muted">{props.PhaseLabel}</span>
			</div>
			<div class="mini">
				<TeamMark {...props.Away}></TeamMark>
				<div><TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={1} overflow="ellipsis" text={props.Away.Name} /><small class="muted matchup-team-line"><TextBlock as="span" class="matchup-team-line__manager" font="400 13px Plus Jakarta Sans" lineHeight={18} maxLines={1} overflow="ellipsis" text={props.Away.Manager} /><span class="matchup-team-line__meta"> · {props.Away.Record}</span></small></div>
				<div class="mini__score">
					<b class="pts score scorebug__score-value" data-score-team={props.Away.ID} data-gosx-live-bind={"scores." + props.Away.ID} data-gosx-live-flash-class="score-flash">{props.Away.Score}</b>
					<span class="pts proj scorebug__proj-value mono" data-gosx-live-bind={"projected." + props.Away.ID}>{props.ProjectedAway}</span>
				</div>
			</div>
			<div class="mini">
				<TeamMark {...props.Home}></TeamMark>
				<div><TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={1} overflow="ellipsis" text={props.Home.Name} /><small class="muted matchup-team-line"><TextBlock as="span" class="matchup-team-line__manager" font="400 13px Plus Jakarta Sans" lineHeight={18} maxLines={1} overflow="ellipsis" text={props.Home.Manager} /><span class="matchup-team-line__meta"> · {props.Home.Record}</span></small></div>
				<div class="mini__score">
					<b class="pts score scorebug__score-value" data-score-team={props.Home.ID} data-gosx-live-bind={"scores." + props.Home.ID} data-gosx-live-flash-class="score-flash">{props.Home.Score}</b>
					<span class="pts proj scorebug__proj-value mono" data-gosx-live-bind={"projected." + props.Home.ID}>{props.ProjectedHome}</span>
				</div>
			</div>
			<div class="scorebug__prob">
				<div class="bar"><i style={"width: " + props.WinProbHomeWidth} role="meter" aria-valuemin="0" aria-valuemax="100" aria-valuenow={props.WinProbHomeAriaValue} aria-label={props.WinProbHomeAriaLabel}></i></div>
				<small class="mono muted"><span data-gosx-live-bind={"winProb." + props.Home.ID}>{props.WinProbHome}</span> to win · <span data-gosx-live-bind={"stillToPlaySentence." + props.ID}>{props.StillToPlaySentence}</span></small>
			</div>
			<span class="visually-hidden" data-gosx-live-bind={"matchupStatus." + props.ID}>{props.Status}</span>
			<span class="visually-hidden" data-gosx-live-bind={"matchupClock." + props.ID}>{props.Clock}</span>
		</summary>
		<div class="matchup-pairs-table" role="table" aria-label={"Starting lineup comparison: " + props.Away.Name + " versus " + props.Home.Name}>
		<div class="slot-row slot-row--head visually-hidden" role="row">
			<span class="section-index" role="columnheader" aria-label={props.Away.Name + " starter"}>Starter</span><span class="section-index" role="columnheader" aria-label={props.Away.Name + " game"}>Game</span><span class="section-index" role="columnheader" aria-label={props.Away.Name + " projected points"}>Proj</span><span class="section-index" role="columnheader" aria-label={props.Away.Name + " points"}>Pts</span><span class="section-index" role="columnheader">Slot</span><span class="section-index" role="columnheader" aria-label={props.Home.Name + " points"}>Pts</span><span class="section-index" role="columnheader" aria-label={props.Home.Name + " projected points"}>Proj</span><span class="section-index" role="columnheader" aria-label={props.Home.Name + " game"}>Game</span><span class="section-index right" role="columnheader" aria-label={props.Home.Name + " starter"}>Starter</span>
		</div>
		<ul class="matchup-pairs" role="rowgroup">
			<Each of={props.Pairs} as="pair">
				<li class="matchup-pair slot-row" role="row">
					<StarterCell {...pair.Mine}></StarterCell>
					<span class="slot-row__slot" role="cell">{pair.Slot}</span>
					<StarterCell {...pair.Theirs}></StarterCell>
				</li>
			</Each>
		</ul>
		</div>
	</details>
}

// Page's status line (below) is one composed sentence for assistive tech
// (AT) — state, source phrase, ledger stamp, games-final count, in the
// mockup's own order. wave-6 item 8: the raw bookkeeping spans a poll
// needs (liveStatus/refreshLabel) used to sit outside the role="status"
// region entirely, visually-hidden — a sighted user saw no freshness
// clause anywhere on the page at all (the 2026-09-01 re-audit), even
// though the live-bind values already carried one, e.g. "Waiting for
// kickoff · Checked Tue Sep 1 · 4:41 PM EDT · Ledger unavailable". They
// are now the status line's own trailing clause, visible to sighted and
// AT users alike (role="status"/aria-live="polite" announces this whole
// paragraph's text as it changes), keeping every data-gosx-live-bind
// attribute so a poll still updates them in place. LEDGER's own
// sourceLine value is the literal string "Weekly ledger (nflverse)"
// (liveSourceLine, feed.go): the same words the static ledger-stamp span
// always opens with, so that span only renders once the live state has
// moved off LEDGER and the two no longer say the same thing back to back
// (item 2).
//
// wave-8 audit item 4: liveStatus (service.go's liveStatusText) used to
// compose its own "... · Checked ... · Ledger ..." clause AND the
// freshness clause below carried its own separate static "· Checked
// {checkedAt}" segment right beside it — the same clock, named twice in
// one rendered sentence. liveStatusText no longer names Checked at all
// (it only names the Ledger clock); the freshness clause's own checkedAt
// bind below is now the sentence's one and only "Checked" mention.
// liveStatusText also no longer says "Ledger Unavailable" before this
// week's first kickoff — a genuine "nothing has posted yet" state, not an
// outage — in favor of "Weekly ledger opens after the first game", which
// no longer contradicts the source clause's "Weekly ledger (nflverse)"
// right above it.
// MatchupStatusBlock is the A5/A6 status paragraph (state chip, source
// line, ledger stamp, games-final count, freshness clause) plus the
// week-notice line, split out of Page() (item 1, wave 7b) so it can
// render on either side of MatchupScoreBlock: Page() puts this block
// first on a still-scheduled week (nothing to score yet, so the status
// context leads) and after it on game day (matchupsIsGameDay,
// page.server.go) once there is a real score to lead with instead. Both
// call sites read the same "data" binding this function does, so moving
// it costs nothing beyond the two-call duplication.
func MatchupStatusBlock() Node {
	return <>
		<p class="matchup-status-line" role="status" aria-live="polite">
			<span class="state-chip" data-live-state={data.status_line.live_state}><span class="live-dot live-dot--bound" aria-hidden="true" data-gosx-live-bind="liveIndicator">{data.live.live_indicator}</span><b data-gosx-live-bind="liveState">{data.status_line.live_state}</b></span>
			{/* F3 (J4 console gap-audit): a forced close can finalize a week
			    while its real NFL games are not final — every score risking
			    0.0 from a missed player-stat join — and this line used to
			    read exactly like an honest FINAL. This badge is a plain
			    static element (no data-gosx-live-bind): the week is final,
			    so live polling has already stopped, and the badge's truth
			    must not depend on a poll that will never run again. */}
			<If cond={data.status_line.closed_early}>
				<span class="state-chip state-chip--closed-early">CLOSED EARLY</span>
			</If>
			<span class="mono matchup-status-line__source" data-gosx-live-bind="sourceLine">{data.status_line.source_line}</span>
			<If cond={data.status_line.live_state != "LEDGER"}>
				<span class="mono muted matchup-status-line__ledger">Weekly ledger (nflverse) · <span data-gosx-live-bind="statsUpdatedAt">{data.status_line.stats_updated_at}</span></span>
			</If>
			<span class="mono muted matchup-status-line__games" data-gosx-live-bind="gamesFinal">{data.status_line.games_final}</span>
			<span class="mono muted matchup-status-line__freshness">· <span data-gosx-live-bind="liveStatus">{data.live.live_status}</span> · Checked <span data-gosx-live-bind="checkedAt">{data.status_line.checked_at}</span> · <span data-gosx-live-bind="refreshLabel">{data.live.refresh_label}</span></span>
			<span class="mono muted matchup-status-line__projection" data-gosx-live-bind="projectionNote">{data.live.projection_note}</span>
		</p>
		<If cond={data.status_line.closed_early}>
			<p class="matchup-week-notice matchup-week-notice--warning" role="status">Week {data.week} closed early: {data.status_line.games_final} at close. Scores may not reflect the final box score.</p>
		</If>
		<If cond={data.has_week_notice}><p class="matchup-week-notice" role="status">{data.week_notice}</p></If>
	</>
}

// MatchupScoreBlock is the featured-card-plus-around-the-league layout —
// the page's actual score content, split out of Page() (item 1, wave 7b)
// for the same before/after-MatchupStatusBlock reason that function's own
// doc comment gives.
func MatchupScoreBlock() Node {
	return <div class="matchup-layout">
		<If cond={data.my_matchup.HasMatchup}><FeaturedMatchup {...data.my_matchup}></FeaturedMatchup></If>
		<If cond={data.matchups_empty}><section class="my-matchup card"><div class="empty-tape"><strong>NO MATCHUPS YET</strong><p>{data.league.season_open_line}</p><a href="/draft" data-gosx-link class="button button--compact">Open the draft room →</a></div></section></If>
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
}

func Page() Node {
	return <main class="page matchups-page" id="main-content" data-live-root data-gosx-live-src="/api/live/week" data-gosx-live-interval={data.live_interval} data-gosx-live-on="scores:changed">
		<header class="matchups-masthead">
			<div class="matchups-masthead__title">
				<h1 class="display"><span data-gosx-live-bind="weekLabel">{data.live.week_label}</span> <span class="matchups-masthead__word">MATCHUPS</span></h1>
				<If cond={data.live.slate_line != ""}><p class="matchups-masthead__sub mono" data-gosx-live-bind="slateLine">{data.live.slate_line}</p></If>
				<p class="matchups-masthead__state mono"><span data-gosx-live-bind="headlineTop">{data.live.headline_top}</span> <span data-gosx-live-bind="headlineBottom">{data.live.headline_bottom}</span> · <span data-gosx-live-bind="status">{data.live.status}</span></p>
			</div>
			<If cond={data.has_weeks}><WeekBrowser HasPrevious={data.has_previous_week} PreviousHref={data.previous_week_href} Options={data.week_options} HasNext={data.has_next_week} NextHref={data.next_week_href} IsCurrent={data.is_current_week} CurrentHref={data.current_week_href}></WeekBrowser></If>
		</header>
		<If cond={data.is_game_day}>
			<MatchupScoreBlock></MatchupScoreBlock>
			<MatchupStatusBlock></MatchupStatusBlock>
		</If>
		<If cond={data.is_game_day == false}>
			<MatchupStatusBlock></MatchupStatusBlock>
			<MatchupScoreBlock></MatchupScoreBlock>
		</If>
	</main>
}
