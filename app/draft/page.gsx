package draft

type DraftBreakdownRow struct {
	Scored bool
	Label  string
	Calc   string
	Points string
}

type DraftPlayerCard struct {
	ID              string
	Name            string
	Position        string
	NFLTeam         string
	Projection      string
	Rank            string
	// HasHouseRank/HouseRank back the secondary "H##" label beside Rank
	// (houserank.go): the format-aware replacement-value rank under the
	// league's active roster preset. Empty/false for a zero-Projection
	// player, who carries no house rank at all.
	HasHouseRank bool
	HouseRank    string
	Detail       string
	// News/HasNews back the legacy DraftQueue pool row's stat-tip panel
	// headline (wave 8 hotfix, item 1 — mirrors board.BoardRowProps'
	// player.news/has_news, sourced from the same playerMap "news"/
	// "has_news" keys, service.go). Detail itself never carries the
	// headline (playerMap's own doc comment on "detail"): a caller that
	// wires draftPlayerCardView (page.server.go, not a page.gsx concern)
	// must populate these two from the identical map keys for the panel
	// line below to render live data; both stay their zero value (empty/
	// false, no panel line) until that population lands.
	News    string
	HasNews bool
	// Injury/HasInjury back the news stat-tip's own secondary line (wave
	// 8 hotfix, item 1 design revision — "the injury note if any"
	// alongside the headline), sourced from playerMap's own "injury"/
	// "has_injury" keys (service.go, the same follow-up wiring gap News/
	// HasNews's own doc comment above describes).
	Injury          string
	HasInjury       bool
	Headshot        string
	HasHeadshot     bool
	Jersey          string
	HasBreakdown    bool
	Breakdown       []DraftBreakdownRow
	BreakdownTotal  string
	HasHist         bool
	Hist            string
	HistLabel       string
	Search          string
	HasDraftCapital bool
	DraftCapital    string
	HasOpponent     bool
	Opponent        string
	HasMatchup      bool
	MatchupTier     string
	MatchupChip     string
	MatchupDetail   string
	CanDraft        bool
	Taken           bool
	// HasValue/ValueLabel back the available pane's VS ADP cell (R4): "if
	// drafted right now" — the upcoming pick minus the player's ADP.
	// Always false/"" for a queue-pane row, which never renders the column.
	HasValue   bool
	ValueLabel string
	// CanMoveUp/CanMoveDown back the queue pane's no-JS up/down reorder
	// forms (mirrors board.BoardRowProps), the same board_can_move_up/down
	// fields BoardData already computes, reused here for the queue's own
	// position within the same underlying board order (service.go's
	// queuePanel). Always false for an available-pool entry, which never
	// renders these controls.
	CanMoveUp   bool
	CanMoveDown bool
	// SpecialistEarly (comb — larch, 2026-09-04, J1 F12): true for a K/P/
	// DST row while more than three rounds remain in the draft — the
	// available pane's own row-level confirm panel shows its "Specialists
	// usually go late" line only then. Always false for a queue-pane
	// entry (draftPlayerProps never sets it there), which never renders
	// the confirm panel this backs.
	SpecialistEarly bool
}

type DraftQueueProps struct {
	Players       []DraftPlayerCard
	Action        string
	CSRF          string
	TeamID        string
	CanPick       bool
	DraftComplete bool
	HasSeat       bool
	Position      string
	Query         string
	Page          int
	Total         int
	Start          int
	End            int
	HasPrevious   bool
	HasNext       bool
	PreviousHref  string
	NextHref      string
	AllHref       string
	RBHref        string
	WRHref        string
	QBHref        string
	TEHref        string
	KHref         string
	DSTHref       string
	PHref         string
}

// DraftQueue is deliberately server-first. The previous island rendered the
// entire live pool into the initial document and then filtered it in-browser.
// GET filters and bounded pages keep search useful without JavaScript, keep the
// draft update stream authoritative, and make the first paint usable on a phone.
func DraftQueue(props DraftQueueProps) Node {
	return <section class="player-pool">
		<If cond={props.DraftComplete == false}>
		<div class="pool-toolbar">
			<div>
				<span class="section-index">PLAYER LIST</span>
				<h2>Available now</h2>
			</div>
			<div class="position-filters" aria-label="Filter draft pool by position">
				<a href={props.AllHref} data-gosx-link class="filter-button" aria-current={props.Position == ""}>All</a>
				<a href={props.RBHref} data-gosx-link class="filter-button" aria-current={props.Position == "RB"}>RB</a>
				<a href={props.WRHref} data-gosx-link class="filter-button" aria-current={props.Position == "WR"}>WR</a>
				<a href={props.QBHref} data-gosx-link class="filter-button" aria-current={props.Position == "QB"}>QB</a>
				<a href={props.TEHref} data-gosx-link class="filter-button" aria-current={props.Position == "TE"}>TE</a>
				<a href={props.KHref} data-gosx-link class="filter-button" aria-current={props.Position == "K"}>K</a>
				<a href={props.DSTHref} data-gosx-link class="filter-button" aria-current={props.Position == "DST"}>DST</a>
				<a href={props.PHref} data-gosx-link class="filter-button" aria-current={props.Position == "P"}>P</a>
			</div>
		</div>
		<form method="get" action="/draft" class="pool-search-bar">
			<label class="mono" for="pool-search">SEARCH //</label>
			<input
				id="pool-search"
				type="search"
				name="q"
				value={props.Query}
				placeholder="Search player, team, or position"
				autocomplete="off"
			 />
			<If cond={props.Position != ""}>
				<input type="hidden" name="pos" value={props.Position}></input>
			</If>
			<button class="filter-button" type="submit">Search</button>
		</form>
		<div class="pool-labels mono" aria-hidden="true">
			<span>RK</span>
			<span>PLAYER</span>
			<span>POS</span>
			<span>PROJ</span>
			<span>ACTION</span>
		</div>
		<div class="pool-list">
			<Each of={props.Players} as="player">
				<article class="pool-row" data-player-position={player.Position} data-search={player.Search}>
					<span class="pool-rank mono">{player.Rank}<If cond={player.HasHouseRank}><small class="house-rank">{player.HouseRank}</small></If></span>
					<span class="pool-player-cell">
					<details class="stat-tip">
						<summary class="pool-player pool-player--photo stat-tip__summary">
						<If cond={player.HasHeadshot}>
							<img class="player-headshot" src={player.Headshot} alt="" loading="lazy" />
						</If>
						<span class="pool-player__text">
							<strong>{player.Name}</strong>
							<If cond={player.HasDraftCapital}>
								<span class="badge-rookie">{player.DraftCapital}</span>
							</If>
							<small>{player.Detail}</small>
							<If cond={player.HasOpponent}>
								<small class="mono">
									{player.Opponent}
									<If cond={player.HasMatchup}>
										·
										<span class="matchup-chip" data-matchup-tier={player.MatchupTier}>{player.MatchupChip}</span>
									</If>
								</small>
							</If>
						</span>
						</summary>
						<div class="stat-tip__panel">
							<div class="stat-tip__head">
								<strong>{player.Name}</strong>
								<span class="mono">{player.Jersey}</span>
								<span class="mono stat-tip__team">{player.NFLTeam}</span>
							</div>
							<If cond={player.HasBreakdown}>
								<div class="stat-tip__rows">
									<Each of={player.Breakdown} as="row">
										<div class="stat-tip__row" data-scored={row.Scored}>
											<span>{row.Label}</span>
											<span class="mono">{row.Calc}</span>
											<b class="mono">{row.Points}</b>
										</div>
									</Each>
									<div class="stat-tip__total">
										<span>League scoring</span>
										<b class="mono">{player.BreakdownTotal}</b>
									</div>
								</div>
							</If>
							<If cond={player.HasBreakdown == false}>
								<p class="stat-tip__empty">No projection detail for this position.</p>
							</If>
							<If cond={player.HasMatchup}>
								<p class="stat-tip__hist mono">{player.MatchupDetail}</p>
							</If>
							<If cond={player.HasHist}>
								<p class="stat-tip__hist mono">{player.Hist}</p>
								<p class="stat-tip__hist-note">{player.HistLabel}</p>
							</If>
						</div>
					</details>
					<If cond={player.HasNews}>
						<details class="stat-tip stat-tip--news">
							<summary class="stat-tip__summary stat-tip__summary--news" aria-label={"News for " + player.Name}>📰</summary>
							<div class="stat-tip__panel">
								<p class="stat-tip__news"><span class="stat-tip__label">NEWS</span> {player.News}</p>
								<If cond={player.HasInjury}>
									<p class="stat-tip__hist-note">{player.Injury}</p>
								</If>
							</div>
						</details>
					</If>
					</span>
					<span class="position-chip">{player.Position}</span>
					<b class="mono">{player.Projection}</b>
					<If cond={props.HasSeat}>
					<form method="post" action={props.Action} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<input type="hidden" name="team_id" value={props.TeamID}></input>
						<input type="hidden" name="player_id" value={player.ID}></input>
						<input type="hidden" name="pos" value={props.Position}></input>
						<input type="hidden" name="q" value={props.Query}></input>
						<input type="hidden" name="page" value={props.Page}></input>
						<If cond={props.CanPick && player.CanDraft}>
							<button class="draft-button" type="submit">Draft</button>
						</If>
						<If cond={props.CanPick && player.CanDraft == false}>
							<span class="control-locked">
								<button class="draft-button" type="button" disabled="disabled" title="Choose a player who keeps every required starter slot fillable">Roster need</button>
								<small class="control-locked__reason">Choose a player who keeps every required starter slot fillable</small>
							</span>
						</If>
						<If cond={props.CanPick == false}>
							<button class="draft-button" type="button" disabled="disabled">Locked</button>
						</If>
					</form>
					</If>
				</article>
			</Each>
		</div>
		<If cond={props.Total == 0}>
			<div class="empty-tape">
				<strong>NO PLAYERS MATCH</strong>
				<p>Try a different position filter or clear your search.</p>
			</div>
		</If>
		<nav class="pool-pagination" aria-label="Draft pool pages">
			<If cond={props.HasPrevious}>
				<a class="filter-button" href={props.PreviousHref} data-gosx-link rel="prev">← Previous</a>
			</If>
			<span class="mono" aria-live="polite">
				<If cond={props.Total > 0}>{props.Start}–{props.End} of {props.Total}</If>
				<If cond={props.Total == 0}>0 players</If>
			</span>
			<If cond={props.HasNext}>
				<a class="filter-button" href={props.NextHref} data-gosx-link rel="next">Next →</a>
			</If>
		</nav>
		</If>
		<If cond={props.DraftComplete}>
			<div class="empty-tape draft-complete-callout">
				<strong>DRAFT CLOSED · ALL PICKS LOCKED</strong>
				<p>Drafted rosters are in the Team terminal. Every remaining player is now available through the Player Pool and waiver rules.</p>
				<div class="hero-actions">
					<a href="/team" data-gosx-link class="button button--primary">Open team terminal →</a>
					<a href="/players" data-gosx-link class="button button--ghost">Open player pool →</a>
				</div>
			</div>
		</If>
	</section>
}

type DraftTeamProps struct {
	TeamID          string
	OnClock        bool
	Tone           string
	HasAvatarImage bool
	AvatarImageURL string
	Name           string
	Abbreviation   string
	Presence       string
	PresenceLabel  string
	PresenceDetail string
	OperatorCount  int
	Manager        string
	Division       string
	Claimed        bool
	Ready          bool
	Autopick       bool
	BoardCount     int
	BoardGap       bool
}

type DraftSeatControlProps struct {
	TeamID          string
	Name            string
	Manager        string
	PresenceLabel   string
	PresenceDetail  string
	OnClock         bool
	Ready           bool
	Autopick        bool
	BoardCount      int
	BoardGap        bool
	Action          string
	ReadyAction     string
	CSRF            string
}

component DraftTeam(props: DraftTeamProps) {
	return <div class="draft-team" data-on-clock={props.OnClock}>
		<span class={"team-mark tone-" + props.Tone}>
			<If cond={props.HasAvatarImage}>
				<img class="avatar-mark__photo" src={props.AvatarImageURL} alt={props.Name} loading="lazy" />
			</If>
			<If cond={props.HasAvatarImage == false}>
				{props.Abbreviation}
			</If>
		</span>
		<div>
			<strong>{props.Name}</strong>
			<small>
				<span class="presence-dot" data-presence={props.Presence}></span>
				{props.Manager}
			</small>
			<small class="mono presence-label">{props.PresenceLabel}</small>
			<small class="mono division-tag">{props.Division}</small>
		</div>
		<If cond={props.Claimed == false}>
			<b class="ready-state">OPEN SEAT</b>
		</If>
		<If cond={props.Claimed}>
			<If cond={props.Ready}>
				<b class="ready-state is-ready">Ready</b>
			</If>
			<If cond={props.Ready == false}>
				<b class="ready-state">Not ready</b>
			</If>
		</If>
		<If cond={props.Autopick}>
			<b class="autopick-badge mono">AUTO</b>
		</If>
		<small class="mono draft-board-summary">BOARD {props.BoardCount} TARGETS</small>
		<If cond={props.BoardGap}>
			<b class="ready-state">BOARD GAP</b>
		</If>
	</div>
}

component DraftSeatControl(props: DraftSeatControlProps) {
	return <article class="draft-seat-control" data-on-clock={props.OnClock}>
		<header>
			<div>
				<span class="mono">{props.Name}</span>
				<strong>{props.Manager}</strong>
			</div>
			<If cond={props.OnClock}><b class="autopick-badge mono">ON CLOCK</b></If>
		</header>
		<p class="mono draft-seat-control__presence">
			<span class="presence-dot" data-presence={props.PresenceLabel}></span>
			{props.PresenceLabel}
			<span> · {props.PresenceDetail}</span>
		</p>
		<div class="draft-seat-control__status">
			<If cond={props.Ready}><span class="mono">READY: YES</span></If>
			<If cond={props.Ready == false}><span class="mono">READY: NO</span></If>
			<If cond={props.Autopick}><span class="autopick-badge mono">AUTO ON</span></If>
			<If cond={props.Autopick == false}><span class="ready-state">MANUAL</span></If>
			<span class="mono">BOARD: {props.BoardCount} TARGETS</span>
			<If cond={props.BoardGap}><span class="ready-state">BOARD GAP</span></If>
		</div>
		<form method="post" action={props.Action} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
			<input type="hidden" name="csrf_token" value={props.CSRF}></input>
			<input type="hidden" name="team_id" value={props.TeamID}></input>
			<If cond={props.Autopick}>
				<input type="hidden" name="on" value="false"></input>
				<button class="button button--compact button--ghost" type="submit">Return manual control</button>
			</If>
			<If cond={props.Autopick == false}>
				<input type="hidden" name="on" value="true"></input>
				<button class="button button--compact autopick-toggle" type="submit">Set AUTO for remaining turns</button>
			</If>
		</form>
		<form method="post" action={props.ReadyAction} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
			<input type="hidden" name="csrf_token" value={props.CSRF}></input>
			<input type="hidden" name="team_id" value={props.TeamID}></input>
			<If cond={props.Ready}>
				<input type="hidden" name="on" value="false"></input>
				<button class="button button--compact button--ghost" type="submit">Clear ready flag</button>
			</If>
			<If cond={props.Ready == false}>
				<input type="hidden" name="on" value="true"></input>
				<button class="button button--compact" type="submit">Mark seat ready</button>
			</If>
		</form>
	</article>
}

type DraftRoomProps struct {
	Data          map[string]any
	CSRF          string
	Actions       map[string]string
	StatusSummary string
}

