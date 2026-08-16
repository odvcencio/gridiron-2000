package login

func Page() Node {
	return <main class="page login-page" id="main-content">
		<section class="login-stage">
			<div class="login-poster">
				<span class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					PRIVATE LEAGUE NETWORK
				</span>
				<p class="page-kicker">Authentication channel 08</p>
				<h1>
					CLAIM
					<br></br>
					YOUR SEAT.
				</h1>
				<p>
					One Google identity maps to one manager seat. No passwords to forget, no open registration, no strangers ruining the group chat.
				</p>
				<div class="seat-meter" aria-label={data.seats + " manager seats"}>
					<Each of={[]int{1,2,3,4,5,6,7,8}} as="seat">
						<span>{seat}</span>
					</Each>
				</div>
			</div>
			<aside class="login-console">
				<If cond={data.viewer.signed_in == false}>
					<span class="section-index">GOOGLE OAUTH // SECURE ENTRY</span>
					<h2>Manager check-in</h2>
					<p>
						Use the Google account your commissioner invited. You’ll be assigned to the first open franchise seat.
					</p>
					<a href="/auth/google/start?next=/" class="google-button">
						<span class="google-mark" aria-hidden="true">G</span>
						Continue with Google
					</a>
					<a href="/" data-gosx-link class="button button--ghost">Explore demo league</a>
					<If cond={data.configured == false}>
						<div class="setup-note">
							<strong>Setup mode</strong>
							<p>
								Add Google credentials to
								<span class="inline-code">.env</span>
								. Until then, the app stays fully explorable in demo mode.
							</p>
						</div>
					</If>
				</If>
				<If cond={data.viewer.signed_in}>
					<span class="section-index">IDENTITY CONFIRMED</span>
					<div class="account-avatar">{data.viewer.initials}</div>
					<h2>{data.viewer.name}</h2>
					<p>{data.viewer.email}</p>
					<div class="account-team">
						<span>Assigned franchise</span>
						<strong>{data.viewer.team_name}</strong>
					</div>
					<a href="/team" data-gosx-link class="button button--primary">Open team terminal</a>
					<form method="post" action="/auth/logout" data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<button type="submit" class="button button--ghost">Sign out</button>
					</form>
				</If>
				<If cond={data.has_notice}>
					<p class="flash-message">{data.notice}</p>
				</If>
				<footer>
					<span>Encrypted cookie session</span>
					<span>CSRF protected</span>
					<span>PKCE flow</span>
				</footer>
			</aside>
		</section>
	</main>
}
