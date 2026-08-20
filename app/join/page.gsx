package join

// Page renders the fantasy-signup form (registration wave, build item
// 2): a signed-in, seatless member picks a team name and an unclaimed
// badge motif and submits once — the service claims the seat, name, and
// badge together (see league.Service.ClaimFantasySeat). An
// already-seated visitor never reaches this template; main.go's
// redirectSeatedFromJoin sends them to /team before the page even loads.
// A full league renders the honest closed state instead of a form.
func Page() Node {
	return <main class="page join-page" id="main-content">
		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_signup_error}>
				<p class="error-message">{data.signup_error}</p>
			</If>
		</div>
		<section class="hero-command">
			<div class="hero-command__copy">
				<div class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					FANTASY SIGNUP
				</div>
				<p class="hero-kicker">{data.league.hero_kicker}</p>
				<h1>
					CLAIM YOUR
					<br></br>
					<span>FRANCHISE.</span>
				</h1>
				<If cond={data.league_full == false}>
					<p class="hero-deck">
						{data.open_seats}
						seat(s) still open. Name your team and pick a badge — one submit claims the seat.
					</p>
					<If cond={data.identity_available}>
					<form method="post" action={actionPath("signup-claim")} data-gosx-managed="true" class="signup-form">
						<label class="signup-form__field">
							<span>Team name</span>
							<input type="text" name="team_name" maxlength="40" placeholder="Your team name" required="required" autofocus="autofocus"></input>
						</label>
						<h2 class="badge-picker-title">Choose your badge</h2>
						<div class="badge-picker" style={"--badge-tone: " + data.badge_tone_hex + ";"}>
							<Each of={data.badge_grid} as="badge">
								<label class="badge-option badge-option--pick" title={badge.Name}>
									<input type="radio" name="motif" value={badge.Slug} class="badge-option__radio"></input>
									<span class="badge-option__art" style={"mask-image:url(/avatars/motifs/" + badge.Slug + ".png);-webkit-mask-image:url(/avatars/motifs/" + badge.Slug + ".png);"} aria-hidden="true"></span>
									<small>{badge.Name}</small>
								</label>
							</Each>
						</div>
						<div class="hero-actions">
							<button class="button button--primary" type="submit">
								Claim your seat
								<span aria-hidden="true">→</span>
							</button>
						</div>
					</form>
					</If>
					<If cond={data.identity_available == false}>
						<p class="error-message" role="status">{data.identity_error}</p>
					</If>
				</If>
				<If cond={data.league_full}>
					<p class="hero-deck">
						Every manager seat is claimed — this league is full. Pick'em is still open to everyone, no seat required.
					</p>
					<div class="hero-actions">
						<a href="/pickem" data-gosx-link class="button button--primary">
							Open Pick'em HQ
							<span aria-hidden="true">→</span>
						</a>
					</div>
				</If>
				<If cond={data.has_seat}>
					<p class="hero-deck">You already hold a team seat.</p>
					<div class="hero-actions">
						<a href="/team" data-gosx-link class="button button--primary">Open your team →</a>
					</div>
				</If>
			</div>
		</section>
	</main>
}
