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
				<TextBlock as="p" class="lede" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={2} overflow="ellipsis">
					See what a live draft looks like before {data.real_draft.day_name}.
				</TextBlock>
				<TextBlock as="p" class="muted" font="400 15px Plus Jakarta Sans" lineHeight={22} text="You take picks on the clock in a copy of the draft room. The other seats are played by bots. Nothing you do here is saved." />
			</div>
			<div class="draft-clock-panel">
				<span>Real draft</span>
				<If cond={data.real_draft.published}>
					<strong class="mono">{data.real_draft.date}</strong>
					<div class="draft-clock-meta">
						<span class="mono">{data.real_draft.time} · {data.real_draft.relative}</span>
						<span class="mono">{data.rounds} rounds · {data.pick_clock_label} per pick</span>
					</div>
				</If>
				<If cond={data.real_draft.published == false}>
					<strong class="mono">NOT SET</strong>
					<div class="draft-clock-meta">
						<span class="mono">Draft time not published yet</span>
						<span class="mono">{data.rounds} rounds · {data.pick_clock_label} per pick</span>
					</div>
				</If>
				<div class="draft-clock-meta practice-checkin">
					<If cond={data.real_draft.has_seat && data.real_draft.checked_in}><span class="mono practice-checkin__state" data-checked-in="true">Checked in ✓</span></If>
					<If cond={data.real_draft.has_seat && data.real_draft.checked_in == false}><span class="mono practice-checkin__state" data-checked-in="false">Not checked in</span></If>
					<a href={data.real_draft.room_href} data-gosx-link>Open the real room →</a>
				</div>
			</div>
		</header>
		<div class="draft-notice" aria-live="polite">
			<If cond={data.has_notice}><TextBlock as="p" class="flash-message" font="400 15px Plus Jakarta Sans" lineHeight={22} text={data.notice} /></If>
			<If cond={data.has_error}><TextBlock as="p" class="error-message" font="400 15px Plus Jakarta Sans" lineHeight={22} role="alert" text={data.error} /></If>
		</div>
		<If cond={data.practice.allowed == false}>
			<section class="empty-tape" aria-labelledby="practice-unavailable-title">
				<strong id="practice-unavailable-title">PRACTICE UNAVAILABLE</strong>
				<TextBlock as="p" font="400 15px Plus Jakarta Sans" lineHeight={22} text={data.practice.reason} />
				<p><a href="/draft" data-gosx-link>Open the draft room →</a></p>
			</section>
		</If>
		<If cond={data.practice.allowed}>
			<section class="practice-start" aria-labelledby="practice-start-title">
				<h2 id="practice-start-title">Choose where to start</h2>
				<TextBlock as="p" class="muted" font="400 15px Plus Jakarta Sans" lineHeight={22} maxLines={3} overflow="ellipsis">
					You sit in your real seat, <strong>{data.practice_team_name}</strong>, in the real draft order. Earlier rounds are filled in for you. The practice runs until the last pick of the sandbox draft, or until you leave.
				</TextBlock>
				<form method="post" action={data.start_action} class="practice-start__form" data-gosx-managed="false">
					<input type="hidden" name="csrf_token" value={data.csrf}></input>
					<fieldset class="practice-start__options">
						<legend class="visually-hidden">Start round</legend>
						<Each of={data.practice.options} as="option">
							<label class="practice-start__option">
								<input type="radio" name="round" value={option.round} checked={option.round == 1}></input>
								<span class="practice-start__label">
									<TextBlock as="strong" font="600 16px Plus Jakarta Sans" lineHeight={22} maxLines={1} overflow="ellipsis" text={option.label} />
									<small class="mono">ROUND {option.round}</small>
								</span>
								<TextBlock as="small" class="practice-start__detail" font="400 13px Plus Jakarta Sans" lineHeight={18} maxLines={2} overflow="ellipsis" text={option.detail} />
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
