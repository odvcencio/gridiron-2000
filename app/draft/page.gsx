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
	Headshot        string
	HasHeadshot     bool
	Jersey          string
	HasBreakdown    bool
	Breakdown       []DraftBreakdownRow
	BreakdownTotal  string
	HasHist         bool
	Hist            string
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
							</If>
						</div>
					</details>
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
							<button class="draft-button" type="button" disabled="disabled" title="Choose a player who keeps every required starter slot fillable">Roster need</button>
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
		<p class="draft-region-stale mono" role="status">The room did not update. This is the last confirmed board. <a href="/draft">Refresh room →</a></p>
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
					<br></br>
					THE FUTURE.
				</h1>
				<p>{props.Data.draft.format}. {props.Data.draft.status_note}</p>
			</div>
			<div class="draft-clock-panel">
				<If cond={props.Data.draft.started == false}>
					<span>Scheduled window</span>
					<strong
					class="mono"
					data-gosx-countdown={props.Data.draft.at}
					data-gosx-countdown-format="dhms"
					>{props.Data.draft.countdown_label}</strong>
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
				<span class="section-index">
					ROUND
					{props.Data.round}
					// SNAKE ORDER
				</span>
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
						<strong>Presence is observational. AUTO is authority.</strong>
						<p>HERE, IDLE, and AWAY retain the normal pick clock. NOT SEEN may receive the short safety clock only after the two-minute boot grace. Set AUTO for a known absence; its explicit grace then follows the seat's Big Board.</p>
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
		<p class="draft-region-stale mono" role="status">LIVE UPDATE FAILED · SHOWING LAST CONFIRMED PLAYER POOL AND PICK TAPE. <a href="/draft">Refresh workspace →</a></p>
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
						<span class="section-index">YOUR BOARD</span>
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
						<a href="/board" data-gosx-link class="mono">BUILD YOUR BOARD →</a>
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
	Actions           map[string]string
}

