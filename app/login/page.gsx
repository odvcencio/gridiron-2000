package login

// Page's signed-in sign-out form must be a native document navigation, the
// same as the shell's own copy (app/layout.gsx PrimaryNavigation):
// /auth/logout answers a plain 303 and rotates the session cookie, and a
// managed submit swallows that redirect. The runtime parses the HTML 303
// body as JSON, fails, and leaves this signed-in console on screen with a
// generic "Action completed." toast and no URL change (2026-09-01 UX
// audit, entry #1). data-gosx-managed="false" opts the form out so the
// browser follows the 303 natively.
func Page() Node {
	return <main class="page login-page" id="main-content">
		<section class="login-stage">
			<div class="login-poster">
				<span class="signal-label">
					<span class="signal-mark" aria-hidden="true"></span>
					PRIVATE LEAGUE NETWORK
				</span>
				<p class="page-kicker">{data.public_entry.state_label}</p>
				<h1>
					{data.league.name}
					{" "}
					<span>{data.public_entry.headline}</span>
				</h1>
				<p>{data.public_entry.detail}</p>
				<p class="login-identity">
					<strong>{data.league.format_blurb}</strong>
					·
					{data.league.seat_count_word} manager league
				</p>
				<p class="login-entry-policy"><strong>{data.public_entry.state_label}</strong> · {data.public_entry.membership_label} — {data.public_entry.membership_detail}</p>
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
				<div class="seat-meter" aria-label={data.seat_meter.aria_label}>
					<Each of={data.seat_meter.seats} as="seat">
						<span data-taken={seat.taken} aria-label={seat.label}>{seat.number}<small>{seat.status}</small></span>
					</Each>
				</div>
			</div>
			<aside class="login-console">
				<If cond={data.has_notice}>
					<p class="flash-message" role="alert">{data.notice}</p>
				</If>
				<If cond={data.viewer.signed_in == false}>
					<span class="section-index">GOOGLE SIGN-IN</span>
					<h2>Manager check-in</h2>
					<p>
						Sign in with the Google account you want to use. After authentication,
						the league will explain whether this identity is admitted and whether
						a fantasy seat is available.
					</p>
					<p class="login-admission-note">Admission policy: {data.public_entry.membership_label} — {data.public_entry.membership_detail}</p>
					<If cond={data.has_return_path}>
						<p class="login-return-note">
							After sign-in, we'll return you to the page you requested.
						</p>
					</If>
					<If cond={data.configured == false}>
						<div class="setup-note" role="alert">
							<p id="google-setup-note">
								Sign-in is not open yet. Ask the commissioner.
							</p>
						</div>
					</If>
					<If cond={data.configured}>
						<a href={data.oauth_start} class="google-button">
							<span class="google-mark" aria-hidden="true">G</span>
							Continue with Google
						</a>
					</If>
					<If cond={data.configured == false}>
						<button type="button" class="google-button" disabled aria-describedby="google-setup-note">
							<span class="google-mark" aria-hidden="true">G</span>
							Continue with Google
						</button>
					</If>
					<If cond={data.viewer.demo}>
						<a href="/" data-gosx-link class="button button--ghost">Explore demo league</a>
					</If>
				</If>
				<If cond={data.viewer.signed_in}>
					<span class="section-index">SIGNED IN</span>
					<div class="account-avatar">{data.viewer.initials}</div>
					<h2>{data.viewer.name}</h2>
					<p>{data.viewer.email}</p>
					<If cond={data.public_entry.has_seat}>
						<div class="account-team">
							<span>{data.public_entry.role_label}</span>
							<strong>{data.public_entry.team_name}</strong>
						</div>
						<p>{data.public_entry.detail}</p>
						<a href="/team" data-gosx-link class="button button--primary">Open team terminal →</a>
					</If>
					<If cond={data.public_entry.has_seat == false}>
						<div class="account-team">
							<span>LEAGUE ACCESS</span>
							<strong>{data.public_entry.state_label}</strong>
						</div>
						<p>{data.public_entry.detail}</p>
						<If cond={data.public_entry.is_co_manager_pending}>
							<a href={data.public_entry.action_href} class="button button--ghost">{data.public_entry.action_label}</a>
						</If>
						<If cond={data.public_entry.admitted == false}>
							<a href={data.public_entry.action_href} data-gosx-link class="button button--ghost">{data.public_entry.action_label}</a>
						</If>
						<If cond={data.public_entry.admitted && data.public_entry.can_claim == false && data.public_entry.league_full == false && data.public_entry.is_co_manager_pending == false}>
							<a href={data.public_entry.action_href} data-gosx-link class="button button--ghost">{data.public_entry.action_label}</a>
						</If>
						<If cond={data.public_entry.can_claim}>
							<a href="/join" data-gosx-link class="button button--primary">{data.public_entry.action_label}</a>
						</If>
						<If cond={data.public_entry.admitted && data.public_entry.league_full && data.public_entry.is_co_manager_pending == false}>
							<a href="/pickem" data-gosx-link class="button button--primary">{data.public_entry.action_label}</a>
						</If>
					</If>
					<If cond={data.public_entry.is_commissioner}>
						<a href={data.public_entry.commissioner_href} data-gosx-link class="button button--ghost">{data.public_entry.commissioner_label}</a>
					</If>
					<form method="post" action="/auth/logout" data-gosx-managed="false">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<button type="submit" class="button button--ghost">Sign out</button>
					</form>
				</If>
			</aside>
		</section>
	</main>
}
