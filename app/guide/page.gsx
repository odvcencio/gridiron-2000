package guide

func Page() Node {
	return <main class="page guide-page" id="main-content">
		<header class="page-masthead guide-masthead">
			<div>
				<span class="signal-label"><span class="signal-mark" aria-hidden="true"></span>MANAGER GUIDE // START HERE</span>
				<p class="page-kicker">{data.league_name} · {data.mode_label} · For managers arriving from any platform</p>
				<h1>PLAY<br></br>THE<br></br>ROOM.</h1>
				<p class="guide-lede">{data.league_name} is a private league room, not a second account at your old provider. Bring your football knowledge; the commissioner sets this league's rules, membership, and draft start. This guide gets you from invite to game day in one pass.</p>
				<nav class="guide-actions" aria-label="Guide actions">
					<a href="#quickstart" class="button button--primary">Jump to the 5-minute start ↓</a>
					<a href="/login" data-gosx-link class="button button--ghost">Get league access →</a>
				</nav>
			</div>
			<aside class="masthead-console guide-console" aria-label="Guide at a glance">
				<div><span>League format</span><strong>{data.league_format_summary}</strong></div>
				<div><span>Roster capacity</span><strong>{data.roster_capacity} draft slots</strong></div>
				<div><span>Draft meeting</span><strong>{data.draft_at}</strong><small>{data.draft_timezone}</small></div>
				<div><span>Membership</span><strong>{data.membership_label}</strong></div>
				<div><span>Draft start</span><strong>Commissioner-controlled</strong></div>
			</aside>
		</header>

		<nav class="guide-toc" aria-label="On this page">
			<a href="#quickstart">5-minute start</a>
			<a href="#commissioner-checklist">Commissioner checklist</a>
			<a href="#different">What is different</a>
			<a href="#draft">Draft room</a>
			<a href="#league-tools">League tools</a>
			<a href="#data-and-identity">Identity + data</a>
			<a href="#commissioner-hq">Commissioner HQ</a>
			<a href="#migration">Manual migration</a>
		</nav>

		<section class="guide-section guide-section--accent" id="quickstart" aria-labelledby="quickstart-heading">
			<header class="guide-section__heading">
				<span class="section-index">01 // FIVE-MINUTE START</span>
				<h2 id="quickstart-heading">From invite to ready.</h2>
				<p>Do these five things before your first pick or weekly lock.</p>
			</header>
			<ol class="guide-steps">
				<li><span class="guide-step__mark mono">01</span><div><h3>Sign in with an admitted identity</h3><p>{data.membership_detail} Use Google sign-in with the account you want to use. Claim the open team seat, or ask the commissioner to adjust the admission policy in <a href="/admin" data-gosx-link>/admin</a>. Your Gridiron seat is separate from any Sleeper, ESPN, or Yahoo account.</p></div></li>
				<li><span class="guide-step__mark mono">02</span><div><h3>Read the live league rules</h3><p>Open <a href="/scoring" data-gosx-link>/scoring</a> and check roster slots, scoring values, draft rounds, lineup locks, free agency, waivers, trades, and the weekly schedule. This is the current league configuration.</p></div></li>
				<li><span class="guide-step__mark mono">03</span><div><h3>Build your team's Big Board</h3><p>Rank players at <a href="/board" data-gosx-link>/board</a>. The order is private to your team seat, shared by its primary and co-manager, and read first when the team is on the clock. Add backup names.</p></div></li>
				<li><span class="guide-step__mark mono">04</span><div><h3>Mark ready and choose autopick</h3><p>In <a href="/draft" data-gosx-link>/draft</a>, use <strong>Mark ready</strong> when prepared and <strong>Mark not ready</strong> if that changes. Turn on autopick if you may miss your turn; it uses your board first and best available ADP when your board is empty.</p></div></li>
				<li><span class="guide-step__mark mono">05</span><div><h3>Set a lineup every week</h3><p>Use <a href="/team" data-gosx-link>/team</a> to fill starting slots before each player's game locks. Bench, reserve, and injury states are visible there.</p></div></li>
			</ol>
		</section>

		<section class="guide-section" id="commissioner-checklist" aria-labelledby="commissioner-checklist-heading">
			<header class="guide-section__heading">
				<span class="section-index">02 // COMMISSIONER OPENING CHECKLIST</span>
				<h2 id="commissioner-checklist-heading">Open the room with intention.</h2>
				<p>Run this list before inviting managers or announcing the draft.</p>
			</header>
			<ol class="guide-checklist">
				<li><strong>Confirm identity and format.</strong><span>League name, season, timezone, teams, divisions, roster shape, and scoring on <a href="/scoring" data-gosx-link>/scoring</a>.</span></li>
				<li><strong>Invite the right Google accounts.</strong><span>Use <a href="/admin" data-gosx-link>/admin</a>, then verify each person signs in with the exact invited address.</span></li>
				<li><strong>Check the player list.</strong><span>Confirm the pool holds enough players to draft from, then read its freshness label and last-success time. LIVE is fresh from the source. CACHED, STALE, or DEGRADED keeps a labeled last-good snapshot usable. OFFLINE or UNAVAILABLE is not eligible to start a real draft.</span></li>
				<li><strong>Publish the draft window and order.</strong><span>Set date, timezone, rounds, pick clock, and order. Randomize before pick one; decide what to do with unclaimed seats.</span></li>
				<li><strong>Ask managers to build boards and mark ready.</strong><span>Readiness is a signal for the commissioner, not a hidden auto-start trigger.</span></li>
				<li><strong>Start the draft yourself.</strong><span>The scheduled window is a reminder. The draft stays closed until the commissioner types <code>START</code>.</span></li>
			</ol>
			<aside class="guide-callout guide-callout--alert" role="note"><strong>Opening rule:</strong> draft readiness and draft start are commissioner-controlled. A calendar clock never starts pick one by itself.</aside>
			<If cond={data.mode_label == "DYNASTY"}>
				<aside class="guide-callout guide-callout--alert" id="dynasty-boundary" role="note"><strong>Dynasty format // year-one boundary:</strong> Dynasty describes the league's long-term format intent. This season starts with a fresh draft. Automated roster rollover is not available yet; before a future season, the commissioner must announce and manage any keepers or carryover manually.</aside>
			</If>
		</section>

		<section class="guide-section" id="different" aria-labelledby="different-heading">
			<header class="guide-section__heading">
				<span class="section-index">03 // ORIENTATION</span>
				<h2 id="different-heading">What is intentionally different.</h2>
				<p>The commissioner chose these rules.</p>
			</header>
			<div class="guide-compare">
				<article class="guide-compare__panel"><span class="section-index">THE PROVIDER HABIT</span><h3>One account does everything</h3><p>On a hosted fantasy platform, the provider owns account, league setup, data feed, draft timer, and usually an import path. You may expect roster, history, and identity to follow automatically.</p></article>
				<article class="guide-compare__panel guide-compare__panel--signal"><span class="section-index">THE GRIDIRON CONTRACT</span><h3>The league owns the room</h3><p>The league keeps its own record. The commissioner rules on every dispute. Gridiron shows player-source status and keeps manager boards private. There is no automatic Sleeper, ESPN, or Yahoo migration.</p></article>
			</div>
			<div class="guide-card-grid guide-card-grid--three">
				<article class="guide-card"><span class="section-index">DIFFERENT // 01</span><h3>Explicit controls</h3><p>The commissioner starts the draft, can pause or extend the pick clock, and settles corrections. The UI says when a control is locked or unavailable.</p></article>
				<article class="guide-card"><span class="section-index">DIFFERENT // 02</span><h3>Honest fallbacks</h3><p>Saved or built-in player list remains usable, but approximate ranks and missing enrichment are labeled. A source that has not published is not presented as a zero.</p></article>
				<article class="guide-card"><span class="section-index">DIFFERENT // 03</span><h3>Private by default</h3><p>Google identity, league membership, picks, boards, and transactions belong to this league. Public guides never require a seat.</p></article>
			</div>
		</section>

		<section class="guide-section guide-section--accent" id="draft" aria-labelledby="draft-heading">
			<header class="guide-section__heading">
				<span class="section-index">04 // DRAFT READINESS + START</span>
				<h2 id="draft-heading">Ready first. Start second.</h2>
				<p>The draft room shows who is ready, who is on the clock, and the player source’s exact freshness state.</p>
			</header>
			<div class="guide-callout guide-callout--alert" role="note"><strong>Commissioner-controlled start:</strong> the scheduled date opens a window, not the draft. Only the commissioner can type <code>START</code> to open pick one and arm the pick clock.</div>
			<div class="guide-card-grid guide-card-grid--three">
				<article class="guide-card"><span class="section-index">READINESS</span><h3>Mark ready — or mark not ready</h3><p>Use the action that matches your current state after you have a board, backup plan, and draft tab you can monitor. Readiness tells the commissioner who is prepared; it never forces a start.</p></article>
				<article class="guide-card"><span class="section-index">BIG BOARD</span><h3>Your team's private queue</h3><p>Primary and co-manager drag players into one shared seat order at <a href="/board" data-gosx-link>/board</a>. Picked players leave the available list; the draft tape and autopick use the same top available targets.</p></article>
				<article class="guide-card"><span class="section-index">AUTOPICK</span><h3>Fallback, not mystery</h3><p>Autopick chooses the first available player on your Big Board. If empty or exhausted, it falls back to best available ADP and is labeled AUTO.</p></article>
			</div>
			<details class="guide-details"><summary>What happens if a manager is away?</summary><p>Leave autopick on, or let the pick clock expire. An unclaimed seat also runs through the clock and then autopicks. The commissioner can pause, extend, force autopick, undo the most recent pick, or reopen a slot.</p></details>
		</section>

		<section class="guide-section" id="league-tools" aria-labelledby="league-tools-heading">
			<header class="guide-section__heading">
				<span class="section-index">05 // THE CONTROL SURFACES</span>
				<h2 id="league-tools-heading">Know which terminal to open.</h2>
				<p>These concepts are separate on purpose. Each page shows its own lock, status, and next action.</p>
			</header>
			<div class="guide-card-grid guide-card-grid--three">
				<article class="guide-card" id="lineups"><span class="section-index">/TEAM</span><h3>Lineups</h3><p>Set starters and bench players in the Team Terminal, plus reserve or injury placements when the league config includes those zones. Set or auto-fill the best lineup before each player's game starts; locked slots cannot be edited after kickoff.</p><a href="/team" data-gosx-link class="guide-card__link">Open Team Terminal →</a></article>
				<article class="guide-card" id="waivers"><span class="section-index">/PLAYERS</span><h3>Waivers + free agency</h3><p>After the draft, undrafted players are free agents. A dropped player, or a player whose game has started, is on waivers until the clear window passes. File a claim in the Player Pool; mode, priority or FAAB, and resolve time are shown there.</p><a href="/players" data-gosx-link class="guide-card__link">Open Player Pool →</a></article>
				<article class="guide-card" id="trades"><span class="section-index">/TRADES</span><h3>Trades</h3><p>Send an offer, accept or decline incoming offers, and counter when needed. An accepted trade may enter a review window; the configured commissioner or league-veto policy decides what happens next.</p><a href="/trades" data-gosx-link class="guide-card__link">Open Trade Desk →</a></article>
				<article class="guide-card" id="scoring-rules"><span class="section-index">/SCORING</span><h3>Scoring</h3><p>Scoring values, roster shape, schedule, locks, waivers, and trade policy are the source of truth. Official corrected stats win over provisional news, and a closed week keeps its locked lineup and result.</p><a href="/scoring" data-gosx-link class="guide-card__link">Read Rules + Scoring →</a></article>
				<article class="guide-card" id="pickem"><span class="section-index">/PICKEM</span><h3>Pick'em</h3><p>Pick one team against the displayed market spread at <a href="/pickem" data-gosx-link>/pickem</a>. Lines freeze Thursday, but each game stays pickable until its own kickoff. After you enter the week, a missed kickoff is a loss while every later game remains open. Pick'em is its own W-L-P game; no fantasy roster seat is required.</p><a href="/pickem" data-gosx-link class="guide-card__link">Open Pick'em HQ →</a></article>
				<article class="guide-card" id="wire"><span class="section-index">/WIRE</span><h3>Signal Wire</h3><p>The Wire is a mixed-source alert surface for publisher feeds, community tips, and curated social posts. Signals stay provisional, never mutate fantasy scores, and show source/trust context. Market sightings are human-entered; this league does not log into or scrape another account.</p><a href="/wire" data-gosx-link class="guide-card__link">Open Signal Wire →</a></article>
			</div>
		</section>

		<section class="guide-section guide-section--accent" id="data-and-identity" aria-labelledby="data-and-identity-heading">
			<header class="guide-section__heading">
				<span class="section-index">06 // IDENTITY + AVAILABILITY</span>
				<h2 id="data-and-identity-heading">Know who you are, and what the data knows.</h2>
				<p>Two kinds of state are visible because they have different consequences.</p>
			</header>
			<div class="guide-card-grid guide-card-grid--two">
				<article class="guide-card" id="identity"><span class="section-index">IDENTITY // {data.membership_label}</span><h3>Sign-in is not migration</h3><p>{data.membership_detail} Google sign-in proves which person is asking for access; it does not pull an old provider account, roster, record, or history. After admission, the manager claims an open seat, and an optional co-manager invite can be attached from the Team Terminal.</p><ul class="guide-bullets"><li>Use an identity admitted by this league's membership policy.</li><li>Ask the commissioner to correct an invite before claiming the wrong seat.</li><li>Keep provider passwords and sessions outside this league.</li></ul></article>
				<article class="guide-card" id="data-states"><span class="section-index">DATA // AVAILABILITY</span><h3>Read the state label</h3><p>LIVE means a fresh successful source refresh. CACHED is a fresh saved snapshot; STALE is older than the refresh window; DEGRADED means the latest refresh failed but last-good data remains. OFFLINE is rehearsal-only, and UNAVAILABLE means there is no usable list. When known, the interface shows the exact last-success time.</p></article>
			</div>
			<article class="guide-math" id="pool-math">
				<span class="section-index">PLAYER POOL // ENOUGH TO DRAFT FROM</span>
				<h3>Coverage is a planning signal, not a player count.</h3>
				<p>The pool holds more players than the draft needs.</p>
			</article>
		</section>

		<section class="guide-section" id="commissioner-hq" aria-labelledby="commissioner-hq-heading">
			<header class="guide-section__heading">
				<span class="section-index">07 // CROSS-LEAGUE COMMISSIONER HQ</span>
				<h2 id="commissioner-hq-heading">One readout. Separate rooms.</h2>
				<p>Commissioner HQ is one page for a commissioner who runs more than one league.</p>
			</header>
			<div class="guide-compare">
				<article class="guide-compare__panel guide-compare__panel--signal"><span class="section-index">WHAT IT DOES</span><h3>Read across leagues</h3><ul class="guide-bullets"><li>Shows configured peers, seats, readiness, draft status, player pool health, and attention flags.</li><li>Links to the owning league's home, admin, or draft page when action is needed.</li><li>Surfaces an unavailable peer without pretending its state is current.</li></ul></article>
				<article class="guide-compare__panel"><span class="section-index">WHAT IT DOES NOT DO</span><h3>Merge league state</h3><ul class="guide-bullets"><li>It never mixes two leagues. Each league keeps its own record.</li><li>It does not give a commissioner a cross-league write button.</li><li>Each league keeps its own identity boundary, rules, and source decisions.</li></ul></article>
			</div>
			<a href="/commissioner" data-gosx-link class="button button--compact">Open Commissioner HQ →</a>
		</section>

		<section class="guide-section guide-section--accent" id="migration" aria-labelledby="migration-heading">
			<header class="guide-section__heading">
				<span class="section-index">08 // MANUAL MIGRATION</span>
				<h2 id="migration-heading">Bring the league over by hand.</h2>
				<p><strong>No automated migration exists.</strong> That is intentional: a commissioner should verify what becomes the new league record instead of silently importing stale or disputed data.</p>
			</header>
			<div class="guide-compare">
				<article class="guide-compare__panel guide-compare__panel--signal"><span class="section-index">CAN BE RECREATED</span><h3>Bring the decisions</h3><ul class="guide-bullets"><li>Manager names and invited email addresses.</li><li>Team names, abbreviations, divisions, and visual identity.</li><li>Roster slots, scoring values, draft date, clock, waiver mode, and trade policy.</li><li>Player preferences, rebuilt as one shared Big Board per team seat.</li></ul></article>
				<article class="guide-compare__panel"><span class="section-index">CANNOT BE IMPORTED AUTOMATICALLY</span><h3>Do not expect a copy button</h3><ul class="guide-bullets"><li>Sleeper, ESPN, or Yahoo login sessions and passwords.</li><li>Existing rosters, draft picks, waiver priority/FAAB, trades, records, or league history.</li><li>Provider-specific scoring quirks or disputed stat corrections without commissioner review.</li><li>Private provider data that this league has not deliberately entered or sourced.</li></ul></article>
			</div>
			<div class="guide-migration">
				<h3>Practical manual migration checklist</h3>
				<ol class="guide-checklist">
					<li><strong>Freeze the old league.</strong><span>Export or screenshot the rules and standings you want to reference, then agree on a cutover time.</span></li>
					<li><strong>Set league rules with the commissioner.</strong><span>Ask the commissioner to set league format, teams, divisions, and timezone.</span></li>
					<li><strong>Use each control where it actually lives.</strong><span>Use <a href="/admin" data-gosx-link>/admin</a> for invites, seats, roster shape, draft order/clock, and announcements. Verify the resolved roster, scoring, schedule, waiver, and trade rules on <a href="/scoring" data-gosx-link>/scoring</a> before inviting the league.</span></li>
					<li><strong>Invite managers.</strong><span>Send the new league URL and invite the Google email each manager will actually use.</span></li>
					<li><strong>Verify every seat.</strong><span>Have each manager claim or confirm the correct team before the first draft action.</span></li>
					<li><strong>Rebuild boards and draft order.</strong><span>Managers rank targets; the commissioner checks order and readiness, then explicitly starts the new draft.</span></li>
					<li><strong>Record exceptions.</strong><span>Put carried-over keeper, credit, or historical notes in the commissioner announcement or league record instead of hiding them in an import.</span></li>
				</ol>
			</div>
		</section>

		<footer class="guide-next">
			<div><span class="section-index">NEXT TRANSMISSION</span><h2>Make the live rules your source of truth.</h2><p>When a guide sentence and the configured league disagree, the current league page wins. Ask the commissioner before a deadline if the state is unclear.</p></div>
			<div class="guide-actions"><a href="/scoring" data-gosx-link class="button button--primary">Open rules + scoring →</a><a href="/" data-gosx-link class="button button--ghost">Back to league HQ</a></div>
		</footer>
	</main>
}
