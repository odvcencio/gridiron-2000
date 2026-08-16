package admin

func SeatRow(props any) Node {
	return <article class="seat-row" data-claimed={props.seat.claimed}>
		<span class={"team-mark tone-" + props.seat.tone}>{props.seat.abbreviation}</span>
		<div class="seat-identity">
			<strong>{props.seat.name}</strong>
			<small>
				<If cond={props.seat.claimed}>
					{props.seat.manager}
					·
					{props.seat.email}
				</If>
				<If cond={props.seat.claimed == false}>
					Awaiting a manager
				</If>
			</small>
			<span class="position-chip">{props.seat.division}</span>
		</div>
		<If cond={props.seat.ready}>
			<b class="ready-state is-ready">Ready</b>
		</If>
		<If cond={props.seat.ready == false}>
			<b class="ready-state">Not ready</b>
		</If>
		<If cond={props.seat.claimed}>
			<form method="post" action={props.ReleaseAction} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="team_id" value={props.seat.id}></input>
				<button class="board-button board-button--cut" type="submit">Release</button>
			</form>
		</If>
		<form method="post" action={props.RenameAction} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
			<input type="hidden" name="csrf_token" value={props.CSRF}></input>
			<input type="hidden" name="team_id" value={props.seat.id}></input>
			<input type="text" name="name" placeholder="Rename team" maxlength="40"></input>
			<button class="board-button" type="submit">Set</button>
		</form>
	</article>
}

func Page() Node {
	return <main class="page admin-page" id="main-content">
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
					Seats, invites, and reset controls. Every action is checked against the commissioner list on the server.
				</p>
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
				<section class="player-pool">
					<div class="pool-toolbar">
						<div>
							<span class="section-index">01 // SEATS</span>
							<h2>Franchise claims</h2>
						</div>
					</div>
					<div class="seat-list">
						<Each of={data.seats} as="seat">
							<SeatRow seat={seat} ReleaseAction={actionPath("seat-release")} RenameAction={actionPath("team-rename")} CSRF={csrf.token} />
						</Each>
					</div>
				</section>
				<section class="player-pool">
					<div class="pool-toolbar">
						<div>
							<span class="section-index">02 // INVITES</span>
							<h2>Who may claim a seat</h2>
						</div>
					</div>
					<If cond={data.league_open}>
						<p class="demo-message">
							<strong>OPEN LEAGUE:</strong>
							no invite list is set, so any Google account may claim a seat. Add the eight manager emails below.
						</p>
					</If>
					<form class="invite-form" method="post" action={actionPath("invite-add")} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<input
							type="email"
							name="email"
							placeholder="manager@gmail.com"
							autocomplete="off"
							required="required"
						 />
						<button class="button button--primary" type="submit">Invite</button>
					</form>
					<div class="invite-list">
						<Each of={data.invites} as="invite">
							<article class="invite-row">
								<b class="mono">{invite.email}</b>
								<span class="position-chip">{invite.source}</span>
								<If cond={data.mail_enabled}>
									<form method="post" action={actionPath("invite-send")} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
										<input type="hidden" name="csrf_token" value={csrf.token}></input>
										<input type="hidden" name="email" value={invite.email}></input>
										<button class="board-button" type="submit">Email</button>
									</form>
								</If>
								<a href={invite.mailto} class="board-button">Mail app</a>
								<If cond={invite.removable}>
									<form method="post" action={actionPath("invite-remove")} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
										<input type="hidden" name="csrf_token" value={csrf.token}></input>
										<input type="hidden" name="email" value={invite.email}></input>
										<button class="board-button board-button--cut" type="submit">✕</button>
									</form>
								</If>
								<If cond={invite.removable == false}>
									<small class="mono">pinned</small>
								</If>
							</article>
						</Each>
					</div>
					<details class="invite-preview">
						<summary class="mono">INVITE TEMPLATE PREVIEW</summary>
						<p class="mono">{data.invite_preview.subject}</p>
						<pre>{data.invite_preview.body}</pre>
					</details>
					<If cond={data.mail_enabled == false}>
						<p class="demo-message">
							Email sending is not configured (RESEND_API_KEY + RESEND_FROM, or SMTP_HOST/USER/PASS). Email buttons hide until it is; the Mail app links always work.
						</p>
					</If>
					<p class="scout-callout">
						Send managers this address: they sign in with Google and the next open seat is theirs.
					</p>
				</section>
				<section class="player-pool admin-danger">
					<div class="pool-toolbar">
						<div>
							<span class="section-index">03 // RESET</span>
							<h2>Danger zone</h2>
						</div>
					</div>
					<div class="danger-grid">
						<form method="post" action={actionPath("draft-reset")} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<strong>Reset draft</strong>
							<p>Clears every pick and ready flag. Seats and boards survive.</p>
							<input type="text" name="confirm" placeholder="type RESET" autocomplete="off"></input>
							<button class="button" type="submit">Reset draft</button>
						</form>
						<form method="post" action={actionPath("league-reset")} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<strong>Reset league</strong>
							<p>Clears seats, picks, ready flags, and boards. Invites survive.</p>
							<input type="text" name="confirm" placeholder="type RESET" autocomplete="off"></input>
							<button class="button" type="submit">Reset league</button>
						</form>
					</div>
				</section>
				<section class="player-pool">
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
								<span class={"team-mark tone-" + team.tone}>{team.abbreviation}</span>
								<div class="seat-identity">
									<strong>{team.name}</strong>
									<small>{team.manager}</small>
									<span class="position-chip">{team.division}</span>
								</div>
							</article>
						</Each>
					</div>
					<form method="post" action={actionPath("order-randomize")} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<button class="button button--primary" type="submit">Randomize order</button>
					</form>
					<p class="scoring-note">
						Run this one hour before the draft. Locked once the first pick is in.
					</p>
				</section>
				<section class="player-pool">
					<div class="pool-toolbar">
						<div>
							<span class="section-index">05 // DATA FEED</span>
							<h2>Player pool sync</h2>
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
							<span>Players</span>
							<b class="mono">{data.pool.players}</b>
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
							<span>Requests used</span>
							<b class="mono">{data.pool.requests}</b>
						</div>
						<div class="pool-stat">
							<span>Last sync</span>
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
			</div>
		</If>
	</main>
}
