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
						Use the Google account your commissioner invited. Your franchise will be waiting.
					</p>
					<a href="/auth/google/start?next=/" class="google-button">
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
					<div class="account-team">
						<span>Your franchise</span>
						<strong>{data.viewer.team_name}</strong>
					</div>
					<a href="/team" data-gosx-link class="button button--primary">Open team terminal</a>
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
