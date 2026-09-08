package admin

// SeatRow's avatar-upload form posts to /avatar/upload as a plain,
// unmanaged (data-gosx-managed="false") full-page submission. GoSX v0.50.0
// supports File/Files and MaxActionBodyBytes for managed actions, but this
// native route remains until the production consumer adopts a
// bounded-multipart contract; its outer middleware applies the complete-
// envelope limit before sessions and CSRF parsing (see avatar_handlers.go in
// the repo root).
//
// SeatRow's avatar-mark__photo image (and the matching one in the
// 07 // DRAFT ORDER list below) carries width="42" height="42": .team-mark
// is a fixed 2.6rem (42px at the 16px root) square (styles.css), and
// .avatar-mark__photo fills it at width/height: 100%, so these attributes
// exist purely to give the browser the badge's 1:1 aspect ratio before the
// image itself loads or decodes. Without them a managed-action redirect's
// scrollIntoView (navigation.ts) could run before every avatar above the
// anchor had reserved its layout box, landing the viewport short of the
// target once those images decoded and shifted content below them
// downward — the ~19,700px admin console renders one avatar per seat and
// per draft-order row, so this is not a one-image problem (wave-2-
// verification item 11).
type SeatRowProps struct {
	Seat              map[string]any
	ReleaseAction     string
	RenameAction      string
	AutopickAction    string
	AvatarResetAction string
	CoDetachAction    string
	CSRF              string
}

func SeatRow(props SeatRowProps) Node {
	return <article class="seat-row" data-claimed={props.seat.claimed}>
		<span class={"team-mark tone-" + props.seat.tone}>
			<If cond={props.seat.has_avatar_image}>
				<img
					class="avatar-mark__photo"
					src={props.seat.avatar_image_url}
					alt={props.seat.name}
					width="42"
					height="42"
					loading="lazy"
				 />
			</If>
			<If cond={props.seat.has_avatar_image == false}>
				{props.seat.abbreviation}
			</If>
		</span>
		<div class="seat-identity">
			<strong>{props.seat.name}</strong>
			<small>
				<If cond={props.seat.claimed}>
					{props.seat.manager}
					·
					{props.seat.email}
				</If>
				<If cond={props.seat.claimed == false}>Awaiting a manager</If>
			</small>
		<If cond={props.seat.has_co}>
			<small>
				co-manager:
				{props.seat.co_email}
			</small>
		</If>
		<small class="mono">{props.seat.presence_label} · {props.seat.presence_detail}</small>
		<small class="mono">BOARD: {props.seat.board_count} TARGETS</small>
		<If cond={props.seat.board_gap}>
			<b class="ready-state">BOARD GAP</b>
		</If>
		<span class="position-chip">{props.seat.division}</span>
		</div>
		<If cond={props.seat.ready}>
			<b class="ready-state is-ready">Ready</b>
		</If>
		<If cond={props.seat.ready == false}>
			<b class="ready-state">Not ready</b>
		</If>
		<If cond={props.seat.claimed}>
			<details class="seat-release-disclosure">
				<summary class="board-button board-button--cut">Release seat</summary>
				<form method="post" action={props.ReleaseAction} data-gosx-managed="true" class="seat-release-form">
					<input type="hidden" name="csrf_token" value={props.CSRF}></input>
					<input type="hidden" name="team_id" value={props.seat.id}></input>
					<input type="hidden" name="seat_token" value={props.seat.release_token}></input>
					<label for={"seat-release-confirm-" + props.seat.id}>Type <span class="mono">{props.seat.release_confirmation}</span> to confirm.</label>
					<input id={"seat-release-confirm-" + props.seat.id} class="typed-confirm-input" type="text" name="confirm" autocomplete="off" enterkeyhint="done" placeholder={props.seat.release_confirmation} required="required"></input>
					<p class="scoring-note">This releases the primary manager, co-manager, pending co-invite, and ready state for this seat.</p>
					{/* F7 (J4 console gap-audit): the consequence sentence above
					    never named the roster, the lineup, or the matchup the
					    team keeps playing with once nobody holds the seat. Empty
					    before the draft completes (season_release_consequence,
					    admin.go), so this line renders only once there is a
					    season to name. */}
					<If cond={props.seat.season_release_consequence != ""}>
						<p class="scoring-note">{props.seat.season_release_consequence}</p>
					</If>
					<button class="board-button board-button--cut" type="submit">Release {props.seat.name}</button>
				</form>
			</details>
		</If>
		<If cond={props.seat.has_co}>
			<form method="post" action={props.CoDetachAction} data-gosx-managed="true">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="team_id" value={props.seat.id}></input>
				<button class="board-button" type="submit">Detach co</button>
			</form>
		</If>
		<form method="post" action={props.RenameAction} data-gosx-managed="true">
			<input type="hidden" name="csrf_token" value={props.CSRF}></input>
			<input type="hidden" name="team_id" value={props.seat.id}></input>
			<label for={"seat-rename-" + props.seat.id} class="visually-hidden">Rename {props.seat.name}</label>
			<input id={"seat-rename-" + props.seat.id} type="text" name="name" placeholder="Rename team" maxlength="40"></input>
			<button class="board-button" type="submit">Set</button>
		</form>
		<If cond={props.seat.claimed}>
			<If cond={props.seat.autopick}>
				<form method="post" action={props.AutopickAction} data-gosx-managed="true">
					<input type="hidden" name="csrf_token" value={props.CSRF}></input>
					<input type="hidden" name="team_id" value={props.seat.id}></input>
					<input type="hidden" name="on" value="false"></input>
					<button class="board-button autopick-toggle is-on" type="submit">AUTO: ON</button>
				</form>
			</If>
			<If cond={props.seat.autopick == false}>
				<form method="post" action={props.AutopickAction} data-gosx-managed="true">
					<input type="hidden" name="csrf_token" value={props.CSRF}></input>
					<input type="hidden" name="team_id" value={props.seat.id}></input>
					<input type="hidden" name="on" value="true"></input>
					<button class="board-button autopick-toggle" type="submit">AUTO: OFF</button>
				</form>
			</If>
		</If>
		<If cond={props.seat.claimed == false}>
			<small class="scoring-note">AUTO unavailable until a manager claims this seat.</small>
		</If>
		<If cond={props.seat.identity_available}>
		<form
			method="post"
			action="/avatar/upload"
			enctype="multipart/form-data"
			data-gosx-managed="false"
			class="avatar-upload-form"
		>
			<input type="hidden" name="csrf_token" value={props.CSRF}></input>
			<input type="hidden" name="team_id" value={props.seat.id}></input>
			<input type="hidden" name="redirect_to" value="/admin"></input>
			<label for={"avatar-upload-" + props.seat.id}>Upload custom image for {props.seat.name}</label>
			<input
				id={"avatar-upload-" + props.seat.id}
				type="file"
				name="avatar"
				accept="image/png,image/jpeg"
				aria-describedby="admin-avatar-upload-help"
				required="required"
			></input>
			<button class="board-button" type="submit">Set avatar</button>
		</form>
		<If cond={props.seat.has_avatar}>
			<form method="post" action={props.AvatarResetAction} data-gosx-managed="true">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="team_id" value={props.seat.id}></input>
				<button class="board-button board-button--cut" type="submit">Reset avatar</button>
			</form>
		</If>
		</If>
	</article>
}

type AdminTaskLinkProps struct {
	Href    string
	Current bool
	Label   string
	Status  string
}

func AdminTaskLink(props AdminTaskLinkProps) Node {
	return <li class="admin-task-nav__item">
		<a
			href={props.Href}
			data-gosx-link
			class="admin-task-nav__link"
			aria-current={props.Current}
		>
			<span class="admin-task-nav__label">{props.Label}</span>
			<span class="admin-task-nav__status">{props.Status}</span>
		</a>
	</li>
}

func AdminAttentionReadout(props adminAttentionReadoutProps) Node {
	return <section class="admin-attention-readout" aria-labelledby="admin-attention-heading">
		<div class="pool-toolbar">
			<div>
				<span class="section-index">COMMISSIONER // LIVE OPERATIONS</span>
				<h2 id="admin-attention-heading">Attention and readiness</h2>
			</div>
			<button type="button" class="board-button" data-gosx-set="$admin.attention.refresh" data-gosx-set-value="manual">Refresh status</button>
		</div>
		<div class="admin-task-nav__readout" aria-live="polite">
			<strong class="mono">{props.SeasonStateSentence}</strong>
			<If cond={props.DraftComplete == false}>
				<span>Draft deadline <span class="mono">{props.DraftDate}<If cond={props.DraftPublished}> · {props.DraftTime}</If></span> · schedule {props.ScheduleStatus}</span>
			</If>
			<If cond={props.ScheduleReady}><span>Week {props.ScheduleWeek} is ready to close.</span></If>
			<If cond={props.ScheduleReady == false}><span>{props.ScheduleReason}</span></If>
		</div>
		{/* F2 + F35 (J4 console gap-audit): the first two screens of the
		    console used to be draft-night telemetry (eight seat rows,
		    board counts) in week 1, with nothing naming the week's own
		    open work. Once the draft is complete, a "This week" card
		    leads instead — the same facts the old stat row already
		    carried (open claims, trades in review, ready, invites),
		    named and linked to the section that answers each one. No new
		    computation: kickoff and a per-team lineup-set count have no
		    existing source on this page, so this card does not claim
		    them. The seat/board readiness that mattered on draft night
		    moves into a closed disclosure below it, exactly as the
		    season-operations runbook already reprioritizes above this
		    panel. */}
		<If cond={props.DraftComplete}>
			<div class="admin-this-week" aria-label="This week">
				<span class="section-index">THIS WEEK</span>
				<ul class="admin-this-week__list">
					<li><a href="#admin-week-close" data-gosx-link><strong>Week {props.ScheduleWeek}</strong> <If cond={props.ScheduleReady}>is ready to close</If><If cond={props.ScheduleReady == false}>{props.ScheduleReason}</If> →</a></li>
					<li><a href="#admin-week-close" data-gosx-link><strong>{props.OpenClaimCount}</strong> open waiver claim<If cond={props.OpenClaimCount != 1}>s</If> →</a></li>
					<li><a href="/trades"><strong>{props.TradesInReviewCount}</strong> trade<If cond={props.TradesInReviewCount != 1}>s</If> in review →</a></li>
					<li><a href="#admin-invites" data-gosx-link><strong>{props.InviteCount}</strong> invite<If cond={props.InviteCount != 1}>s</If> pending →</a></li>
				</ul>
				<p class="admin-this-week__today">
					<strong>Needs you today:</strong>
					<If cond={props.ScheduleReady == false}> {props.ScheduleReason}</If>
					<If cond={props.ScheduleReady && props.OpenClaimCount > 0}> {props.OpenClaimCount} open waiver claim<If cond={props.OpenClaimCount != 1}>s</If> — <a href="#admin-week-close" data-gosx-link>run waivers</a>.</If>
					<If cond={props.ScheduleReady && props.OpenClaimCount == 0 && props.TradesInReviewCount > 0}> {props.TradesInReviewCount} trade<If cond={props.TradesInReviewCount != 1}>s</If> waiting on review — <a href="/trades">open trades</a>.</If>
					<If cond={props.ScheduleReady && props.OpenClaimCount == 0 && props.TradesInReviewCount == 0}> Nothing needs you right now.</If>
				</p>
			</div>
			<details class="commissioner-hq__draft-night">
				<summary class="board-button">Draft night (complete)</summary>
				<AdminAttentionProvenance {...props}></AdminAttentionProvenance>
			</details>
		</If>
		<If cond={props.DraftComplete == false}>
			<AdminAttentionProvenance {...props}></AdminAttentionProvenance>
		</If>
	</section>
}