func DraftRoom(props DraftRoomProps) Node {
	return <div class="draft-live-room">
		<p class="draft-region-stale mono" role="status">The room did not update. This is the last confirmed board. <a href={props.Data.room_path}>Refresh room →</a></p>
		<p class="visually-hidden" role="status" aria-live="polite" aria-atomic="true">{props.StatusSummary}</p>
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="signal-mark" aria-hidden="true"></span>
					DRAFT EVENT //
					{props.Data.draft.date}
				</span>
				<p class="page-kicker">{props.Data.draft.long_date}</p>
				<h1>
					BUILD
					{" "}
					THE FUTURE.
				</h1>
				<p>{props.Data.draft.format}. {props.Data.draft.status_note}</p>
			</div>
			<div class="draft-clock-panel">
				<If cond={props.Data.draft.started == false && props.Data.draft.published}>
					<span>Scheduled window</span>
					<strong
					class="mono"
					data-gosx-countdown={props.Data.draft.at}
					data-gosx-countdown-format="dhms"
					>{props.Data.draft.countdown_label}</strong>
				</If>
				<If cond={props.Data.draft.started == false && props.Data.draft.published == false}>
					<span>Scheduled window</span>
					<strong class="mono">NOT SET</strong>
				</If>
				<If cond={props.Data.draft.started && props.Data.draft.complete == false}>
					<span>Draft status</span>
					<strong class="mono">LIVE</strong>
				</If>
				<If cond={props.Data.draft.complete}>
					<span>Draft status</span>
					<strong class="mono">COMPLETE</strong>
				</If>
				<div class="draft-clock-meta">
					<If cond={props.Data.draft.complete == false}>
						<span>{props.Data.ready_count} / {props.Data.manager_count} ready</span>
						<span>Pick # {props.Data.pick_number}</span>
					</If>
					<If cond={props.Data.draft.complete}>
						<span>{props.Data.round} rounds complete</span>
						<span>{props.Data.pick_number} picks locked</span>
					</If>
				</div>
				<If cond={props.Data.draft.complete == false}>
				<div class="pick-clock-row">
					<span class="mono">ON THE CLOCK //</span>
					<If cond={props.Data.clock.paused}>
						<strong class="pick-clock mono">PAUSED</strong>
					</If>
					<If cond={props.Data.clock.paused == false && props.Data.clock.armed == false}>
						<strong class="pick-clock mono">—:—</strong>
					</If>
					<If cond={props.Data.clock.paused == false && props.Data.clock.armed}>
						<strong
							class="pick-clock mono"
							data-gosx-countdown={props.Data.clock.effective_deadline}
							data-gosx-countdown-format="mm:ss"
							data-gosx-countdown-warn="30s:pick-clock--warn"
							data-gosx-countdown-cue="10s:beep"
						>{props.Data.clock.remaining_label}</strong>
					</If>
					<span class="mono pick-clock-reason">{props.Data.clock.reason}</span>
				</div>
				</If>
				<If cond={props.Data.draft.complete}>
					<div class="pick-clock-row"><span class="mono">DRAFT CLOSED //</span><strong class="pick-clock mono">FINAL</strong><span class="mono pick-clock-reason">ALL PICKS LOCKED</span></div>
				</If>
				<span
					class="visually-hidden"
					data-on-clock={props.Data.draft.started && props.Data.draft.complete == false && props.Data.viewer.team_id == props.Data.on_clock_id}
					data-gosx-watch="data-on-clock=true"
					data-gosx-watch-effect="class:is-on-clock@body,title,cue:chime"
					data-gosx-watch-title="YOUR PICK IS ON THE CLOCK"
				></span>
				<If cond={props.Data.viewer.has_seat && props.Data.draft.complete == false}>
					<div class="manager-draft-controls" aria-label="Your draft controls">
						<div class="manager-draft-control manager-draft-control--checkin" id="ready-toggle" data-ready={props.Data.viewer_ready}>
							<div class="manager-draft-control__copy">
								<If cond={props.Data.viewer_ready}>
									<span class="mono">CHECK-IN COMPLETE</span>
								</If>
								<If cond={props.Data.viewer_ready == false}>
									<span class="mono">CHECK-IN REQUIRED</span>
								</If>
								<If cond={props.Data.viewer_ready}>
									<strong class="draft-checkin-status ready-state is-ready" role="status">READY ✓</strong>
									<small id="ready-checkin-help">You are checked in. Keep this room open so the commissioner can also see your live presence.</small>
								</If>
								<If cond={props.Data.viewer_ready == false}>
									<strong class="draft-checkin-status ready-state" role="status">YOU ARE NOT READY</strong>
									<small id="ready-checkin-help">Make this your first draft-day action. Check in once your Big Board is set and you are ready for the commissioner to begin.</small>
								</If>
							</div>
							<form method="post" action={props.Actions.toggle_ready} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
								<input type="hidden" name="csrf_token" value={props.CSRF}></input>
								<input type="hidden" name="team_id" value={props.Data.viewer.team_id}></input>
								<If cond={props.Data.viewer_ready}>
									<button class="button button--ghost button--compact" type="submit" aria-pressed="true" aria-describedby="ready-checkin-help">Undo ready check-in</button>
								</If>
								<If cond={props.Data.viewer_ready == false}>
									<button class="button button--primary draft-checkin-button" type="submit" aria-pressed="false" aria-describedby="ready-checkin-help">I’m here · Mark me ready</button>
								</If>
							</form>
						</div>
						<div class="manager-draft-control" id="autopick-toggle">
							<div class="manager-draft-control__copy">
								<span class="mono">YOUR AUTOPICK</span>
								<If cond={props.Data.viewer_autopick}>
									<strong class="autopick-badge mono">ON</strong>
									<small>Autopick uses your Big Board, then best available. Enabling it does not reset this turn's grace; if grace has elapsed, the next clock tick may pick.</small>
								</If>
								<If cond={props.Data.viewer_autopick == false}>
									<strong class="ready-state">OFF</strong>
									<small>Manual control keeps the full pick clock. If it expires, auto-select uses your Big Board first, then the best available player.</small>
								</If>
							</div>
						<form method="post" action={props.Actions.toggle_autopick} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
							<input type="hidden" name="csrf_token" value={props.CSRF}></input>
							<input type="hidden" name="team_id" value={props.Data.viewer.team_id}></input>
							<If cond={props.Data.viewer_autopick}>
									<button class="button autopick-toggle is-on button--compact" type="submit">Turn autopick off</button>
								</If>
							<If cond={props.Data.viewer_autopick == false}>
									<button class="button autopick-toggle button--compact" type="submit">Turn autopick on</button>
								</If>
							</form>
						</div>
					</div>
				</If>
				<If cond={props.Data.viewer.has_seat == false && props.Data.draft.complete == false}>
					<div class="manager-draft-controls">
						<div class="manager-draft-control">
							<div class="manager-draft-control__copy">
								<span class="mono">DRAFT CONTROLS</span>
								<If cond={props.Data.public_entry.can_claim}>
								<strong class="ready-state">NO TEAM SEAT</strong>
								<small>{props.Data.public_entry.detail}</small>
								</If>
								<If cond={props.Data.public_entry.can_claim == false}>
								<strong class="ready-state">{props.Data.public_entry.state_label}</strong>
								<small>{props.Data.public_entry.detail}</small>
								</If>
							</div>
							<If cond={props.Data.public_entry.can_claim}>
								<a href={props.Data.public_entry.action_href} data-gosx-link class="button button--primary button--compact">{props.Data.public_entry.action_label}</a>
							</If>
							<If cond={props.Data.public_entry.can_claim == false}>
								<a href={props.Data.public_entry.action_href} data-gosx-link class="button button--ghost button--compact">{props.Data.public_entry.action_label}</a>
								<If cond={props.Data.public_entry.admitted == false}>
									<a href="/pickem" data-gosx-link class="button button--ghost button--compact">Open Pick'em HQ →</a>
								</If>
								<If cond={props.Data.public_entry.admitted && props.Data.public_entry.league_full}>
									<a href="/players" data-gosx-link class="button button--ghost button--compact">Browse player pool →</a>
								</If>
							</If>
						</div>
					</div>
				</If>
				<If cond={props.Data.draft.complete}>
					<div class="manager-draft-controls">
						<div class="manager-draft-control manager-draft-control--checkin">
							<div class="manager-draft-control__copy"><span class="mono">YOUR NEXT ACTION</span><strong class="draft-checkin-status ready-state is-ready">SET YOUR LINEUP</strong><small>Your drafted roster is ready. Review every starter and bench spot before NFL week {data.season_start_week}.</small></div>
							<a href="/team" data-gosx-link class="button button--primary button--compact">Open team terminal →</a>
						</div>
					</div>
				</If>
			</div>
		</section>
		<div class="notice-stack">
			<If cond={props.Data.clock.paused}>
				<p class="demo-message">
					<strong>CLOCK PAUSED:</strong>
					the commissioner paused the pick clock. Picks stay open.
				</p>
			</If>
			<If cond={props.Data.demo_mode && props.Data.draft.complete == false}>
				<p class="demo-message">
					<strong>REHEARSAL MODE:</strong>
					the commissioner must still type START; after that, rehearsal picks control the current team on the clock.
				</p>
			</If>
			<If cond={props.Data.pool_status.has_notice}>
				<p class="demo-message">
					<strong>{props.Data.pool_status.label}:</strong>
					{props.Data.pool_status.detail}
					<If cond={props.Data.pool_status.has_last_success}>
						<br></br>
						<span class="mono">LAST SUCCESS · {props.Data.pool_status.last_success} · {props.Data.pool_status.last_success_relative}</span>
					</If>
				</p>
			</If>
			<If cond={props.Data.has_matchup_source}>
				<p class="demo-message">
					<strong>MATCHUP RANKS:</strong>
					ranked from the {props.Data.matchup_source_label}. A higher "-toughest" number is a softer matchup; a lower one is tougher.
				</p>
			</If>
		</div>
		<If cond={props.Data.draft.started == false}>
			<section class="player-pool draft-checklist">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">BEFORE THE ROOM OPENS</span>
						<h2>Get your seat ready</h2>
					</div>
				</div>
				<div class="checklist">
					<div class="checklist-item">
						<span class="checklist-mark mono">01</span>
						<div class="checklist-item__text">
							<strong>Build your big board</strong>
							<small>Rank your targets now. Autopick and the pool both read your board first.</small>
						</div>
						<a href="/board" data-gosx-link class="board-button">Open board →</a>
					</div>
				<If cond={props.Data.viewer.has_seat}>
					<div class="checklist-item">
						<span class="checklist-mark mono">02</span>
						<div class="checklist-item__text">
							<strong>Check in as ready</strong>
							<small>Mark yourself ready after your Big Board is set. Then keep this tab open so the commissioner can also see that you are HERE.</small>
						</div>
						<a href="#ready-toggle" class="board-button">Check in now ↑</a>
					</div>
					</If>
					<If cond={props.Data.viewer.has_seat == false && props.Data.public_entry.can_claim}>
					<div class="checklist-item">
						<span class="checklist-mark mono">02</span>
						<div class="checklist-item__text">
							<strong>Claim a franchise</strong>
							<small>{props.Data.public_entry.detail}</small>
						</div>
						<a href={props.Data.public_entry.action_href} data-gosx-link class="board-button">{props.Data.public_entry.action_label}</a>
					</div>
					</If>
					<If cond={props.Data.viewer.has_seat == false && props.Data.public_entry.can_claim == false}>
					<div class="checklist-item">
						<span class="checklist-mark mono">02</span>
						<div class="checklist-item__text">
							<strong>{props.Data.public_entry.state_label}</strong>
							<small>{props.Data.public_entry.detail}</small>
						</div>
						<a href={props.Data.public_entry.action_href} data-gosx-link class="board-button">{props.Data.public_entry.action_label}</a>
						<If cond={props.Data.public_entry.admitted == false}>
							<a href="/pickem" data-gosx-link class="board-button">Open Pick'em HQ →</a>
						</If>
						<If cond={props.Data.public_entry.admitted && props.Data.public_entry.league_full}>
							<a href="/players" data-gosx-link class="board-button">Browse player pool →</a>
						</If>
					</div>
					</If>
					<div class="checklist-item">
						<span class="checklist-mark mono">03</span>
						<div class="checklist-item__text">
							<strong>Keep this tab open with sound on</strong>
							<small>One click anywhere on the page arms the on-clock chime for your turn.</small>
						</div>
					</div>
					<If cond={props.Data.viewer.has_seat}>
					<div class="checklist-item">
						<span class="checklist-mark mono">04</span>
						<div class="checklist-item__text">
							<strong>Autopick covers you if you disappear</strong>
							<small>Turn it on before the draft if you might miss your pick.</small>
						</div>
						<a href="#autopick-toggle" class="board-button">Autopick toggle ↑</a>
					</div>
					</If>
				</div>
			</section>
		</If>
		<section class="draft-order-strip">
			<header>
				<If cond={props.Data.draft.complete == false}>
				<span class="section-index">ROUND {props.Data.round} // SNAKE ORDER</span>
				<span class="mono">
					ON CLOCK:
					{props.Data.on_clock.abbreviation}
				</span>
				</If>
				<If cond={props.Data.draft.complete}>
					<span class="section-index">FINAL // DRAFT ORDER</span>
					<span class="mono">{props.Data.pick_number} PICKS LOCKED</span>
				</If>
				<span class="mono">
					ORDER:
					<If cond={props.Data.order_randomized}>
						<b>RANDOMIZED</b>
					</If>
					<If cond={props.Data.order_randomized == false}>
						<b>DEFAULT</b>
					</If>
				</span>
			</header>
			<div class="draft-team-grid">
				<Each of={props.Data.teams} as="team">
					<DraftTeam {...team}></DraftTeam>
				</Each>
			</div>
		</section>
		<If cond={props.Data.viewer.is_commissioner}>
			<If cond={props.Data.draft.complete == false}>
			<section class="draft-order-strip commissioner-clock-controls">
				<header>
					<span class="section-index">COMMISSIONER // CLOCK</span>
					<span class="mono">{props.Data.clock.reason}</span>
				</header>
				<div class="draft-seat-controls" aria-label="Commissioner seat coverage">
					<div class="draft-seat-controls__intro">
						{/* comb — oleander, item 7: plain words for what used
						    to read "Presence is observational. AUTO is
						    authority" — the same three facts (presence never
						    changes the clock on its own; the normal clock
						    holds until two minutes of silence after a
						    restart; AUTO is the one setting that actually
						    changes anything, and it drafts from the seat's
						    own Big Board), said the way a manager who has
						    never read the engine's own internal vocabulary
						    ("observational," "authority," "boot grace")
						    would still understand on a first read. */}
						<strong>Seat presence is informational; autopick runs from the seat's own setting.</strong>
						<p>Seats get two minutes after a restart before they count as unseen for the short backup clock. Turn on AUTO for a seat you know will be away; it then drafts from that seat's own Big Board.</p>
					</div>
					<Each of={props.Data.seat_controls} as="seat">
						<DraftSeatControl {...seat}></DraftSeatControl>
					</Each>
				</div>
				<If cond={props.Data.draft.started == false}>
					<form method="post" action={props.Actions.draft_start} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh" class="clock-controls">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<label class="mono" for="draft-start-confirm">TYPE START //</label>
						<input id="draft-start-confirm" class="scoring-input" name="confirm" autocomplete="off" placeholder="START"></input>
						<button class="button button--primary" type="submit">Start draft + pick clock</button>
					</form>
					<p class="scoring-note">This is intentional and immediate. The scheduled window does not start the draft.</p>
				</If>
				<If cond={props.Data.draft.started}>
				<div class="clock-controls">
					<form method="post" action={props.Actions.clock_pause} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<button class="button button--compact" type="submit">Pause clock</button>
					</form>
					<form method="post" action={props.Actions.clock_resume} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<button class="button button--compact button--primary" type="submit">Resume clock</button>
					</form>
					<form method="post" action={props.Actions.clock_extend} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<input type="hidden" name="current_pick_token" value={props.Data.current_pick_token}></input>
						<input class="scoring-input" type="number" name="seconds" placeholder="30" min="1" max="600"></input>
						<button class="button button--compact" type="submit">Extend pick</button>
					</form>
					<form method="post" action={props.Actions.clock_duration} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<input class="scoring-input" type="number" name="seconds" placeholder="90" min="10" max="600"></input>
						<button class="button button--compact" type="submit">Set duration</button>
					</form>
					<details class="draft-destructive-control">
						<summary class="button button--compact button--ghost">Force current pick now</summary>
						<form method="post" action={props.Actions.clock_autopick} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
							<input type="hidden" name="csrf_token" value={props.CSRF}></input>
							<input type="hidden" name="current_pick_token" value={props.Data.current_pick_token}></input>
							<p>This immediately consumes the on-clock seat's Big Board target, or the best available player when its board is empty. It advances the draft even if the clock is paused.</p>
							<label class="mono" for="draft-force-current-pick-confirm">TYPE FORCE CURRENT PICK //</label>
							<input id="draft-force-current-pick-confirm" class="scoring-input" type="text" name="confirm" value={props.Data.force_current_pick_confirm} autocomplete="off" placeholder="FORCE CURRENT PICK" required="required"></input>
							<button class="button button--compact button--ghost" type="submit">Confirm force current pick</button>
						</form>
					</details>
				</div>
				</If>
			</section>
			</If>
		</If>
	</div>
}

type DraftWorkspaceProps struct {
	Data           map[string]any
	Players        []DraftPlayerCard
	CSRF           string
	MakePickAction string
}

func DraftWorkspace(props DraftWorkspaceProps) Node {
	return <div class="draft-live-workspace">
		<p class="draft-region-stale mono" role="status">LIVE UPDATE FAILED · SHOWING LAST CONFIRMED PLAYER POOL AND PICK TAPE. <a href={props.Data.room_path}>Refresh workspace →</a></p>
		<div class="pool-count-bar">
			<If cond={props.Data.draft_complete == false}>
			<span class="mono pool-count">
				{props.Data.pool_count}
				PLAYERS
			</span>
			</If>
			<If cond={props.Data.draft_complete}>
				<span class="mono pool-count">{props.Data.pick_number} PICKS LOCKED</span>
			</If>
		</div>
		<div class="draft-workspace">
			<DraftQueue
				Players={props.Players}
				Action={props.MakePickAction}
				CSRF={props.CSRF}
				TeamID={props.Data.on_clock_id}
				CanPick={props.Data.can_pick}
				DraftComplete={props.Data.draft_complete}
				HasSeat={props.Data.viewer.has_seat}
				Position={props.Data.pool_position}
				Query={props.Data.pool_query}
				Page={props.Data.pool_page}
				Total={props.Data.pool_total}
				Start={props.Data.pool_page_start}
				End={props.Data.pool_page_end}
				HasPrevious={props.Data.pool_has_previous}
				HasNext={props.Data.pool_has_next}
				PreviousHref={props.Data.pool_previous_href}
				NextHref={props.Data.pool_next_href}
				AllHref={props.Data.pool_all_href}
				RBHref={props.Data.pool_rb_href}
				WRHref={props.Data.pool_wr_href}
				QBHref={props.Data.pool_qb_href}
				TEHref={props.Data.pool_te_href}
				KHref={props.Data.pool_k_href}
				DSTHref={props.Data.pool_dst_href}
				PHref={props.Data.pool_p_href}
			 />
			<aside class="pick-tape">
				<If cond={props.Data.board_count > 0}>
					<header>
						<span class="section-index">YOUR BIG BOARD</span>
						<a href="/board" data-gosx-link class="mono">EDIT →</a>
					</header>
					<div class="pick-list board-peek">
						<Each of={props.Data.board} as="target">
							<div class="pick-row">
								<span class="mono">{target.rank}</span>
								<div>
									<strong>{target.name}</strong>
									<small>
										{target.position}
										·
										{target.nfl_team}
									</small>
								</div>
								<b>{target.projection}</b>
							</div>
						</Each>
					</div>
				</If>
				<If cond={props.Data.board_count == 0 && props.Data.draft_complete == false}>
					<div class="board-peek-empty">
						<a href="/board" data-gosx-link class="mono">BUILD YOUR BIG BOARD →</a>
					</div>
				</If>
				<If cond={props.Data.draft_complete}>
					<div class="board-peek-empty"><a href="/players" data-gosx-link class="mono">SCOUT FREE AGENTS →</a></div>
				</If>
				<header>
					<span class="section-index">PICK TAPE</span>
					<If cond={props.Data.draft.started == false}><b class="mono">DRAFT LOG</b></If>
					<If cond={props.Data.draft.started && props.Data.draft.complete == false}>
						<b class="mono">LIVE LOG</b>
					</If>
					<If cond={props.Data.draft_complete}><b class="mono">FINAL LEDGER</b></If>
				</header>
				<If cond={props.Data.picks_empty}>
					<div class="empty-tape">
						<strong>NO PICKS YET</strong>
						<p>
							The tape starts moving when the first selection is locked.
						</p>
					</div>
				</If>
				<div class="pick-list">
					<Each of={props.Data.picks} as="pick">
						<div class="pick-row">
							<span class="mono">{pick.number}</span>
							<div>
								<strong>{pick.player.name}</strong>
								<small>
									{pick.player.position}
									·
									{pick.player.nfl_team}
								</small>
							</div>
							<div class="pick-tape-meta">
								<If cond={pick.is_auto}>
									<b class="pick-tag pick-tag--auto mono">AUTO</b>
								</If>
								<If cond={pick.is_commissioner}>
									<b class="pick-tag pick-tag--comm mono">COMM</b>
								</If>
								<b class="mono">{pick.team.abbreviation}</b>
							</div>
						</div>
					</Each>
				</div>
			</aside>
		</div>
	</div>
}

// --- The app shell (D2, D5): the always-visible command bar plus three
// independently-refreshing panes (history/tape, available players, my
// team). Task 5a's contract; DraftRoom and DraftWorkspace above stay for
// the legacy /draft/fragment/room|workspace routes — Task 8 (target mode,
// 2026-08-30) kept both mounted rather than retiring them: they are
// inert, unused by Page() (below no longer renders either), and
// TestDraftRegionContractIsPushDrivenAndMounted (fragment_test.go) pins
// them present by name as a documented, deliberately deferred cleanup.

type DraftCommandBarProps struct {
	Data          map[string]any
	CSRF          string
	Actions       map[string]string
	StatusSummary string
}

// BestAvailableCard is one best-available-at-this-pick entry inside a
// pick-detail accordion: identity only, the fields the markup renders.
type BestAvailableCard struct {
	Name     string
	Position string
	NFLTeam  string
}

// TapePick is one pick's full tape row, mirroring league.TapePick
// field-for-field (page.server.go's tapePickProps converts one into the
// other). The pick-detail fields (Projection, Source, BestAvailable,
// TeamPicks) are populated only for a tape row's own inline accordion —
// TeamColumn.Picks and DraftHistoryProps.Picks (the ledger/CSV) carry the
// same struct with those left at their zero value, since neither ever
// renders a <DraftPickDetail>.
type TapePick struct {
	Number, Round, Slot, Column                   int
	Label                                          string
	TeamID, TeamName, TeamAbbr, TeamTone, Manager  string
	HasAvatarImage                                 bool
	AvatarImageURL                                 string
	PlayerID, PlayerName, Position, NFLTeam        string
	MadeBy                                         string
	IsAuto, IsCommissioner, Mine                   bool
	TimeToPickSec                                  int
	TimeToPick                                     string
	HasValue                                       bool
	Value                                          int
	ValueLabel                                     string
	MadeAt                                         string
	Projection                                     string
	Source                                         string
	BestAvailable                                  []BestAvailableCard
	TeamPicks                                      []TapePick
	// Open/Href: item 1 (2026-08-30 review). Href is this row's own
	// data-gosx-link soft-navigation target: "/draft?view=tape&pick=N&
	// pos=...&q=...&page=..." to open the row, or the same URL with "pick"
	// dropped to close it (page.server.go builds whichever applies).
	// Open, when true, is the signal DraftPickDetail renders its own
	// inline DraftPickDetailBody for THIS row and marks it aria-current.
	Open bool
	Href string
}

