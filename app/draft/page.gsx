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
	Detail          string
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
					<span class="pool-rank mono">{player.Rank}</span>
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
						<b class="mono"><span class="live-dot live-dot--bound" aria-hidden="true">LIVE</span> LIVE LOG</b>
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

func Page() Node {
	return <main class="page draft-page" id="main-content">
		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_notice}><p class="flash-message">{data.notice}</p></If>
			<If cond={data.has_pick_error}><p class="error-message">{data.pick_error}</p></If>
		</div>
		<div class="draft-region" data-gosx-region data-gosx-region-url="/draft/fragment/room" data-gosx-region-signal="$draft.state.refresh" data-gosx-region-on="draft:pick draft:undo draft:state draft:clock">
			<DraftRoom {...data.room}></DraftRoom>
		</div>
		<div class="draft-region" data-gosx-region data-gosx-region-url={data.workspace_fragment_url} data-gosx-region-signal="$draft.state.refresh" data-gosx-region-on="draft:pick draft:undo draft:state draft:clock">
			<DraftWorkspace {...data.workspace}></DraftWorkspace>
		</div>
	</main>
}