// AdminAttentionProvenance is the seat/board readiness block F2 (J4
// console gap-audit) moved out of AdminAttentionReadout's own body: it
// renders unwrapped before the draft completes (the only relevant work
// that phase has) and inside a closed "Draft night (complete)" disclosure
// once the draft is done, so the same markup and the same props serve
// both phases without duplication.
func AdminAttentionProvenance(props adminAttentionReadoutProps) Node {
	return <>
		<div class="commissioner-hq__provenance">
			<span><strong>SEATS</strong><span class="mono">{props.ClaimedCount} / {props.SeatCount} CLAIMED</span></span>
			<span><strong>READY</strong><span class="mono">{props.ReadyCount} / {props.SeatCount}</span></span>
			<span><strong>INVITES</strong><span class="mono">{props.InviteCount} PENDING</span></span>
			<span><strong>BOARD GAPS</strong><span class="mono">{props.BoardGapCount}</span></span>
			<span><strong>PRESENCE</strong><span class="mono">{props.PresenceHere} HERE · {props.PresenceIdle} IDLE · {props.PresenceAway} AWAY · {props.PresenceNotSeen} NOT SEEN · {props.PresenceUnclaimed} OPEN</span></span>
			<span>
				<strong>READ AT</strong>
				<If cond={props.GeneratedAtISO != ""}>
					<time class="mono" datetime={props.GeneratedAtISO}>{props.GeneratedAt}<If cond={props.GeneratedAtRelative != ""}> · {props.GeneratedAtRelative}</If></time>
				</If>
				<If cond={props.GeneratedAtISO == ""}>
					<span class="mono">{props.GeneratedAt}</span>
				</If>
			</span>
		</div>
		{/* F6 + F19 (gap-audit J2): "READY 4 / 8" named no one and gave the
		    commissioner nothing to act on — the ready toggle exists only in
		    the draft room's own drawer. This names the claimed-but-not-
		    ready seats by their manager's own first name, in plain words,
		    next to a link straight into the room where the toggle lives. */}
		<If cond={props.HasNotCheckedIn}>
			<TextBlock as="p" class="admin-attention-not-checked-in" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={3} overflow="ellipsis">
				Not checked in: {props.NotCheckedInSummary} · <a href="/draft" data-gosx-link>Open the draft room</a>
			</TextBlock>
		</If>
		{/* J4 F28 + J2 F18 (gap-audit): every row here used to lead with an
		    unexplained two-letter-and-digit code ("AQ1 · In Shedeur
		    Time"), asking the commissioner to memorise eight codes to
		    read his own console. The team name leads now; the code moves
		    to a secondary mono chip, and this one line defines it the
		    first time a code appears on the page. */}
		<p class="scoring-note">Codes like <span class="position-chip">AQ1</span> are each team's short seat code; the team name always leads.</p>
		<div class="commissioner-hq__ledger" aria-label="Seat readiness and presence">
			<Each of={props.Seats} as="seat">
				<div class="commissioner-hq__attention" data-presence={seat.Presence}>
					<div class="commissioner-hq__attention-copy">
						<TextBlock as="span" class="section-index" font="600 13px IBM Plex Mono" lineHeight={18} maxLines={2} overflow="ellipsis">
							{seat.Name}<If cond={seat.Manager != ""}> · {seat.Manager}</If>
						</TextBlock>
						<strong><If cond={seat.Claimed}>CLAIMED</If><If cond={seat.Claimed == false}>OPEN</If> · <If cond={seat.Ready}>READY</If><If cond={seat.Ready == false}>NOT READY</If></strong>
						<span class="seat-presence">{seat.PresenceLabel} · {seat.PresenceDetail} · board {seat.BoardCount}</span>
					</div>
					<span class="position-chip mono">{seat.Abbreviation}</span>
					<If cond={seat.BoardGap}><span class="position-chip">BOARD GAP</span></If>
				</div>
			</Each>
		</div>
	</>
}