// TapeRound groups one round's made picks, newest pick first.
type TapeRound struct {
	Round, First, Last int
	Direction          string
	Current            bool
	Made, Total        int
	Picks              []TapePick
	// ShowHeader/MadeBindKey/CurrentBindAttr: see draftTapeRoundView's own
	// doc comment (page.server.go).
	ShowHeader      bool
	MadeBindKey     string
	CurrentBindAttr string
}

// BoardCell is one round x column slot of the pick board.
type BoardCell struct {
	Round, Column, Number  int
	Label                  string
	Filled, Mine, OnClock  bool
	PlayerName, Position   string
	NFLTeam                string
	IsAuto, IsCommissioner bool
	// CellBindKey/PosBindAttr: see draftBoardCellView's own doc comment
	// (page.server.go).
	CellBindKey string
	PosBindAttr string
}

// BoardRow is one round's full column strip, one BoardCell per team.
type BoardRow struct {
	Round     int
	Direction string
	Cells     []BoardCell
}

// BoardView is the whole pick board.
type BoardView struct {
	Columns []map[string]any
	Rows    []BoardRow
	// ColumnCount is len(Columns), computed in Go and pre-rendered as a
	// string: never call len() in a .gsx expression (a follow-up probe
	// against gosx@v0.53.9, 2026-08-30 review, proved it returns empty
	// through route.RenderProgramComponent in every shape tried, direct
	// or nested, slice or string — not a narrower case), so DraftBoard's
	// own style={"--board-columns:" + ...} needs this already-formatted
	// field rather than a len(props.Columns) call inline.
	ColumnCount string
	// HasMine/MineID: wave 7 item 1's "jump to my picks" affordance.
	// HasMine is true only when the viewer holds a seat (one of Columns
	// carries "mine" == true); MineID is that column's own team id, the
	// same value DraftBoard's own "board-team-<id>" anchor id (below)
	// already carries on every column unconditionally. Both computed in
	// Go (boardViewProps, page.server.go) rather than scanning
	// props.Columns from inside the template, matching this file's own
	// rule against relying on GSX template-side map iteration/comparison
	// for anything that gates a whole element's presence.
	HasMine bool
	MineID  string
}

// TeamColumn is one team's full pick history plus its roster-needs tally.
// DraftBoardTeam is the compact team-identity shape the by-team column
// header and the board's column header both need: a real declared struct,
// not map[string]any, because a typed component's field access (unlike
// the page-level "data"/"props.Data" escape hatch) must resolve through a
// declared struct (gosx check: "declare the renderer-visible struct
// beside the component").
type DraftBoardTeam struct {
	ID             string
	Name           string
	Abbreviation   string
	Tone           string
	Manager        string
	HasAvatarImage bool
	AvatarImageURL string
	Mine           bool
}

type TeamColumn struct {
	Team  DraftBoardTeam
	Picks []TapePick
	Needs []map[string]any
	// PicksEmpty (D12, spruce audit): len(Picks) == 0, computed in Go
	// (page.server.go's teamColumnsProps) — never len()/nil-compared here,
	// matching this file's own rule (BoardView.ColumnCount's doc comment).
	PicksEmpty bool
}

// DraftHistoryProps backs the pick-tape pane's Tape/Board/Teams tabs:
// Rounds (newest round first, D4), Board (round-ascending grid), Teams
// (by-team columns), Complete (the final-ledger CSV export), and Latest
// (the most recent pick number, the "↓ Latest" jump target). Since is the
// tape's own "?since=" cursor (fragment.go): -1 means "unset", the pane's
// full render; DraftTapeRows reads it directly, DraftHistory ignores it.
type DraftHistoryProps struct {
	Rounds   []TapeRound
	Board    BoardView
	Teams    []TeamColumn
	Complete bool
	Latest   int
	Since    int
	// ShowTape/ShowBoard/ShowTeams select the ONE sub-view DraftHistory
	// actually renders (item 1a, 2026-08-30 review): the server picks
	// exactly one, from "?view=" (fragment.go's attachDraftFragmentView),
	// rather than rendering all three and hiding two with CSS — the tape
	// sub-view alone used to carry far more bytes than the D3 refresh-
	// budget's 4 KB ceiling allows once every poll rendered all three.
	// Computed in Go, not compared here as a bare View string, matching
	// this file's own rule against relying on GSX template-side
	// comparisons for anything that gates a whole subtree's presence.
	ShowTape  bool
	ShowBoard bool
	ShowTeams bool
	// The on-the-clock synthetic row DraftTapeRows leads its newest round
	// with (Task 7 Step 4). HasOnClock is false once the draft is complete
	// and on every "?since=" incremental fragment (the row belongs on a
	// full pane render only, never repeated on each poll).
	HasOnClock   bool
	NextLabel    string
	OnClockName  string
	OnClockAbbr  string
	OnClockTone  string
	// OnClockHasAvatarImage/OnClockAvatarImageURL let the on-the-clock
	// synthetic row share the same avatar-or-abbreviation badge partial
	// every made row already uses (P10, 2026-08-30 review), instead of
	// always falling back to the plain abbreviation.
	OnClockHasAvatarImage bool
	OnClockAvatarImageURL string
	// RoundsEmpty is len(Rounds) == 0, computed in Go (page.server.go) and
	// passed down as a plain bool rather than evaluated as len(props.Rounds)
	// inside DraftTapeRows: a follow-up probe against gosx@v0.53.9
	// (2026-08-30 review) proved len() returns empty through
	// route.RenderProgramComponent in EVERY shape tried — direct or
	// nested, a slice or a string — not only the rebound-slice-prop case
	// this fix was first written against. The rule is broader than the
	// original comment stated: never call len() in a .gsx expression;
	// compute the length in Go and pass it down, the way this field
	// already does. False on every "?since=" incremental poll
	// (attachDraftFragmentSince, fragment.go), matching HasOnClock: the
	// empty-tape message belongs on a full pane render only.
	RoundsEmpty bool
	// HasOlderRounds/OlderHref: item 3 (2026-08-30 review), the "Older
	// rounds ↓" link at the tape's foot. False/empty on every "?since="
	// incremental poll, matching HasOnClock/RoundsEmpty above.
	HasOlderRounds bool
	OlderHref      string
	// TapeURL/TargetMode: see draftHistoryView's own doc comment
	// (page.server.go).
	TapeURL    string
	TargetMode bool
	// RoomPath/LiveSrc/LiveHub: the room this pane belongs to (practice
	// draft, internal/league/practice.go) — "/draft", "/draft/live.json",
	// and "draft-live" for the real room, the practice room's own for a
	// practice, so the pane's stale link and live root never point at the
	// other room.
	RoomPath string
	LiveSrc  string
	LiveHub  string
}

type DraftAvailableProps struct {
	Data           map[string]any
	Players        []DraftPlayerCard
	CSRF           string
	MakePickAction string
	QueueAddAction string
	Actions        map[string]string
}

type DraftMyTeamProps struct {
	Data              map[string]any
	Queue             []DraftPlayerCard
	CSRF              string
	QueueRemoveAction string
	// QueueMoveAction is the no-JS up/down reorder forms' post target
	// (mirrors board.BoardRowProps.MoveAction): the same BoardMove the
	// drag-reorder POST already calls through, routed here as
	// "queue-move" (page.server.go) so a session with JavaScript off can
	// still reorder its queue one step at a time.
	QueueMoveAction string
	Actions         map[string]string
}

type DraftAvailableHeadProps struct {
	SearchPlaceholder string
	// RoomPath is the GET search form's action: "/draft" for the real
	// room, the practice room's own path for a practice (data.room_path,
	// service.go's draftData), so a no-JS search never leaves the room the
	// visitor is in.
	RoomPath string
	// Query/Position/Sort (comb — oleander, item 1): the current pool
	// state, so the no-JS search form below can resubmit it as a real GET
	// (value on the input, hidden fields for pos/sort) and a reload never
	// silently drops the position filter or sort a visitor already had
	// active — the same q/pos-preservation rule draftPositionChips and
	// draftSortHref (page.server.go) already hold for their own hrefs.
	Query    string
	Position string
	Sort     string
	// Positions/SortOptions: D7/D9 (spruce audit). Built server-side
	// (page.server.go's draftPositionChips/draftSortOptions) as plain
	// label/value/href/active maps — the same []map[string]any shape
	// BoardView.Columns already uses (page.gsx) for a simple list of
	// key/value entries with no nested struct of their own — from the
	// SAME pool_position/pool_sort the pane's own rows were paged and
	// ordered by, so the chip row and the sort toggle can never disagree
	// with what actually rendered below them.
	Positions   []map[string]any
	SortOptions []map[string]any
}

type DraftHistoryHeadProps struct {
	Started   bool
	Complete  bool
	ShowTape  bool
	ShowBoard bool
	ShowTeams bool
	// TapeHref/BoardHref/TeamsHref (item 6, 2026-08-30 review): the
	// segment's own three navigation targets, built server-side
	// (page.server.go's draftHistoryHref) carrying the viewer's current
	// pool q/pos/page so switching Tape/Board/Teams never resets a
	// filtered/paged pool search.
	TapeHref  string
	BoardHref string
	TeamsHref string
}

