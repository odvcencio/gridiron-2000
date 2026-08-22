package login

func Page() Node {
	return <main class="page login-page" id="main-content">
		<section class="login-stage">
			<div class="login-poster">
				<span class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					PRIVATE LEAGUE NETWORK
				</span>
				<p class="page-kicker">Manager sign-in</p>
				<h1>
					CLAIM
					<br></br>
					YOUR SEAT.
				</h1>
				<p>
					This league is invite-only. Every seat belongs to one manager.
				</p>
				<p class="login-identity">
					<strong>{data.league.format_blurb}</strong>
					·
					{data.league.seat_count_word} manager league
				</p>
				<div class="login-event" aria-labelledby="login-event-heading">
					<span class="section-index">UP NEXT</span>
					<h2 id="login-event-heading">{data.draft.event_label}</h2>
					<time class="event-date">{data.draft.long_date}</time>
					<div class="event-time">
						<strong>{data.draft.time}</strong>
						<span>{data.draft.timezone}</span>
					</div>
					<div class="event-state" role="status">
						<span class="event-state__mark" aria-hidden="true"></span>
						<strong>{data.draft.status_label}</strong>
					</div>
					<p class="event-note">{data.draft.status_note}</p>
				</div>
				<div class="seat-meter" aria-label={data.seats + " manager seats"}>
					<Each of={data.seat_numbers} as="seat">
						<span>{seat}</span>
					</Each>
				</div>
			</div>
			<aside class="login-console">
				<If cond={data.viewer.signed_in == false}>
					<span class="section-index">GOOGLE SIGN-IN</span>
					<h2>Manager check-in</h2>
					<p>
						Use the Google account your commissioner invited. Your league access will be waiting. You can claim an open fantasy seat after sign-in.
					</p>
					<If cond={data.has_return_path}>
						<p class="login-return-note">
							After sign-in, we'll return you to the page you requested.
						</p>
					</If>
					<a href={data.oauth_start} class="google-button">
						<span class="google-mark" aria-hidden="true">G</span>
						Continue with Google
					</a>
					<If cond={data.viewer.demo}>
						<a href="/" data-gosx-link class="button button--ghost">Explore demo league</a>
					</If>
					<If cond={data.configured == false}>
						<div class="setup-note">
							<strong>Setup mode</strong>
							<p>
								Add Google credentials to
								<span class="inline-code">.env</span>
								to turn on sign-in. Until then, explore the app in demo mode.
							</p>
						</div>
					</If>
				</If>
				<If cond={data.viewer.signed_in}>
					<span class="section-index">SIGNED IN</span>
					<div class="account-avatar">{data.viewer.initials}</div>
					<h2>{data.viewer.name}</h2>
					<p>{data.viewer.email}</p>
					<If cond={data.viewer.has_seat}>
						<div class="account-team">
							<span>Your franchise</span>
							<strong>{data.viewer.team_name}</strong>
						</div>
						<a href="/team" data-gosx-link class="button button--primary">Open team terminal</a>
					</If>
					<If cond={data.viewer.has_seat == false}>
						<div class="account-team">
							<span>League membership</span>
							<strong>ACTIVE · NO FRANCHISE</strong>
						</div>
						<p>You are signed in with league access, but no fantasy team is attached yet. Claim an open franchise to unlock roster, draft, waiver, and trade controls.</p>
						<a href="/join" data-gosx-link class="button button--primary">Claim a franchise →</a>
					</If>
					<form method="post" action="/auth/logout" data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<button type="submit" class="button button--ghost">Sign out</button>
					</form>
				</If>
				<If cond={data.has_notice}>
					<p class="flash-message">{data.notice}</p>
				</If>
			</aside>
		</section>
	</main>
}
