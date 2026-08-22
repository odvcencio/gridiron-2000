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
}

type DraftQueueProps struct {
	Players       []DraftPlayerCard
	Action        string
	CSRF          string
	TeamID        string
	CanPick       bool
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
// draft update poll authoritative, and make the first paint usable on a phone.
func DraftQueue(props DraftQueueProps) Node {
	return <section class="player-pool">
		<div class="pool-toolbar">
			<div>
				<span class="section-index">PLAYER DATABASE</span>
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
			<label class="mono" for="pool-search">DATABASE QUERY //</label>
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
				<article class="pool-row" data-player-position={player.position} data-search={player.search}>
					<span class="pool-rank mono">{player.rank}</span>
					<div class="pool-player pool-player--photo stat-tip" tabindex="0">
						<If cond={player.has_headshot}>
							<img class="player-headshot" src={player.headshot} alt="" loading="lazy" />
						</If>
						<div class="pool-player__text">
							<strong>{player.name}</strong>
							<If cond={player.has_draft_capital}>
								<span class="badge-rookie">{player.draft_capital}</span>
							</If>
							<small>{player.detail}</small>
							<If cond={player.has_opponent}>
								<small class="mono">
									{player.opponent}
									<If cond={player.has_matchup}>
										·
										<span class="matchup-chip" data-matchup-tier={player.matchup_tier}>{player.matchup_chip}</span>
									</If>
								</small>
							</If>
						</div>
						<div class="stat-tip__panel" role="tooltip" aria-hidden="true">
							<div class="stat-tip__head">
								<strong>{player.name}</strong>
								<span class="mono">{player.jersey}</span>
								<span class="mono stat-tip__team">{player.nfl_team}</span>
							</div>
							<If cond={player.has_breakdown}>
								<div class="stat-tip__rows">
									<Each of={player.breakdown} as="row">
										<div class="stat-tip__row" data-scored={row.scored}>
											<span>{row.label}</span>
											<span class="mono">{row.calc}</span>
											<b class="mono">{row.points}</b>
										</div>
									</Each>
									<div class="stat-tip__total">
										<span>League scoring</span>
										<b class="mono">{player.breakdown_total}</b>
									</div>
								</div>
							</If>
							<If cond={player.has_breakdown == false}>
								<p class="stat-tip__empty">No projection detail for this position.</p>
							</If>
							<If cond={player.has_matchup}>
								<p class="stat-tip__hist mono">{player.matchup_detail}</p>
							</If>
							<If cond={player.has_hist}>
								<p class="stat-tip__hist mono">{player.hist}</p>
							</If>
						</div>
					</div>
					<span class="position-chip">{player.position}</span>
					<b class="mono">{player.projection}</b>
					<If cond={props.HasSeat}>
					<form method="post" action={props.Action} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<input type="hidden" name="team_id" value={props.TeamID}></input>
						<input type="hidden" name="player_id" value={player.id}></input>
						<input type="hidden" name="pos" value={props.Position}></input>
						<input type="hidden" name="q" value={props.Query}></input>
						<input type="hidden" name="page" value={props.Page}></input>
						<If cond={props.CanPick}>
							<button class="draft-button" type="submit">Draft</button>
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
	</section>
}

type DraftTeamProps struct {
	OnClock        bool
	Tone           string
	HasAvatarImage bool
	AvatarImageURL string
	Name           string
	Abbreviation   string
	Presence       string
	Manager        string
	Division       string
	Ready          bool
	Autopick       bool
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
			<small class="mono division-tag">{props.Division}</small>
		</div>
		<If cond={props.Ready}>
			<b class="ready-state is-ready">Ready</b>
		</If>
		<If cond={props.Ready == false}>
			<b class="ready-state">Not ready</b>
		</If>
		<If cond={props.Autopick}>
			<b class="autopick-badge mono">AUTO</b>
		</If>
	</div>
}

type DraftRoomProps struct {
	Data          map[string]any
	CSRF          string
	Actions       map[string]string
	StatusSummary string
}

func DraftRoom(props DraftRoomProps) Node {
	return <div class="draft-live-room">
		<p class="draft-region-stale mono" role="status">LIVE UPDATE FAILED · SHOWING LAST CONFIRMED ROOM STATE. <a href="/draft">Refresh room →</a></p>
		<p class="visually-hidden" role="status" aria-live="polite" aria-atomic="true">{props.StatusSummary}</p>
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
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
				<If cond={props.Data.draft.started}>
					<span>Draft status</span>
					<strong class="mono">LIVE</strong>
				</If>
				<div class="draft-clock-meta">
					<span>
						{props.Data.ready_count}
						/
						{props.Data.manager_count}
						ready
					</span>
					<span>
						Pick #
						{props.Data.pick_number}
					</span>
				</div>
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
				<span
					class="visually-hidden"
					data-on-clock={props.Data.viewer.team_id == props.Data.on_clock_id}
					data-gosx-watch="data-on-clock=true"
					data-gosx-watch-effect="class:is-on-clock@body,title,cue:chime"
					data-gosx-watch-title="YOUR PICK IS ON THE CLOCK"
				></span>
				<If cond={props.Data.viewer.has_seat}>
					<div class="manager-draft-controls" aria-label="Your draft controls">
						<div class="manager-draft-control" id="ready-toggle">
							<div class="manager-draft-control__copy">
								<span class="mono">YOUR CHECK-IN</span>
							<If cond={props.Data.viewer_ready}>
									<strong class="ready-state is-ready">READY</strong>
									<small>The commissioner sees you as present.</small>
								</If>
							<If cond={props.Data.viewer_ready == false}>
									<strong class="ready-state">NOT READY</strong>
									<small>Check in when you are set for draft night.</small>
								</If>
							</div>
						<form method="post" action={props.Actions.toggle_ready} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={props.CSRF}></input>
							<input type="hidden" name="team_id" value={props.Data.viewer.team_id}></input>
							<If cond={props.Data.viewer_ready}>
									<button class="button button--ghost button--compact" type="submit">Mark not ready</button>
								</If>
							<If cond={props.Data.viewer_ready == false}>
									<button class="button button--primary button--compact" type="submit">Mark ready</button>
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
									<small>Autopick will not pick while you are present. This turn's saved deadline still applies, and being marked away can shorten it.</small>
								</If>
							</div>
						<form method="post" action={props.Actions.toggle_autopick} data-gosx-managed="true">
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
				<If cond={props.Data.viewer.has_seat == false}>
					<div class="manager-draft-controls">
						<div class="manager-draft-control">
							<div class="manager-draft-control__copy">
								<span class="mono">DRAFT CONTROLS</span>
								<strong class="ready-state">NO TEAM SEAT</strong>
								<small>Claim a franchise before setting readiness or autopick.</small>
							</div>
							<a href="/join" data-gosx-link class="button button--primary button--compact">Claim a team →</a>
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
			<If cond={props.Data.demo_mode}>
				<p class="demo-message">
					<strong>REHEARSAL MODE:</strong>
					the commissioner must still type START; after that, rehearsal picks control the current team on the clock.
				</p>
			</If>
			<If cond={props.Data.pool_live == false}>
				<p class="demo-message">
					<strong>OFFLINE POOL:</strong>
					player ranks are approximate until live data is turned on.
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
							<strong>Toggle your ready state</strong>
							<small>Set yourself ready so the commissioner can confirm the room at 4PM.</small>
						</div>
						<a href="#ready-toggle" class="board-button">Ready toggle ↑</a>
					</div>
					</If>
					<If cond={props.Data.viewer.has_seat == false}>
					<div class="checklist-item">
						<span class="checklist-mark mono">02</span>
						<div class="checklist-item__text">
							<strong>Claim a franchise</strong>
							<small>A team seat unlocks draft check-in, autopick, and picks when the commissioner starts the room.</small>
						</div>
						<a href="/join" data-gosx-link class="board-button">Claim franchise →</a>
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
				<span class="section-index">
					ROUND
					{props.Data.round}
					// SNAKE ORDER
				</span>
				<span class="mono">
					ON CLOCK:
					{props.Data.on_clock.abbreviation}
				</span>
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
			<section class="draft-order-strip commissioner-clock-controls">
				<header>
					<span class="section-index">COMMISSIONER // CLOCK</span>
					<span class="mono">{props.Data.clock.reason}</span>
				</header>
				<If cond={props.Data.draft.started == false}>
					<form method="post" action={props.Actions.draft_start} data-gosx-managed="true" class="clock-controls">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<label class="mono" for="draft-start-confirm">TYPE START //</label>
						<input id="draft-start-confirm" class="scoring-input" name="confirm" autocomplete="off" placeholder="START"></input>
						<button class="button button--primary" type="submit">Start draft + pick clock</button>
					</form>
					<p class="scoring-note">This is intentional and immediate. The scheduled window does not start the draft.</p>
				</If>
				<If cond={props.Data.draft.started}>
				<div class="clock-controls">
					<form method="post" action={props.Actions.clock_pause} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<button class="button button--compact" type="submit">Pause clock</button>
					</form>
					<form method="post" action={props.Actions.clock_resume} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<button class="button button--compact button--primary" type="submit">Resume clock</button>
					</form>
					<form method="post" action={props.Actions.clock_extend} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<input class="scoring-input" type="number" name="seconds" placeholder="30" min="1" max="600"></input>
						<button class="button button--compact" type="submit">Extend pick</button>
					</form>
					<form method="post" action={props.Actions.clock_duration} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<input class="scoring-input" type="number" name="seconds" placeholder="90" min="10" max="600"></input>
						<button class="button button--compact" type="submit">Set duration</button>
					</form>
					<form method="post" action={props.Actions.clock_autopick} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<button class="button button--compact button--ghost" type="submit">Force autopick</button>
					</form>
				</div>
				</If>
			</section>
		</If>
		<p class="draft-manual-refresh mono"><a href="/draft">Refresh room manually →</a></p>
	</div>
}

type DraftWorkspaceProps struct {
	Data           map[string]any
	CSRF           string
	MakePickAction string
}

func DraftWorkspace(props DraftWorkspaceProps) Node {
	return <div class="draft-live-workspace">
		<p class="draft-region-stale mono" role="status">LIVE UPDATE FAILED · SHOWING LAST CONFIRMED PLAYER POOL AND PICK TAPE. <a href="/draft">Refresh workspace →</a></p>
		<div class="pool-count-bar">
			<span class="mono pool-count">
				{props.Data.pool_count}
				PLAYERS
			</span>
		</div>
		<div class="draft-workspace">
			<DraftQueue
				Players={props.Data.available}
				Action={props.MakePickAction}
				CSRF={props.CSRF}
				TeamID={props.Data.on_clock_id}
				CanPick={props.Data.can_pick}
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
				<If cond={props.Data.board_count == 0}>
					<div class="board-peek-empty">
						<a href="/board" data-gosx-link class="mono">BUILD YOUR BOARD →</a>
					</div>
				</If>
				<header>
					<span class="section-index">PICK TAPE</span>
					<b class="mono">LIVE LOG</b>
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
		<p class="draft-manual-refresh mono"><a href="/draft">Refresh player pool manually →</a></p>
	</div>
}

func Page() Node {
	return <main class="page draft-page" id="main-content">
		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_notice}><p class="flash-message">{data.notice}</p></If>
			<If cond={data.has_pick_error}><p class="error-message">{data.pick_error}</p></If>
		</div>
		<div class="draft-region" data-gosx-region data-gosx-region-url="/draft/fragment/room" data-gosx-region-interval="4s">
			<DraftRoom {...data.room}></DraftRoom>
		</div>
		<div class="draft-region" data-gosx-region data-gosx-region-url={data.workspace_fragment_url} data-gosx-region-interval="4s">
			<DraftWorkspace {...data.workspace}></DraftWorkspace>
		</div>
	</main>
}