// DraftCommandBar is the shell's one always-visible surface: the pick
// count, the on-clock team and its live clock, the room summary, the sound
// and commissioner controls, the banner, and (while seated) the manager's
// own ready/autopick controls.
// Task 8 (target mode): every dynamic figure below sits inside its own
// span carrying data-gosx-live-bind (text) or data-gosx-live-bind-attr
// (an attribute — never data-gosx-live-bind-class here: v0.53.10's
// -bind-class toggles ONE fixed class token from a boolean, which fits a
// yes/no state, not a pick among several team tones, so the on-clock
// mark's tone rides a mirrored data-tone attribute instead of the
// tone-<name> class a fresh SSR/soft-nav render still uses). The header
// element (Page()) is this whole subtree's one data-gosx-live-mode="event"
// root; every bind key below matches internal/league/draft_events.go's own
// payload keys field for field. "Next: X · then Y" and the "You are on the
// clock" ⁄ "On the clock" idx text stay server-render-only: neither
// draft:pick/undo/clock/state payload carries next/after-next team names
// or a pre-worded on-clock phrase, so both go stale until the next full
// navigation — an accepted Task 8 scope cut (reported).
//
// Wave 7b item 1 (phone-width pill): the audit measured the pre-fix
// command bar at 250px of a 390px-tall viewport (44% of the pane) and, in
// landscape (844x390), at 260px of 390 (67%) — zero pick rows visible.
// The fix keeps every element above completely unmodified (so
// data-pick-clock/data-pick-label stay the single elements
// sim_browser_test.go's browserPickClockSelector and readDraftPickLabel
// require — a duplicate of either would break
// TestBrowserRoomMeetsRefreshBudgetAndKeepsClockIdentity's identity check
// and signInAsManagerAtViewport's WaitVisible at every phone-width browser
// scenario, since a SECOND [data-pick-clock] match, even hidden, would
// still count) and adds, never replaces:
//   - .draft-command__pill-meta / .draft-command__pill-status: new spans
//     duplicating pick.round/pick.number/onclock.name's own live-bind
//     keys (proven safe to bind twice — room.managers already does, in
//     .draft-command__room below) into one compact "R3·P34"/"YOU’RE UP"
//     reading, phone-width only; the CSS hides the two-line originals at
//     that width instead of restyling them, since neither wraps its
//     "SNAKE dir" or "PICK" text in an isolable span to compact in place.
//   - .draft-command__pill-toggle: a phone-only <details> (display: none
//     at desktop, unconditional, matching DraftMobileTabs/DraftPickBar's
//     own established "duplicate the mobile-only surface, hide at
//     desktop" pattern in this same file) whose <summary> is the pill's
//     own ▾ toggle and whose body is the CSS-fixed bottom sheet: ready
//     state, a second sound toggle, autopick, the League-navigation
//     trigger DraftMobileTabs used to carry, and (commissioner only) a
//     second trigger for the SAME #draft-commissioner dialog the desktop
//     Commissioner button already opens — no duplicate drawer markup
//     needed. .draft-command__room (with its own live sound/commissioner
//     controls) stays exactly as it renders today; only its VISIBILITY at
//     phone width changes (display: none there, unchanged everywhere
//     else), the sheet taking its place instead of hiding its content
//     outright. Item 2 (wave-7 re-audit — yew): the <summary> itself
//     carries a visible "MENU" label beside the ▾ glyph, not the glyph
//     alone — a bare glyph is the whole hit target's own content, so it
//     rendered only as wide as one character (8.8px measured pre-fix)
//     even with its own min-height already holding at 2.75rem (the
//     generic mobile touch-floor rule for .site-frame summary zeroes
//     min-width by design; public/styles.css re-asserts it for this one
//     selector the same way it already does for the pool-pagination and
//     lineup-week-form controls). The visible label also becomes the
//     accessible name directly (no separate aria-label needed), so
//     WCAG 2.5.3 Label in Name holds trivially — the spoken name and the
//     printed label are the identical string.
//   - .draft-command__pill-row: a new wrapper around .draft-command__pick,
//     __turn, __clock, and __pill-toggle only (never __room, which stays
//     a direct sibling in its original desktop grid slot). display:
//     contents at desktop (public/styles.css) unboxes it entirely, so the
//     four children resume being direct .draft-command__inner grid items
//     — pixel-identical to the pre-wrap desktop layout, auto-placed
//     pick/turn/clock/room across the same four explicit columns, since a
//     display: none item (the toggle, at desktop) consumes no grid cell
//     either way. At phone width the wrapper becomes a real flex row
//     (flex-wrap: nowrap, each child shrinking-with-ellipsis instead of
//     wrapping onto a second line) so it can sit beside
//     .draft-command__banner as ONE flex item of .draft-command__inner's
//     own flex-wrap: wrap — the banner (rare: paused/rehearsal/offline-
//     pool notices) gets its own full-width line only when actually
//     present, without ever splitting the pill row itself across two
//     lines the way giving .draft-command__inner's own flex-wrap: wrap to
//     five ungrouped children did in an earlier draft of this fix
//     (measured live: pick/turn/clock/toggle spread across 3 separate
//     flex lines instead of shrinking to fit one).
func DraftCommandBar(props DraftCommandBarProps) Node {
	return <div class="draft-command__inner">
		<p class="draft-region-stale mono" role="status">The room did not update. This is the last confirmed state. <a href={props.Data.room_path}>Refresh room →</a></p>
		<p class="visually-hidden" role="status" aria-live="polite" aria-atomic="true">{props.StatusSummary}</p>
		<div class="draft-command__pill-row">
		<div class="draft-command__pick">
			<span class="idx">ROUND <span data-gosx-live-bind="pick.round">{props.Data.round}</span> · <abbr title="a draft order that reverses every round">SNAKE</abbr> <span data-gosx-live-bind="pick.direction">{props.Data.snake_direction}</span></span>
			<span class="mono draft-command__number" data-pick-label>PICK <span data-gosx-live-bind="pick.number">{props.Data.pick_number}</span> <span class="muted">/ <span data-gosx-live-bind="pick.total">{props.Data.picks_total}</span></span></span>
			<span class="draft-command__pill-meta mono">R<span data-gosx-live-bind="pick.round">{props.Data.round}</span>·P<span data-gosx-live-bind="pick.number">{props.Data.pick_number}</span></span>
		</div>
		<div class="draft-command__turn">
			<If cond={props.Data.draft.started && props.Data.draft.complete == false}>
				<span class={"team-mark draft-command__mark tone-" + props.Data.on_clock.tone} data-tone={props.Data.on_clock.tone} data-gosx-live-bind-attr="data-tone:onclock.tone">
					<If cond={props.Data.on_clock.has_avatar_image}>
						<img class="avatar-mark__photo" src={props.Data.on_clock.avatar_image_url} alt={props.Data.on_clock.name} loading="lazy" />
					</If>
					<If cond={props.Data.on_clock.has_avatar_image == false}>
						<span data-gosx-live-bind="onclock.abbreviation">{props.Data.on_clock.abbreviation}</span>
					</If>
				</span>
			</If>
			<div class="draft-command__team">
				<If cond={props.Data.draft.started == false}>
					<span class="idx">Before the room opens</span>
					<strong class="display">Draft not started</strong>
					<small class="muted"><span data-gosx-live-bind="room.ready">{props.Data.ready_count}</span> of <span data-gosx-live-bind="room.managers">{props.Data.manager_count}</span> ready</small>
					<If cond={props.Data.viewer.is_commissioner}>
						<button type="button" class="btn btn-sm btn-primary draft-command__start" data-gosx-disclosure-target="#draft-commissioner" aria-controls="draft-commissioner" aria-expanded="false">Start the draft →</button>
					</If>
				</If>
				<If cond={props.Data.draft.started && props.Data.draft.complete}>
					<span class="idx">Draft complete</span>
					<strong class="display">Every pick is locked</strong>
				</If>
				<If cond={props.Data.draft.started && props.Data.draft.complete == false}>
					<If cond={props.Data.viewer_on_clock}><span class="idx idx--hot">You are on the clock</span></If>
					<If cond={props.Data.viewer_on_clock == false}><span class="idx">On the clock</span></If>
					<strong class="display" data-gosx-live-bind="onclock.name">{props.Data.on_clock.name}</strong>
					<small class="muted">Next: {props.Data.next_team.name} · then {props.Data.after_next_team.name}</small>
					{/* comb — larch (2026-09-04), J2 F34 (opportunity): the
					    room's own header carried only an AGGREGATE count
					    ("N/8 here"), never the one seat that matters most
					    the second a pick arms — whether the seat NOW on
					    the clock has opened the room at all. presence
					    already tracks this per seat (draftTeamMaps,
					    service.go) for the commissioner drawer's own seat
					    cards; on_clock_not_in_room (page.server.go) is
					    that same seat's own bucket, read once per
					    request, off data this request already built. */}
					<If cond={props.Data.on_clock_not_in_room}>
						<small class="draft-command__not-in-room">Not in the room</small>
					</If>
				</If>
				<span class="draft-command__pill-status mono">
					<If cond={props.Data.draft.started == false}>DRAFT NOT STARTED · <span data-gosx-live-bind="room.ready">{props.Data.ready_count}</span>/<span data-gosx-live-bind="room.managers">{props.Data.manager_count}</span> READY</If>
					<If cond={props.Data.draft.started && props.Data.draft.complete}>DRAFT COMPLETE</If>
					<If cond={props.Data.draft.started && props.Data.draft.complete == false && props.Data.viewer_on_clock}>YOU’RE UP</If>
					{/* comb — larch (2026-09-04), J2 F14: "ON CLOCK: " ate 10
					    characters of this line's own one-line ellipsis
					    budget before the team's name even started, so a
					    long name (or this pill's own narrowest phone width)
					    clipped to a single letter — "P…" for a whole team.
					    The pill-status line's OWN idx sibling above already
					    reads "On the clock"/"You are on the clock" for
					    sighted users with more room; dropping the inline
					    prefix here hands its width back to the name. */}
					<If cond={props.Data.draft.started && props.Data.draft.complete == false && props.Data.viewer_on_clock == false}><span data-gosx-live-bind="onclock.name">{props.Data.on_clock.name}</span></If>
				</span>
			</div>
		</div>
		<div class="draft-command__clock" data-clock-state={props.Data.clock.state} data-gosx-live-bind-attr="data-clock-state:clock.state">
			<If cond={props.Data.draft.started == false && props.Data.draft.published}>
				<strong class="pick-clock mono" data-pick-clock data-gosx-countdown={props.Data.draft.at} data-gosx-countdown-format="dhms" aria-live="off">{props.Data.draft.countdown_label}</strong>
				<span class="idx">Scheduled window</span>
			</If>
			<If cond={props.Data.draft.started == false && props.Data.draft.published == false}>
				<strong class="pick-clock mono">NOT SET</strong>
				<span class="idx">Scheduled window</span>
			</If>
			<If cond={props.Data.draft.started}>
				<If cond={props.Data.draft.complete == false && props.Data.clock.state == "RUNNING"}>
					<strong class="pick-clock mono" data-pick-clock data-gosx-countdown={props.Data.clock.effective_deadline} data-gosx-countdown-format="mm:ss" data-gosx-countdown-warn="30s:pick-clock--warn" data-gosx-countdown-cue="10s:beep" data-gosx-live-bind-attr="data-gosx-countdown:clock.effective_deadline" aria-live="off">{props.Data.clock.remaining_label}</strong>
				</If>
				<If cond={props.Data.draft.complete == false && props.Data.clock.state != "RUNNING"}>
					<strong class="pick-clock mono" data-pick-clock data-gosx-countdown-format="mm:ss" aria-live="off">{props.Data.clock.state}</strong>
				</If>
				<If cond={props.Data.draft.complete}>
					<strong class="pick-clock mono" data-pick-clock data-gosx-countdown-format="mm:ss" aria-live="off">FINAL</strong>
				</If>
				{/* comb — larch (2026-09-04), J1 F32: this "of <duration>"
				    suffix used to render unconditionally, so a paused
				    clock read "PAUSED OF 2:00" — a sentence that stops
				    parsing in the one state a manager most wants the
				    remaining time named clearly. RUNNING keeps "of
				    <duration>" beside its own live countdown ("1:12 of
				    2:00" still reads as a fraction); every other
				    unfinished state names the same figure as time left
				    instead. */}
				<If cond={props.Data.draft.complete == false && props.Data.clock.state == "RUNNING"}>
					<span class="idx">of <span data-gosx-live-bind="clock.duration_label">{props.Data.clock.duration_label}</span></span>
				</If>
				<If cond={props.Data.draft.complete == false && props.Data.clock.state != "RUNNING"}>
					<span class="idx">· <span data-gosx-live-bind="clock.duration_label">{props.Data.clock.duration_label}</span> left</span>
				</If>
			</If>
		</div>
		<details class="draft-command__pill-toggle">
			{/* comb — larch (2026-09-04), J1 F14: the open-state rotate
			    (public/styles.css, "[open] .draft-command__pill-caret")
			    targeted the WHOLE summary, so the "MENU" label rotated
			    with the ▾ glyph — mirrored and upside-down once open, on
			    the one control that leads out of the room. A class on
			    the glyph alone gives this comb's own override something
			    narrower to rotate. */}
			<summary class="draft-command__pill-caret" aria-controls="draft-command-sheet">
				<span class="draft-command__pill-caret-label mono">MENU</span>
				<span class="draft-command__pill-caret-glyph" aria-hidden="true">▾</span>
			</summary>
			<div class="draft-command__sheet" id="draft-command-sheet">
				<div class="draft-command__sheet-room mono">
					<span data-gosx-live-bind="room.here">{props.Data.here_count}</span>/<span data-gosx-live-bind="room.managers">{props.Data.manager_count}</span> here · <span data-gosx-live-bind="room.ready">{props.Data.ready_count}</span>/<span data-gosx-live-bind="room.managers">{props.Data.manager_count}</span> ready
					<If cond={props.Data.your_pick_in > 0}>
						<span class="draft-command__yourpick" data-gosx-live-bind={props.Data.yourpick_bind_key}> · your pick in {props.Data.your_pick_in}</span>
					</If>
				</div>
				<div class="draft-command__sheet-controls">
					<If cond={props.Data.viewer.has_seat && props.Data.draft.complete == false}>
						<If cond={props.Data.practice.active == false}>
						<form method="post" action={props.Actions.toggle_ready} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={props.CSRF}></input>
							<input type="hidden" name="team_id" value={props.Data.viewer.team_id}></input>
							<If cond={props.Data.viewer_ready}>
								<button class="btn btn-sm" type="submit" aria-pressed="true">Undo ready check-in</button>
							</If>
							<If cond={props.Data.viewer_ready == false}>
								<button class="btn btn-sm btn-primary" type="submit" aria-pressed="false">Mark me ready</button>
							</If>
						</form>
						</If>
						<form method="post" action={props.Actions.toggle_autopick} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={props.CSRF}></input>
							<input type="hidden" name="team_id" value={props.Data.viewer.team_id}></input>
							<If cond={props.Data.viewer_autopick}>
								<input type="hidden" name="on" value="false"></input>
								<button class="btn btn-sm" type="submit">Turn autopick off</button>
							</If>
							<If cond={props.Data.viewer_autopick == false}>
								<input type="hidden" name="on" value="true"></input>
								<button class="btn btn-sm" type="submit">Turn autopick on</button>
							</If>
						</form>
					</If>
					<button type="button" class="btn btn-sm draft-command__pill-sound" data-gosx-cue-toggle data-gosx-cue-label-on="Sound on" data-gosx-cue-label-off="Sound off" aria-pressed="true">Sound on</button>
					<If cond={props.Data.practice.active == false}>
						<button type="button" class="btn btn-sm" aria-label="Open league navigation" aria-controls="primary-navigation-dialog" aria-expanded="false" data-gosx-disclosure-target="#primary-navigation-dialog">League</button>
					</If>
					<If cond={props.Data.practice.active}>
						<form method="post" action={props.Actions.practice_leave} data-gosx-managed="false">
							<input type="hidden" name="csrf_token" value={props.CSRF}></input>
							<button class="btn btn-sm" type="submit">Leave practice</button>
						</form>
					</If>
					<If cond={props.Data.viewer.is_commissioner && props.Data.draft.started == false}>
						<button type="button" class="btn btn-sm btn-primary" data-gosx-disclosure-target="#draft-commissioner" aria-controls="draft-commissioner" aria-expanded="false">Start the draft →</button>
					</If>
					<If cond={props.Data.viewer.is_commissioner}>
						<button type="button" class="btn btn-sm" data-gosx-disclosure-target="#draft-commissioner" aria-controls="draft-commissioner" aria-expanded="false">Commissioner</button>
					</If>
				</div>
			</div>
		</details>
		</div>
		<div class="draft-command__room">
			<span class="idx">Room</span>
			<span class="mono"><span class="live-dot live-dot--bound" aria-hidden="true"><If cond={props.Data.draft.started && props.Data.draft.complete == false}>LIVE</If></span> <span data-gosx-live-bind="room.here">{props.Data.here_count}</span>/<span data-gosx-live-bind="room.managers">{props.Data.manager_count}</span> here · <span data-gosx-live-bind="room.ready">{props.Data.ready_count}</span>/<span data-gosx-live-bind="room.managers">{props.Data.manager_count}</span> ready<span class="draft-command__auto"> · <span data-gosx-live-bind="room.auto">{props.Data.auto_count}</span> auto</span></span>
			<If cond={props.Data.your_pick_in > 0}>
				<span class="mono draft-command__yourpick" data-gosx-live-bind={props.Data.yourpick_bind_key}>your pick in {props.Data.your_pick_in}</span>
			</If>
			<button type="button" class="btn btn-sm draft-command__sound" data-gosx-cue-toggle data-gosx-cue-label-on="Sound on" data-gosx-cue-label-off="Sound off" aria-pressed="true">Sound on</button>
			<If cond={props.Data.viewer.is_commissioner}>
				<button type="button" class="btn btn-sm" data-gosx-disclosure-target="#draft-commissioner" aria-controls="draft-commissioner" aria-expanded="false">Commissioner</button>
			</If>
			<button type="button" class="btn btn-sm btn-ghost draft-command__rail" data-gosx-toggle-target="#main-content" data-gosx-toggle-attribute="data-rail-open" aria-expanded="false">Rail</button>
		</div>
		{/* D16 (spruce audit): a stale/cached pool snapshot gets the SAME
		    labelled .demo-message shape /board's own pool-status banner
		    uses (app/board/page.gsx) — a "CACHED SNAPSHOT:" label plus
		    the detail sentence, and (when known) a "LAST SUCCESS ·
		    <time>" line — instead of the old unlabelled, truncation-
		    prone single mono line. props.Data.pool_status is the exact
		    same map service.go's poolFreshnessMap already builds for
		    /board; nothing new to wire. The plain banner (clock-paused/
		    rehearsal-mode copy, service.go's own banner switch) still
		    falls back to the old one-line shape for every OTHER reason a
		    banner shows, since those carry no label/last-success pair of
		    their own.

		    comb — oleander, item 4: the phone-width query below used to
		    clamp this whole block to 2.6em (34px measured live) — not
		    enough room for the label, the full detail sentence, AND the
		    "LAST SUCCESS ·" line after it, so a 390px visitor saw the
		    detail sentence cut off mid-word ("...while") with no LAST
		    SUCCESS line at all. .draft-command__banner-row keeps the
		    ellipsized summary line and the "Details" toggle on ONE row
		    (matching the old rule's own single-line footprint almost
		    exactly) when closed; everything else — the same detail
		    sentence in full, plus LAST SUCCESS — sits behind that toggle,
		    which a manager can open with no data lost. The outer element
		    is a <div>, not a <p>, since <details> is not phrasing content
		    a <p> can legally contain. */}
		<If cond={props.Data.pool_status.has_notice}>
			<div class="demo-message draft-command__banner">
				<div class="draft-command__banner-row">
					<p class="draft-command__banner-line">
						<strong>{props.Data.pool_status.label}:</strong>
						{props.Data.pool_status.detail}
					</p>
					<details class="draft-command__banner-details">
						<summary>Details</summary>
						<p>{props.Data.pool_status.detail}</p>
						<If cond={props.Data.pool_status.has_last_success}>
							<span class="mono">LAST SUCCESS · {props.Data.pool_status.last_success} · {props.Data.pool_status.last_success_relative}</span>
						</If>
					</details>
				</div>
			</div>
		</If>
		<If cond={props.Data.pool_status.has_notice == false && props.Data.banner != ""}>
			<p class="draft-command__banner mono" title={props.Data.banner}>{props.Data.banner}</p>
		</If>
		<span
			class="visually-hidden"
			data-on-clock={props.Data.draft.started && props.Data.draft.complete == false && props.Data.viewer_on_clock}
			data-gosx-watch="data-on-clock=true"
			data-gosx-watch-effect="class:is-on-clock@body,title,cue:chime"
			data-gosx-watch-title="YOUR PICK IS ON THE CLOCK"
		></span>
	</div>
}

// DraftPracticeStrip is the practice room's own status strip (practice
// draft, internal/league/practice.go): plain language first — this is
// practice, picks do not count, which rounds this practice covers, when
// the REAL draft starts (league-local, with its relative phrase) — and the
// two verbs a practice ever needs, Leave and (once complete) Practice
// again. It renders inside the command header, above the command bar, as
// its own region so a bot's pick or the practice's end re-renders it
// without touching the bar's live binds. The forms are native (not
// data-gosx-managed): Leave must navigate the whole document back to the
// real room, the same full-navigation rule the sign-out form follows.
func DraftPracticeStrip(props DraftCommandBarProps) Node {
	return <div class="draft-practice-strip" role="status" data-practice-complete={props.Data.practice.complete}>
		<span class="draft-practice-strip__tag mono">PRACTICE</span>
		<If cond={props.Data.practice.complete == false}>
			<p class="draft-practice-strip__text">
				<strong>Practice draft.</strong> Picks here do not count. Round {props.Data.practice.round} of {props.Data.practice.end_round}, practice rounds {props.Data.practice.start_round} to {props.Data.practice.end_round}.
				<If cond={props.Data.practice.real_draft_known}> The real draft starts {props.Data.practice.real_draft_label}, {props.Data.practice.real_draft_relative}.</If>
			</p>
		</If>
		<If cond={props.Data.practice.complete}>
			<p class="draft-practice-strip__text">
				<strong>Practice complete.</strong> You drafted rounds {props.Data.practice.start_round} to {props.Data.practice.end_round}. Nothing was saved.
				<If cond={props.Data.practice.real_draft_known}> The real draft starts {props.Data.practice.real_draft_label}, {props.Data.practice.real_draft_relative}.</If>
			</p>
		</If>
		<div class="draft-practice-strip__actions">
			<If cond={props.Data.practice.complete}>
				<form method="post" action={props.Actions.practice_restart} data-gosx-managed="false">
					<input type="hidden" name="csrf_token" value={props.CSRF}></input>
					<input type="hidden" name="round" value={props.Data.practice.start_round}></input>
					<button class="btn btn-sm btn-primary" type="submit">Practice again</button>
				</form>
			</If>
			<form method="post" action={props.Actions.practice_leave} data-gosx-managed="false">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<button class="btn btn-sm btn-ghost" type="submit">Leave practice</button>
			</form>
		</div>
	</div>
}

// DraftCommissionerDrawer holds the commissioner-only clock and lifecycle
// controls (draft-start, pause/resume/extend/duration presets, undo,
// force-autopick) and every claimed seat's presence override, behind the
// command bar's Commissioner disclosure toggle. The start form stays
// visible for the whole draft, not just before it starts: AdminStartDraft
// is idempotent (a repeat call reports "Draft was already live" and
// leaves the clock untouched), so keeping it up costs nothing and saves a
// commissioner from a dead end if the room ever needs a manual restart.
func DraftCommissionerDrawer(props DraftCommandBarProps) Node {
	return <aside id="draft-commissioner" class="draft-drawer" data-gosx-disclosure data-gosx-disclosure-modal hidden role="dialog" aria-modal="true" aria-labelledby="draft-commissioner-title">
		<header class="draft-drawer__head">
			<h2 id="draft-commissioner-title">Commissioner</h2>
			<button type="button" class="btn btn-sm" aria-label="Close commissioner controls" data-gosx-disclosure-close="#draft-commissioner" data-gosx-disclosure-initial-focus>✕</button>
		</header>
		<div class="draft-drawer__body">
			<If cond={props.Data.draft.complete == false && props.Data.draft.started == false}>
				<form method="post" action={props.Actions.draft_start} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh" class="clock-controls">
					<input type="hidden" name="csrf_token" value={props.CSRF}></input>
					<label class="mono" for="draft-start-confirm">TYPE START //</label>
					<input id="draft-start-confirm" class="scoring-input" name="confirm" autocomplete="off" placeholder="START"></input>
					<button class="button button--primary" type="submit">Start draft + pick clock</button>
				</form>
			</If>
			<If cond={props.Data.draft.complete == false && props.Data.draft.started}>
				<p class="mono draft-drawer__note">Draft is running. The clock controls below are live.</p>
			</If>
			<If cond={props.Data.draft.started && props.Data.draft.complete == false}>
				<div class="clock-controls">
					<form method="post" action={props.Actions.clock_pause} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<button class="button button--compact" type="submit">Pause clock</button>
					</form>
					<form method="post" action={props.Actions.clock_resume} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<button class="button button--compact button--primary" type="submit">Resume clock</button>
					</form>
					<form method="post" action={props.Actions.clock_extend} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<input type="hidden" name="current_pick_token" value={props.Data.current_pick_token}></input>
						<input class="scoring-input" type="number" name="seconds" placeholder="30" min="1" max="600"></input>
						<button class="button button--compact" type="submit">Extend pick</button>
					</form>
					<div class="draft-drawer__presets" aria-label="Pick clock presets">
						<form method="post" action={props.Actions.clock_duration} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
							<input type="hidden" name="csrf_token" value={props.CSRF}></input>
							<input type="hidden" name="seconds" value="60"></input>
							<button class="button button--compact" type="submit">1:00</button>
						</form>
						<form method="post" action={props.Actions.clock_duration} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
							<input type="hidden" name="csrf_token" value={props.CSRF}></input>
							<input type="hidden" name="seconds" value="90"></input>
							<button class="button button--compact" type="submit">1:30</button>
						</form>
						<form method="post" action={props.Actions.clock_duration} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
							<input type="hidden" name="csrf_token" value={props.CSRF}></input>
							<input type="hidden" name="seconds" value="120"></input>
							<button class="button button--compact" type="submit">2:00</button>
						</form>
						<form method="post" action={props.Actions.clock_duration} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
							<input type="hidden" name="csrf_token" value={props.CSRF}></input>
							<input type="hidden" name="seconds" value="180"></input>
							<button class="button button--compact" type="submit">3:00</button>
						</form>
						<form method="post" action={props.Actions.clock_duration} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
							<input type="hidden" name="csrf_token" value={props.CSRF}></input>
							<input type="hidden" name="seconds" value="300"></input>
							<button class="button button--compact" type="submit">5:00</button>
						</form>
					</div>
					<form method="post" action={props.Actions.clock_duration} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<input class="scoring-input" type="number" name="seconds" placeholder="120" min="10" max="600"></input>
						<button class="button button--compact" type="submit">Set duration</button>
					</form>
					<details class="draft-destructive-control">
						<summary class="button button--compact button--ghost">Force current pick now</summary>
						<form method="post" action={props.Actions.clock_autopick} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
							<input type="hidden" name="csrf_token" value={props.CSRF}></input>
							<input type="hidden" name="current_pick_token" value={props.Data.current_pick_token}></input>
							<p>This immediately consumes the on-clock seat's Big Board target, or the best available player when its board is empty. It advances the draft even if the clock is paused.</p>
							<label class="mono" for="draft-force-current-pick-confirm">TYPE FORCE CURRENT PICK //</label>
							<input id="draft-force-current-pick-confirm" class="scoring-input" type="text" name="confirm" value={props.Data.force_current_pick_confirm} autocomplete="off" placeholder="FORCE CURRENT PICK" required="required"></input>
							<button class="button button--compact button--ghost" type="submit">Confirm force current pick</button>
						</form>
					</details>
					<details class="draft-destructive-control">
						<summary class="button button--compact button--ghost">Undo last pick</summary>
						<form method="post" action="/admin/__actions/draft-undo" data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh">
							<input type="hidden" name="csrf_token" value={props.CSRF}></input>
							<input type="hidden" name="previous_pick_token" value={props.Data.previous_pick_token}></input>
							<label class="mono" for="draft-undo-confirm">TYPE UNDO //</label>
							<input id="draft-undo-confirm" class="scoring-input" name="confirm" autocomplete="off" placeholder="UNDO" required="required"></input>
							<button class="button button--compact button--ghost" type="submit">Confirm undo</button>
						</form>
					</details>
				</div>
			</If>
			<section class="draft-seat-controls" aria-label="Commissioner seat coverage">
				{/* comb — oleander, item 7: same plain-language rewrite as
				    the "By Team" panel's own copy above — see that
				    location's doc comment for the full rationale. */}
				<p class="draft-drawer__help">Seat presence is informational; autopick runs from the seat's own setting. Seats get two minutes after a restart before they count as unseen for the short backup clock. Turn on AUTO for a seat you know will be away; it then drafts from that seat's own Big Board.</p>
				<Each of={props.Data.seat_controls} as="seat"><DraftSeatControl {...seat}></DraftSeatControl></Each>
			</section>
		</div>
	</aside>
}

