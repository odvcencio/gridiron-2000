package practice

// Page is the practice draft's LOBBY: /draft/practice before a practice
// is open (practice draft, internal/league/practice.go). Once a practice
// is open, page.server.go's Render hook renders the real room's own
// page.gsx (one directory up) with the sandbox's data instead of this
// component, so the practice room IS the draft room — this file only ever
// renders the choose-a-round step and the disabled-with-reason state.
func Page() Node {
	return <main class="page practice-page" id="main-content">
		<header class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="signal-mark" aria-hidden="true"></span>
					{data.league.name}
				</span>
				<h1>Practice draft</h1>
				<p class="lede">Take a few picks on the clock in a copy of the draft room. The other seats are played by bots. Nothing you do here is saved.</p>
			</div>
			<div class="draft-clock-panel">
				<span>Real draft</span>
				<If cond={data.draft.published}>
					<strong class="mono">{data.draft.date}</strong>
					<div class="draft-clock-meta">
						<span class="mono">{data.draft.time} · {data.draft.timezone}</span>
						<span class="mono">{data.rounds} rounds · {data.pick_clock_label} per pick</span>
					</div>
				</If>
				<If cond={data.draft.published == false}>
					<strong class="mono">NOT SET</strong>
					<div class="draft-clock-meta">
						<span class="mono">{data.rounds} rounds · {data.pick_clock_label} per pick</span>
					</div>
				</If>
			</div>
		</header>
		<div class="draft-notice" aria-live="polite">
			<If cond={data.has_notice}><p class="flash-message">{data.notice}</p></If>
			<If cond={data.has_error}><p class="error-message" role="alert">{data.error}</p></If>
		</div>
		<If cond={data.practice.allowed == false}>
			<section class="empty-tape" aria-labelledby="practice-unavailable-title">
				<strong id="practice-unavailable-title">PRACTICE UNAVAILABLE</strong>
				<p>{data.practice.reason}</p>
				<p><a href="/draft" data-gosx-link>Open the draft room →</a></p>
			</section>
		</If>
		<If cond={data.practice.allowed}>
			<section class="practice-start" aria-labelledby="practice-start-title">
				<h2 id="practice-start-title">Choose where to start</h2>
				<p class="muted">You sit in your real seat, <strong>{data.practice_team_name}</strong>, in the real draft order. Earlier rounds are filled in for you. The practice runs for {data.practice_span} rounds, then stops.</p>
				<form method="post" action={data.start_action} class="practice-start__form" data-gosx-managed="false">
					<input type="hidden" name="csrf_token" value={data.csrf}></input>
					<fieldset class="practice-start__options">
						<legend class="visually-hidden">Start round</legend>
						<Each of={data.practice.options} as="option">
							<label class="practice-start__option">
								<input type="radio" name="round" value={option.round} checked={option.round == 1}></input>
								<span class="practice-start__label"><strong>{option.label}</strong> <small class="mono">ROUND {option.round}</small></span>
								<small class="practice-start__detail">{option.detail}</small>
							</label>
						</Each>
					</fieldset>
					<button class="button button--primary" type="submit">Start the practice →</button>
				</form>
				<p class="muted"><a href="/draft" data-gosx-link>Back to the draft room</a></p>
			</section>
		</If>
	</main>
}