type DraftAvailableHeadProps struct {
	SearchPlaceholder string
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
func DraftCommandBar(props DraftCommandBarProps) Node {
	return <div class="draft-command__inner">
		<p class="draft-region-stale mono" role="status">The room did not update. This is the last confirmed state. <a href="/draft">Refresh room →</a></p>
		<p class="visually-hidden" role="status" aria-live="polite" aria-atomic="true">{props.StatusSummary}</p>
		<div class="draft-command__pick">
			<span class="idx">ROUND <span data-gosx-live-bind="pick.round">{props.Data.round}</span> · SNAKE <span data-gosx-live-bind="pick.direction">{props.Data.snake_direction}</span></span>
			<span class="mono draft-command__number" data-pick-label>PICK <span data-gosx-live-bind="pick.number">{props.Data.pick_number}</span> <span class="muted">/ <span data-gosx-live-bind="pick.total">{props.Data.picks_total}</span></span></span>
		</div>
		<div class="draft-command__turn">
			<If cond={props.Data.draft.complete == false}>
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
				<If cond={props.Data.draft.complete}>
					<span class="idx">Draft complete</span>
					<strong class="display">Every pick is locked</strong>
				</If>
				<If cond={props.Data.draft.complete == false}>
					<If cond={props.Data.viewer_on_clock}><span class="idx idx--hot">You are on the clock</span></If>
					<If cond={props.Data.viewer_on_clock == false}><span class="idx">On the clock</span></If>
					<strong class="display" data-gosx-live-bind="onclock.name">{props.Data.on_clock.name}</strong>
					<small class="muted">Next: {props.Data.next_team.name} · then {props.Data.after_next_team.name}</small>
				</If>
			</div>
		</div>
		<div class="draft-command__clock" data-clock-state={props.Data.clock.state} data-gosx-live-bind-attr="data-clock-state:clock.state">
			<If cond={props.Data.draft.started == false}>
				<strong class="pick-clock mono" data-pick-clock data-gosx-countdown={props.Data.draft.at} data-gosx-countdown-format="dhms" aria-live="off">{props.Data.draft.countdown_label}</strong>
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
				<If cond={props.Data.draft.complete == false}>
					<span class="idx">of <span data-gosx-live-bind="clock.duration_label">{props.Data.clock.duration_label}</span></span>
				</If>
			</If>
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
		<If cond={props.Data.banner != ""}>
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
				<p class="draft-drawer__help">Presence is observational. AUTO is authority. HERE, IDLE, and AWAY retain the normal pick clock. NOT SEEN may receive the short safety clock only after the two-minute boot grace. Set AUTO for a known absence; its explicit grace then follows the seat's Big Board.</p>
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
	// PicksHref/TeamsHref (item 4/6, 2026-08-30 review): both tabs are
	// plain data-gosx-link navigations, like Teams already was — Picks
	// forces "?view=tape" so its own pane content is never whatever the
	// viewer had last navigated to on desktop (Board or Teams), the bug a
	// pure CSS-only radio toggle could show under the wrong label.
	PicksHref string
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
func DraftMobileTabs(props DraftMobileTabsProps) Node {
	return <nav class="draft-tabbar" aria-label="Draft room panels">
		<input type="radio" name="draft-tab" id="tab-players" class="visually-hidden" checked={props.ShowTeams == false && props.ShowBoard == false && props.Complete == false && props.TapeExplicit == false}></input>
		<label class="draft-tabbar__tab" for="tab-players">Players</label>
		<input type="radio" name="draft-tab" id="tab-queue" class="visually-hidden"></input>
		<label class="draft-tabbar__tab" for="tab-queue">Queue</label>
		<input type="radio" name="draft-tab" id="tab-picks" class="visually-hidden" checked={props.ShowTeams == false && (props.ShowBoard || props.Complete || props.TapeExplicit)}></input>
		<a class="draft-tabbar__tab" href={props.PicksHref} data-gosx-link aria-current={props.ShowTeams == false && (props.ShowBoard || props.Complete || props.TapeExplicit)}>Picks</a>
		<input type="radio" name="draft-tab" id="tab-teams" class="visually-hidden" checked={props.ShowTeams}></input>
		<a class="draft-tabbar__tab" href={props.TeamsHref} data-gosx-link aria-current={props.ShowTeams}>Teams</a>
	</nav>
}

// DraftPickBar is the seated manager's sticky mobile action strip, docked
// above the tab bar (V1): the viewer's own top still-draftable queue
// target with one Draft button when it is their turn, or (the rest of the
// time) their own ready/autopick status and toggle — the same controls
// desktop keeps in pane 3's Room tab (DraftMyTeam) — so a phone never
// shows an empty gap between the panes and the tab bar, and nothing about
// ready/autopick ever renders between the command bar and the panes.
func DraftPickBar(props DraftAvailableProps) Node {
	return <If cond={props.Data.viewer.has_seat && props.Data.draft.complete == false}>
		<div class="draft-pickbar">
			<If cond={props.Data.draft.started && props.Data.viewer_on_clock && props.Data.next_queued.has}>
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
			<If cond={(props.Data.draft.started && props.Data.viewer_on_clock && props.Data.next_queued.has) == false && props.Data.viewer_ready == false}>
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
			<If cond={(props.Data.draft.started && props.Data.viewer_on_clock && props.Data.next_queued.has) == false && props.Data.viewer_ready && props.Data.viewer_autopick == false}>
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
			<If cond={(props.Data.draft.started && props.Data.viewer_on_clock && props.Data.next_queued.has) == false && props.Data.viewer_ready && props.Data.viewer_autopick}>
				<div>
					<span class="idx">Ready ✓</span>
					<strong>Autopick on</strong>
				</div>
			</If>
		</div>
	</If>
}

// DraftAvailableHead is the available pane's fixed head: the client-side
// search filter (data-gosx-filter targets the region's own id, so the
// input itself sits outside the swapped subtree and survives every
// refetch) and the position chips that drive the region's own refetch
// signal.
func DraftAvailableHead(props DraftAvailableHeadProps) Node {
	return <div class="draft-available-head">
		<h2 id="draft-available-title" class="visually-hidden">Available players</h2>
		<input id="draft-search" type="search" class="draft-search" placeholder={props.SearchPlaceholder} data-gosx-filter="draft-available-list" data-gosx-filter-announce="true" />
		<div class="draft-available-head__chips">
			<button type="button" class="chip" data-gosx-set="$draft.available.pos" data-gosx-set-value="" aria-pressed="true">ALL</button>
			<button type="button" class="chip" data-gosx-set="$draft.available.pos" data-gosx-set-value="QB">QB</button>
			<button type="button" class="chip" data-gosx-set="$draft.available.pos" data-gosx-set-value="RB">RB</button>
			<button type="button" class="chip" data-gosx-set="$draft.available.pos" data-gosx-set-value="WR">WR</button>
			<button type="button" class="chip" data-gosx-set="$draft.available.pos" data-gosx-set-value="TE">TE</button>
			<button type="button" class="chip" data-gosx-set="$draft.available.pos" data-gosx-set-value="K">K</button>
			<button type="button" class="chip" data-gosx-set="$draft.available.pos" data-gosx-set-value="DST">DST</button>
		</div>
	</div>
}

// DraftAvailable is the available-players pane's swapped body: the pool
// grid pre/live, or the pre-draft checklist and the post-draft callout in
// place of it.
func DraftAvailable(props DraftAvailableProps) Node {
	return <div class="draft-available" data-has-adp={props.Data.has_adp}>
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
		<div class="avail-row avail-row--head">
			<span class="idx">RK</span><span class="idx">PLAYER</span><span class="idx">POS</span><span class="idx">PROJ</span><If cond={props.Data.has_adp}><span class="idx">VS ADP</span></If><span class="idx">ACTION</span>
		</div>
		<Each of={props.Players} as="player">
			<article class="avail-row" data-player-id={player.ID} data-gosx-filter-text={player.Search} data-taken={player.Taken} data-gosx-live-bind-attr={"data-taken:player." + player.ID + ".taken"}>
				<span class="num">{player.Rank}<If cond={player.HasHouseRank}><small class="house-rank">{player.HouseRank}</small></If></span>
				<div><strong>{player.Name}</strong><small>{player.Detail}</small></div>
				<span class={"pos pos-" + player.Position}>{player.Position}</span>
				<span class="num">{player.Projection}</span>
				<If cond={props.Data.has_adp}><span class="num">{player.ValueLabel}</span></If>
				<div class="avail-row__actions">
				<If cond={props.Data.viewer.has_seat}>
					<form method="post" action={props.QueueAddAction} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<input type="hidden" name="player_id" value={player.ID}></input>
						<input type="hidden" name="pos" value={props.Data.pool_position}></input>
						<input type="hidden" name="q" value={props.Data.pool_query}></input>
						<input type="hidden" name="page" value={props.Data.pool_page}></input>
						<button class="btn btn-sm" type="submit">+ Queue</button>
					</form>
					<form method="post" action={props.MakePickAction} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<input type="hidden" name="team_id" value={props.Data.viewer.team_id}></input>
						<input type="hidden" name="player_id" value={player.ID}></input>
						<input type="hidden" name="pos" value={props.Data.pool_position}></input>
						<input type="hidden" name="q" value={props.Data.pool_query}></input>
						<input type="hidden" name="page" value={props.Data.pool_page}></input>
						<If cond={props.Data.can_pick && player.CanDraft}>
							<button class="btn btn-sm btn-primary" type="submit">Draft</button>
						</If>
						<If cond={props.Data.can_pick && player.CanDraft == false}>
							<button class="btn btn-sm" type="button" disabled="disabled" title="Choose a player who keeps every required starter slot fillable">Roster need</button>
						</If>
					</form>
				</If>
				</div>
			</article>
		</Each>
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
			<label class="segment__option" for="mine-queue">Queue</label>
			<input type="radio" name="draft-mine-view" id="mine-roster" class="visually-hidden"></input>
			<label class="segment__option" for="mine-roster">Roster</label>
			<input type="radio" name="draft-mine-view" id="mine-room" class="visually-hidden"></input>
			<label class="segment__option" for="mine-room">Room</label>
		</div>
		<div class="draft-mine__view draft-mine__view--queue">
			<div class="pool-list" data-gosx-reorder data-gosx-reorder-action="POST /draft/queue" data-gosx-csrf-token={props.CSRF}>
				<Each of={props.Queue} as="player">
					<article class="q-row" data-gosx-reorder-item={player.ID} data-taken={player.Taken} data-gosx-live-bind-attr={"data-taken:queue." + player.ID + ".taken"}>
						<span class="board-row__handle" data-gosx-reorder-handle aria-label={"Reorder " + player.Name}>⠿</span>
						<span class="mono">{player.Rank}</span>
						<div><strong>{player.Name}</strong><small>{player.Position} · {player.NFLTeam} · proj {player.Projection}</small></div>
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
					</article>
				</Each>
			</div>
			<If cond={props.Data.queue_empty}>
				<div class="board-peek-empty"><a href="/board" data-gosx-link class="mono">BUILD YOUR BOARD →</a></div>
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
				<span data-gosx-live-bind="room.here">{props.Data.here_count}</span>/<span data-gosx-live-bind="room.managers">{props.Data.manager_count}</span> here · <span data-gosx-live-bind="room.ready">{props.Data.ready_count}</span>/<span data-gosx-live-bind="room.managers">{props.Data.manager_count}</span> ready · <span data-gosx-live-bind="room.auto">{props.Data.auto_count}</span> auto
				<If cond={props.Data.your_pick_in > 0}>
					<span class="draft-mine__yourpick" data-gosx-live-bind={props.Data.yourpick_bind_key}>your pick in {props.Data.your_pick_in}</span>
				</If>
			</div>
			<If cond={props.Data.viewer.has_seat && props.Data.draft.complete == false}>
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
			<nav class="segment" aria-label="Pick history panels">
				<a class="segment__option" href={props.TapeHref} data-gosx-link aria-current={props.ShowTape}>Tape</a>
				<a class="segment__option" href={props.BoardHref} data-gosx-link aria-current={props.ShowBoard}>Board</a>
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
				<span class="mono">#{props.Number}</span>
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
		<p class="mono muted">Drafted by {props.TeamName} · {props.Manager}</p>
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
// then one sticky round header and one board-cell row per round. The pane
// owns overflow: auto so the grid scrolls both ways at phone width. The
// header shows the team's full name under its badge, wrapped over up to
// two lines (mockup divergence, 2026-08-30 review — it used to truncate
// to one ellipsized line, "Kern…"); a filled cell reads "1.01 · WR · CIN"
// (label · position · NFL team, PickBoard.dc.html — the NFL team was
// missing before this same pass).
func DraftBoard(props BoardView) Node {
	return <div class="board-grid" style={"--board-columns:" + props.ColumnCount}>
		<div class="board-grid__corner"></div>
		<Each of={props.Columns} as="column">
			<div class="board-grid__team" data-mine={column.mine}>
				<span class={"team-mark tone-" + column.tone}>{column.abbreviation}</span>
				<span class="board-grid__name">{column.name}<If cond={column.mine}> · you</If></span>
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
					<div><strong>{column.Team.Name}</strong><small>{column.Team.Manager}</small></div>
				</header>
				<div class="team-column__picks">
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

func DraftHistoryBoardTeamsLedger(props DraftHistoryBoardTeamsLedgerProps) Node {
	return <>
		<If cond={props.ShowBoard}>
			<div class="draft-history__view draft-history__view--board"><DraftBoard {...props.Board}></DraftBoard></div>
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
	<div class="draft-history" data-latest={props.Latest} data-gosx-live-mode="event" data-gosx-live-src="/draft/live.json" data-gosx-live-hub="draft-live" data-gosx-live-on="draft:pick draft:undo draft:state">
		<p class="draft-region-stale mono" role="status">Pick history did not update. <a href="/draft">Refresh room →</a></p>
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
		<p class="draft-region-stale mono" role="status">Pick history did not update. <a href="/draft">Refresh room →</a></p>
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
func Page() Node {
	return <main class={"draft-shell" + data.shell_modifier} id="main-content" data-draft-live-mode={data.live_mode}>
		<div class="draft-notice" aria-live="polite">
			<If cond={data.has_notice}><p class="flash-message">{data.notice}</p></If>
			<If cond={data.has_pick_error}><p class="error-message">{data.pick_error}</p></If>
		</div>
		<If cond={data.live_mode == "target"}>
		<header class="draft-command" data-gosx-live-mode="event" data-gosx-live-src="/draft/live.json" data-gosx-live-hub="draft-live" data-gosx-live-on="draft:pick draft:undo draft:clock draft:seat draft:state">
			<DraftCommandBar {...data.command}></DraftCommandBar>
		</header>
		</If>
		<If cond={data.live_mode != "target"}>
		<header class="draft-command" data-gosx-region data-gosx-region-url="/draft/fragment/command" data-gosx-region-signal="$draft.state.refresh" data-gosx-region-on="draft:pick draft:undo draft:clock draft:state">
			<DraftCommandBar {...data.command}></DraftCommandBar>
		</header>
		</If>
		<DraftMobileTabs Complete={data.draft.complete} ShowBoard={data.history_view_board} ShowTeams={data.history_view_teams} TapeExplicit={data.history_tape_explicit} PicksHref={data.history_tape_href} TeamsHref={data.history_teams_href}></DraftMobileTabs>
		<div class="draft-panes" data-history-board={data.history_view_board}>
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
				<DraftAvailableHead SearchPlaceholder={data.available_search_placeholder}></DraftAvailableHead>
				<If cond={data.live_mode == "target"}>
				<div class="draft-pane__body" data-gosx-live-mode="event" data-gosx-live-src="/draft/live.json" data-gosx-live-hub="draft-live" data-gosx-live-on="draft:pick draft:undo draft:state">
					<div id="draft-available-list" data-gosx-region data-gosx-region-url="/draft/fragment/available?pos={value}" data-gosx-region-signal="$draft.available.pos" data-gosx-region-allow-empty>
						<DraftAvailable {...data.available}></DraftAvailable>
					</div>
				</div>
				</If>
				<If cond={data.live_mode != "target"}>
				<div id="draft-available-list" class="draft-pane__body" data-gosx-region data-gosx-region-url="/draft/fragment/available?pos={value}" data-gosx-region-signal="$draft.available.pos" data-gosx-region-allow-empty data-gosx-region-on="draft:pick draft:undo draft:state">
					<DraftAvailable {...data.available}></DraftAvailable>
				</div>
				</If>
			</section>
			<section class="draft-pane draft-pane--mine" aria-labelledby="draft-mine-title">
				<If cond={data.live_mode == "target"}>
				<div class="draft-pane__body" data-gosx-live-mode="event" data-gosx-live-src="/draft/live.json" data-gosx-live-hub="draft-live" data-gosx-live-on="draft:pick draft:undo draft:seat draft:state">
					<div data-gosx-region data-gosx-region-url="/draft/fragment/queue" data-gosx-region-on="draft:pick draft:undo draft:seat draft:state">
						<DraftMyTeam {...data.queue}></DraftMyTeam>
					</div>
				</div>
				</If>
				<If cond={data.live_mode != "target"}>
				<div class="draft-pane__body" data-gosx-region data-gosx-region-url="/draft/fragment/queue" data-gosx-region-on="draft:pick draft:undo draft:state draft:seat">
					<DraftMyTeam {...data.queue}></DraftMyTeam>
				</div>
				</If>
			</section>
		</div>
		<DraftPickBar {...data.available}></DraftPickBar>
		<If cond={data.viewer.is_commissioner}><DraftCommissionerDrawer {...data.command}></DraftCommissionerDrawer></If>
	</main>
}