// DraftMobileTabs is the bottom radio-driven tab bar (mobile only, hidden
// on desktop by the stylesheet's breakpoint): the checked radio drives
// which pane the mobile stylesheet reveals via :has(), no JavaScript.
type DraftMobileTabsProps struct {
	Complete  bool
	ShowBoard bool
	ShowTeams bool
	// TapeExplicit is true only when THIS request's own "?view=" named
	// tape explicitly — a click on #tab-picks or the desktop segment's
	// Tape link, or a shared/bookmarked "?view=tape" URL — never for the
	// bare "/draft" landing request, where the tape sub-view is ALSO what
	// renders (the server's own ambient default) but nothing was actually
	// requested. #tab-picks' own checked expression needs this to stay
	// mutually exclusive with #tab-players (item 4, 2026-08-30 review).
	TapeExplicit bool
	// PicksHref/BoardHref/TeamsHref (item 4/6, 2026-08-30 review; BoardHref
	// added wave 7 item 1): every tab is a plain data-gosx-link navigation,
	// like Teams already was — Picks forces "?view=tape" so its own pane
	// content is never whatever the viewer had last navigated to on
	// desktop (Board or Teams), the bug a pure CSS-only radio toggle could
	// show under the wrong label. BoardHref carries data.history_board_href
	// (fragment.go), the exact same URL the desktop segment's own "Draft
	// grid" link already uses — one href, two navigation surfaces.
	PicksHref string
	BoardHref string
	TeamsHref string
}

// DraftMobileTabs defaults to the Players tab while the draft is running
// (S5): once it is complete that pane hides (nothing left to draft, the
// mobile equivalent of the desktop available pane collapsing), so a
// completed draft instead opens on the tape/ledger.
//
// The Teams tab is a plain data-gosx-link to "/draft?view=teams" (item
// 6/P8, item 1a, 2026-08-30 review — see DraftHistoryHead's own doc
// comment for why not data-gosx-set or a managed form): #tab-teams and
// the desktop-style history segment are otherwise two independent native
// radio groups, so a checked #tab-teams alone reveals the history pane
// (the existing phone-width CSS) but says nothing about WHICH sub-view
// shows inside it. #tab-teams itself stays a real radio in the
// "draft-tab" group, its own checked state now driven by ShowTeams (the
// same server-computed flag DraftHistory branches on) rather than
// Complete alone — a soft nav to "?view=teams" re-renders this whole
// component with ShowTeams true, checking #tab-teams in the SAME
// response that also lands on the teams sub-view, no client-side write
// needed to keep the two in agreement.
//
// #tab-picks needs the same ShowBoard-aware treatment for its own
// reason: the desktop segment's Board link (DraftHistoryHead) is a real
// page navigation too, and a viewer can reach "/draft?view=board" from a
// phone-width layout by rotating a tablet, resizing a browser window, or
// following a shared/bookmarked link. Without ShowBoard, #tab-picks'
// checked state read only Complete, so a soft/full nav to "?view=board"
// on an in-progress draft (Complete false) landed back on #tab-players —
// the wrong tab, showing the available pool instead of the board a
// "?view=board" URL asked for — a real regression the T-rider pass'
// own browser screenshots caught.
//
// Item 4 (2026-08-30 review): #tab-picks' own visible trigger is now a
// plain data-gosx-link to props.PicksHref ("/draft?view=tape&..."),
// exactly like #tab-teams' link already was, rather than a <label for=
// "tab-picks"> pure CSS toggle. The pure-CSS shape had its own real bug:
// resizing from desktop after clicking the Board or Teams segment link
// left STALE Board/Teams markup sitting in the history pane's DOM, and a
// later tap on the (client-only) Picks tab revealed that stale content
// under the "Picks" label — no fetch ever ran to correct it. Forcing a
// real "?view=tape" navigation on every Picks tap makes the content
// server-authoritative again, always Tape, never whatever view happened
// to be rendered last. TapeExplicit (its own doc comment above) keeps
// #tab-picks and #tab-players mutually exclusive despite ShowTape being
// true in BOTH the "just landed" and "just clicked Picks" cases.
//
// gap-audit item 8 (wave 3): the mobile top bar (brand and hamburger,
// opening #primary-navigation-dialog) is deliberately hidden on /draft
// (styles.css, "body:has(.draft-shell) .mobile-navigation-enhanced" --
// the draft-shell's own height: 100dvh math needs that clearance back),
// and the desktop command bar's own Rail toggle only affects the site
// rail above the 56.25rem desktop breakpoint, leaving a phone-width
// visitor with no way to reach the rest of the league at all. A sixth
// "League" tab used to sit here for that reason, reopening the SAME
// dialog Layout()'s hamburger button targets.
//
// Wave 7b item 2 (2026-08-31 audit) moved that League trigger out of this
// bar: five real content tabs (Pool, Big Board, Picks, Draft grid, Teams)
// at 390px already measured 72.4-84.4px each — a sixth "flex: 1 1 0" slot
// (public/styles.css's shared .draft-tabbar__tab rule gives every tab
// equal width regardless of its own label) narrowed every tab further for
// a control that is not one of the room's named views at all, and "BIG
// BOARD" was already the tightest label in the row. DraftCommandBar's own
// new .draft-command__pill-toggle sheet (page.gsx, wave 7b item 1) now
// carries a second data-gosx-disclosure-target="#primary-navigation-
// dialog" trigger instead — the same dialog, reachable one tap from the
// always-visible pill rather than a seventh-width slot in this bar, and
// TestDraftPageHasSingleH1AndMobileNavExitFixtureProcess
// (mobile_nav_exit_test.go) now pins that button's new location instead
// of this bar's old sixth slot.
//
// Wave 7 item 1: a dedicated "Draft grid" tab (#tab-board) joins Picks/
// Teams — before this, ShowBoard folded into #tab-picks' own checked
// expression (item 1a's board segment had no phone-width equivalent at
// all, so reaching "/draft?view=board" left the tab bar showing "Picks"
// selected over the grid). #tab-picks' condition drops the ShowBoard
// disjunct it used to carry, since ShowTape/ShowBoard/ShowTeams are
// already mutually exclusive server-side (DraftHistory renders exactly
// one), so #tab-board's own plain `checked={props.ShowBoard}` can never
// disagree with #tab-picks or #tab-teams — no radio group ever ends up
// with two checked inputs sharing the same name.
// D14 (spruce audit): the three link tabs (Picks/Draft grid/Teams) used
// to carry BOTH a server-computed aria-current AND a sibling radio's
// checked state, updated by two independent paths — a real "?view="
// navigation refreshes both together (server-consistent), but a bare
// click on the Pool or Big Board LABEL unchecks whichever of tab-picks/
// tab-board/tab-teams the server had checked (native same-name radio-
// group behavior, no JS needed) while that anchor's own static
// aria-current="true" attribute never changes — a screen reader kept
// announcing "Picks, current page" after a sighted user's tap already
// moved the highlighted tab to Pool. The fix drops aria-current from
// these three anchors entirely and lets EVERY tab (all five) rely on
// the ONE signal that is always correct with zero script: a native
// <input type="radio">'s own implicit aria-checked, which the browser
// derives straight from its checked DOM property and can never go
// stale. tab-picks/tab-board/tab-teams pair with a plain <a>, not a
// <label for="...">, so they carry their own aria-label matching the
// visible tab text (tab-players/tab-queue already get their accessible
// name from label/for, same as before).
func DraftMobileTabs(props DraftMobileTabsProps) Node {
	return <nav class="draft-tabbar" aria-label="Draft room panels">
		<input type="radio" name="draft-tab" id="tab-players" class="visually-hidden" checked={props.ShowTeams == false && props.ShowBoard == false && props.Complete == false && props.TapeExplicit == false}></input>
		<label class="draft-tabbar__tab" for="tab-players">Pool</label>
		<input type="radio" name="draft-tab" id="tab-queue" class="visually-hidden"></input>
		<label class="draft-tabbar__tab" for="tab-queue">Big Board</label>
		<input type="radio" name="draft-tab" id="tab-picks" class="visually-hidden" checked={props.ShowTeams == false && props.ShowBoard == false && (props.Complete || props.TapeExplicit)} aria-label="Picks"></input>
		<a class="draft-tabbar__tab" href={props.PicksHref} data-gosx-link>Picks</a>
		<input type="radio" name="draft-tab" id="tab-board" class="visually-hidden" checked={props.ShowBoard} aria-label="Draft grid"></input>
		<a class="draft-tabbar__tab" href={props.BoardHref} data-gosx-link>Draft grid</a>
		<input type="radio" name="draft-tab" id="tab-teams" class="visually-hidden" checked={props.ShowTeams} aria-label="Teams"></input>
		<a class="draft-tabbar__tab" href={props.TeamsHref} data-gosx-link>Teams</a>
	</nav>
}

// DraftPickBar is the seated manager's sticky mobile action strip, docked
// above the tab bar (V1): the viewer's own top still-draftable queue
// target with one Draft button when it is their turn, or (the rest of the
// time) their own ready/autopick status and toggle — the same controls
// desktop keeps in pane 3's Room tab (DraftMyTeam) — so a phone never
// shows an empty gap between the panes and the tab bar, and nothing about
// ready/autopick ever renders between the command bar and the panes.
//
// comb — larch (2026-09-04), J1 F2: the six branches below used to be
// four, gated by "(started && on_clock && queued) == false", which never
// tested started at all — a live draft with the viewer on the clock but
// no queued player, or simply not their turn, still fell into the FIRST
// false branch, "Before the room opens · Check in for draft night," the
// exact pre-draft-only copy a live manager saw mid-draft (root cause).
// Every branch now names one of three top-level states explicitly —
// complete (a results link), on the clock (queued pick or "open the
// pool"), or neither (pre-draft check-in, or the ready/autopick status a
// live-but-not-on-clock or pre-draft seat both still need) — so no two
// branches can ever paint at once and no live state falls through to
// pre-draft copy.
func DraftPickBar(props DraftAvailableProps) Node {
	return <If cond={props.Data.viewer.has_seat}>
		<div class="draft-pickbar">
			<If cond={props.Data.draft.complete}>
				<div>
					<span class="idx">Draft complete</span>
					<strong>See your results</strong>
				</div>
				<a class="btn btn-primary" href="/draft/results" data-gosx-link>View results</a>
			</If>
			<If cond={props.Data.draft.complete == false && props.Data.draft.started && props.Data.viewer_on_clock && props.Data.next_queued.has}>
				<div>
					<span class="idx idx--hot">Your pick · queue #1</span>
					<strong>{props.Data.next_queued.name} · {props.Data.next_queued.position} · {props.Data.next_queued.nfl_team}</strong>
				</div>
				<form method="post" action={props.MakePickAction} data-gosx-managed="true">
					<input type="hidden" name="csrf_token" value={props.CSRF}></input>
					<input type="hidden" name="team_id" value={props.Data.viewer.team_id}></input>
					<input type="hidden" name="player_id" value={props.Data.next_queued.id}></input>
					<button class="btn btn-primary" type="submit">Draft</button>
				</form>
			</If>
			<If cond={props.Data.draft.complete == false && props.Data.draft.started && props.Data.viewer_on_clock && props.Data.next_queued.has == false}>
				<div>
					<span class="idx idx--hot">Your pick</span>
					<strong>Pick from the pool</strong>
				</div>
				<label class="btn btn-primary" for="tab-players">Open the pool</label>
			</If>
			<If cond={props.Data.draft.complete == false && props.Data.draft.started == false && props.Data.viewer_ready == false}>
				<div>
					<span class="idx">Before the room opens</span>
					<strong>Check in for draft night</strong>
				</div>
				<form method="post" action={props.Actions.toggle_ready} data-gosx-managed="true">
					<input type="hidden" name="csrf_token" value={props.CSRF}></input>
					<input type="hidden" name="team_id" value={props.Data.viewer.team_id}></input>
					<input type="hidden" name="on" value="true"></input>
					<button class="btn btn-primary" id="mobile-ready-toggle" type="submit">Check in now</button>
				</form>
			</If>
			<If cond={props.Data.draft.complete == false && (props.Data.draft.started && props.Data.viewer_on_clock) == false && props.Data.draft.started && props.Data.viewer_ready == false}>
				<div>
					<span class="idx">Room is live</span>
					<strong>Waiting for your turn</strong>
				</div>
			</If>
			<If cond={props.Data.draft.complete == false && (props.Data.draft.started && props.Data.viewer_on_clock) == false && props.Data.viewer_ready && props.Data.viewer_autopick == false}>
				<div>
					<span class="idx">Ready ✓</span>
					<strong>Autopick is off</strong>
				</div>
				<form method="post" action={props.Actions.toggle_autopick} data-gosx-managed="true">
					<input type="hidden" name="csrf_token" value={props.CSRF}></input>
					<input type="hidden" name="team_id" value={props.Data.viewer.team_id}></input>
					<input type="hidden" name="on" value="true"></input>
					<button class="btn btn-sm" id="mobile-autopick-toggle" type="submit">Turn autopick on</button>
				</form>
			</If>
			<If cond={props.Data.draft.complete == false && (props.Data.draft.started && props.Data.viewer_on_clock) == false && props.Data.viewer_ready && props.Data.viewer_autopick}>
				<div>
					<span class="idx">Ready ✓</span>
					<strong>Autopick on</strong>
				</div>
			</If>
		</div>
	</If>
}

// DraftAvailableHead is the available pane's fixed head: a real GET
// search form, the position chips that drive the region's own refetch
// signal, and (D9) the ADP/HOUSE sort toggle.
//
// comb — oleander, item 1: the search input used to sit bare, with no
// surrounding <form> and no name attribute, so it did nothing at all
// without JavaScript, and its data-gosx-filter (the client-side "hide
// non-matching rows" pass) had no matching CSS for .avail-row — every
// row it marked gosx-filter-row--hidden stayed painted (the shared rule
// only ever covered .pool-row, /players' own row class; see this comb's
// block at the end of styles.css for the .avail-row fix). Wrapping the
// input in method="get" action="/draft", naming it "q", and carrying
// pos/sort forward as hidden fields makes a plain Enter/submit reach the
// server's own already-working ?q= filter (service.go's draftData,
// playerMatchesQuery) with no JavaScript required at all — the exact
// no-JS GET fallback /players' own search form already uses. The input
// keeps data-gosx-filter for the fast, no-round-trip live narrowing a
// JavaScript session still gets while typing; Enter (or the Search
// button) still reaches the server for a canonical, shareable, and
// no-JS-safe result either way.
func DraftAvailableHead(props DraftAvailableHeadProps) Node {
	return <div class="draft-available-head">
		<h2 id="draft-available-title" class="visually-hidden">Available players</h2>
		<form method="get" action={props.RoomPath} class="draft-search-form">
			<input id="draft-search" type="search" class="draft-search" name="q" value={props.Query} placeholder={props.SearchPlaceholder} inputmode="search" enterkeyhint="search" autocomplete="off" data-gosx-filter="draft-available-list" data-gosx-filter-announce="true" />
			<If cond={props.Position != ""}>
				<input type="hidden" name="pos" value={props.Position}></input>
			</If>
			<If cond={props.Sort != "" && props.Sort != "adp"}>
				<input type="hidden" name="sort" value={props.Sort}></input>
			</If>
			<button class="filter-button" type="submit">Search</button>
		</form>
		<div class="draft-available-head__chips" role="group" aria-label="Filter the pool by position">
			<Each of={props.Positions} as="chip">
				<a href={chip.href} class="chip" data-gosx-set="$draft.available.pos" data-gosx-set-value={chip.value} aria-pressed={chip.active}>{chip.label}</a>
			</Each>
		</div>
		<div class="draft-available-head__sort" role="group" aria-label="Sort the pool">
			<span class="idx">SORT</span>
			<Each of={props.SortOptions} as="option">
				<a href={option.href} class="chip chip--sort" aria-pressed={option.active}>{option.label}</a>
			</Each>
		</div>
		<details class="pool-legend">
			<summary>What do RK, PROJ, VS ADP, and H### mean?</summary>
			<p>RK — rank by draft market (<abbr title="average draft position">ADP</abbr>), a 1-QB market that undervalues quarterbacks for this league's superflex roster. PROJ — projected points per game. VS ADP — value if drafted at the next pick, versus <abbr title="average draft position">ADP</abbr> (only once the draft is under way). H### — house rank: this league's own superflex-aware value order (your scoring and roster rules), the SORT toggle's "HOUSE" option. <a href="/help#glossary" data-gosx-link>More terms in the glossary →</a></p>
		</details>
	</div>
}

// AvailRowRankProps/AvailRowRank (comb — oleander, item 5): the RK cell's
// own rank markup, pulled into its own component so the RK column
// (desktop/tablet) and the phone-width name-cell chip (styles.css hides
// one and shows the other, complementary, at the same breakpoint — never
// both at once) render the identical two ranks from one source, not two
// copies that could drift. A literal space between the two ranks (not
// the original's bare concatenation) is D9's own fix carried forward:
// H### run directly against the market rank read as one seven-digit
// number ("H001001") with no cue two separate ranks were even present.
type AvailRowRankProps struct {
	Sort         string
	HasHouseRank bool
	HouseRank    string
	Rank         string
}

func AvailRowRank(props AvailRowRankProps) Node {
	return <>
		<If cond={props.Sort == "house" && props.HasHouseRank}>{props.HouseRank} <small class="house-rank">{props.Rank}</small></If>
		<If cond={(props.Sort == "house" && props.HasHouseRank) == false}>{props.Rank}<If cond={props.HasHouseRank}> <small class="house-rank">{props.HouseRank}</small></If></If>
	</>
}