// Page's 00 // DRAFT NIGHT heading: draftSummaryForState (service.go)
// prints the sentinel "TBD" into data.draft.date whenever the draft is
// neither published nor started (its default case) — a plain date != ""
// check let "TBD runbook" through (wave-2-verification finding). The
// heading below instead branches on published || started, which covers
// both the normal published-date form and the rarer started-with-an-
// unpublished-date override that draftSummaryForState already backfills
// with a real date; only the true TBD case drops to "Draft night
// runbook" with no date fragment. GoSX markup carries no comment syntax
// of its own, hence this note living beside the enclosing Page() instead
// of inline at the heading.
func Page() Node {
	return <main
		class="page admin-page"
		id="main-content"
		data-gosx-revalidate-interval="4s"
		data-gosx-revalidate-src="/api/league/version"
	>
		<If cond={data.is_commissioner}>
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="signal-mark" aria-hidden="true"></span>
					COMMISSIONER CONSOLE
				</span>
				<h1>Commissioner console</h1>
				<p>
					<strong>Run the league.</strong> Seats, invites, and reset controls. The league checks every action against the commissioner list.
				</p>
				<If cond={data.is_default_config}>
					<p class="demo-message">
						<strong>BUILT-IN REFERENCE LEAGUE:</strong>
						This league still uses the standard default setup. Ask the commissioner to enter your own league details.
					</p>
				</If>
			</div>
			<div class="draft-clock-panel">
				<If cond={data.has_league_switcher}>
					<form class="admin-league-switcher" method="get" action="/commissioner/switch">
						<div class="admin-league-switcher__heading">
							<label for="admin-league-switcher">League console</label>
							<a href="/commissioner" data-gosx-link>All leagues →</a>
						</div>
						<div class="admin-league-switcher__controls">
							<select id="admin-league-switcher" name="league" aria-label="Switch commissioner league">
								<Each of={data.league_options} as="league">
									<option value={league.id} selected={league.current}>{league.label}</option>
								</Each>
							</select>
							<input type="hidden" name="section" value={data.admin_section}></input>
							<button class="board-button" type="submit">Open</button>
						</div>
					</form>
				</If>
				<span>League status</span>
				<strong class="mono">
					{data.member_count}
					/
					{data.seat_count}
					SEATS ·
					{data.pick_count}
					PICKS
				</strong>
				<div class="draft-clock-meta">
					<span>
						Draft
						{data.draft.date}
						<If cond={data.draft.published}>
							·
							{data.draft.time}
						</If>
					</span>
					<span class="mono ready-count-tag">
						{data.ready_count}
						/
						{data.seat_count}
						READY
					</span>
				</div>
			</div>
		</section>
		</If>
		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_notice}>
				<TextBlock as="p" class="flash-message" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={3} overflow="ellipsis" text={data.notice} />
			</If>
			<If cond={data.has_admin_error}>
				<TextBlock as="p" class="error-message" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={3} overflow="ellipsis" text={data.admin_error} />
			</If>
			<If cond={data.has_avatar_error}>
				<TextBlock as="p" class="error-message" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={3} overflow="ellipsis" text={data.avatar_error} />
			</If>
			<If cond={data.demo_mode}>
				<TextBlock as="p" class="demo-message" font="400 15px Plus Jakarta Sans" lineHeight={22}>
					<strong>REHEARSAL MODE:</strong>
					the console is open to everyone while demo mode is on.
				</TextBlock>
			</If>
		</div>
		<If cond={data.is_commissioner}>
			<div class="admin-attention-region" data-gosx-region data-gosx-region-url="/admin/fragment" data-gosx-region-interval="4s" data-gosx-region-signal="$admin.attention.refresh">
				<AdminAttentionReadout {...data.admin_attention}></AdminAttentionReadout>
			</div>
		</If>
		<If cond={data.is_commissioner == false}>
			<section class="player-pool">
				<h1>Commissioner console</h1>
				<div class="empty-tape">
					<strong>RESTRICTED</strong>
					<p>
						This console is limited to the league commissioner.
					</p>
					<a href="/" data-gosx-link>Back to HQ →</a>
				</div>
			</section>
		</If>
		<If cond={data.is_commissioner}>
			<nav class="admin-task-nav" aria-labelledby="admin-task-nav-heading">
				<div class="admin-task-nav__header">
					<div>
						<span class="section-index">COMMISSIONER // TASK BOARD</span>
						<h2 id="admin-task-nav-heading">Choose a league job</h2>
					</div>
					<p class="admin-task-nav__hint">Routine controls are grouped by when and why you use them. Danger Zone stays at the bottom.</p>
				</div>
				<div class="admin-task-nav__readout" aria-live="polite">
					<strong class="mono">CURRENT CONSOLE STATE</strong>
					<If cond={data.draft.complete}>
						<span>Draft is complete. There is no current pick; move on to season operations.</span>
					</If>
					<If cond={data.draft.complete == false}>
						<If cond={data.draft_started}>
							<span>Draft is live. Operate the current pick from Draft clock.</span>
						</If>
						<If cond={data.draft_started == false}>
							<span>Draft is not live. Confirm seats, draw order, then start it intentionally.</span>
						</If>
					</If>
				</div>
				<div class="admin-task-nav__groups">
					<div class="admin-task-nav__group">
						<h3>Draft preparation and live operation</h3>
						<ul>
							<If cond={data.draft.complete}>
								<AdminTaskLink Label="Start and monitor draft" Href="/admin?section=draft-control#admin-draft-control" Current={data.admin_section == "draft-control"} Status="COMPLETE" />
							</If>
							<If cond={data.draft.complete == false}>
								<If cond={data.draft_started}>
									<AdminTaskLink Label="Start and monitor draft" Href="/admin?section=draft-control#admin-draft-control" Current={data.admin_section == "draft-control"} Status="LIVE · operate now" />
								</If>
								<If cond={data.draft_started == false}>
									<AdminTaskLink Label="Start and monitor draft" Href="/admin?section=draft-control#admin-draft-control" Current={data.admin_section == "draft-control"} Status="START REQUIRED" />
								</If>
							</If>
							<If cond={data.order_randomized}>
								<AdminTaskLink Label="Draw draft order" Href="/admin?section=draft-order#admin-draft-order" Current={data.admin_section == "draft-order"} Status="PUBLISHED" />
							</If>
							<If cond={data.order_randomized == false}>
								<AdminTaskLink Label="Draw draft order" Href="/admin?section=draft-order#admin-draft-order" Current={data.admin_section == "draft-order"} Status="DRAW REQUIRED" />
							</If>
							<If cond={data.pool.error != ""}>
								<AdminTaskLink Label="Verify player pool" Href="/admin?section=data#admin-data" Current={data.admin_section == "data"} Status="DEGRADED" />
							</If>
							<If cond={data.pool.error == ""}>
								<AdminTaskLink Label="Verify player pool" Href="/admin?section=data#admin-data" Current={data.admin_section == "data"} Status="AVAILABLE" />
							</If>
							{/* F2/F27 (J2 draft-night audit): the pick-clock job kept
							    reading ARMED/WAITING after the draft ended, sitting
							    beside "Start and monitor draft · COMPLETE" as the one
							    job in its group that never caught up to the phase. */}
							<If cond={data.draft.complete}>
								<AdminTaskLink Label="Run pick clock" Href="/admin?section=clock#admin-clock" Current={data.admin_section == "clock"} Status="DONE" />
							</If>
							<If cond={data.draft.complete == false}>
								<If cond={data.clock.armed}>
									<AdminTaskLink Label="Run pick clock" Href="/admin?section=clock#admin-clock" Current={data.admin_section == "clock"} Status="ARMED" />
								</If>
								<If cond={data.clock.armed == false}>
									<AdminTaskLink Label="Run pick clock" Href="/admin?section=clock#admin-clock" Current={data.admin_section == "clock"} Status="WAITING" />
								</If>
							</If>
						</ul>
					</div>
					<div class="admin-task-nav__group">
						<h3>Season operation</h3>
						<ul>
							<If cond={data.draft.complete}>
								<AdminTaskLink Label="Configure roster shape" Href="/admin?section=roster#admin-roster" Current={data.admin_section == "roster"} Status="LOCKED · DRAFT COMPLETE" />
							</If>
							<If cond={data.draft.complete == false}>
								<If cond={data.roster_shape.draft_started}>
									<AdminTaskLink Label="Configure roster shape" Href="/admin?section=roster#admin-roster" Current={data.admin_section == "roster"} Status="LOCKED · DRAFT STARTED" />
								</If>
								<If cond={data.roster_shape.draft_started == false}>
									<AdminTaskLink Label="Configure roster shape" Href="/admin?section=roster#admin-roster" Current={data.admin_section == "roster"} Status="OPEN" />
								</If>
							</If>
							<If cond={data.schedule.has_schedule}>
								<AdminTaskLink Label="Publish regular-season schedule" Href="/admin?section=schedule#admin-schedule" Current={data.admin_section == "schedule"} Status="PUBLISHED" />
							</If>
							<If cond={data.schedule.has_schedule == false}>
								<AdminTaskLink Label="Publish regular-season schedule" Href="/admin?section=schedule#admin-schedule" Current={data.admin_section == "schedule"} Status="NEEDS PLAN" />
							</If>
							<If cond={data.schedule.has_schedule}>
								<AdminTaskLink Label="Week close" Href="/admin?section=week-close#admin-week-close" Current={data.admin_section == "week-close"} Status="CHECK READINESS" />
							</If>
									<If cond={data.schedule.has_schedule == false}>
										<AdminTaskLink Label="Week close" Href="/admin?section=week-close#admin-week-close" Current={data.admin_section == "week-close"} Status="NO SCHEDULE" />
									</If>
									<AdminTaskLink Label="Operate playoff truth" Href="/admin?section=playoffs#admin-playoffs" Current={data.admin_section == "playoffs"} Status={data.playoff_truth.status_label} />
									{/* F17 (J4 console gap-audit): the task board never listed a row
									    for running waivers or reviewing a trade, so a commissioner
									    fell back to scrolling to find them. Status reads the same
									    counts the attention readout above already computes — no new
									    number, just a second place to see it. */}
									<If cond={data.waivers.has_open_claims}>
										<AdminTaskLink Label="Run waivers" Href="/admin?section=week-close#admin-week-close" Current={data.admin_section == "week-close"} Status={data.waivers.open_claim_count + " OPEN"} />
									</If>
									<If cond={data.waivers.has_open_claims == false}>
										<AdminTaskLink Label="Run waivers" Href="/admin?section=week-close#admin-week-close" Current={data.admin_section == "week-close"} Status="NONE DUE" />
									</If>
									<If cond={data.admin_attention.TradesInReviewCount > 0}>
										<AdminTaskLink Label="Review a trade" Href="/trades" Current={false} Status={data.admin_attention.TradesInReviewCount + " IN REVIEW"} />
									</If>
									<If cond={data.admin_attention.TradesInReviewCount == 0}>
										<AdminTaskLink Label="Review a trade" Href="/trades" Current={false} Status="NONE PENDING" />
									</If>
								</ul>
					</div>
					<div class="admin-task-nav__group">
						<h3>People and access</h3>
						<ul>
							<AdminTaskLink Label="Manage seats and managers" Href="/admin?section=seats#admin-seats" Current={data.admin_section == "seats"} Status={data.ready_count + "/" + data.seat_count + " READY"} />
							<AdminTaskLink Label="Manage invites" Href="/admin?section=invites#admin-invites" Current={data.admin_section == "invites"} Status="ACCESS LIST" />
							{/* F17 (J4 console gap-audit): this used to sit after the </ul>
							    as a bare disclosure triangle, the one job on the board not
							    rendered as a boxed row. The summary now wears the same
							    .admin-task-nav__link box every other row uses; opening it still
							    reveals the per-team list beneath. */}
							<li class="admin-task-nav__item">
								<details id="admin-task-nav-lineup" class="admin-task-nav__lineup-intervention">
									<summary class="admin-task-nav__link admin-task-nav__lineup-summary">
										<span class="admin-task-nav__label">Set a lineup for a manager</span>
										<span class="admin-task-nav__status">ANY TEAM</span>
									</summary>
									<p class="admin-task-nav__hint">A commissioner can set any team's lineup on a missing manager's behalf; this never locks out the manager's own changes once they return.</p>
									<ul class="admin-task-nav__lineup-list">
										<Each of={data.seats} as="seat">
											<li><a href={"/team?team=" + seat.id + "#lineup"} data-gosx-link>{seat.name}</a></li>
										</Each>
									</ul>
								</details>
							</li>
						</ul>
					</div>
					<div class="admin-task-nav__group">
						<h3>League configuration and communication</h3>
						<ul>
							<AdminTaskLink Label="Post league notes" Href="/admin?section=announcements#admin-announcements" Current={data.admin_section == "announcements"} Status="POST / REVIEW" />
							<AdminTaskLink Label="Download league backup" Href="/admin?section=backup#admin-backup" Current={data.admin_section == "backup"} Status="LOCAL SNAPSHOT" />
							<AdminTaskLink Label="Change scoring" Href="/scoring" Current={false} Status="RULES AND WEIGHTS" />
							<AdminTaskLink Label="Read the league log" Href="/activity" Current={false} Status="ACTIVITY FEED" />
						</ul>
					</div>
					<div class="admin-task-nav__group admin-task-nav__group--danger">
						<h3>Danger Zone</h3>
						<ul>
							<AdminTaskLink Label="Reset and recovery controls" Href="/admin?section=danger#admin-danger" Current={data.admin_section == "danger"} Status="IRREVERSIBLE" />
						</ul>
					</div>
				</div>
			</nav>
			{/* F5 + F18 + F25 (J4 console gap-audit): plain #anchor hrefs (no
			    ?section= query) so a jump is an instant in-page scroll, not
			    a full reload of a ~14,000px document — the slowest path,
			    per F25's own root cause. Each label now equals the heading
			    it lands on (F18), and aria-current names the section the
			    commissioner last opened (data.admin_section, the same
			    truth the task board's own AdminTaskLink already reads),
			    since this page has no client-side scroll tracking to name
			    the section actually in view. */}
			<div class="admin-section-strip-wrap">
			<nav class="admin-section-strip" aria-label="Jump to a console section">
				<a href="#admin-draft-control" class="board-button" aria-current={data.admin_section == "draft-control"}>Runbook</a>
				<a href="#admin-schedule" class="board-button" aria-current={data.admin_section == "schedule"}>Schedule</a>
				<a href="#admin-week-close" class="board-button" aria-current={data.admin_section == "week-close"}>Week close</a>
				<a href="#admin-playoffs" class="board-button" aria-current={data.admin_section == "playoffs"}>Playoffs</a>
				<a href="#admin-seats" class="board-button" aria-current={data.admin_section == "seats"}>Seats</a>
				<a href="#admin-invites" class="board-button" aria-current={data.admin_section == "invites"}>Invites</a>
				<a href="#admin-draft-order" class="board-button" aria-current={data.admin_section == "draft-order"}>Draft order</a>
				<a href="#admin-data" class="board-button" aria-current={data.admin_section == "data"}>Data</a>
				<a href="#admin-clock" class="board-button" aria-current={data.admin_section == "clock"}>Clock</a>
				<a href="#admin-roster" class="board-button" aria-current={data.admin_section == "roster"}>Roster</a>
				<a href="#admin-announcements" class="board-button" aria-current={data.admin_section == "announcements"}>Notes</a>
				<a href="#admin-backup" class="board-button" aria-current={data.admin_section == "backup"}>Backup</a>
				<a href="#admin-danger" class="board-button" aria-current={data.admin_section == "danger"}>Danger zone</a>
			</nav>
			</div>
			<div class="admin-grid">
				<section id="admin-draft-control" aria-labelledby="admin-draft-control-heading" tabindex="-1" data-admin-section="draft-control" class={"player-pool draft-runbook" + data.section_class_draft_control}>
					<If cond={data.draft.complete}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">00 // SEASON OPERATIONS</span>
							<h2 id="admin-draft-control-heading">Season operations runbook</h2>
						</div>
					</div>
					<div class="checklist">
						<div class="checklist-item">
							<span class="checklist-mark mono">01</span>
							<div class="checklist-item__text">
								<strong>Close each scoring week</strong>
								<small>
									Use <a href="/admin?section=week-close#admin-week-close" data-gosx-link>02 // WEEK CLOSE</a> once every real game and the player-stat ledger are settled. The forced override stays a separate, explicit action for a data stall.
								</small>
							</div>
						</div>
						<div class="checklist-item">
							<span class="checklist-mark mono">02</span>
							<div class="checklist-item__text">
								<strong>Watch the waiver run</strong>
								<small>
									The daily processor resolves every due claim on its own schedule. Force an out-of-cycle run from <a href="/admin?section=week-close#admin-week-close" data-gosx-link>02 // WEEK CLOSE</a> only when one is stuck or overdue.
								</small>
							</div>
						</div>
						<div class="checklist-item">
							<span class="checklist-mark mono">03</span>
							<div class="checklist-item__text">
								<strong>Review trades</strong>
								<small>
									This league's veto model is commissioner review. Watch for a manager-flagged trade and settle it before the next week closes.
								</small>
							</div>
						</div>
						<div class="checklist-item">
							<span class="checklist-mark mono">04</span>
							<div class="checklist-item__text">
								<strong>Step in on a lineup</strong>
								<small>
									Set a lineup for a manager who cannot before kickoff from <a href="/admin#admin-task-nav-lineup" data-gosx-link>the task board</a>.
								</small>
							</div>
						</div>
						<div class="checklist-item">
							<span class="checklist-mark mono">05</span>
							<div class="checklist-item__text">
								<strong>Keep a backup</strong>
								<small>
									Download a snapshot from <a href="/admin?section=backup#admin-backup" data-gosx-link>12 // BACKUP</a> before any risky change; a nightly copy also saves automatically.
								</small>
							</div>
						</div>
					</div>
					</If>
					<If cond={data.draft.complete == false}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">00 // DRAFT NIGHT</span>
							<h2 id="admin-draft-control-heading">
								<If cond={data.draft.published || data.draft.started}>{data.draft.date} runbook</If>
								<If cond={data.draft.published == false && data.draft.started == false}>Draft night runbook</If>
							</h2>
						</div>
					</div>
					<div class="checklist">
						<div class="checklist-item" data-runbook-state={data.runbook_step_1_state}>
							<span class="checklist-mark mono">01</span>
							<div class="checklist-item__text">
								<TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis" text="About an hour early, drop the seats nobody claimed" />
								<TextBlock as="small" font="400 13px Plus Jakarta Sans" lineHeight={18} text="Use Drop unclaimed seats in 07 // DRAFT ORDER. Do this before you randomize, or the order still lists the seats you are about to remove." />
							</div>
							<If cond={data.runbook_step_1_state == "done"}><span class="position-chip">DONE ✓</span></If>
							<If cond={data.runbook_step_1_state == "next"}><span class="position-chip">NEXT →</span></If>
							<If cond={data.runbook_step_1_state == "later"}><span class="position-chip">LATER</span></If>
						</div>
						<div class="checklist-item" data-runbook-state={data.runbook_step_2_state}>
							<span class="checklist-mark mono">02</span>
							<div class="checklist-item__text">
								<TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis" text="Draw the final order and publish the schedule" />
								<TextBlock as="small" font="400 13px Plus Jakarta Sans" lineHeight={18} text="Use Draw order + schedule in 07 // DRAFT ORDER. It runs six shuffle passes, saves only the final result, publishes the schedule, then reports the reminder queue outcome. Draft order locks when the commissioner starts the draft." />
							</div>
							<If cond={data.runbook_step_2_state == "done"}><span class="position-chip">DONE ✓</span></If>
							<If cond={data.runbook_step_2_state == "next"}><span class="position-chip">NEXT →</span></If>
							<If cond={data.runbook_step_2_state == "later"}><span class="position-chip">LATER</span></If>
						</div>
						<div class="checklist-item" data-runbook-state={data.runbook_step_3_state}>
							<span class="checklist-mark mono">03</span>
							<div class="checklist-item__text">
								<TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis" text="Confirm every seat is ready" />
								<TextBlock as="small" font="400 13px Plus Jakarta Sans" lineHeight={18} text="Check the ready count above and the Ready badges in 05 // SEATS." />
							</div>
							<If cond={data.runbook_step_3_state == "done"}><span class="position-chip">DONE ✓</span></If>
							<If cond={data.runbook_step_3_state == "next"}><span class="position-chip">NEXT →</span></If>
							<If cond={data.runbook_step_3_state == "later"}><span class="position-chip">LATER</span></If>
						</div>
						<div class="checklist-item" data-runbook-state={data.runbook_step_4_state}>
							<span class="checklist-mark mono">04</span>
							<div class="checklist-item__text">
								<If cond={data.draft.time != ""}>
									<TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis">
										At
										{data.draft.time},
										confirm everyone is present and start the draft
									</TextBlock>
								</If>
								<If cond={data.draft.time == ""}>
									<TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis" text="Confirm everyone is present and start the draft" />
								</If>
								<TextBlock as="small" font="400 13px Plus Jakarta Sans" lineHeight={18} text="The scheduled time never opens the room. Type START below when you intentionally begin pick one." />
							</div>
							<If cond={data.runbook_step_4_state == "done"}><span class="position-chip">DONE ✓</span></If>
							<If cond={data.runbook_step_4_state == "next"}><span class="position-chip">NEXT →</span></If>
							<If cond={data.runbook_step_4_state == "later"}><span class="position-chip">LATER</span></If>
						</div>
						<div class="checklist-item">
							<span class="checklist-mark mono">05</span>
							<div class="checklist-item__text">
								<TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis" text="Pause or extend for a break" />
								<TextBlock as="small" font="400 13px Plus Jakarta Sans" lineHeight={18} text="Use Pause clock, Resume clock, or Extend pick in 09 // DRAFT CLOCK." />
							</div>
						</div>
						<div class="checklist-item">
							<span class="checklist-mark mono">06</span>
							<div class="checklist-item__text">
								<strong>Undo a misclick</strong>
								<small>Type UNDO into Undo last pick in 99 // DANGER ZONE. It re-arms the clock for that slot.</small>
							</div>
						</div>
						<div class="checklist-item">
							<span class="checklist-mark mono">07</span>
							<div class="checklist-item__text">
								<strong>Autopick catches an absent manager</strong>
								<small>Toggle AUTO for a seat in 05 // SEATS, or force one pick now in 09 // DRAFT CLOCK.</small>
							</div>
						</div>
						<div class="checklist-item">
							<span class="checklist-mark mono">08</span>
							<div class="checklist-item__text">
								<strong>There's always a backup</strong>
								<small>A safety copy saves automatically before every pick, undo, and auto-pick — on top of Undo last pick.</small>
							</div>
						</div>
					</div>
					</If>
					<If cond={data.draft_started == false}>
					<form method="post" action={actionPath("draft-reschedule")} data-gosx-managed="true" class="clock-controls draft-reschedule-form">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<div class="draft-reschedule-form__copy">
							<strong>Reschedule the meeting point</strong>
							<p>
								<If cond={data.draft.published}>
									Current meeting: <b>{data.draft.time}</b> on {data.draft.long_date} ({data.draft.timezone}).
								</If>
								<If cond={data.draft.published == false}>
									No draft meeting is published yet.
								</If>
								<If cond={data.draft.overridden}><span class="position-chip">COMMISSIONER OVERRIDE</span></If>
								<If cond={data.draft.overridden == false}><span class="position-chip">LEAGUE CONFIG</span></If>
							</p>
							<small>Choose a future local time in the configured league timezone. This updates what managers and reminders show; it never starts the draft.</small>
						</div>
						<label class="roster-shape-field" for="admin-draft-meeting-at">
							<span class="mono">NEW MEETING · {data.draft.timezone}</span>
							<input id="admin-draft-meeting-at" class="scoring-input admin-datetime-input" type="datetime-local" name="meeting_at" value={data.draft_reschedule.meeting_at} required="required"></input>
						</label>
						<button class="button button--primary" type="submit">Save meeting time</button>
					</form>
					</If>
					<If cond={data.draft_started}>
						<p class="scoring-note"><strong>MEETING LOCKED:</strong> The draft has started, so its meeting record is read-only.</p>
					</If>
					<If cond={data.draft_started == false}>
						<form method="post" action={actionPath("draft-start")} data-gosx-managed="true" class="clock-controls">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<label class="mono" for="admin-draft-start-confirm">TYPE START //</label>
							<input id="admin-draft-start-confirm" class="scoring-input typed-confirm-input" name="confirm" autocomplete="off" enterkeyhint="done" placeholder="START"></input>
							<button class="button button--primary" type="submit">Start draft + pick clock</button>
						</form>
						<p class="scoring-note">This opens the room immediately and starts pick one’s timer. Scheduled time alone never starts it. Pool: {data.pool.mode}, {data.pool.players} players for {data.draft_required_players} draft slots ({data.pool.coverage} target coverage).</p>
					</If>
					<If cond={data.draft.complete}>
						<p class="flash-message"><strong>DRAFT COMPLETE:</strong> Every pick is locked. Continue with season operations.</p>
					</If>
					<If cond={data.draft.complete == false}>
						<If cond={data.draft_started}>
							<p class="flash-message"><strong>DRAFT LIVE:</strong> The commissioner started the draft. That start rules.</p>
						</If>
					</If>
				</section>
				<section id="admin-schedule" aria-labelledby="admin-schedule-heading" tabindex="-1" data-admin-section="schedule" class={"player-pool admin-season-ops" + data.section_class_schedule}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">01 // SCHEDULE</span>
							<h2 id="admin-schedule-heading">Schedule</h2>
							<p class="scoring-note">
								This plan is durable league state. The final draft-order draw publishes the first 14-week plan automatically; an emergency order redraw preserves it.
							</p>
						</div>
						<span class="position-chip">{data.schedule.status}</span>
					</div>
					<If cond={data.schedule.has_schedule == false}>
						<div class="empty-tape">
							<strong>NO SCHEDULE GENERATED</strong>
							<p>Drawing the final draft order publishes a 14-week plan beginning with NFL week 1. Use this manual control only when a custom span is required before the draw.</p>
						</div>
						<form method="post" action={actionPath("schedule-generate")} data-gosx-managed="true" class="season-control-form">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<div class="roster-shape-form-grid">
								<label class="roster-shape-field">
									<span class="mono">WEEKS</span>
									<input class="scoring-input" type="number" name="weeks" value={data.schedule_generation.weeks} min="1" max="18" required="required"></input>
								</label>
								<label class="roster-shape-field">
									<span class="mono">FIRST NFL WEEK</span>
									<input class="scoring-input" type="number" name="start_week" value={data.schedule_generation.start_week} min="1" max="18" required="required"></input>
								</label>
								<label class="roster-shape-field">
									<span class="mono">SEED (OPTIONAL)</span>
									<input class="scoring-input" type="number" name="seed" value={data.schedule_generation.seed} placeholder="draw at generation"></input>
								</label>
							</div>
							<button class="button button--primary" type="submit">Generate regular-season schedule</button>
						</form>
					</If>
					<If cond={data.schedule.has_schedule}>
						<div class="pool-stats">
							<div class="pool-stat"><span>Season</span><b class="mono">{data.schedule.season}</b></div>
							<div class="pool-stat"><span>Weeks</span><b class="mono">{data.schedule.start_week}–{data.schedule.end_week}</b></div>
							<div class="pool-stat"><span>Generated</span><b class="mono">{data.schedule.generated_at}</b></div>
							<div class="pool-stat"><span>Phase</span><b class="mono">{data.schedule.phase}</b></div>
							<div class="pool-stat"><span>Final</span><b class="mono">{data.schedule.final_weeks}/{data.schedule.week_count} WEEKS · {data.schedule.final_matchups}/{data.schedule.total_matchups} MATCHUPS</b></div>
						</div>
						{/* J2 F33 (gap-audit): a nineteen-digit machine seed sat beside
						    plain-language stats on a page that otherwise reads in
						    words — it moves behind its own closed disclosure, plainly
						    labelled, with the explanation that already named it the
						    redraw trail. */}
						<details class="admin-redraw-trail">
							<summary class="mono">Redraw trail</summary>
							<div class="pool-stat"><span>Seed</span><b class="mono">{data.schedule.seed}</b></div>
							<p class="scoring-note">The stored seed is the redraw trail. A redraw creates a new seed and is available only before season start and before any matchup is final.</p>
						</details>
						<If cond={data.schedule.regenerate_allowed}>
							<form method="post" action={actionPath("schedule-regenerate")} data-gosx-managed="true" class="clock-controls">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<label class="mono" for="admin-schedule-regenerate-confirm">TYPE REDRAW SCHEDULE //</label>
								<input id="admin-schedule-regenerate-confirm" class="scoring-input typed-confirm-input" name="confirm" value={data.schedule_regeneration.confirm} autocomplete="off" enterkeyhint="done" placeholder="REDRAW SCHEDULE"></input>
								<button class="button button--ghost" type="submit">Redraw schedule</button>
							</form>
						</If>
						<If cond={data.schedule.regenerate_allowed == false}>
							<p class="demo-message"><strong>REDRAW LOCKED:</strong> {data.schedule.regenerate_lock_reason}.</p>
						</If>
					</If>
				</section>
				<section id="admin-week-close" aria-labelledby="admin-week-close-heading" tabindex="-1" data-admin-section="week-close" class={"player-pool admin-season-ops" + data.section_class_week_close}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">02 // WEEK CLOSE</span>
							<h2 id="admin-week-close-heading">Week close</h2>
							<p class="scoring-note">Readiness is advisory. The normal close waits for every real game and a settled player ledger; the override is explicit and records the override in the league log.</p>
						</div>
					</div>
					<If cond={data.schedule.has_schedule == false}>
						<div class="empty-tape"><strong>GENERATE THE SCHEDULE FIRST</strong><p>Week close controls appear after a regular-season plan exists.</p></div>
					</If>
					<If cond={data.schedule.has_schedule}>
						<div class="pool-stats">
							<div class="pool-stat"><span>Selected week</span><b class="mono">WEEK {data.schedule.close.week}</b></div>
							<div class="pool-stat"><span>Games</span><b class="mono">{data.schedule.close.games_final}/{data.schedule.close.games_total} FINAL</b></div>
							<div class="pool-stat"><span>Stats updated</span><b class="mono">{data.schedule.close.stats_updated}</b></div>
							<div class="pool-stat"><span>Readiness</span><b class="mono">{data.schedule.close.ready_label}</b></div>
						</div>
						<p class="scoring-note"><strong>WHY:</strong> {data.schedule.close.reason}</p>
						<If cond={data.schedule.close.final}>
							<p class="flash-message"><strong>ALREADY FINAL:</strong> Closing the week again changes nothing.</p>
						</If>
						<If cond={data.schedule.close.final == false}>
							<form method="post" action={actionPath("close-week-ready")} data-gosx-managed="true" class="clock-controls">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<label class="mono" for="admin-close-ready-week">WEEK //</label>
								<input id="admin-close-ready-week" class="scoring-input" type="number" name="week" value={data.close_form.week} min="1" max="18" required="required"></input>
								<If cond={data.schedule.close.ready}>
									<button class="button button--primary" type="submit">Close ready week {data.close_form.week}</button>
								</If>
								<If cond={data.schedule.close.ready == false}>
									<button class="button" type="submit" disabled="disabled">Normal close waits for readiness</button>
								</If>
							</form>
							{/* F3 (J4 console gap-audit): a forced close used to report bare
							    success with no warning beforehand — the commissioner learned
							    the week scored 0.0 for every starter only after the click.
							    This states the consequence before the confirm field, in the
							    same place the typed-confirm phrase already asks for intent. */}
							<p class="scoring-note"><strong>BEFORE YOU FORCE THIS:</strong> Force close scores week {data.close_form.week} with whatever stats exist right now. A game or a player-stat join that has not settled yet scores 0.0, and this cannot be undone from this screen.</p>
							<form method="post" action={actionPath("close-week-force")} data-gosx-managed="true" class="clock-controls">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<label class="mono" for="admin-close-force-week">WEEK //</label>
								<input id="admin-close-force-week" class="scoring-input" type="number" name="week" value={data.close_form.week} min="1" max="18" required="required"></input>
								<label class="mono" for="admin-close-week-confirm">TYPE CLOSE WEEK {data.close_form.week} //</label>
								<input id="admin-close-week-confirm" class="scoring-input typed-confirm-input" name="confirm" value={data.close_form.confirm} autocomplete="off" enterkeyhint="done" placeholder={"CLOSE WEEK " + data.close_form.week}></input>
								{/* F11 (J4 console gap-audit): this was the panel's one live,
								    irreversible control, sharing class="button button--ghost"
								    with its two disabled neighbors above — the panel's own
								    scoped muting rule made all three read as unavailable. It
								    now carries the same destructive treatment RELEASE SEAT and
								    the Danger Zone resets already use. */}
								<button class="button button--danger" type="submit">Force close week {data.close_form.week}</button>
							</form>
						</If>
					</If>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">03 // WAIVER RUN</span>
							<h2 id="admin-waivers-heading">Force an out-of-cycle waiver run</h2>
							<p class="scoring-note">The daily processor already resolves every due claim on its own schedule. Use this only when a run is stuck or overdue — it resolves every currently due claim immediately and cannot be undone from this screen.</p>
						</div>
					</div>
					<div class="pool-stats">
						<div class="pool-stat"><span>Open claims</span><b class="mono">{data.waivers.open_claim_count}</b></div>
						<div class="pool-stat"><span>Last processed</span><b class="mono">{data.waivers.processed_through}</b></div>
						<div class="pool-stat"><span>Run state</span><b class="mono">{data.waivers.run_state}</b></div>
					</div>
					<If cond={data.waivers.has_open_claims == false}>
						<p class="scoring-note">No claims are open right now — there is nothing for a forced run to resolve.</p>
					</If>
					<form method="post" action={actionPath("run-waivers")} data-gosx-managed="true" class="clock-controls">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<input type="hidden" name="waiver_run_token" value={data.waivers.run_token}></input>
						<label class="mono" for="admin-run-waivers-confirm">TYPE RUN WAIVERS NOW //</label>
						<input id="admin-run-waivers-confirm" class="scoring-input typed-confirm-input" name="confirm" value={data.waivers_run_confirm} autocomplete="off" enterkeyhint="done" placeholder="RUN WAIVERS NOW"></input>
						<If cond={data.waivers.has_open_claims == true}>
							<button class="button button--ghost" type="submit">Force run waivers now</button>
						</If>
						<If cond={data.waivers.has_open_claims == false}>
							<button class="button" type="submit" disabled="disabled">No open claims to run</button>
						</If>
					</form>
					<p class="demo-message"><strong>PLAYOFF TIMING:</strong> preview and publish the bracket only after final regular-season standings exist. Weekly advancement is gated on the authoritative starter ledger; see 04 // PLAYOFFS below.</p>
				</section>
				<section id="admin-playoffs" aria-labelledby="admin-playoffs-heading" tabindex="-1" data-admin-section="playoffs" class={"player-pool admin-season-ops" + data.section_class_playoffs}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">04 // PLAYOFFS</span>
							<h2 id="admin-playoffs-heading">Playoffs</h2>
							<p class="scoring-note">One persisted bracket is shared everywhere. A preview is commissioner-only; publication is explicit and idempotent. Scores are never accepted from this browser.</p>
						</div>
						<span class="position-chip">{data.playoff_truth.status_label}</span>
					</div>
					<div class="pool-stats">
						<div class="pool-stat"><span>Phase</span><b class="mono">{data.playoff_truth.season_phase_label}</b></div>
						{/* J2 F17 (gap-audit): "Source" read as a labelled cell with
						    nothing in it before any bracket is persisted — this states
						    that plainly instead of leaving the cell blank. */}
						<If cond={data.playoff_truth.source != ""}>
							<div class="pool-stat"><span>Source</span><b class="mono">{data.playoff_truth.source}</b></div>
						</If>
						<If cond={data.playoff_truth.source == ""}>
							<div class="pool-stat"><span>Source</span><b class="mono">— no bracket yet</b></div>
						</If>
						<div class="pool-stat"><span>Final week</span><b class="mono">{data.playoff_truth.final_week}</b></div>
						<div class="pool-stat"><span>Revision</span><b class="mono">{data.playoff_truth.revision}</b></div>
					</div>
					<p class="scoring-note"><strong>STATUS:</strong> {data.playoff_truth.detail}</p>
					<If cond={data.playoff_truth.recovery != ""}><p class="demo-message"><strong>RECOVERY:</strong> {data.playoff_truth.recovery}</p></If>
					<If cond={data.playoff_truth.season_phase == "playoffs"}>
						<form method="post" action={actionPath("playoff-preview")} data-gosx-managed="true" class="clock-controls">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<button class="button button--primary" type="submit">Build commissioner preview</button>
						</form>
					</If>
					<If cond={data.playoff_truth.season_phase != "playoffs"}>
						<button class="button button--primary" type="button" disabled="disabled">Preview unavailable - league is in {data.playoff_truth.season_phase_label}, not playoffs</button>
					</If>
					<If cond={data.playoff_truth.is_preview}>
						<form method="post" action={actionPath("playoff-publish")} data-gosx-managed="true" class="clock-controls">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<label class="mono" for="admin-playoff-preview-id">PREVIEW ID //</label>
							<input id="admin-playoff-preview-id" class="scoring-input" name="preview_id" value={data.playoff_truth.preview_id} required="required"></input>
							<label class="mono" for="admin-playoff-publish-confirm">TYPE PUBLISH PLAYOFF BRACKET //</label>
							<input id="admin-playoff-publish-confirm" class="scoring-input typed-confirm-input" name="confirm" autocomplete="off" enterkeyhint="done" placeholder="PUBLISH PLAYOFF BRACKET" required="required"></input>
							<button class="button button--primary" type="submit">Publish this preview</button>
						</form>
					</If>
					<If cond={data.playoff_truth.is_published}>
						<form method="post" action={actionPath("playoff-advance")} data-gosx-managed="true" class="clock-controls">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<button class="button button--primary" type="submit">Apply final scoring ledger</button>
						</form>
						<p class="scoring-note">Advancement waits until every active matchup week is final, complete, available, and authoritative. A retry is safe.</p>
						<form method="post" action={actionPath("playoff-correct")} data-gosx-managed="true" class="season-control-form">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<div class="roster-shape-form-grid">
								<label class="roster-shape-field"><span class="mono">MATCHUP ID</span><input class="scoring-input" name="matchup_id" required="required"></input></label>
								<label class="roster-shape-field"><span class="mono">WINNER TEAM ID</span><input class="scoring-input" name="winner_team_id" required="required"></input></label>
								<label class="roster-shape-field"><span class="mono">HOME SCORE (OPTIONAL)</span><input class="scoring-input" name="home_score" inputmode="decimal"></input></label>
								<label class="roster-shape-field"><span class="mono">AWAY SCORE (OPTIONAL)</span><input class="scoring-input" name="away_score" inputmode="decimal"></input></label>
								<label class="roster-shape-field"><span class="mono">AUDIT REASON</span><input class="scoring-input" name="reason" required="required"></input></label>
								<label class="roster-shape-field typed-confirm-row"><span class="mono">TYPE CORRECT PLAYOFF BRACKET</span><input class="scoring-input typed-confirm-input" name="confirm" autocomplete="off" enterkeyhint="done" placeholder="CORRECT PLAYOFF BRACKET" required="required"></input></label>
							</div>
							<button class="button button--ghost" type="submit">Record confirmed correction</button>
						</form>
						<p class="demo-message"><strong>CONSEQUENCE:</strong> terminal corrections are audited. Earlier-round corrections are refused here because downstream participants would otherwise be stale; build a fresh preview instead.</p>
					</If>
					<If cond={data.playoff_truth.has_matchups}>
						<div class="activity-feed" aria-label="Persisted playoff matchups">
							<Each of={data.playoff_truth.matchups} as="matchup"><div class="activity-item"><p><strong>{matchup.bracket} R{matchup.round} · WEEK {matchup.week}</strong> {matchup.home_team_name} {matchup.home_score_text} — {matchup.away_team_name} {matchup.away_score_text}<b>{matchup.tie_break_explanation}</b></p></div></Each>
						</div>
					</If>
				</section>
				<section id="admin-seats" aria-labelledby="admin-seats-heading" tabindex="-1" data-admin-section="seats" class={"player-pool" + data.section_class_seats}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">05 // SEATS</span>
							<h2 id="admin-seats-heading">Seats</h2>
							<p class="scoring-note" id="admin-avatar-upload-help">PNG or JPEG, 10 MB maximum, from 64×64 through 4096×4096 pixels. Images are center-cropped and resized to a 512×512 PNG with metadata removed. If this seat has a claimed badge, uploading a custom image releases it so another team can use it.</p>
						</div>
					</div>
					<If cond={data.identity_available == false}>
						<p class="error-message" id="admin-identity-status" role="status">{data.identity_error}</p>
					</If>
					<div class="seat-list">
						<Each of={data.seats} as="seat">
							<SeatRow
								seat={seat}
								ReleaseAction={actionPath("seat-release")}
								RenameAction={actionPath("team-rename")}
								AutopickAction={actionPath("clock-set-autopick")}
								AvatarResetAction={actionPath("avatar-reset")}
								CoDetachAction={actionPath("co-detach")}
								CSRF={csrf.token}
							 />
						</Each>
					</div>
				</section>
				<section id="admin-invites" aria-labelledby="admin-invites-heading" tabindex="-1" data-admin-section="invites" class={"player-pool" + data.section_class_invites}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">06 // INVITES</span>
							<h2 id="admin-invites-heading">Invites</h2>
							<If cond={data.invite_count > 0}>
								{/* J2 F21 (gap-audit): this card's own count used to read
								    "N seated" beside a league-status badge that reads "M / M
								    SEATS" — two different counts (admitted addresses vs
								    claimed seats) with no label saying so, so a commissioner
								    reading both on the same page got two contradictory
								    answers. This now names what it counts. */}
								<p class="scoring-note invite-progress" aria-label="Invitation progress">
									<strong>{data.invite_seated_count} of {data.invite_count} admitted addresses hold a seat</strong>
									<span>{data.invite_ready_count} ready</span>
									<If cond={data.invite_seatless_count > 0}>
										<span>{data.invite_seatless_count} signed in without a seat</span>
									</If>
									<span>{data.invite_waiting_count} waiting to sign in</span>
								</p>
							</If>
						</div>
					</div>
					<If cond={data.has_unclaimed_seats}>
						<If cond={data.league_open}>
							<p class="demo-message">
								<strong>OPEN LEAGUE:</strong>
								no invite list or membership domain is set, so any Google account may claim a seat. Add the
								{data.league.seat_count_word}
								manager emails below.
							</p>
						</If>
						<If cond={data.league_domain_gated}>
							<p class="demo-message">
								<strong>DOMAIN-GATED:</strong>
								any Google account ending in
								<b class="mono">@{data.league_domain}</b>
								may claim a seat automatically. Add an email below only to invite someone outside that domain.
							</p>
						</If>
					</If>
					<If cond={data.has_unclaimed_seats == false}>
						<p class="demo-message">
							<strong>SEATS FULL:</strong>
							every seat is claimed; a new Google sign-in has no seat left to claim. Release a seat in 05 // SEATS to open one, or assign an admitted, seatless member below.
						</p>
					</If>
					<form class="invite-form" method="post" action={actionPath("invite-add")} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<label for="admin-invite-email" class="visually-hidden">Manager email to invite</label>
						<input
							id="admin-invite-email"
							type="email"
							name="email"
							placeholder="manager@example.com"
							autocomplete="email"
							inputmode="email"
							enterkeyhint="done"
							required="required"
						 />
						<button class="button button--primary" type="submit">Invite</button>
					</form>
					<div class="invite-list">
						<Each of={data.invites} as="invite">
							<article class="invite-row" data-status={invite.status}>
								<div class="invite-identity">
									<TextBlock as="b" class="mono" font="600 13px IBM Plex Mono" lineHeight={18} maxLines={2} overflow="ellipsis" text={invite.email} />
									<TextBlock as="small" font="400 13px Plus Jakarta Sans" lineHeight={18} maxLines={2} overflow="ellipsis" text={invite.status_detail} />
									<span class="position-chip">{invite.source}</span>
								</div>
								<b class={"ready-state " + invite.status_class}>{invite.status}</b>
								<div class="invite-actions">
									<If cond={data.mail_enabled}>
										<form method="post" action={actionPath("invite-send")} data-gosx-managed="true">
											<input type="hidden" name="csrf_token" value={csrf.token}></input>
											<input type="hidden" name="email" value={invite.email}></input>
											<button class="board-button" type="submit">Email</button>
										</form>
									</If>
									<a href={invite.mailto} class="board-button">Mail app</a>
									<If cond={invite.removable}>
										<details class="invite-remove-disclosure">
											<summary class="board-button board-button--cut" aria-label={"Remove invitation for " + invite.email}>✕</summary>
											<form method="post" action={actionPath("invite-remove")} data-gosx-managed="true">
												<input type="hidden" name="csrf_token" value={csrf.token}></input>
												<input type="hidden" name="email" value={invite.email}></input>
												<p>Remove the invitation for {invite.email}? They will no longer be able to join using this invite.</p>
												<button class="board-button board-button--cut" type="submit">Confirm remove</button>
											</form>
										</details>
									</If>
									<If cond={invite.removable == false}>
										<small class="mono">pinned</small>
									</If>
								</div>
							</article>
						</Each>
					</div>
					<details class="invite-preview">
						<summary class="mono">INVITE PREVIEW</summary>
						<p class="mono">{data.invite_preview.subject}</p>
						<pre>{data.invite_preview.body}</pre>
					</details>
					<If cond={data.mail_enabled == false}>
						<p class="demo-message">
							The league cannot send email yet. Use the Mail app links.
						</p>
					</If>
					<If cond={data.has_unclaimed_seats}>
						<p class="scout-callout">
							Send managers this address: they sign in with Google and the next open seat is theirs.
						</p>
					</If>
					<If cond={data.has_unclaimed_seats == false}>
						<If cond={data.seatless_members_empty == false}>
							<h3 class="mono">SEATLESS MEMBERS — SIGNED IN, NO SEAT</h3>
							<div class="invite-list">
								<Each of={data.seatless_members} as="member">
									<article class="invite-row">
										<div class="invite-identity">
											<TextBlock as="b" class="mono" font="600 13px IBM Plex Mono" lineHeight={18} maxLines={2} overflow="ellipsis" text={member.email} />
											<TextBlock as="small" font="400 13px Plus Jakarta Sans" lineHeight={18} maxLines={2} overflow="ellipsis" text={member.name} />
										</div>
										<small class="mono">Assign a seat in 05 // SEATS, or release a claimed one to make room.</small>
									</article>
								</Each>
							</div>
						</If>
						<If cond={data.seatless_members_empty}>
							<p class="scoring-note">Every admitted member already holds a seat.</p>
						</If>
					</If>
				</section>

				<section id="admin-draft-order" aria-labelledby="admin-draft-order-heading" tabindex="-1" data-admin-section="draft-order" class={"player-pool" + data.section_class_draft_order}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">07 // DRAFT ORDER</span>
							<h2 id="admin-draft-order-heading">Draft order</h2>
						</div>
						<If cond={data.order_randomized}>
							<span class="position-chip">RANDOMIZED</span>
						</If>
						<If cond={data.order_randomized == false}>
							<span class="position-chip">DEFAULT ORDER</span>
						</If>
					</div>
					<div class="order-list">
						{/* F29 (gap-audit J2): "who picks seventh?" is the
						    week's most common question; the order used to
						    list eight teams with a division chip and no
						    ordinal at all. Pick numbers here name the real
						    draft slot, not a seat index — Commissioner HQ's
						    own numbered order (a different card) already
						    used seat indexes and stays unchanged. */}
						<Each of={data.draft_order} as="team">
							<article class="order-row">
								<span class={"team-mark tone-" + team.tone}>
									<If cond={team.has_avatar_image}>
										<img class="avatar-mark__photo" src={team.avatar_image_url} alt={team.name} width="42" height="42" loading="lazy" />
									</If>
									<If cond={team.has_avatar_image == false}>
										{team.abbreviation}
									</If>
								</span>
								<div class="seat-identity">
									<TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={1} overflow="ellipsis">
										<span class="mono">Pick {team.pick_number} ·</span> {team.name}
									</TextBlock>
									<TextBlock as="small" font="400 13px Plus Jakarta Sans" lineHeight={18} maxLines={1} overflow="ellipsis" text={team.manager} />
									<span class="position-chip">{team.division}</span>
									<If cond={team.id == data.viewer.team_id}>
										<span class="position-chip">YOUR SEAT</span>
									</If>
								</div>
							</article>
						</Each>
					</div>
					<If cond={data.has_unclaimed_seats}>
						<If cond={data.draft_started == false}>
							<form method="post" action={actionPath("seat-trim")} data-gosx-managed="true" class="seat-trim-form">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name="unclaimed_seat_token" value={data.unclaimed_seat_token}></input>
								<label for="admin-seat-trim-confirm">Type <span class="mono">{data.unclaimed_seat_confirm}</span> to confirm.</label>
								<input id="admin-seat-trim-confirm" class="scoring-input typed-confirm-input" type="text" name="confirm" value="" autocomplete="off" enterkeyhint="done" placeholder={data.unclaimed_seat_confirm} required="required"></input>
								<button class="button board-button--cut" type="submit">Drop {data.unclaimed_seat_label}</button>
							</form>
							<p class="demo-message">
								<strong>SCHEDULE WARNING:</strong>
								if a schedule already exists, this action discards that unplayed schedule. The final order draw will publish a replacement for the kept teams.
							</p>
							<p class="scoring-note">
								{data.unclaimed_seat_label}
								{data.unclaimed_seat_verb} no manager. Drop them first, then randomize. An unclaimed seat takes a turn
								in every round: it runs the full pick clock down, then autopicks a player. Reload this page if the claim count changes before you confirm.
							</p>
						</If>
					</If>
					<If cond={data.draft_started}>
						<button class="button button--primary" type="button" disabled="disabled">Draw order unavailable - the draft has already started</button>
						<p class="scoring-note">The order and schedule lock once the commissioner starts the draft. Reset the draft in 99 // DANGER ZONE to change them again.</p>
					</If>
					<If cond={data.draft_started == false}>
						<If cond={data.order_randomized == false}>
							<form method="post" action={actionPath("order-randomize")} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name={data.admin_return_target_field} value={data.admin_draft_order_return_target}></input>
								<input type="hidden" name="order_token" value=""></input>
								<button class="button button--primary" type="submit">Draw order + schedule · queue reminders</button>
							</form>
							<p class="scoring-note">
								One click runs six shuffle passes in memory, atomically publishes the final order and 14-week schedule, then reports how many manager reminders were queued. Queued is not delivery.
							</p>
						</If>
						<If cond={data.order_randomized}>
							<p class="demo-message">
								<strong>FINAL ORDER PUBLISHED:</strong>
								an ordinary second click cannot redraw it or queue the league again.
							</p>
							<form method="post" action={actionPath("order-randomize")} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name={data.admin_return_target_field} value={data.admin_draft_order_return_target}></input>
								<input type="hidden" name="order_token" value={data.draft_order_token}></input>
								<label for="draft-order-redraw-confirm">Emergency replacement draw</label>
								<input id="draft-order-redraw-confirm" class="typed-confirm-input" type="text" name="confirm" placeholder="type REDRAW ORDER" autocomplete="off" enterkeyhint="done"></input>
								<button class="button" type="submit">Redraw and queue replacement</button>
							</form>
							<p class="scoring-note">
								Replacement draws run six passes, preserve the published schedule, and queue exactly one new notice. Queued is not delivery. Use only when the published draw must be replaced.
							</p>
						</If>
					</If>
					<p class="scoring-note">Run this one hour before the draft. Locked once the commissioner starts the draft.</p>
				</section>
				<section id="admin-data" aria-labelledby="admin-data-heading" tabindex="-1" data-admin-section="data" class={"player-pool" + data.section_class_data}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">08 // PLAYER DATA</span>
							<h2 id="admin-data-heading">Data</h2>
						</div>
					</div>
					<If cond={data.pool.error != ""}>
						<p class="error-message">{data.pool.error}</p>
					</If>
					<div class="pool-stats">
						<div class="pool-stat">
							<span>Mode</span>
							<b class="mono">{data.pool.mode}</b>
						</div>
						<div class="pool-stat">
							<span>Pool coverage</span>
							<b class="mono">ACTUAL {data.pool.actual_coverage} · TARGET {data.pool.coverage}</b>
						</div>
						<div class="pool-stat">
							<span>Players / roster slots</span>
							<b class="mono">{data.pool.players} / {data.pool.roster_capacity}</b>
						</div>
						<div class="pool-stat">
							<span>Post-draft cushion</span>
							<b class="mono">{data.pool.cushion}</b>
						</div>
						<div class="pool-stat">
							<span>ADP</span>
							<b class="mono">{data.pool.with_adp}</b>
						</div>
						<div class="pool-stat">
							<span>Projections</span>
							<b class="mono">{data.pool.with_proj}</b>
						</div>
						<div class="pool-stat">
							<span>Projection week</span>
							<b class="mono">{data.pool.projection_week}</b>
						</div>
						<div class="pool-stat">
							<span>Byes</span>
							<b class="mono">{data.pool.with_bye}</b>
						</div>
						<div class="pool-stat">
							<span>Data calls used</span>
							<b class="mono">{data.pool.requests}</b>
						</div>
						<div class="pool-stat">
							<span>Last updated</span>
							<b class="mono">{data.pool.last_sync}</b>
						</div>
					</div>
					<If cond={data.pool.players > 0}>
						<div class="position-filters">
							<Each of={data.pool.positions_list} as="entry">
								<span class="position-chip">
									{entry.pos}
									{entry.count}
								</span>
							</Each>
						</div>
					</If>
				</section>
				<section id="admin-clock" aria-labelledby="admin-clock-heading" tabindex="-1" data-admin-section="clock" class={"player-pool" + data.section_class_clock}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">09 // DRAFT CLOCK</span>
							<h2 id="admin-clock-heading">Clock</h2>
						</div>
						<span class="position-chip">{data.clock.state}</span>
					</div>
					<div class="pool-stats">
						<div class="pool-stat">
							<span>State</span>
							<b class="mono">{data.clock.state}</b>
						</div>
						<div class="pool-stat">
							<span>Reason</span>
							<b class="mono">{data.clock.reason}</b>
						</div>
						<div class="pool-stat">
							<span>Deadline</span>
							{/* F16 (gap-audit J2): this printed the raw RFC3339 UTC
							    instant ("2026-09-04T10:00:03Z") on the one row that
							    matters during a live pick, four hours off the
							    league's own clock and unlike every other timestamp
							    on this page. Mirrors the READ AT row's own display/
							    ISO/relative split above. */}
							<If cond={data.clock.deadline != ""}>
								<b class="mono"><time datetime={data.clock.deadline}>{data.clock.deadline_display}<If cond={data.clock.deadline_relative != ""}> · {data.clock.deadline_relative}</If></time></b>
							</If>
							<If cond={data.clock.deadline == ""}>
								<b class="mono">— no pick is armed</b>
							</If>
						</div>
						<div class="pool-stat">
							{/* J2 F32 (gap-audit): the raw-seconds form ("120 S") never
							    matched the room's own mm:ss clock ("2:00") for the same
							    quantity — duration_label/remaining_label (service.go)
							    already carry that mm:ss form; this just prints them. */}
							<span>Duration</span>
							<b class="mono">{data.clock.duration_label}</b>
						</div>
						<div class="pool-stat">
							<span>Duration source</span>
							<If cond={data.clock.duration_overridden}>
								<b class="mono">OVERRIDE</b>
							</If>
							<If cond={data.clock.duration_overridden == false}>
								<b class="mono">DEFAULT</b>
							</If>
						</div>
						<div class="pool-stat">
							<span>Remaining</span>
							<b class="mono">{data.clock.remaining_label}</b>
						</div>
					</div>
					<div class="clock-controls">
						<If cond={data.clock.can_pause}>
							<form method="post" action={actionPath("clock-pause")} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<button class="button" type="submit">Pause running clock</button>
							</form>
						</If>
						<If cond={data.clock.can_pause == false}>
							<button class="button" type="button" disabled="disabled">Pause unavailable - clock is {data.clock.state}</button>
						</If>
						<If cond={data.clock.can_resume}>
							<form method="post" action={actionPath("clock-resume")} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<button class="button button--primary" type="submit">Resume / start clock</button>
							</form>
						</If>
						<If cond={data.clock.can_resume == false}>
							<button class="button button--primary" type="button" disabled="disabled">Resume unavailable - {data.clock.state}</button>
						</If>
						<If cond={data.clock.can_extend}>
							<form method="post" action={actionPath("clock-extend")} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name="current_pick_token" value={data.current_pick_token}></input>
								<label for="admin-clock-extend-seconds" class="visually-hidden">Seconds to add to the running pick</label>
								<input id="admin-clock-extend-seconds" class="scoring-input" type="number" name="seconds" value="60" min="1" max="600"></input>
								<button class="button" type="submit">Extend running pick</button>
							</form>
						</If>
						<If cond={data.clock.can_extend == false}>
							<button class="button" type="button" disabled="disabled">Extend unavailable - no running pick</button>
						</If>
						<form method="post" action={actionPath("clock-set-duration")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<input type="hidden" name="seconds" value="60"></input>
							<button class="button button--compact" type="submit">1:00</button>
						</form>
						<form method="post" action={actionPath("clock-set-duration")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<input type="hidden" name="seconds" value="90"></input>
							<button class="button button--compact" type="submit">1:30</button>
						</form>
						<form method="post" action={actionPath("clock-set-duration")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<input type="hidden" name="seconds" value="120"></input>
							<button class="button button--compact" type="submit">2:00</button>
						</form>
						<form method="post" action={actionPath("clock-set-duration")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<input type="hidden" name="seconds" value="180"></input>
							<button class="button button--compact" type="submit">3:00</button>
						</form>
						<form method="post" action={actionPath("clock-set-duration")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<input type="hidden" name="seconds" value="300"></input>
							<button class="button button--compact" type="submit">5:00</button>
						</form>
						<form method="post" action={actionPath("clock-set-duration")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<label for="admin-clock-set-duration-seconds" class="visually-hidden">Custom pick clock duration in seconds</label>
							<input id="admin-clock-set-duration-seconds" class="scoring-input" type="number" name="seconds" placeholder="120" min="10" max="600"></input>
							<button class="button" type="submit">Set duration</button>
						</form>
						<details class="draft-destructive-control">
							<summary class="button button--ghost">Force current pick now</summary>
							<form method="post" action={actionPath("clock-force-autopick")} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name="current_pick_token" value={data.current_pick_token}></input>
								<p>This immediately consumes the on-clock seat's Big Board target, or best available if its board is empty. It advances the draft even when the clock is paused.</p>
								<label class="mono" for="admin-force-current-pick-confirm">TYPE FORCE CURRENT PICK //</label>
								<input id="admin-force-current-pick-confirm" class="scoring-input typed-confirm-input" type="text" name="confirm" value={data.force_current_pick_confirm} autocomplete="off" enterkeyhint="done" placeholder="FORCE CURRENT PICK" required="required"></input>
								<button class="button button--ghost" type="submit">Confirm force current pick</button>
							</form>
						</details>
					</div>
					<p class="scoring-note">
						Extend adds seconds to the current pick. Set duration applies from the next arm; it does not change the running deadline.
					</p>
				</section>
				<section id="admin-roster" aria-labelledby="admin-roster-heading" tabindex="-1" data-admin-section="roster" class={"player-pool" + data.section_class_roster}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">10 // ROSTER SHAPE</span>
							<h2 id="admin-roster-heading">Roster</h2>
						</div>
						<If cond={data.roster_shape.has_override}>
							<span class="position-chip">CUSTOM</span>
						</If>
						<If cond={data.roster_shape.has_override == false}>
							<span class="position-chip">DEFAULT</span>
						</If>
					</div>
					<div class="roster-shape-grid">
						<Each of={data.roster_shape.slots} as="slot">
							<div class="pool-stat">
								<span>{slot.key}</span>
								<b class="mono">{slot.count}</b>
							</div>
						</Each>
						<div class="pool-stat">
							<span>BENCH</span>
							<b class="mono">{data.roster_shape.bench}</b>
						</div>
						<If cond={data.roster_shape.reserve_total > 0}>
							<div class="pool-stat">
								<span>RESERVE</span>
								<b class="mono">{data.roster_shape.reserve_total}</b>
							</div>
						</If>
						<If cond={data.roster_shape.ir > 0}>
							<div class="pool-stat">
								<span>IR</span>
								<b class="mono">{data.roster_shape.ir}</b>
							</div>
						</If>
					</div>
					<p class="scoring-note">
						{data.roster_shape.starters}
						starters +
						{data.roster_shape.bench}
						bench + reserve =
						{data.roster_shape.rounds}
						draft rounds. IR (
						{data.roster_shape.ir}
						) sits outside that total — in-season stash only, not draftable.
					</p>
					<If cond={data.roster_shape.draft_started}>
						<p class="demo-message">
							<strong>LOCKED:</strong>
							the roster shape locks once the draft starts. Reset the draft in 99 // DANGER ZONE to change it again.
						</p>
					</If>
					<If cond={data.roster_shape.draft_started == false}>
						<form method="post" action={actionPath("roster-shape-apply")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<div class="roster-shape-form-grid">
								<Each of={data.roster_shape.slots} as="slot">
									<label class="roster-shape-field">
										<span class="mono">{slot.key}</span>
										<input class="scoring-input" type="number" name={slot.field_name} value={slot.count} min="0" max="4"></input>
									</label>
								</Each>
								<label class="roster-shape-field">
									<span class="mono">BENCH</span>
									<input class="scoring-input" type="number" name="bench" value={data.roster_shape.bench} min="0" max="10"></input>
								</label>
							</div>
							<p class="scoring-note">
								RESERVE (position-gated, counts toward rounds) — 0 leaves a position out of the zone entirely.
							</p>
							<div class="roster-shape-form-grid">
								<Each of={data.roster_shape.reserve_rows} as="row">
									<label class="roster-shape-field">
										<span class="mono">{row.key}</span>
										<input class="scoring-input" type="number" name={row.field_name} value={row.count} min="0" max="4"></input>
									</label>
								</Each>
							</div>
							<p class="scoring-note">
								IR (injury-gated, outside rounds) and per-position LIMITS (0 = unlimited).
							</p>
							<div class="roster-shape-form-grid">
								<label class="roster-shape-field">
									<span class="mono">IR</span>
									<input class="scoring-input" type="number" name="ir" value={data.roster_shape.ir} min="0" max="10"></input>
								</label>
								<Each of={data.roster_shape.limit_rows} as="row">
									<label class="roster-shape-field">
										<span class="mono">
											MAX
											{row.key}
										</span>
										<input class="scoring-input" type="number" name={row.field_name} value={row.count} min="0" max="20"></input>
									</label>
								</Each>
							</div>
							<button class="button button--primary" type="submit">Apply roster shape</button>
						</form>
						<If cond={data.roster_shape.has_override}>
							<form method="post" action={actionPath("roster-shape-reset")} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<button class="button button--ghost" type="submit">Reset to default</button>
							</form>
						</If>
					</If>
					<div class="pool-toolbar" id="roster-correction">
						<div>
							<span class="section-index">10B // ROSTER CORRECTION</span>
							<h2 id="admin-roster-correction-heading">Correct a team's roster</h2>
							<p class="scoring-note">
								Corrects one named team's roster on its own behalf — for example, undoing a bad autopick right after the draft. This applies immediately, with no waiver period. Review the change, then confirm; the reason is recorded in the audit trail and activity feed, and the team's manager sees a one-time notice on their next visit to Team.
							</p>
						</div>
					</div>
					<form method="get" action="/admin" class="clock-controls">
						<input type="hidden" name="section" value="roster"></input>
						<label class="mono" for="admin-roster-correction-team">TEAM //</label>
						<select id="admin-roster-correction-team" name="correction_team">
							<option value="">Choose a team</option>
							<Each of={data.roster_correction.team_options} as="opt">
								<option value={opt.id} selected={opt.selected}>{opt.label}</option>
							</Each>
						</select>
						<button class="button button--ghost" type="submit">Choose team</button>
					</form>
					<If cond={data.roster_correction.has_team == false}>
						<div class="empty-tape">
							<strong>CHOOSE A TEAM</strong>
							<p>Choose the team whose roster needs a correction.</p>
						</div>
					</If>
					<If cond={data.roster_correction.has_team && data.roster_correction.draft_complete == false}>
						<div class="empty-tape">
							<strong>DRAFT NOT COMPLETE</strong>
							<p>Roster corrections are available once the draft is complete and real rosters exist.</p>
						</div>
					</If>
					<If cond={data.roster_correction.has_team && data.roster_correction.draft_complete}>
						<p class="scoring-note">
							Correcting
							<strong>{data.roster_correction.selected_team_name}</strong>
							.
						</p>
						<form method="get" action="/admin" class="clock-controls">
							<input type="hidden" name="section" value="roster"></input>
							<input type="hidden" name="correction_team" value={data.roster_correction.selected_team_id}></input>
							<label class="mono" for="admin-roster-correction-pos">FILTER ADD BY POSITION //</label>
							<select id="admin-roster-correction-pos" name="correction_pos">
								<Each of={data.roster_correction.pos_tabs} as="tab">
									<option value={tab.value} selected={tab.selected}>{tab.label}</option>
								</Each>
							</select>
							<button class="button button--ghost" type="submit">Filter free agents</button>
						</form>
						<form method="post" action={actionPath("roster-correction")} data-gosx-managed="true" class="season-control-form">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<input type="hidden" name="team_id" value={data.roster_correction.selected_team_id}></input>
							<div class="roster-shape-form-grid">
								<label class="roster-shape-field" for="admin-roster-correction-drop">
									<span class="mono">DROP (OPTIONAL)</span>
									<select id="admin-roster-correction-drop" name="drop_id">
										<option value="">No drop</option>
										<Each of={data.roster_correction.drop_options} as="opt">
											<option value={opt.id} disabled={opt.disabled}>{opt.label}</option>
										</Each>
									</select>
								</label>
								<label class="roster-shape-field" for="admin-roster-correction-add">
									<span class="mono">ADD (OPTIONAL)</span>
									<select id="admin-roster-correction-add" name="add_id">
										<option value="">No add</option>
										<Each of={data.roster_correction.add_options} as="opt">
											<option value={opt.id}>{opt.label}</option>
										</Each>
									</select>
								</label>
							</div>
							<If cond={data.roster_correction.drop_options_empty}>
								<p class="scoring-note">This team's roster is empty, or every player is locked by a kicked-off game this week; only an add is possible right now.</p>
							</If>
							<If cond={data.roster_correction.add_options_empty}>
								<p class="scoring-note">No free agent matches this position filter.</p>
							</If>
							<label class="roster-shape-field" for="admin-roster-correction-reason">
								<span class="mono">REASON (REQUIRED)</span>
								<input id="admin-roster-correction-reason" class="scoring-input" name="reason" maxlength={data.roster_correction.reason_max_length} value={data.roster_correction_form.reason} required="required"></input>
							</label>
							<button class="button button--primary" type="submit">Review correction</button>
						</form>
					</If>
					<If cond={data.roster_correction_review_pending}>
						<div class="demo-message">
							<p>
								<strong>REVIEW:</strong>
								{data.roster_correction_review_summary}
								. Confirming applies this now; it cannot be undone from this screen.
							</p>
							<form method="post" action={actionPath("roster-correction")} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name="team_id" value={data.roster_correction_form.team_id}></input>
								<input type="hidden" name="drop_id" value={data.roster_correction_form.drop_id}></input>
								<input type="hidden" name="add_id" value={data.roster_correction_form.add_id}></input>
								<input type="hidden" name="reason" value={data.roster_correction_form.reason}></input>
								<input type="hidden" name="confirmation" value="correct-roster"></input>
								<button class="button button--primary" type="submit">Confirm correction</button>
							</form>
						</div>
					</If>
				</section>
				<section id="admin-announcements" aria-labelledby="admin-announcements-heading" tabindex="-1" data-admin-section="announcements" class={"player-pool" + data.section_class_announcements}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">11 // ANNOUNCEMENTS</span>
							<h2 id="admin-announcements-heading">Notes</h2>
						</div>
					</div>
					<form method="post" action={actionPath("announcement-post")} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<input type="hidden" name={data.admin_return_target_field} value={data.admin_announcements_return_target}></input>
						<label for="admin-announcement-body" class="visually-hidden">League announcement text</label>
						<textarea id="admin-announcement-body" name="body" class="announcement-textarea" placeholder="Post a note to the whole league..." maxlength="500" rows="3" aria-describedby="admin-announcement-limit"></textarea>
						<small id="admin-announcement-limit" class="scoring-note">Up to 500 characters.</small>
						<If cond={data.mail_enabled}>
							<label class="announcement-email-toggle">
								<input type="checkbox" name="also_email" value="true"></input>
								Also queue an email to the league
							</label>
						</If>
						<If cond={data.mail_enabled == false}>
							<label class="announcement-email-toggle">
								<input type="checkbox" name="also_email" value="true" disabled="disabled"></input>
								Also queue an email to the league — unavailable, delivery is off
							</label>
						</If>
						<button class="button button--primary" type="submit">Post announcement</button>
					</form>
					<If cond={data.announcements_empty}>
						<div class="empty-tape">
							<strong>NO ANNOUNCEMENTS YET</strong>
							<p>
								Posts show here, newest first, and on the home page.
							</p>
						</div>
					</If>
					<div class="announcement-list">
						<Each of={data.announcements} as="note">
							<article class="announcement-item">
								<TextBlock as="p" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={3} overflow="ellipsis" text={note.body} />
								<div class="announcement-item__meta">
									<TextBlock as="small" class="mono" font="600 13px IBM Plex Mono" lineHeight={18} maxLines={1} overflow="ellipsis">
										{note.posted_by}
										·
										{note.posted_at}
									</TextBlock>
									<details class="announcement-delete-disclosure">
										<summary class="board-button board-button--cut" aria-label={"Delete announcement posted " + note.posted_at_absolute}>✕</summary>
										<form method="post" action={actionPath("announcement-delete")} data-gosx-managed="true">
											<input type="hidden" name="csrf_token" value={csrf.token}></input>
											<input type="hidden" name={data.admin_return_target_field} value={data.admin_announcements_return_target}></input>
											<input type="hidden" name="id" value={note.id}></input>
											<p>Delete the announcement posted {note.posted_at}? This removes it from the league notes and the home page; it cannot be undone.</p>
											<button class="board-button board-button--cut" type="submit">Confirm delete</button>
										</form>
									</details>
								</div>
							</article>
						</Each>
					</div>
				</section>
				<section id="admin-backup" aria-labelledby="admin-backup-heading" tabindex="-1" data-admin-section="backup" class={"player-pool" + data.section_class_backup}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">12 // BACKUP</span>
							<h2 id="admin-backup-heading">Backup</h2>
						</div>
					</div>
					{/* J4 F30 (gap-audit): this section used to speak to an
					    operator — internal file formats, a checksum,
					    repository paths, shell commands. It now speaks to a
					    commissioner: what is backed up, when the last one
					    ran, and what to do if it fails. Downloading and its
					    consequences are unchanged. */}
					<p class="scoring-note">
						One file with your whole league. Keep it somewhere safe.
					</p>
					<p class="scoring-note">
						Downloading changes nothing in the league — do it as often as you like.
						The file holds no passwords or sign-in secrets.
					</p>
					<a class="button button--primary" href="/admin/backup.tar.gz">Download league backup</a>
					<If cond={data.admin_backup.has_last_run}>
						<p class="scoring-note">
							Gridiron also keeps its own copy automatically, once a night. The last one saved
							<b class="mono"> {data.admin_backup.last_run}</b>. The most recent
							{data.admin_backup.keep} nights are kept.
						</p>
					</If>
					<If cond={data.admin_backup.has_last_run == false}>
						<p class="scoring-note">
							Gridiron also keeps its own copy automatically, once a night — no local copy has
							saved yet. The most recent {data.admin_backup.keep} nights are kept.
						</p>
					</If>
					<p class="scoring-note">
						<strong>If a backup ever fails to open:</strong> download a fresh copy above and try
						that one first. If neither opens, see docs/backup-restore.md, or ask for help before
						you reset anything in 99 // DANGER ZONE.
					</p>
				</section>
				<section id="admin-danger" aria-labelledby="admin-danger-heading" tabindex="-1" data-admin-section="danger" class={"player-pool admin-danger" + data.section_class_danger}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">99 // DANGER ZONE</span>
							<h2 id="admin-danger-heading">Danger zone</h2>
						</div>
					</div>
					<div class="danger-grid">
						<form method="post" action={actionPath("draft-reset")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis">Reset {data.league.name}'s draft</TextBlock>
							<ul class="reset-contract-list">
								<li><TextBlock as="span" font="400 13px Plus Jakarta Sans" lineHeight={18}><strong>Destroyed:</strong> every draft pick, ready status for every seat, the draft clock and autopick settings, the transaction log, every set lineup, pending waiver claims, the waiver claim history, the waiver processing clock, pending and past trade offers, reserve/IR roster assignments, and draft-related notification history.</TextBlock></li>
								<li><TextBlock as="span" font="400 13px Plus Jakarta Sans" lineHeight={18}><strong>Preserved:</strong> team seats and managers, pending co-manager invites, draft boards, the invite list, custom team names, the draft order, the regular-season schedule, the playoff bracket, the season phase, the custom roster shape, the trimmed-seat list, the scheduled meeting time, scoring rules, pick'em picks, blitz contest entries, claimed badges, custom avatar images, league announcements, notification preferences, and unrelated sent-notification history.</TextBlock></li>
							</ul>
							<TextBlock as="p" class="scoring-note" font="400 13px Plus Jakarta Sans" lineHeight={18}>This cannot be undone from this screen; only a restored backup can bring {data.league.name}'s destroyed draft data back. <a href="#admin-backup" class="board-button">Back up first →</a></TextBlock>
							<label>Type <span class="mono">RESET DRAFT</span> to confirm.<input type="text" name="confirm" placeholder="RESET DRAFT" autocomplete="off"></input></label>
							<button class="button button--danger" type="submit">Reset {data.league.name}'s draft</button>
						</form>
						<form method="post" action={actionPath("draft-undo")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<input type="hidden" name="previous_pick_token" value={data.previous_pick_token}></input>
							<strong>Undo last pick</strong>
							<TextBlock as="p" font="400 13px Plus Jakarta Sans" lineHeight={18} text="Removes the most recent pick and re-arms the clock for that slot. The form is bound to the exact pick shown now; reload if another browser acts first." />
							<label for="admin-draft-undo-confirm">Type <span class="mono">UNDO</span> to confirm.</label>
							<input id="admin-draft-undo-confirm" type="text" name="confirm" placeholder="type UNDO" autocomplete="off"></input>
							<button class="button button--danger" type="submit">Undo last pick</button>
						</form>
						<form method="post" action={actionPath("league-reset")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis">Reset {data.league.name} to a blank league</TextBlock>
							<ul class="reset-contract-list">
								<li><TextBlock as="span" font="400 13px Plus Jakarta Sans" lineHeight={18}><strong>Destroyed:</strong> every team seat and manager, pending co-manager invites, every draft pick, ready status for every seat, draft boards, pick'em picks and markets, blitz contest entries, the draft order, the regular-season schedule, the playoff bracket, the season phase, the custom roster shape, the trimmed-seat list, the draft clock and autopick settings, the transaction log, every set lineup, pending waiver claims, the waiver claim history, the waiver processing clock, pending and past trade offers, reserve/IR roster assignments, claimed badges, custom avatar images, the scheduled meeting time, and league notification history.</TextBlock></li>
								<li><TextBlock as="span" font="400 13px Plus Jakarta Sans" lineHeight={18}><strong>Preserved:</strong> the invite list, custom team names, scoring rules, league announcements, notification preferences, and unrelated sent-notification history.</TextBlock></li>
							</ul>
							<TextBlock as="p" class="scoring-note" font="400 13px Plus Jakarta Sans" lineHeight={18}>This cannot be undone from this screen; only a restored backup can bring {data.league.name}'s destroyed data back. <a href="#admin-backup" class="board-button">Back up first →</a></TextBlock>
							<label>Type <span class="mono">RESET LEAGUE</span> to confirm.<input type="text" name="confirm" placeholder="RESET LEAGUE" autocomplete="off"></input></label>
							<button class="button button--danger" type="submit">Reset {data.league.name} to a blank league</button>
						</form>
					</div>
				</section>
			</div>
		</If>
	</main>
}
