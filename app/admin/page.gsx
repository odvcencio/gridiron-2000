package admin

// SeatRow's avatar-upload form posts to /avatar/upload as a plain,
// unmanaged (data-gosx-managed="false") full-page submission. GoSX v0.50.0
// supports File/Files and MaxActionBodyBytes for managed actions, but this
// native route remains until the production consumer adopts a
// bounded-multipart contract; its outer middleware applies the complete-
// envelope limit before sessions and CSRF parsing (see avatar_handlers.go in
// the repo root).
func SeatRow(props any) Node {
	return <article class="seat-row" data-claimed={props.seat.claimed}>
		<span class={"team-mark tone-" + props.seat.tone}>
			<If cond={props.seat.has_avatar_image}>
				<img
					class="avatar-mark__photo"
					src={props.seat.avatar_image_url}
					alt={props.seat.name}
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
			<span class="position-chip">{props.seat.division}</span>
		</div>
		<If cond={props.seat.ready}>
			<b class="ready-state is-ready">Ready</b>
		</If>
		<If cond={props.seat.ready == false}>
			<b class="ready-state">Not ready</b>
		</If>
		<If cond={props.seat.claimed}>
			<form method="post" action={props.ReleaseAction} data-gosx-managed="true">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="team_id" value={props.seat.id}></input>
				<button class="board-button board-button--cut" type="submit">Release</button>
			</form>
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
			<input type="text" name="name" placeholder="Rename team" maxlength="40"></input>
			<button class="board-button" type="submit">Set</button>
		</form>
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

func Page() Node {
	return <main
		class="page admin-page"
		id="main-content"
		data-gosx-revalidate-interval="4s"
		data-gosx-revalidate-src="/api/league/version"
	>
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					COMMISSIONER CONSOLE
				</span>
				<h1>
					RUN THE
					<br></br>
					LEAGUE.
				</h1>
				<p>
					Seats, invites, and reset controls. The league checks every action against the commissioner list.
				</p>
				<If cond={data.is_default_config}>
					<p class="demo-message">
						<strong>BUILT-IN REFERENCE LEAGUE:</strong>
						This league still uses the standard default setup. Ask the commissioner to enter your own league details.
					</p>
				</If>
			</div>
			<div class="draft-clock-panel">
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
						·
						{data.draft.time}
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
		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_notice}>
				<p class="flash-message">{data.notice}</p>
			</If>
			<If cond={data.has_admin_error}>
				<p class="error-message">{data.admin_error}</p>
			</If>
			<If cond={data.has_avatar_error}>
				<p class="error-message">{data.avatar_error}</p>
			</If>
			<If cond={data.demo_mode}>
				<p class="demo-message">
					<strong>REHEARSAL MODE:</strong>
					the console is open to everyone while demo mode is on.
				</p>
			</If>
		</div>
		<If cond={data.is_commissioner == false}>
			<section class="player-pool">
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
			<div class="admin-grid">
				<section id="admin-draft-control" data-admin-section="draft-control" class={"player-pool draft-runbook" + data.section_class_draft_control}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">00 // DRAFT NIGHT</span>
							<h2>
								{data.draft.date}
								runbook
							</h2>
						</div>
					</div>
					<div class="checklist">
						<div class="checklist-item">
							<span class="checklist-mark mono">01</span>
							<div class="checklist-item__text">
								<strong>About an hour early, drop the seats nobody claimed</strong>
								<small>
									Use Drop unclaimed seats in 04 // DRAFT ORDER. Do this before you randomize, or the
									order still lists the seats you are about to remove.
								</small>
							</div>
						</div>
						<div class="checklist-item">
							<span class="checklist-mark mono">02</span>
							<div class="checklist-item__text">
								<strong>Randomize the draft order</strong>
								<small>
									Use Randomize order in 04 // DRAFT ORDER. Draft order locks when the commissioner starts the draft.
								</small>
							</div>
						</div>
						<div class="checklist-item">
							<span class="checklist-mark mono">03</span>
							<div class="checklist-item__text">
								<strong>Confirm every seat is ready</strong>
								<small>Check the ready count above and the Ready badges in 01 // SEATS.</small>
							</div>
						</div>
						<div class="checklist-item">
							<span class="checklist-mark mono">04</span>
							<div class="checklist-item__text">
								<strong>
									At
									{data.draft.time}
									, confirm everyone is present and start the draft
								</strong>
								<small>The scheduled time never opens the room. Type START below when you intentionally begin pick one.</small>
							</div>
						</div>
						<div class="checklist-item">
							<span class="checklist-mark mono">05</span>
							<div class="checklist-item__text">
								<strong>Pause or extend for a break</strong>
								<small>Use Pause clock, Resume clock, or Extend pick in 06 // DRAFT CLOCK.</small>
							</div>
						</div>
						<div class="checklist-item">
							<span class="checklist-mark mono">06</span>
							<div class="checklist-item__text">
								<strong>Undo a misclick</strong>
								<small>Type UNDO into Undo last pick in 03 // RESET. It re-arms the clock for that slot.</small>
							</div>
						</div>
						<div class="checklist-item">
							<span class="checklist-mark mono">07</span>
							<div class="checklist-item__text">
								<strong>Autopick catches an absent manager</strong>
								<small>Toggle AUTO for a seat in 01 // SEATS, or force one pick now in 06 // DRAFT CLOCK.</small>
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
					<If cond={data.draft_started == false}>
						<form method="post" action={actionPath("draft-start")} data-gosx-managed="true" class="clock-controls">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<label class="mono" for="admin-draft-start-confirm">TYPE START //</label>
							<input id="admin-draft-start-confirm" class="scoring-input" name="confirm" autocomplete="off" placeholder="START"></input>
							<button class="button button--primary" type="submit">Start draft + pick clock</button>
						</form>
						<p class="scoring-note">This opens the room immediately and starts pick one’s timer. Scheduled time alone never starts it. Pool: {data.pool.mode}, {data.pool.players} players for {data.draft_required_players} draft slots ({data.pool.coverage} target coverage).</p>
					</If>
					<If cond={data.draft_started}>
						<p class="flash-message"><strong>DRAFT LIVE:</strong> The commissioner started the draft. That start rules.</p>
					</If>
				</section>
				<section id="admin-schedule" data-admin-section="schedule" class={"player-pool admin-season-ops" + data.section_class_schedule}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">SEASON // SCHEDULE</span>
							<h2>Regular-season control</h2>
							<p class="scoring-note">
								This plan is durable league state. Generating or redrawing it does not start the draft, score a game, or seed playoffs.
							</p>
						</div>
						<span class="position-chip">{data.schedule.status}</span>
					</div>
					<If cond={data.schedule.has_schedule == false}>
						<div class="empty-tape">
							<strong>NO SCHEDULE GENERATED</strong>
							<p>Choose the regular-season span and first NFL week. A blank seed draws a fresh seed and records it with the plan.</p>
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
							<div class="pool-stat"><span>Seed</span><b class="mono">{data.schedule.seed}</b></div>
							<div class="pool-stat"><span>Phase</span><b class="mono">{data.schedule.phase}</b></div>
							<div class="pool-stat"><span>Final</span><b class="mono">{data.schedule.final_weeks}/{data.schedule.week_count} WEEKS · {data.schedule.final_matchups}/{data.schedule.total_matchups} MATCHUPS</b></div>
						</div>
						<p class="scoring-note">The stored seed is the redraw trail. A redraw creates a new seed and is available only before season start and before any matchup is final.</p>
						<If cond={data.schedule.regenerate_allowed}>
							<form method="post" action={actionPath("schedule-regenerate")} data-gosx-managed="true" class="clock-controls">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<label class="mono" for="admin-schedule-regenerate-confirm">TYPE REDRAW SCHEDULE //</label>
								<input id="admin-schedule-regenerate-confirm" class="scoring-input" name="confirm" value={data.schedule_regeneration.confirm} autocomplete="off" placeholder="REDRAW SCHEDULE"></input>
								<button class="button button--ghost" type="submit">Redraw schedule</button>
							</form>
						</If>
						<If cond={data.schedule.regenerate_allowed == false}>
							<p class="demo-message"><strong>REDRAW LOCKED:</strong> {data.schedule.regenerate_lock_reason}.</p>
						</If>
					</If>
				</section>
				<section id="admin-week-close" data-admin-section="week-close" class={"player-pool admin-season-ops" + data.section_class_week_close}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">SEASON // WEEK CLOSE</span>
							<h2>Close a scoring week</h2>
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
							<div class="pool-stat"><span>Readiness</span><b class="mono">{data.schedule.close.ready}</b></div>
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
							<form method="post" action={actionPath("close-week-force")} data-gosx-managed="true" class="clock-controls">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<label class="mono" for="admin-close-force-week">WEEK //</label>
								<input id="admin-close-force-week" class="scoring-input" type="number" name="week" value={data.close_form.week} min="1" max="18" required="required"></input>
								<label class="mono" for="admin-close-week-confirm">TYPE CLOSE WEEK {data.close_form.week} //</label>
								<input id="admin-close-week-confirm" class="scoring-input" name="confirm" value={data.close_form.confirm} autocomplete="off" placeholder="CLOSE WEEK N"></input>
								<button class="button button--ghost" type="submit">Force close week {data.close_form.week}</button>
							</form>
						</If>
					</If>
					<p class="demo-message"><strong>YEAR ONE:</strong> playoff seeding is not available yet; closing the final regular-season week records the phase transition only.</p>
				</section>
				<section id="admin-seats" data-admin-section="seats" class={"player-pool" + data.section_class_seats}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">01 // SEATS</span>
							<h2>Franchise claims</h2>
							<p class="scoring-note" id="admin-avatar-upload-help">PNG or JPEG, 2 MB maximum, from 64×64 through 4096×4096 pixels. If this seat has a claimed badge, uploading a custom image releases it so another team can use it.</p>
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
				<section id="admin-invites" data-admin-section="invites" class={"player-pool" + data.section_class_invites}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">02 // INVITES</span>
							<h2>Who may claim a seat</h2>
							<If cond={data.invite_count > 0}>
								<p class="scoring-note invite-progress" aria-label="Invitation progress">
									<strong>{data.invite_seated_count} seated</strong>
									<span>{data.invite_ready_count} ready</span>
									<If cond={data.invite_seatless_count > 0}>
										<span>{data.invite_seatless_count} signed in without a seat</span>
									</If>
									<span>{data.invite_waiting_count} waiting to sign in</span>
								</p>
							</If>
						</div>
					</div>
					<If cond={data.league_open}>
						<p class="demo-message">
							<strong>OPEN LEAGUE:</strong>
							no invite list is set, so any Google account may claim a seat. Add the
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
					<form class="invite-form" method="post" action={actionPath("invite-add")} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<input
							type="email"
							name="email"
							placeholder="manager@example.com"
							autocomplete="off"
							required="required"
						 />
						<button class="button button--primary" type="submit">Invite</button>
					</form>
					<div class="invite-list">
						<Each of={data.invites} as="invite">
							<article class="invite-row" data-status={invite.status}>
								<div class="invite-identity">
									<b class="mono">{invite.email}</b>
									<small>{invite.status_detail}</small>
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
										<form method="post" action={actionPath("invite-remove")} data-gosx-managed="true">
											<input type="hidden" name="csrf_token" value={csrf.token}></input>
											<input type="hidden" name="email" value={invite.email}></input>
											<button class="board-button board-button--cut" type="submit">✕</button>
										</form>
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
					<p class="scout-callout">
						Send managers this address: they sign in with Google and the next open seat is theirs.
					</p>
				</section>
				<section id="admin-danger" data-admin-section="danger" class={"player-pool admin-danger" + data.section_class_danger}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">03 // RESET</span>
							<h2>Danger zone</h2>
						</div>
					</div>
					<div class="danger-grid">
						<form method="post" action={actionPath("draft-reset")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<strong>Reset draft</strong>
							<p>
								Clears every pick and ready flag. Seats and boards survive.
							</p>
							<input type="text" name="confirm" placeholder="type RESET" autocomplete="off"></input>
							<button class="button" type="submit">Reset draft</button>
						</form>
						<form method="post" action={actionPath("draft-undo")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<strong>Undo last pick</strong>
							<p>
								Removes the most recent pick and re-arms the clock for that slot.
							</p>
							<input type="text" name="confirm" placeholder="type UNDO" autocomplete="off"></input>
							<button class="button" type="submit">Undo last pick</button>
						</form>
						<form method="post" action={actionPath("league-reset")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<strong>Reset league</strong>
							<p>
								Clears seats, picks, ready flags, and boards. Invites survive.
							</p>
							<input type="text" name="confirm" placeholder="type RESET" autocomplete="off"></input>
							<button class="button" type="submit">Reset league</button>
						</form>
					</div>
				</section>
				<section id="admin-draft-order" data-admin-section="draft-order" class={"player-pool" + data.section_class_draft_order}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">04 // DRAFT ORDER</span>
							<h2>Snake order</h2>
						</div>
						<If cond={data.order_randomized}>
							<span class="position-chip">RANDOMIZED</span>
						</If>
						<If cond={data.order_randomized == false}>
							<span class="position-chip">DEFAULT ORDER</span>
						</If>
					</div>
					<div class="order-list">
						<Each of={data.draft_order} as="team">
							<article class="order-row">
								<span class={"team-mark tone-" + team.tone}>
									<If cond={team.has_avatar_image}>
										<img class="avatar-mark__photo" src={team.avatar_image_url} alt={team.name} loading="lazy" />
									</If>
									<If cond={team.has_avatar_image == false}>
										{team.abbreviation}
									</If>
								</span>
								<div class="seat-identity">
									<strong>{team.name}</strong>
									<small>{team.manager}</small>
									<span class="position-chip">{team.division}</span>
								</div>
							</article>
						</Each>
					</div>
					<If cond={data.has_unclaimed_seats}>
						<If cond={data.draft_started == false}>
							<form method="post" action={actionPath("seat-trim")} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<button class="button" type="submit">Drop unclaimed seats</button>
							</form>
							<p class="demo-message">
								<strong>SCHEDULE WARNING:</strong>
								if a schedule already exists, this action discards that unplayed schedule. Regenerate it afterward so every matchup names only the kept teams.
							</p>
							<p class="scoring-note">
								{data.unclaimed_seat_count}
								seat(s) have no manager. Drop them first, then randomize. An unclaimed seat takes a turn
								in every round: it runs the full pick clock down, then autopicks a player.
							</p>
						</If>
					</If>
					<form method="post" action={actionPath("order-randomize")} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<button class="button button--primary" type="submit">Randomize order</button>
					</form>
					<p class="scoring-note">
						Run this one hour before the draft. Locked once the commissioner starts the draft.
					</p>
				</section>
				<section id="admin-data" data-admin-section="data" class={"player-pool" + data.section_class_data}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">05 // PLAYER DATA</span>
							<h2>Player list update</h2>
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
							<b class="mono">{data.pool.coverage}</b>
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
							<span>Byes</span>
							<b class="mono">{data.pool.with_bye}</b>
						</div>
						<div class="pool-stat">
							<span>Data calls used</span>
							<b class="mono">{data.pool.requests}</b>
						</div>
						<div class="pool-stat">
							<span>Last update</span>
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
				<section id="admin-clock" data-admin-section="clock" class={"player-pool" + data.section_class_clock}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">06 // DRAFT CLOCK</span>
							<h2>Pick clock controls</h2>
						</div>
						<If cond={data.clock.armed}>
							<span class="position-chip">ARMED</span>
						</If>
						<If cond={data.clock.armed == false}>
							<span class="position-chip">UNARMED</span>
						</If>
					</div>
					<div class="pool-stats">
						<div class="pool-stat">
							<span>State</span>
							<If cond={data.clock.paused}>
								<b class="mono">PAUSED</b>
							</If>
							<If cond={data.clock.paused == false}>
								<b class="mono">RUNNING</b>
							</If>
						</div>
						<div class="pool-stat">
							<span>Reason</span>
							<b class="mono">{data.clock.reason}</b>
						</div>
						<div class="pool-stat">
							<span>Deadline</span>
							<b class="mono">{data.clock.deadline}</b>
						</div>
						<div class="pool-stat">
							<span>Duration</span>
							<b class="mono">
								{data.clock.duration_seconds}
								S
							</b>
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
							<b class="mono">
								{data.clock.remaining_seconds}
								S
							</b>
						</div>
					</div>
					<div class="clock-controls">
						<form method="post" action={actionPath("clock-pause")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<button class="button" type="submit">Pause clock</button>
						</form>
						<form method="post" action={actionPath("clock-resume")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<button class="button button--primary" type="submit">Resume / start clock</button>
						</form>
						<form method="post" action={actionPath("clock-extend")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<input class="scoring-input" type="number" name="seconds" placeholder="30" min="1" max="600"></input>
							<button class="button" type="submit">Extend pick</button>
						</form>
						<form method="post" action={actionPath("clock-set-duration")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<input class="scoring-input" type="number" name="seconds" placeholder="90" min="10" max="600"></input>
							<button class="button" type="submit">Set duration</button>
						</form>
						<form method="post" action={actionPath("clock-force-autopick")} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<button class="button button--ghost" type="submit">Force auto-pick now</button>
						</form>
					</div>
					<p class="scoring-note">
						Extend adds seconds to the current pick. Set duration applies from the next arm; it does not change the running deadline.
					</p>
				</section>
				<section id="admin-roster" data-admin-section="roster" class={"player-pool" + data.section_class_roster}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">07 // ROSTER SHAPE</span>
							<h2>Starting lineup and bench</h2>
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
							the roster shape locks once the draft starts. Reset the draft in 03 // RESET to change it again.
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
				</section>
				<section id="admin-announcements" data-admin-section="announcements" class={"player-pool" + data.section_class_announcements}>
					<div class="pool-toolbar">
						<div>
							<span class="section-index">08 // ANNOUNCEMENTS</span>
							<h2>League notes</h2>
						</div>
					</div>
					<form method="post" action={actionPath("announcement-post")} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<textarea name="body" class="announcement-textarea" placeholder="Post a note to the whole league..." maxlength="500" rows="3"></textarea>
						<label class="announcement-email-toggle">
							<input type="checkbox" name="also_email" value="true"></input>
							Also email the league
						</label>
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
								<p>{note.body}</p>
								<div class="announcement-item__meta">
									<small class="mono">
										{note.posted_by}
										·
										{note.posted_at}
									</small>
									<form method="post" action={actionPath("announcement-delete")} data-gosx-managed="true">
										<input type="hidden" name="csrf_token" value={csrf.token}></input>
										<input type="hidden" name="id" value={note.id}></input>
										<button class="board-button board-button--cut" type="submit">✕</button>
									</form>
								</div>
							</article>
						</Each>
					</div>
				</section>
			</div>
		</If>
	</main>
}