// DraftAvailable is the available-players pane's swapped body: the pool
// grid pre/live, or the pre-draft checklist and the post-draft callout in
// place of it.
func DraftAvailable(props DraftAvailableProps) Node {
	return <div class="draft-available" data-has-adp={props.Data.has_adp}>
		<table class="avail-table">
			<thead>
				<tr class="avail-row avail-row--head">
					{/* comb — oleander, item 5: the header used to describe
					    ADP unconditionally even while the SORT toggle
					    (DraftAvailableHead) had HOUSE active and every RK
					    cell below led with H###, not market rank — a header
					    that named the wrong sort. Two variants, gated the
					    same way the RK cell itself already picks which rank
					    leads (props.Data.pool_sort). */}
					<th scope="col" class="idx">
						<If cond={props.Data.pool_sort == "house"}><abbr title="house rank: this league's own superflex-aware value order (your scoring and roster rules)">RK</abbr></If>
						<If cond={props.Data.pool_sort != "house"}><abbr title="rank by draft market (average draft position)">RK</abbr></If>
					</th>
					<th scope="col" class="idx">PLAYER</th>
					<th scope="col" class="idx">POS</th>
					<th scope="col" class="idx"><abbr title="projected points per game">PROJ</abbr></th>
					<If cond={props.Data.has_adp && props.Data.draft.started}><th scope="col" class="idx avail-row__vsadp"><abbr title="value if drafted at the next pick, versus average draft position">VS ADP</abbr></th></If>
					<th scope="col" class="idx avail-row__info-head"><span class="visually-hidden">Player info</span></th>
					<th scope="col" class="idx">ACTION</th>
				</tr>
			</thead>
			<tbody id="draft-available-rows">
				<Each of={props.Players} as="player">
					<tr class="avail-row" data-player-id={player.ID} data-gosx-filter-text={player.Search} data-taken={player.Taken} data-gosx-live-bind-attr={"data-taken:player." + player.ID + ".taken"}>
						<td class="num">
							{/* D9 (spruce audit): the RK cell leads with whichever
							    rank the active sort actually used — HOUSE's own
							    H### first (market rank demoted to the small
							    secondary label) when props.Data.pool_sort is
							    "house", market rank first otherwise. A player with
							    no house rank at all (HasHouseRank false — a zero-
							    Projection player, houserank.go) always shows the
							    market rank alone, regardless of the active sort. */}
							<AvailRowRank Sort={props.Data.pool_sort} HasHouseRank={player.HasHouseRank} HouseRank={player.HouseRank} Rank={player.Rank}></AvailRowRank>
						</td>
						<td class="avail-row__player">
							{/* comb — oleander, item 5: .avail-row > :first-child
							    (the RK cell above) goes display: none at phone
							    width (styles.css), so this chip is the only
							    surviving exposure of the active rank there —
							    same AvailRowRank component, same two ranks, just
							    inline before the name instead of its own column. */}
							{/* .avail-row__player-body (2026-09-03 mobile pass): the phone
							    layout lays this cell out as a two-line grid (name, then
							    rank + detail). The grid sits on this inner block, not the
							    td: a td that stops being display: table-cell is wrapped
							    in an anonymous cell together with its inline-grid .pos
							    neighbour, which drops the POS chip under the name. */}
							<div class="avail-row__player-body">
								<span class="avail-row__rank-chip mono"><AvailRowRank Sort={props.Data.pool_sort} HasHouseRank={player.HasHouseRank} HouseRank={player.HouseRank} Rank={player.Rank}></AvailRowRank></span>
								<strong>{player.Name}</strong> <If cond={player.Detail != ""}><small>· {player.Detail}</small></If>
							</div>
						</td>
						<td class={"pos pos-" + player.Position}>{player.Position}</td>
						<td class="num">{player.Projection}</td>
						<If cond={props.Data.has_adp && props.Data.draft.started && player.HasValue}><td class="num avail-row__vsadp" title={player.ValueLabel + " vs ADP: value if drafted at the next pick, versus average draft position"}>{player.ValueLabel}</td></If>
						<If cond={props.Data.has_adp && props.Data.draft.started && player.HasValue == false}><td class="num avail-row__vsadp" title="no market ADP for punters">—</td></If>
						<td class="avail-row__info">
							<If cond={player.HasNews}><details class="stat-tip stat-tip--news"><summary class="stat-tip__summary stat-tip__summary--news" aria-label={"News for " + player.Name}>📰</summary><div class="stat-tip__panel"><p class="stat-tip__news"><span class="stat-tip__label">NEWS</span> {player.News}</p><If cond={player.HasInjury}><p class="stat-tip__hist-note">{player.Injury}</p></If></div></details></If>
						</td>
						<td class="avail-row__actions">
						<If cond={props.Data.viewer.has_seat}>
							<If cond={props.Data.practice.active == false}>
							<form method="post" action={props.QueueAddAction} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={props.CSRF}></input>
								<input type="hidden" name="player_id" value={player.ID}></input>
								<input type="hidden" name="pos" value={props.Data.pool_position}></input>
								<input type="hidden" name="q" value={props.Data.pool_query}></input>
								<input type="hidden" name="page" value={props.Data.pool_page}></input>
								<button class="button button--ghost" type="submit" aria-label={"Add " + player.Name + " to your Big Board"}>+ RANK</button>
							</form>
							</If>
							{/* comb — larch (2026-09-04), J1 F12: one tap used to
							    post the pick outright — the row's own Draft
							    button WAS the form's only submit control, so a
							    mis-tap on a 44px-tall row of fifty identical
							    buttons drafted whoever it landed on, with no
							    undo. <details> turns the same button into a
							    two-tap disclosure, the exact no-JS pattern
							    /players' own "Confirm drop" (public/styles.css
							    .action-confirmation, this file's own doc
							    comment) already ships: the first tap only
							    opens it (native <summary> behavior, no request
							    sent); the SECOND tap hits the real submit
							    button inside, still in this one <form>, so the
							    pick posts exactly like before once confirmed.
							    Tapping the summary again (or anywhere outside,
							    for a mouse) closes it with no server round
							    trip — the built-in Cancel. */}
							<form method="post" action={props.MakePickAction} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={props.CSRF}></input>
								<input type="hidden" name="team_id" value={props.Data.viewer.team_id}></input>
								<input type="hidden" name="player_id" value={player.ID}></input>
								<input type="hidden" name="pos" value={props.Data.pool_position}></input>
								<input type="hidden" name="q" value={props.Data.pool_query}></input>
								<input type="hidden" name="page" value={props.Data.pool_page}></input>
								<If cond={props.Data.can_pick && player.CanDraft}>
									<details class="draft-row-confirm">
										<summary class="btn btn-sm btn-primary" aria-label={"Draft " + player.Name}>
											<span class="draft-row-confirm__closed-label">Draft</span>
											<span class="draft-row-confirm__open-label">{"Confirm " + player.Name}</span>
										</summary>
										<div class="draft-row-confirm__panel">
											<If cond={player.SpecialistEarly}>
												<p class="draft-row-confirm__warning">Specialists usually go late. Draft anyway?</p>
											</If>
											<button class="btn btn-sm btn-primary" type="submit">{"Confirm " + player.Name}</button>
											<p class="draft-row-confirm__cancel-hint muted">Tap Draft again to cancel.</p>
										</div>
									</details>
								</If>
								<If cond={props.Data.can_pick && player.CanDraft == false}>
									<span class="control-locked">
										<button class="btn btn-sm" type="button" disabled="disabled" title="Choose a player who keeps every required starter slot fillable">Roster need</button>
										<small class="control-locked__reason">Choose a player who keeps every required starter slot fillable</small>
									</span>
								</If>
							</form>
						</If>
						</td>
					</tr>
				</Each>
			</tbody>
		</table>
		<If cond={props.Data.available_count == 0}>
			<div class="empty-tape">
				<strong>NO PLAYERS MATCH</strong>
				<p>Try a different position filter or clear your search.</p>
			</div>
		</If>
		<nav class="pool-pagination" aria-label="Draft pool pages">
			<If cond={props.Data.pool_has_previous}>
				<a class="filter-button" href={props.Data.pool_previous_href} data-gosx-link rel="prev">← Previous</a>
			</If>
			<span class="mono" aria-live="polite">
				<If cond={props.Data.pool_total > 0}>{props.Data.pool_page_start}–{props.Data.pool_page_end} of {props.Data.pool_total}</If>
				<If cond={props.Data.pool_total == 0}>0 players</If>
			</span>
			<If cond={props.Data.pool_has_next}>
				<a class="filter-button" href={props.Data.pool_next_href} data-gosx-link rel="next">Next →</a>
			</If>
		</nav>
		<If cond={props.Data.draft.complete}>
			<div class="empty-tape draft-complete-callout">
				<strong>DRAFT CLOSED · ALL PICKS LOCKED</strong>
				<p>Drafted rosters are in the Team terminal. Every remaining player is now available through the Player Pool and waiver rules.</p>
				<div class="hero-actions">
					<a href="/team" data-gosx-link class="button button--primary">Open team terminal →</a>
					<a href="/players" data-gosx-link class="button button--ghost">Open player pool →</a>
				</div>
			</div>
		</If>
	</div>
}

// DraftMyTeam is the "my team" pane's swapped body: the viewer's own
// personal queue (including taken entries, struck through client-side),
// roster needs, autopick status, and the room's full seat grid.
func DraftMyTeam(props DraftMyTeamProps) Node {
	return <div class="draft-mine">
		<h2 id="draft-mine-title" class="visually-hidden">Your team</h2>
		<div class="segment" role="radiogroup" aria-label="My team panels">
			<input type="radio" name="draft-mine-view" id="mine-queue" class="visually-hidden" checked></input>
			<label class="segment__option" for="mine-queue">Big Board</label>
			<input type="radio" name="draft-mine-view" id="mine-roster" class="visually-hidden"></input>
			<label class="segment__option" for="mine-roster">Roster</label>
			<input type="radio" name="draft-mine-view" id="mine-room" class="visually-hidden"></input>
			<label class="segment__option" for="mine-room">Room</label>
		</div>
		<div class="draft-mine__view draft-mine__view--queue">
			<div class="pool-list pool-list--reorder-scroll" data-gosx-reorder data-gosx-reorder-action={props.Data.queue_move_url} data-gosx-csrf-token={props.CSRF}>
				<If cond={props.Data.practice.active}>
					<p class="draft-practice-note mono" role="note">Your Big Board is read-only in practice. <a href="/board" data-gosx-link>Edit it in the real room →</a></p>
				</If>
				<If cond={props.Data.queue_empty == false}>
					<div class="q-list__header mono">NEXT UP</div>
				</If>
				<Each of={props.Queue} as="player">
					<article class="q-row" data-gosx-reorder-item={player.ID} data-taken={player.Taken} data-gosx-live-bind-attr={"data-taken:queue." + player.ID + ".taken"}>
						<If cond={props.Data.practice.active == false}>
							<span class="board-row__handle" data-gosx-reorder-handle aria-label={"Reorder " + player.Name}>⠿</span>
						</If>
						<div class="q-row__player">
							<span class="q-row__rank mono">{player.Rank}</span>
							<strong class="q-row__name">{player.Name}</strong>
							<small class="q-row__meta">{player.Position} · {player.NFLTeam} · proj {player.Projection}</small>
						</div>
						<div class="q-row__info">
							<If cond={player.HasNews}><details class="stat-tip stat-tip--news"><summary class="stat-tip__summary stat-tip__summary--news" aria-label={"News for " + player.Name}>📰</summary><div class="stat-tip__panel"><p class="stat-tip__news"><span class="stat-tip__label">NEWS</span> {player.News}</p><If cond={player.HasInjury}><p class="stat-tip__hist-note">{player.Injury}</p></If></div></details></If>
						</div>
						<If cond={props.Data.practice.active == false}>
						<div class="q-row__actions">
							<form method="post" action={props.QueueMoveAction} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={props.CSRF}></input>
								<input type="hidden" name="player_id" value={player.ID}></input>
								<input type="hidden" name="direction" value="up"></input>
								<input type="hidden" name="pos" value={props.Data.pool_position}></input>
								<input type="hidden" name="q" value={props.Data.pool_query}></input>
								<input type="hidden" name="page" value={props.Data.pool_page}></input>
								<If cond={player.CanMoveUp}>
									<button class="board-button board-button--move" type="submit" aria-label={"Move " + player.Name + " up"}>↑ <span class="visually-hidden">Move up</span></button>
								</If>
								<If cond={player.CanMoveUp == false}>
									<button class="board-button board-button--move" type="button" disabled="disabled" aria-label={player.Name + " is already first"}>↑ <span class="visually-hidden">Already first</span></button>
								</If>
							</form>
							<form method="post" action={props.QueueMoveAction} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={props.CSRF}></input>
								<input type="hidden" name="player_id" value={player.ID}></input>
								<input type="hidden" name="direction" value="down"></input>
								<input type="hidden" name="pos" value={props.Data.pool_position}></input>
								<input type="hidden" name="q" value={props.Data.pool_query}></input>
								<input type="hidden" name="page" value={props.Data.pool_page}></input>
								<If cond={player.CanMoveDown}>
									<button class="board-button board-button--move" type="submit" aria-label={"Move " + player.Name + " down"}>↓ <span class="visually-hidden">Move down</span></button>
								</If>
								<If cond={player.CanMoveDown == false}>
									<button class="board-button board-button--move" type="button" disabled="disabled" aria-label={player.Name + " is already last"}>↓ <span class="visually-hidden">Already last</span></button>
								</If>
							</form>
							<If cond={player.Taken}>
								<form method="post" action={props.QueueRemoveAction} data-gosx-managed="true">
									<input type="hidden" name="csrf_token" value={props.CSRF}></input>
									<input type="hidden" name="player_id" value={player.ID}></input>
									<input type="hidden" name="pos" value={props.Data.pool_position}></input>
									<input type="hidden" name="q" value={props.Data.pool_query}></input>
									<input type="hidden" name="page" value={props.Data.pool_page}></input>
									<button class="btn btn-sm btn-ghost" type="submit">Clear</button>
								</form>
							</If>
						</div>
						</If>
					</article>
				</Each>
				<p class="reorder-status reorder-status--pending">Saving order…</p>
				<p class="reorder-status reorder-status--error">Reorder failed. The previous order was restored.</p>
			</div>
			<If cond={props.Data.queue_empty}>
				<div class="board-peek-empty"><a href="/board" data-gosx-link class="mono">BUILD YOUR BIG BOARD →</a></div>
			</If>
		</div>
		<div class="draft-mine__view draft-mine__view--roster">
			<div class="draft-mine__needs">
				<span class="idx">Roster needs</span>
				<Each of={props.Data.roster_needs} as="need">
					<If cond={need.open}><span class="need need--open">{need.label} {need.filled}/{need.total}</span></If>
					<If cond={need.open == false}><span class="need need--full">{need.label} {need.filled}/{need.total}</span></If>
				</Each>
				<span class="mono draft-mine__autopick">
					<If cond={props.Data.viewer_autopick}>AUTOPICK · ON</If>
					<If cond={props.Data.viewer_autopick == false}>AUTOPICK · OFF</If>
				</span>
			</div>
		</div>
		<div class="draft-mine__view draft-mine__view--room">
		<div class="draft-mine__room">
			<div class="draft-mine__room-summary mono">
				<span class="draft-mine__room-summary--comfortable">
					<span data-gosx-live-bind="room.here">{props.Data.here_count}</span> of <span data-gosx-live-bind="room.managers">{props.Data.manager_count}</span> managers online · <span data-gosx-live-bind="room.ready">{props.Data.ready_count}</span> ready · <span data-gosx-live-bind="room.auto">{props.Data.auto_count}</span> on autopick
				</span>
				<span class="draft-mine__room-summary--compact">
					<span data-gosx-live-bind="room.here">{props.Data.here_count}</span>/<span data-gosx-live-bind="room.managers">{props.Data.manager_count}</span> here · <span data-gosx-live-bind="room.ready">{props.Data.ready_count}</span>/<span data-gosx-live-bind="room.managers">{props.Data.manager_count}</span> ready · <span data-gosx-live-bind="room.auto">{props.Data.auto_count}</span> auto
				</span>
				<If cond={props.Data.your_pick_in > 0}>
					<span class="draft-mine__yourpick" data-gosx-live-bind={props.Data.yourpick_bind_key}>your pick in {props.Data.your_pick_in}</span>
				</If>
			</div>
			<If cond={props.Data.viewer.has_seat && props.Data.draft.complete == false}>
				<If cond={props.Data.practice.active == false}>
				<div class="manager-draft-control" id="ready-toggle" data-ready={props.Data.viewer_ready}>
					<If cond={props.Data.viewer_ready}><small class="visually-hidden">You are checked in for draft night.</small></If>
					<If cond={props.Data.viewer_ready == false}><small class="visually-hidden">Check in once your Big Board is set.</small></If>
					<form method="post" action={props.Actions.toggle_ready} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<input type="hidden" name="team_id" value={props.Data.viewer.team_id}></input>
						<If cond={props.Data.viewer_ready}>
							<button class="btn btn-sm" type="submit" aria-pressed="true">Undo ready check-in</button>
						</If>
						<If cond={props.Data.viewer_ready == false}>
							<button class="btn btn-sm btn-primary" type="submit" aria-pressed="false">Mark me ready</button>
						</If>
					</form>
				</div>
				</If>
				<div class="manager-draft-control" id="autopick-toggle">
					<If cond={props.Data.viewer_autopick}><small class="visually-hidden">Autopick uses your Big Board, then best available.</small></If>
					<If cond={props.Data.viewer_autopick == false}><small class="visually-hidden">Manual control keeps your full pick clock.</small></If>
					<form method="post" action={props.Actions.toggle_autopick} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<input type="hidden" name="team_id" value={props.Data.viewer.team_id}></input>
						<If cond={props.Data.viewer_autopick}>
							<input type="hidden" name="on" value="false"></input>
							<button class="btn btn-sm" type="submit">Turn autopick off</button>
						</If>
						<If cond={props.Data.viewer_autopick == false}>
							<input type="hidden" name="on" value="true"></input>
							<button class="btn btn-sm" type="submit">Turn autopick on</button>
						</If>
					</form>
				</div>
			</If>
			<If cond={props.Data.viewer.has_seat == false && props.Data.draft.complete == false}>
				<div class="manager-draft-control">
					<If cond={props.Data.public_entry.can_claim}>
						<strong class="ready-state">NO TEAM SEAT</strong>
						<small>{props.Data.public_entry.detail}</small>
						<a href={props.Data.public_entry.action_href} data-gosx-link class="btn btn-sm btn-primary">{props.Data.public_entry.action_label}</a>
					</If>
					<If cond={props.Data.public_entry.can_claim == false}>
						<strong class="ready-state">{props.Data.public_entry.state_label}</strong>
						<small>{props.Data.public_entry.detail}</small>
						<a href={props.Data.public_entry.action_href} data-gosx-link class="btn btn-sm">{props.Data.public_entry.action_label}</a>
						<If cond={props.Data.public_entry.admitted == false}>
							<a href="/pickem" data-gosx-link class="btn btn-sm">Open Pick'em HQ →</a>
						</If>
						<If cond={props.Data.public_entry.admitted && props.Data.public_entry.league_full}>
							<a href="/players" data-gosx-link class="btn btn-sm">Browse player pool →</a>
						</If>
					</If>
				</div>
			</If>
			<If cond={props.Data.draft.complete}>
				<div class="manager-draft-control">
					<strong class="ready-state is-ready">SET YOUR LINEUP</strong>
					<a href="/team" data-gosx-link class="btn btn-sm btn-primary">Open team terminal →</a>
				</div>
			</If>
			<Each of={props.Data.teams} as="team">
				<DraftTeam {...team}></DraftTeam>
			</Each>
		</div>
		</div>
	</div>
}

