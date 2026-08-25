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
					<span class="signal-mark" aria-hidden="true"></span>
					FANTASY SIGNUP
				</div>
				<p class="hero-kicker">{data.league.hero_kicker}</p>
				<h1>
					CLAIM YOUR
					<br></br>
					<span>FRANCHISE.</span>
				</h1>
				<If cond={data.public_entry.can_claim}>
					<p class="hero-deck">
						Open seats remaining:
						<strong>{data.open_seats}</strong>.
						Name your team and pick a badge — one submit secures the franchise.
					</p>
					<section class="join-steps" aria-labelledby="join-steps-heading">
						<h2 class="visually-hidden" id="join-steps-heading">What happens next</h2>
						<div class="join-step">
							<span class="mono">01 // CLAIM</span>
							<strong>Secure your seat</strong>
							<small>Your seat, team name, and badge save together.</small>
						</div>
						<div class="join-step">
							<span class="mono">02 // PREP</span>
							<strong>Build your board</strong>
							<small>Rank players and review league scoring before draft day.</small>
						</div>
						<div class="join-step">
							<span class="mono">03 // CHECK IN</span>
							<strong>Mark yourself ready</strong>
							<small>The commissioner starts the draft intentionally; the schedule never starts it for them.</small>
						</div>
					</section>
					<If cond={data.identity_available}>
					<form method="post" action={actionPath("signup-claim")} data-gosx-managed="true" class="signup-form">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<label class="signup-form__field">
							<span>Team name</span>
							<input type="text" name="team_name" value={data.team_name_value} maxlength="40" placeholder="Your team name" aria-describedby="team-name-help" required="required" autofocus="autofocus"></input>
							<small id="team-name-help">Up to 40 characters. You can rename the team later.</small>
						</label>
						<p class="error-message form-error signup-form__error" data-gosx-field-error="team_name" aria-live="polite"></p>
						<h2 class="badge-picker-title">Choose your badge</h2>
						<p class="signup-form__help" id="badge-picker-help">Only available badges are shown. You can change this later; uploading a custom team image releases the badge for another manager.</p>
						<div class="badge-picker" style={"--badge-tone: " + data.badge_tone_hex + ";"}>
							<Each of={data.badge_grid} as="badge">
								<label class="badge-option badge-option--pick" title={badge.Name}>
									<input type="radio" name="motif" value={badge.Slug} checked={badge.Slug == data.selected_motif} aria-describedby="badge-picker-help" class="badge-option__radio"></input>
									<span class="badge-option__art" style={"mask-image:url(/avatars/motifs/" + badge.Slug + ".png);-webkit-mask-image:url(/avatars/motifs/" + badge.Slug + ".png);"} aria-hidden="true"></span>
									<small>{badge.Name}</small>
								</label>
							</Each>
						</div>
						<p class="error-message form-error signup-form__error" data-gosx-field-error="motif" aria-live="polite"></p>
						<p id="signup-form-status" class="form-status signup-form__status" role="status" aria-live="polite"></p>
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
				<If cond={data.public_entry.can_claim == false}>
					<p class="hero-deck">
						<strong>{data.public_entry.state_label}</strong>
						<br></br>
						{data.public_entry.detail}
					</p>
					<If cond={data.public_entry.action_href != "/join"}>
						<div class="hero-actions">
							<a href={data.public_entry.action_href} data-gosx-link class="button button--primary">
								{data.public_entry.action_label}
								<span aria-hidden="true">→</span>
							</a>
						</div>
					</If>
				</If>
				<If cond={data.public_entry.is_commissioner}>
					<div class="hero-actions">
						<a href={data.public_entry.commissioner_href} data-gosx-link class="button button--secondary">{data.public_entry.commissioner_label}</a>
					</div>
				</If>
			</div>
		</section>
	</main>
}
