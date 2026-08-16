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
		</div>
		<If cond={props.seat.ready}>
			<b class="ready-state is-ready">Ready</b>
		</If>
		<If cond={props.seat.ready == false}>
			<b class="ready-state">Not ready</b>
		</If>
		<If cond={props.seat.claimed}>
			<form method="post" action={props.ReleaseAction}>
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="team_id" value={props.seat.id}></input>
				<button class="board-button board-button--cut" type="submit">Release</button>
			</form>
		</If>
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
							<SeatRow seat={seat} ReleaseAction={actionPath("seat-release")} CSRF={csrf.token} />
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
					<form class="invite-form" method="post" action={actionPath("invite-add")}>
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
								<If cond={invite.removable}>
									<form method="post" action={actionPath("invite-remove")}>
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
						<form method="post" action={actionPath("draft-reset")}>
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<strong>Reset draft</strong>
							<p>Clears every pick and ready flag. Seats and boards survive.</p>
							<input type="text" name="confirm" placeholder="type RESET" autocomplete="off"></input>
							<button class="button" type="submit">Reset draft</button>
						</form>
						<form method="post" action={actionPath("league-reset")}>
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<strong>Reset league</strong>
							<p>Clears seats, picks, ready flags, and boards. Invites survive.</p>
							<input type="text" name="confirm" placeholder="type RESET" autocomplete="off"></input>
							<button class="button" type="submit">Reset league</button>
						</form>
					</div>
				</section>
			</div>
		</If>
	</main>
}