// DraftHistoryHead is the history pane's always-visible chrome (sits
// outside the pane's own swapped .draft-pane__body region, so it never
// re-renders on a poll): the Tape/Board/Teams segment, the "↓ Latest"
// jump link inline at its right edge (mockup divergence, 2026-08-30
// review — it used to sit inside the swapped tape sub-view instead), and
// the tape's own position/mine filter chips (a second mockup divergence:
// ALL | QB | RB | WR | TE | Mine under the tab row).
//
// The segment is three plain data-gosx-link navigations to
// "/draft?view=tape|board|teams" (item 1a), not a native radio group
// signalling a client-side write: every click-based signal-writing
// attribute gosx@v0.53.9 offers (data-gosx-set, data-gosx-toggle-target)
// unconditionally calls e.preventDefault() on its own triggering click
// (client/runtime/host/actions.ts) — placed on a <label for="..."> or
// its target radio, that cancels the SAME click's native "check this
// radio" default action, so the radio never actually became checked
// (found live: the segment stayed on Tape and the pane went blank after
// a Board click, even though the region fetch itself succeeded). A
// managed-form workaround has the identical problem one layer up
// (data-gosx-managed's own JSON response has no path into a shared
// signal at all — confirmed by reading navigation.ts's managed-submit
// pipeline directly, not just the docs). A plain link sidesteps all of
// it: data-gosx-link soft-navigates to the new URL (client/runtime/
// host/navigation.ts, the same mechanism the pool pagination links on
// this same page already use), re-running Load with the new "?view=",
// so ShowTape/ShowBoard/ShowTeams (below) and the pane body's own
// region URL (data.history_tape_url, Page()) both come from that ONE
// fresh server render — no client-side signal needed at all. The
// active option gets aria-current, computed here in Go from the same
// three bools DraftHistory itself branches on.
//
// The filter chips are a separate, plain native radio group
// (draft-tape-filter) — CSS alone (.tape-row rules scoped under
// .draft-history__view--tape, public/styles.css) hides a non-matching row
// once its :checked state changes, client-side: no server round trip
// needed, so no data-gosx-link concern applies to them. Item 9 (2026-08-30
// review): the chips render on the Tape sub-view only — they filter
// .tape-row entries that exist only there (Board/Teams render no
// .tape-row with data-position/data-mine at all), and (unlike this head's
// own segment/jump-link row) the pre-fix markup rendered them
// unconditionally, so switching to Board or Teams left a fully non-
// functional filter row visible above content it could never affect.
func DraftHistoryHead(props DraftHistoryHeadProps) Node {
	return <div class="draft-history-head">
		<h2 id="draft-history-title" class="visually-hidden">Pick history</h2>
		<div class="draft-history-head__row">
			{/* comb — larch (2026-09-04), J1 F19: this label used to read
			    "PICK HISTORY" for every one of the pane's three views —
			    a manager who tapped TEAMS or Draft grid landed on a panel
			    whose own heading named the tab they had just left, a
			    second of doubt every time (consistency). Three names, one
			    per view, in the same order the segment nav beside it
			    already offers them. */}
			<If cond={props.ShowBoard}><span class="draft-history-head__label mono">DRAFT GRID</span></If>
			<If cond={props.ShowTeams}><span class="draft-history-head__label mono">TEAMS</span></If>
			<If cond={props.ShowBoard == false && props.ShowTeams == false}><span class="draft-history-head__label mono">PICK HISTORY</span></If>
			<nav class="segment" aria-label="Pick history panels">
				<a class="segment__option" href={props.TapeHref} data-gosx-link aria-current={props.ShowTape}>Picks</a>
				<a class="segment__option" href={props.BoardHref} data-gosx-link aria-current={props.ShowBoard}>Draft grid</a>
				<a class="segment__option" href={props.TeamsHref} data-gosx-link aria-current={props.ShowTeams}>Teams</a>
			</nav>
			<a class="btn btn-sm draft-history__jump" href="#tape-latest">↓ Latest</a>
		</div>
		<If cond={props.ShowTape}>
			<div class="draft-history-filters" role="radiogroup" aria-label="Filter the tape">
				<input type="radio" name="draft-tape-filter" id="tape-filter-all" class="visually-hidden" checked></input>
				<label class="chip" for="tape-filter-all">ALL</label>
				<input type="radio" name="draft-tape-filter" id="tape-filter-qb" class="visually-hidden"></input>
				<label class="chip" for="tape-filter-qb">QB</label>
				<input type="radio" name="draft-tape-filter" id="tape-filter-rb" class="visually-hidden"></input>
				<label class="chip" for="tape-filter-rb">RB</label>
				<input type="radio" name="draft-tape-filter" id="tape-filter-wr" class="visually-hidden"></input>
				<label class="chip" for="tape-filter-wr">WR</label>
				<input type="radio" name="draft-tape-filter" id="tape-filter-te" class="visually-hidden"></input>
				<label class="chip" for="tape-filter-te">TE</label>
				<input type="radio" name="draft-tape-filter" id="tape-filter-mine" class="visually-hidden"></input>
				<label class="chip" for="tape-filter-mine">Mine</label>
			</div>
		</If>
	</div>
}

// DraftPickDetail is one made pick's tape row AND (once open) its expanded
// detail panel together (item 1, 2026-08-30 review — replacing the
// <details><summary data-gosx-set> shape T3/T4 built): the row's own
// visible content lives in a plain <a class="tape-row__summary"
// data-gosx-link>, a 44px soft-navigation target to props.Href, never a
// client-side signal write. This is the BLOCKING fix the plan's own
// verified fact demands: gosx@v0.53.9's capture-phase document click
// listener finds the nearest [data-gosx-set] ancestor of any click and
// unconditionally calls preventDefault() on it (client/runtime/host/
// actions.ts ~474-478) — placed on (or under) a <summary>, that cancels
// the SAME click's native "open this <details>" default action, so the
// old row never actually opened; found live, the pane stayed closed
// after every tap. A plain <a data-gosx-link> has no such ancestor and
// soft-navigates normally, re-running Load with "?pick=N" so the SERVER
// renders this exact row open, its detail body inline — no fetch race,
// no client state to lose on the next region swap the way the old
// per-row <details open> state was (that limitation is gone with the
// <details> element itself).
//
// The player line leads (bold, strongest, with its position chip and any
// AUTO/COMM tag inline); the team+manager+NFL-team+time-to-pick line sits
// under it, muted; the slot label and pick number stay in mono at the
// row's two edges, matching the mockup — all preserved from the T3/T4
// polish pass, now inside the <a> rather than a <summary>.
// .pick-detail__chevron is a decorative marker only (aria-hidden); the
// open state itself is data-open on the wrapping <article> plus
// aria-current="true" on the <a>, both server-computed from props.Open,
// never a client-side derived state to go stale.
func DraftPickDetail(props TapePick) Node {
	return <article class="tape-row tape-row--detail" data-tape-key={"pick-" + props.Number} data-pick-number={props.Number} data-mine={props.Mine} data-auto={props.IsAuto} data-position={props.Position} data-open={props.Open}>
		<a class="tape-row__summary" data-gosx-link href={props.Href} aria-current={props.Open}>
			<span class="mono tape-row__slot">{props.Label}</span>
			<span class={"team-mark tone-" + props.TeamTone}>
				<If cond={props.HasAvatarImage}>
					<img class="avatar-mark__photo" src={props.AvatarImageURL} alt={props.TeamName} loading="lazy" />
				</If>
				<If cond={props.HasAvatarImage == false}>{props.TeamAbbr}</If>
			</span>
			<div class="tape-row__body">
				<div class="tape-row__player">
					<strong>{props.PlayerName}</strong>
					<span class={"pos pos-" + props.Position}>{props.Position}</span>
					<If cond={props.IsAuto}><b class="tag tag--auto">AUTO</b></If>
					<If cond={props.IsCommissioner}><b class="tag tag--comm">COMM</b></If>
				</div>
				<div class="tape-row__who">
					<small>
						{props.TeamName}
						<If cond={props.Manager != ""}> · {props.Manager}</If>
						<If cond={props.NFLTeam != ""}> · {props.NFLTeam}</If>
						<If cond={props.TimeToPickSec > 0}> · {props.TimeToPick}</If>
					</small>
				</div>
			</div>
			<div class="tape-row__meta">
				<span class="mono tape-row__number">#{props.Number}</span>
				<span class="pick-detail__chevron" aria-hidden="true"></span>
			</div>
		</a>
		<If cond={props.Open}>
			<div class="pick-detail__body" id={"pick-detail-" + props.Number}>
				<DraftPickDetailBody {...props}></DraftPickDetailBody>
			</div>
		</If>
	</article>
}

// DraftPickDetailBody is one pick's expanded accordion content ALONE.
// Item 1 (2026-08-30 review) has DraftPickDetail render this component
// directly, inline, for the single row a "?pick=" query opened — no
// fetch, no client-side signal, the whole point of the fix — so this
// component's fields arrive already hydrated by attachDraftFragmentPick
// (fragment.go). GET /draft/fragment/pick/{n} (PickDetailFragmentHandler,
// fragment.go) still renders this SAME component and stays mounted for
// gosx v0.53.10's target mode (Task 8): a click-driven region bind with
// no fallback-mode page reload at all. Eagerly rendering this for EVERY
// made pick, rather than only the one row a viewer opened, was the
// single largest contributor to the tape fragment's own gzip size at a
// full 120-pick draft, well over the D3 refresh-budget's 4 KB ceiling in
// fallback mode (item 1b's original fix, folded into item 1 here).
func DraftPickDetailBody(props TapePick) Node {
	return <>
		<div class="pick-detail__stats">
			<span class="mono">Proj {props.Projection}</span>
			<If cond={props.HasValue}><span class="mono">vs ADP {props.ValueLabel}</span></If>
		</div>
		<p class="mono muted">Drafted by {props.TeamName}<If cond={props.Manager != ""}> · {props.Manager}</If></p>
		<p class="mono muted">Source: {props.Source}</p>
		<div class="pick-detail__best">
			<span class="idx">Best available at this pick</span>
			<Each of={props.BestAvailable} as="candidate">
				<span>{candidate.Name} <small>{candidate.Position} · {candidate.NFLTeam}</small></span>
			</Each>
		</div>
		<div class="pick-detail__team">
			<span class="idx">{props.TeamName} so far</span>
			<Each of={props.TeamPicks} as="teamPick">
				<span class={"chip pos-" + teamPick.Position}>{teamPick.PlayerName}</span>
			</Each>
		</div>
		<a class="btn btn-sm" href={"/players?q=" + props.PlayerName}>Player card →</a>
	</>
}

// DraftTapeRows is the tape's own body (D4): every round newest first, each
// with a sticky one-line header (round, snake direction, pick span, "N of
// M made" — T1 polish, 2026-08-30 review), then one row per pick, newest
// pick first, each row doubling as its own DraftPickDetail toggle. It
// backs THREE callers: target mode's own single tape-rows region
// (findings 1/2/3/6, 2026-08-30 review — a full replace on every
// draft:pick/draft:undo/draft:state, so round.ShowHeader is always true
// here, every visible round's header rendering fresh on every response),
// fallback mode's full "tape" region, and the "?since=" partial the
// server still answers for API compatibility (prepareDraftData/
// attachDraftFragmentSince pre-filter Rounds to picks numbered above
// Since before this renders, and set ShowHeader per round — see
// filterTapeRoundsSince, fragment.go). This function itself carries no
// outer element (a GSX fragment, "<>"): the caller (DraftHistory's
// ShowTape branch, and Page()'s pane-body full render) owns the
// ".draft-tape-rows" wrapper and its class instead.
func DraftTapeRows(props DraftHistoryProps) Node {
	return <>
		<Each of={props.Rounds} as="round">
			<If cond={round.ShowHeader}>
				<div class="tape-round" data-tape-key={"round-" + round.Round} data-current={round.Current} data-gosx-live-bind-attr={round.CurrentBindAttr}>
					<span class="idx">ROUND {round.Round}</span>
					<span class="mono muted tape-round__meta">{round.Direction} picks {round.First}–{round.Last} · <span data-gosx-live-bind={round.MadeBindKey}>{round.Made}</span> of {round.Total} made</span>
				</div>
			</If>
			<If cond={round.Current && props.HasOnClock}>
				<article class="tape-row tape-row--clock">
					<span class="mono tape-row__slot">{props.NextLabel}</span>
					<span class={"team-mark tone-" + props.OnClockTone}>
						<If cond={props.OnClockHasAvatarImage}>
							<img class="avatar-mark__photo" src={props.OnClockAvatarImageURL} alt={props.OnClockName} loading="lazy" />
						</If>
						<If cond={props.OnClockHasAvatarImage == false}>{props.OnClockAbbr}</If>
					</span>
					<div class="tape-row__body"><strong>On the clock</strong><small>{props.OnClockName}</small></div>
				</article>
			</If>
			<Each of={round.Picks} as="pick">
				<DraftPickDetail {...pick}></DraftPickDetail>
			</Each>
		</Each>
		<If cond={props.RoundsEmpty && props.HasOnClock}>
			<article class="tape-row tape-row--clock">
				<span class="mono tape-row__slot">{props.NextLabel}</span>
				<span class={"team-mark tone-" + props.OnClockTone}>
					<If cond={props.OnClockHasAvatarImage}>
						<img class="avatar-mark__photo" src={props.OnClockAvatarImageURL} alt={props.OnClockName} loading="lazy" />
					</If>
					<If cond={props.OnClockHasAvatarImage == false}>{props.OnClockAbbr}</If>
				</span>
				<div class="tape-row__body"><strong>On the clock</strong><small>{props.OnClockName}</small></div>
			</article>
		</If>
		<If cond={props.RoundsEmpty}>
			<div class="empty-tape">
				<strong>NO PICKS YET</strong>
				<p>The tape starts moving when the first selection is locked.</p>
			</div>
		</If>
		<If cond={props.HasOlderRounds}>
			<a class="btn btn-sm draft-tape-older" data-gosx-link href={props.OlderHref}>Older rounds ↓</a>
		</If>
	</>
}

// DraftBoard is the round x team grid (D4): a sticky team-column header row,
// then one sticky round header and one board-cell row per round. The
// scroll container (.results-board-scroll, the caller's own wrapper div)
// owns overflow on both axes, bounded to 70dvh, so the grid scrolls both
// ways at phone width without growing the page past the viewport. The
// header shows the team's full name under its badge, wrapped over up to
// two lines (mockup divergence, 2026-08-30 review — it used to truncate
// to one ellipsized line, "Kern…"); a filled cell reads "1.01 · WR · CIN"
// (label · position · NFL team, PickBoard.dc.html — the NFL team was
// missing before this same pass).
//
// Wave 7 item 1 (mobile delight): every column header carries its own
// "board-team-<id>" id unconditionally (a plain, always-unique anchor
// target, cheaper than a conditional id="" GSX cannot express cleanly
// inline — see BoardView.MineID's own doc comment); the "Jump to my
// picks" link only renders while HasMine, and targets that same id on
// the viewer's own column. A plain in-page <a href="#...">, no JS: the
// browser's own fragment-navigation scroll already brings a target into
// view within its nearest scrolling ancestor on both axes, and
// .board-grid__team's own scroll-margin-left (styles.css) keeps the
// sticky round column from covering it once it lands.
//
// Wave-7 re-audit item 3 (yew): .board-grid's own doc comment (public/
// styles.css) has the full account of the two compounding bugs that used
// to break both sticky headers here — in short, the grid had no explicit
// width (so it clamped to the viewport instead of its own wider content,
// starving every sticky item's containing block of the room it needed to
// hold a "stuck" position) and the scroll container had no vertical
// overflow at all (so sticky top never had anything to engage against).
// Fixed at the CSS layer only; this component's own markup is unchanged.
func DraftBoard(props BoardView) Node {
	return <>
		<If cond={props.HasMine}>
			<a class="board-jump" href={"#board-team-" + props.MineID}>↓ Jump to my picks</a>
		</If>
		<div class="board-grid" style={"--board-columns:" + props.ColumnCount}>
		<div class="board-grid__corner"></div>
		<Each of={props.Columns} as="column">
			<div class="board-grid__team" id={"board-team-" + column.id} data-mine={column.mine}>
				<span class={"team-mark tone-" + column.tone}>{column.abbreviation}</span>
				<span class="board-grid__name">
					<span class="board-grid__code mono">{column.abbreviation}</span>
					<span class="board-grid__fullname" title={column.name}>{column.name}<If cond={column.mine}> · you</If></span>
				</span>
			</div>
		</Each>
		<Each of={props.Rows} as="row">
			<div class="board-grid__round">
				<span class="idx">ROUND {row.Round}</span>
				<span class="mono muted">{row.Direction}</span>
			</div>
			<Each of={row.Cells} as="cell">
				<div class={"board-cell c-" + cell.Position} data-round={cell.Round} data-column={cell.Column} data-filled={cell.Filled} data-mine={cell.Mine} data-clock={cell.OnClock} data-pos={cell.Position} data-gosx-live-bind-attr={cell.PosBindAttr}>
					<If cond={cell.OnClock}>
						<strong data-gosx-live-bind={cell.CellBindKey}>on the clock</strong>
					</If>
					<If cond={cell.OnClock == false && cell.Filled}>
						<strong data-gosx-live-bind={cell.CellBindKey}>{cell.PlayerName}</strong>
						<small>{cell.Label} · {cell.Position} · {cell.NFLTeam}<If cond={cell.IsAuto}> · AUTO</If></small>
					</If>
					<If cond={cell.OnClock == false && cell.Filled == false}>
						<small data-gosx-live-bind={cell.CellBindKey}>{cell.Label}<If cond={cell.Mine}> · you</If></small>
					</If>
				</div>
			</Each>
		</Each>
		</div>
	</>
}

// DraftByTeamProps wraps the by-team column slice: GSX component props
// must be a struct (a bare slice cannot be a component's prop type), so
// this is Teams alone, invoked with a named attribute rather than spread.
type DraftByTeamProps struct {
	Teams []TeamColumn
}

// DraftByTeam is the "By team" tab (D4): one team column per franchise, its
// own picks in draft order, then its roster-needs chips.
func DraftByTeam(props DraftByTeamProps) Node {
	return <div class="team-columns">
		<Each of={props.Teams} as="column">
			<section class="team-column" data-mine={column.Team.Mine}>
				<header class="team-column__head">
					<span class={"team-mark tone-" + column.Team.Tone}>{column.Team.Abbreviation}</span>
					<div class="team-column__identity">
						<strong class="team-column__name">{column.Team.Name}</strong>
						<small class="team-column__manager">{column.Team.Manager}</small>
					</div>
				</header>
				<div class="team-column__picks">
					<If cond={column.PicksEmpty}>
						<p class="team-column__picks-empty muted">Picks appear here as they are made.</p>
					</If>
					<Each of={column.Picks} as="pick">
						<article class="tape-row" data-tape-key={"team-pick-" + pick.Number} data-pick-number={pick.Number}>
							<span class="mono tape-row__slot">{pick.Label}</span>
							<div class="tape-row__player">
								<strong>{pick.PlayerName}</strong>
								<small><span class={"pos pos-" + pick.Position}>{pick.Position}</span> · {pick.NFLTeam}</small>
							</div>
						</article>
					</Each>
				</div>
				<div class="team-column__needs">
					<Each of={column.Needs} as="need">
						<If cond={need.open}><span class="need need--open">{need.label} {need.filled}/{need.total}</span></If>
						<If cond={need.open == false}><span class="need need--full">{need.label} {need.filled}/{need.total}</span></If>
					</Each>
				</div>
			</section>
		</Each>
	</div>
}

// DraftHistory is the pick-tape pane's swapped body (D4): exactly ONE of
// the Tape/Board/Teams views (item 1a, 2026-08-30 review — the server
// picks which, from "?view=", fragment.go's attachDraftFragmentView; the
// ShowTape/ShowBoard/ShowTeams bools this branches on are computed there,
// never a bare view-string compare inside the template itself), plus the
// completed draft's final ledger and CSV export link. The
// .draft-history__view--X wrapper classes and the draft-history-head
// segment's CSS :has() rules stay as a second, redundant safety net (T2's
// own board-expand rule also keys off the same radios): normally at most
// one wrapper exists in the DOM at all, since the server already sent
// only one view's markup.
// DraftHistoryBoardTeamsLedgerProps is Board/Teams/the Complete ledger
// only — split out of DraftHistory (review item 1, 2026-08-30) so that
// component's own two mode-specific branches (below) can share this
// identical markup instead of triplicating it.
type DraftHistoryBoardTeamsLedgerProps struct {
	ShowBoard bool
	ShowTeams bool
	Complete  bool
	Board     BoardView
	Teams     []TeamColumn
}

// Item 3 (wave-7 re-audit — yew): results-board-scroll (shared with
// /draft/results, same class, public/styles.css) makes this the grid's
// own bounded, both-axes scroll container — see DraftBoard's own doc
// comment below for why a single-axis-only, unbounded wrapper broke both
// of the grid's sticky headers.
func DraftHistoryBoardTeamsLedger(props DraftHistoryBoardTeamsLedgerProps) Node {
	return <>
		<If cond={props.ShowBoard}>
			<div class="draft-history__view draft-history__view--board results-board-scroll"><DraftBoard {...props.Board}></DraftBoard></div>
		</If>
		<If cond={props.ShowTeams}>
			<div class="draft-history__view draft-history__view--teams"><DraftByTeam Teams={props.Teams}></DraftByTeam></div>
		</If>
		<If cond={props.Complete}>
			<div class="draft-history__ledger">
				<span class="idx">FINAL LEDGER</span>
				<a class="btn btn-sm" href="/draft/ledger.csv">Export CSV</a>
			</div>
		</If>
	</>
}

// DraftHistory renders in one of two SEPARATE root elements, never one
// with conditionally-present attributes (review items 1/8, 2026-08-30):
// GoSX's rescan after a region swap (client/runtime/host/dom.ts's
// replaceRuntimeContent → gosxHost.regions.mount) rebinds a NESTED PLAIN
// region correctly, but never calls setupLiveRegions — a
// data-gosx-live-mode root nested inside ANY region (a parent's own
// replace-and-swap, this pane's OLD structure) dies the first time that
// region refetches, taking every board/available/mine bind with it for
// the rest of the session (proven live; board froze after one undo).
// TargetMode's own branch below therefore keeps its data-gosx-live-mode
// root OUTSIDE every region.
//
// Findings 1/2/3/6 (2026-08-30 review) replaced the tape's own TWO nested
// regions (an outer undo-scoped replace wrapping an inner pick/state-
// scoped prepend) with exactly ONE plain nested region: a growable
// prepend can only ever add rows, so it could never remove an undone
// pick, went stale the moment a real row landed above its own
// on-the-clock/"NO PICKS YET" placeholders, and (word for word what a
// browser proved) duplicated a WHOLE second .draft-history — live root
// and all — inside itself on its own sibling region's first undo-scoped
// refetch. A single REPLACE region (the default mode: no
// data-gosx-region-mode, no data-gosx-region-key, no growable-cursor token)
// nested inside the live root is safe by the same verified GoSX rule
// above (a plain region rebinds correctly after its own swap), and a
// full replace on every draft:pick/draft:undo/draft:state can never grow
// stale or duplicate anything — TapeRowsFragmentHandler's own endpoint
// (fragment.go) always answers with a fresh, complete, correctly-capped
// rows body. draft:state carries this same fresh body whether it is a
// regular full-pane resync OR sendDraftRepair's own coalesced repair off
// the queue-drop path (internal/league/draft_events.go, finding 3): the
// region does not distinguish the two, so an undone pick a client missed
// (a dropped draft:undo) still disappears the moment the trailing
// draft:state repair's own draft:state trigger re-fetches this region.
// Fallback's branch (DRAFT_LIVE_MODE=fallback, TargetMode false) restores
// the pre-Task-8 shape exactly: no live root, no nested regions — the
// WHOLE pane refetches on every draft:pick/undo/state through Page()'s
// own outer .draft-pane__body region instead (see Page()'s own
// two-branch history pane).
func DraftHistory(props DraftHistoryProps) Node {
	return <>
	<If cond={props.TargetMode}>
	<div class="draft-history" data-latest={props.Latest} data-gosx-live-mode="event" data-gosx-live-src={props.LiveSrc} data-gosx-live-hub={props.LiveHub} data-gosx-live-on="draft:pick draft:undo draft:state">
		<p class="draft-region-stale mono" role="status">Pick history did not update. <a href={props.RoomPath}>Refresh room →</a></p>
		<If cond={props.ShowTape}>
			<div class="draft-history__view draft-history__view--tape">
				<div class="tape" id="tape-latest">
					<div class="draft-tape-rows" data-gosx-region data-gosx-region-url={props.TapeURL} data-gosx-region-on="draft:pick draft:undo draft:state">
						<DraftTapeRows Rounds={props.Rounds} Since={0} HasOnClock={props.HasOnClock} NextLabel={props.NextLabel} OnClockName={props.OnClockName} OnClockAbbr={props.OnClockAbbr} OnClockTone={props.OnClockTone} OnClockHasAvatarImage={props.OnClockHasAvatarImage} OnClockAvatarImageURL={props.OnClockAvatarImageURL} RoundsEmpty={props.RoundsEmpty} HasOlderRounds={props.HasOlderRounds} OlderHref={props.OlderHref}></DraftTapeRows>
					</div>
				</div>
			</div>
		</If>
		<DraftHistoryBoardTeamsLedger ShowBoard={props.ShowBoard} ShowTeams={props.ShowTeams} Complete={props.Complete} Board={props.Board} Teams={props.Teams}></DraftHistoryBoardTeamsLedger>
	</div>
	</If>
	<If cond={props.TargetMode == false}>
	<div class="draft-history" data-latest={props.Latest}>
		<p class="draft-region-stale mono" role="status">Pick history did not update. <a href={props.RoomPath}>Refresh room →</a></p>
		<If cond={props.ShowTape}>
			<div class="draft-history__view draft-history__view--tape">
				<div class="tape draft-tape-rows" id="tape-latest">
					<DraftTapeRows Rounds={props.Rounds} Since={0} HasOnClock={props.HasOnClock} NextLabel={props.NextLabel} OnClockName={props.OnClockName} OnClockAbbr={props.OnClockAbbr} OnClockTone={props.OnClockTone} OnClockHasAvatarImage={props.OnClockHasAvatarImage} OnClockAvatarImageURL={props.OnClockAvatarImageURL} RoundsEmpty={props.RoundsEmpty} HasOlderRounds={props.HasOlderRounds} OlderHref={props.OlderHref}></DraftTapeRows>
				</div>
			</div>
		</If>
		<DraftHistoryBoardTeamsLedger ShowBoard={props.ShowBoard} ShowTeams={props.ShowTeams} Complete={props.Complete} Board={props.Board} Teams={props.Teams}></DraftHistoryBoardTeamsLedger>
	</div>
	</If>
	</>
}

// Page (review item 8, 2026-08-30): every pane below renders one of TWO
// separate wrapper elements, gated on data.live_mode — never one element
// with conditionally-present region/live-mode attributes — mirroring
// DraftHistory's own two-branch rule (its own doc comment explains why a
// live root and a region can never safely share one element's fate).
// Target mode's command/available/mine wrappers carry data-gosx-live-mode
// with NO ancestor region, so a region swap elsewhere on the page can
// never orphan them; fallback (DRAFT_LIVE_MODE=fallback) restores the
// pre-Task-8 data-gosx-region*-on-every-pane wiring exactly.
//
// Findings 4/5 (2026-08-30 review): the command header's own live root
// now lists draft:seat alongside draft:pick/undo/clock/state, so its
// room.* binds (the room-count summary) stay current after a seat's
// ready/autopick toggle, not only after a pick/undo/clock tick. The mine
// pane's own inner region likewise now lists draft:pick/undo/seat/state
// (it carried NO data-gosx-region-on at all before this fix, so it never
// refetched on anything) — "Roster needs" and "AUTOPICK · ON/OFF" are
// both server-computed off DraftMyTeam's Roster view, so a region
// refetch, not a live bind, is what keeps them current.
//
// gap-audit item 8 (wave 3): /draft had no h1 at all (DraftRoom's own
// "BUILD THE FUTURE." h1 belongs to a different, unrendered component --
// see that component's doc comment). wave-6 item 2a: that first fix hid
// the one h1 (.visually-hidden), so a sighted user still saw no page
// title anywhere — the only visible round/pick numbers lived inside
// DraftCommandBar's own props.Data (data.command), which can read stale
// or clamped relative to this page's own authoritative data.round/
// data.pick_number/data.picks_total. The h1 is now the document's single
// one AND the visible title, reading this page's own authoritative
// fields directly, not data.command's copy — one copy per <header
// class="draft-command" ...> branch below (target/fallback), never a
// SIBLING of either: an h1 sitting between .draft-notice and the header
// would add a sixth top-level child to .draft-shell's own explicit
// grid-template-rows track list, which is sized for exactly the five
// existing children (notice, command, tabbar, panes, pickbar) — see
// the ≤56.1875rem block's own comment on that grid. Inside the fallback
// branch's header, the h1 sits ahead of a NEW inner div that now alone
// carries data-gosx-region: CommandFragmentHandler's own response is
// still DraftCommandBar's .draft-command__inner alone (fragment_test.go
// pins that exact class), so had the h1 stayed a direct child of the
// region-carrying header itself, it would render once at first load and
// vanish on the header's first region-swap. Sentence case, matching
// every other heading on this page (h2 "Available now", "Pick history",
// etc.).

// DraftPreflight is the pre-draft "get your seat ready" checklist (D4,
// spruce audit): it used to open DraftAvailable's own swapped body,
// pushing the pool pane's head row and every .avail-row 500+ px below
// the pane's own top edge — the pool showed ZERO players on first paint
// even though every row was already in the response. It now sits ABOVE
// .draft-panes, a compact strip of its own (a new "draft-preflight"
// class — redwood's own CSS names the exact layout; the inner
// .checklist/.checklist-item classes are unchanged, so their existing
// rules keep applying), so the available pane always starts with its
// head row on every viewport, pre-draft included. It renders only while
// data.draft.started is false (Page() gates the call) and reads the
// exact same data.viewer/data.public_entry fields DraftAvailable's own
// checklist used to read off props.Data — Page()'s "data" carries every
// key prepareDraftData copies into viewData, so nothing new needed
// wiring from page.server.go.
//
// Item 3's copy (D15) no longer claims a first click "arms" the chime:
// the command bar's Sound button already renders aria-pressed="true" by
// default (DraftCommandBar, below), so a checklist claiming sound starts
// OFF disagreed with the control that says it is already ON. The one
// click is real (browser autoplay policy gates audio until a page
// gesture), but that is a browser permission, not a feature toggle this
// page controls — the copy now describes it that way instead of
// contradicting the button.
type DraftPreflightProps struct {
	Data map[string]any
}

func DraftPreflight(props DraftPreflightProps) Node {
	return <details class="draft-preflight" aria-labelledby="draft-preflight-title">
		<summary class="draft-preflight__summary">
			<span class="section-index">BEFORE THE ROOM OPENS</span>
			<h2 id="draft-preflight-title">Get your seat ready</h2>
			<small class="draft-preflight__hint mono">Open the checklist</small>
		</summary>
		<div class="checklist">
			<div class="checklist-item">
				<span class="checklist-mark mono">01</span>
				<div class="checklist-item__text">
					<strong>Build your big board</strong>
					<small>Rank your targets now. Autopick and the pool both read your board first.</small>
				</div>
				<a href="/board" data-gosx-link class="board-button">Open board →</a>
			</div>
			<If cond={props.Data.practice.allowed}>
				<div class="checklist-item checklist-item--practice">
					<span class="checklist-mark mono">02</span>
					<div class="checklist-item__text">
						<strong>Practice the draft room</strong>
						<small>Take a few picks on the clock against the other seats, played by bots. Nothing you do there counts.</small>
					</div>
					<a href={props.Data.practice.href} data-gosx-link class="board-button">Practice now →</a>
				</div>
			</If>
			<If cond={props.Data.practice.allowed == false && props.Data.viewer.has_seat}>
				<div class="checklist-item checklist-item--practice">
					<span class="checklist-mark mono">02</span>
					<div class="checklist-item__text">
						<strong>Practice the draft room</strong>
						<small>{props.Data.practice.reason}</small>
					</div>
					<span class="board-button board-button--disabled" aria-disabled="true">Practice unavailable</span>
				</div>
			</If>
			<If cond={props.Data.viewer.has_seat}>
				<div class="checklist-item">
					<span class="checklist-mark mono">03</span>
					<div class="checklist-item__text">
						<strong>Check in as ready</strong>
						<small>Mark yourself ready after your Big Board is set. Then keep this tab open so the commissioner can also see that you are HERE.</small>
					</div>
					<a href="#ready-toggle" class="board-button">Check in now ↑</a>
				</div>
			</If>
			<If cond={props.Data.viewer.has_seat == false && props.Data.public_entry.can_claim}>
				<div class="checklist-item">
					<span class="checklist-mark mono">03</span>
					<div class="checklist-item__text">
						<strong>Claim a franchise</strong>
						<small>{props.Data.public_entry.detail}</small>
					</div>
					<a href={props.Data.public_entry.action_href} data-gosx-link class="board-button">{props.Data.public_entry.action_label}</a>
				</div>
			</If>
			<If cond={props.Data.viewer.has_seat == false && props.Data.public_entry.can_claim == false}>
				<div class="checklist-item">
					<span class="checklist-mark mono">03</span>
					<div class="checklist-item__text">
						<strong>{props.Data.public_entry.state_label}</strong>
						<small>{props.Data.public_entry.detail}</small>
					</div>
					<a href={props.Data.public_entry.action_href} data-gosx-link class="board-button">{props.Data.public_entry.action_label}</a>
					<If cond={props.Data.public_entry.admitted == false}>
						<a href="/pickem" data-gosx-link class="board-button">Open Pick'em HQ →</a>
					</If>
					<If cond={props.Data.public_entry.admitted && props.Data.public_entry.league_full}>
						<a href="/players" data-gosx-link class="board-button">Browse player pool →</a>
					</If>
				</div>
			</If>
			<div class="checklist-item">
				<span class="checklist-mark mono">04</span>
				<div class="checklist-item__text">
					<strong>Keep this tab open with sound on</strong>
					<small>Sound is already on. Click anywhere on the page once so your browser allows the on-clock chime to play.</small>
				</div>
			</div>
			<If cond={props.Data.viewer.has_seat}>
				<div class="checklist-item">
					<span class="checklist-mark mono">05</span>
					<div class="checklist-item__text">
						<strong>Autopick covers you if you disappear</strong>
						<small>Turn it on before the draft if you might miss your pick.</small>
					</div>
					<a href="#autopick-toggle" class="board-button">Autopick toggle ↑</a>
				</div>
			</If>
		</div>
	</details>
}

func Page() Node {
	return <main class={"draft-shell" + data.shell_modifier} id="main-content" data-draft-live-mode={data.live_mode}>
		<div class="draft-notice" aria-live="polite">
			<If cond={data.has_notice}><p class="flash-message">{data.notice}</p></If>
			<If cond={data.has_pick_error}><p class="error-message">{data.pick_error}</p></If>
		</div>
		<If cond={data.live_mode == "target"}>
		<header class="draft-command" data-gosx-live-mode="event" data-gosx-live-src={data.live_src} data-gosx-live-hub={data.live_hub} data-gosx-live-on="draft:pick draft:undo draft:clock draft:seat draft:state">
			<If cond={data.practice.active}><h1 class="draft-command__title">Practice draft · Round {data.round} · Pick {data.pick_number} of {data.picks_total}</h1></If>
			<If cond={data.practice.active == false && data.draft.started}><h1 class="draft-command__title">Draft room · Round {data.round} · Pick {data.pick_number} of {data.picks_total}</h1></If>
			<If cond={data.practice.active == false && data.draft.started == false}><h1 class="draft-command__title">Draft room · {data.draft.opens_label}</h1></If>
			<If cond={data.practice.active}>
				<div class="draft-practice-region" data-gosx-region data-gosx-region-url={data.fragment_base + "/practice"} data-gosx-region-on="draft:pick draft:undo draft:clock draft:state">
					<DraftPracticeStrip {...data.command}></DraftPracticeStrip>
				</div>
			</If>
			<DraftCommandBar {...data.command}></DraftCommandBar>
		</header>
		</If>
		<If cond={data.live_mode != "target"}>
		<header class="draft-command">
			<If cond={data.practice.active}><h1 class="draft-command__title">Practice draft · Round {data.round} · Pick {data.pick_number} of {data.picks_total}</h1></If>
			<If cond={data.practice.active == false && data.draft.started}><h1 class="draft-command__title">Draft room · Round {data.round} · Pick {data.pick_number} of {data.picks_total}</h1></If>
			<If cond={data.practice.active == false && data.draft.started == false}><h1 class="draft-command__title">Draft room · {data.draft.opens_label}</h1></If>
			<If cond={data.practice.active}>
				<div class="draft-practice-region" data-gosx-region data-gosx-region-url={data.fragment_base + "/practice"} data-gosx-region-on="draft:pick draft:undo draft:clock draft:state">
					<DraftPracticeStrip {...data.command}></DraftPracticeStrip>
				</div>
			</If>
			<div data-gosx-region data-gosx-region-url={data.fragment_base + "/command"} data-gosx-region-signal="$draft.state.refresh" data-gosx-region-on="draft:pick draft:undo draft:clock draft:state">
				<DraftCommandBar {...data.command}></DraftCommandBar>
			</div>
		</header>
		</If>
		<DraftMobileTabs Complete={data.draft.complete} ShowBoard={data.history_view_board} ShowTeams={data.history_view_teams} TapeExplicit={data.history_tape_explicit} PicksHref={data.history_tape_href} BoardHref={data.history_board_href} TeamsHref={data.history_teams_href}></DraftMobileTabs>
		<div class="draft-panes" data-history-board={data.history_view_board}>
			<If cond={data.draft.started == false}>
				<DraftPreflight Data={data}></DraftPreflight>
			</If>
			<section class="draft-pane draft-pane--history" aria-labelledby="draft-history-title">
				<DraftHistoryHead Started={data.draft.started} Complete={data.draft.complete} ShowTape={data.history_view_tape} ShowBoard={data.history_view_board} ShowTeams={data.history_view_teams} TapeHref={data.history_tape_href} BoardHref={data.history_board_href} TeamsHref={data.history_teams_href}></DraftHistoryHead>
				<If cond={data.live_mode == "target"}>
				<div class="draft-pane__body">
					<DraftHistory {...data.history}></DraftHistory>
				</div>
				</If>
				<If cond={data.live_mode != "target"}>
				<div class="draft-pane__body" data-gosx-region data-gosx-region-url={data.history_tape_url} data-gosx-region-on="draft:pick draft:undo draft:state">
					<DraftHistory {...data.history}></DraftHistory>
				</div>
				</If>
			</section>
			<section class="draft-pane draft-pane--available" aria-labelledby="draft-available-title">
				<DraftAvailableHead RoomPath={data.room_path} SearchPlaceholder={data.available_search_placeholder} Query={data.pool_query} Position={data.pool_position} Sort={data.pool_sort} Positions={data.pool_position_chips} SortOptions={data.pool_sort_options}></DraftAvailableHead>
				<If cond={data.live_mode == "target"}>
				<div class="draft-pane__body" data-gosx-live-mode="event" data-gosx-live-src={data.live_src} data-gosx-live-hub={data.live_hub} data-gosx-live-on="draft:pick draft:undo draft:state">
					<div id="draft-available-list" data-gosx-region data-gosx-region-url={data.fragment_base + "/available?pos={value}&sort=" + data.pool_sort} data-gosx-region-signal="$draft.available.pos" data-gosx-region-allow-empty>
						<DraftAvailable {...data.available}></DraftAvailable>
					</div>
				</div>
				</If>
				<If cond={data.live_mode != "target"}>
				<div id="draft-available-list" class="draft-pane__body" data-gosx-region data-gosx-region-url={data.fragment_base + "/available?pos={value}&sort=" + data.pool_sort} data-gosx-region-signal="$draft.available.pos" data-gosx-region-allow-empty data-gosx-region-on="draft:pick draft:undo draft:state">
					<DraftAvailable {...data.available}></DraftAvailable>
				</div>
				</If>
			</section>
			<section class="draft-pane draft-pane--mine" aria-labelledby="draft-mine-title">
				<If cond={data.live_mode == "target"}>
				<div class="draft-pane__body" data-gosx-live-mode="event" data-gosx-live-src={data.live_src} data-gosx-live-hub={data.live_hub} data-gosx-live-on="draft:pick draft:undo draft:seat draft:state">
					<div data-gosx-region data-gosx-region-url={data.fragment_base + "/queue"} data-gosx-region-on="draft:pick draft:undo draft:seat draft:state">
						<DraftMyTeam {...data.queue}></DraftMyTeam>
					</div>
				</div>
				</If>
				<If cond={data.live_mode != "target"}>
				<div class="draft-pane__body" data-gosx-region data-gosx-region-url={data.fragment_base + "/queue"} data-gosx-region-on="draft:pick draft:undo draft:state draft:seat">
					<DraftMyTeam {...data.queue}></DraftMyTeam>
				</div>
				</If>
			</section>
		</div>
		<DraftPickBar {...data.available}></DraftPickBar>
		<If cond={data.viewer.is_commissioner}><DraftCommissionerDrawer {...data.command}></DraftCommissionerDrawer></If>
	</main>
}

