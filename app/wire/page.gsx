package wire

func SignalCard(props any) Node {
	return <article class={"wire-event wire-event--" + props.category} data-wire-event={props.id} data-wire-category={props.category}>
		<header>
			<div class="wire-event__heading">
				<span class="wire-event__label">{props.label}</span>
				<span class="wire-event__evidence">{props.evidence}</span>
			</div>
			<span class="wire-event__trust mono">{props.trust} · {props.confidence}%</span>
		</header>
		<p>{props.text}</p>
		<footer>
			<span class="mono">{props.source}</span>
			<If cond={props.has_reporter}>
				<span class="mono">VIA {props.reported_by}</span>
			</If>
			<If cond={props.has_corroboration}>
				<span class="wire-event__corroboration mono">{props.corroboration_label}</span>
			</If>
			<time class="mono">{props.time}</time>
			<If cond={props.has_url}>
				<a href={props.url} target="_blank" rel="noreferrer">Inspect source ↗</a>
			</If>
		</footer>
	</article>
}

// WireEmptyState is the "no signals yet" panel shown inside the wire feed
// region — both on the page's own first render and inside the
// data-gosx-region fragment /wire/fragment answers when a later poll finds
// zero signals, so the two paths render byte-identical markup (see
// FeedFragment in page.server.go).
func WireEmptyState(props any) Node {
	return <div class="wire-empty" data-wire-empty>
		<span class="mono">NO SIGNALS YET</span>
		<h3>Your wire is quiet—not broken.</h3>
		<If cond={props.wire_configured == false}>
			<p>{props.wire_issue}</p>
			<p>
				Enable the built-in public feeds, add a feed file, or put trusted reporter and team handles in
				<span class="inline-code">BLUESKY_HANDLES</span>.
			</p>
		</If>
		<If cond={props.wire_configured}>
			<p>Relevant feed items and league sightings appear here, and stay provisional until the official stats catch up.</p>
		</If>
	</div>
}

func Page() Node {
	return <main class="page wire-page" id="main-content" data-wire-root data-wire-last={data.last_event_id} data-gosx-live-src="/api/wire/pulse" data-gosx-live-interval="20s">
		<header class="page-masthead wire-masthead">
			<div>
				<span class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					PRIVATE INTELLIGENCE MESH
				</span>
				<p class="page-kicker">News, community tips, and social · always free</p>
				<h1>
					SIGNAL
					<br></br>
					WIRE.
				</h1>
			</div>
			<div class="masthead-console wire-console">
				<div>
					<span>Wire state</span>
					<strong data-wire-mode data-gosx-live-bind="mode">{data.wire_mode}</strong>
				</div>
				<div>
					<span>Open channels</span>
					<strong class="mono">{data.source_count}</strong>
				</div>
				<div>
					<span>Signals</span>
					<strong class="mono" data-wire-count data-gosx-live-bind="count">{data.signal_count}</strong>
				</div>
				<div>
					<span>Updates</span>
					<strong class="mono">{data.refresh_seconds} SEC</strong>
				</div>
			</div>
		</header>

		<section class="wire-trust-strip" aria-label="Data confidence">
			<div>
				<span>01</span>
				<strong>Crowd + publishers alert us</strong>
				<small>Fast, mixed-source, provisional</small>
			</div>
			<i aria-hidden="true">→</i>
			<div>
				<span>02</span>
				<strong>League clusters the evidence</strong>
				<small>Links, trust tier, timestamps</small>
			</div>
			<i aria-hidden="true">→</i>
			<div>
				<span>03</span>
				<strong>Open stats reconcile</strong>
				<small>Corrected ledger wins every dispute</small>
			</div>
		</section>

		<div class="wire-layout">
			<section class="wire-stage">
				<header class="section-heading section-heading--split">
					<div>
						<span class="section-index">INTEL BUS // CHANNEL 03</span>
						<h2>Fantasy-relevant dispatches</h2>
					</div>
					<div class="sync-state" role="status" aria-live="polite">
						<span class="live-dot" aria-hidden="true"></span>
						<span data-wire-status data-gosx-live-bind="status">Monitoring the free-source mesh</span>
					</div>
				</header>
				<div class="wire-filters" aria-label="Filter signals">
					<Each of={data.filters} as="filter">
						<If cond={filter.active}>
							<a class="wire-filter is-active" href={filter.href}>{filter.label}</a>
						</If>
						<If cond={filter.active == false}>
							<a class="wire-filter" href={filter.href}>{filter.label}</a>
						</If>
					</Each>
				</div>
				<div class="wire-feed" data-wire-list data-gosx-region data-gosx-region-url={data.fragment_url} data-gosx-region-interval="20s">
					<If cond={data.empty}>
						<WireEmptyState wire_configured={data.wire_configured} wire_issue={data.wire_issue}></WireEmptyState>
					</If>
					<Each of={data.signals} as="signal">
						<SignalCard {...signal}></SignalCard>
					</Each>
				</div>
			</section>

			<aside class="wire-rail">
				<section class="wire-system-panel">
					<header>
						<span class="section-index">OUR DATA // NOT RENTED</span>
						<b>Owner-operated data</b>
					</header>
					<div class="wire-system-row">
						<span class="status-pin" aria-hidden="true"></span>
						<div>
							<strong>Public feeds</strong>
							<small>{data.feed_ready}/{data.feed_count} ready · updates every 2 min</small>
						</div>
					</div>
					<If cond={data.bluesky_count > 0}>
						<div class="wire-system-row">
							<span class="status-pin" aria-hidden="true"></span>
							<div>
								<strong>Bluesky event wire</strong>
								<small>{data.wire_mode} · {data.bluesky_count} tracked accounts</small>
							</div>
						</div>
					</If>
					<div class="wire-system-row">
						<span class="status-pin status-pin--cyan" aria-hidden="true"></span>
						<div>
							<strong>Schedule data</strong>
							<small>{data.schedule_state} · {data.schedule_rows} games · {data.schedule_updated}</small>
						</div>
					</div>
					<div class="wire-system-row">
						<span class="status-pin status-pin--hot" aria-hidden="true"></span>
						<div>
							<strong>{data.season} player ledger</strong>
							<small>{data.player_state} · {data.player_rows} players · {data.player_updated}</small>
						</div>
					</div>
					<div class="wire-system-row">
						<span class="status-pin status-pin--cyan" aria-hidden="true"></span>
						<div>
							<strong>{data.season} injury ledger</strong>
							<small>{data.injury_state} · {data.injury_rows} reports · {data.injury_updated}</small>
						</div>
					</div>
					<a href={data.attribution_url} target="_blank" rel="noreferrer" class="wire-license">
						nflverse · {data.license} ↗
					</a>
				</section>

				<section class="wire-source-panel">
					<header>
						<span class="section-index">SOURCE LOCKER // OPEN WEB</span>
						<b>Signal mesh</b>
					</header>
					<div class="wire-source-list">
						<Each of={data.feeds} as="feed">
							<div>
								<a href={feed.url} target="_blank" rel="noreferrer"><strong>{feed.name} ↗</strong></a>
								<small class="mono">{feed.evidence} · {feed.state} · {feed.accepted} kept</small>
							</div>
						</Each>
						<Each of={data.sources} as="source">
							<div>
								<strong>Bluesky · @{source.name}</strong>
								<small class="mono">CURATED SOCIAL · {source.did}</small>
							</div>
						</Each>
					</div>
				</section>

				<section class="wire-submit-panel" id="community-input">
					<header>
						<span class="section-index">LEAGUE EYES // CHANNEL 08</span>
						<b>Add a sighting</b>
					</header>
					<If cond={data.has_notice}>
						<p class="flash-message" role="status">{data.notice}</p>
					</If>
					<If cond={data.can_submit}>
						<form method="post" action={actionPath("submit-sighting")}>
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<label>
								<span>What kind?</span>
								<select name="evidence_type" required>
									<option value="community" selected={data.evidence_community}>League / social tip</option>
									<option value="submitted_news" selected={data.evidence_news}>News link</option>
									<option value="market" selected={data.evidence_market}>Market sighting</option>
								</select>
								<If cond={data.has_evidence_type_error}><small class="field-error">{data.evidence_type_error}</small></If>
							</label>
							<label>
								<span>Where did you see it?</span>
								<input name="source_name" value={data.submit_source_name} maxlength="100" placeholder="PrizePicks, local reporter, TV broadcast…" required></input>
								<If cond={data.has_source_name_error}><small class="field-error">{data.source_name_error}</small></If>
							</label>
							<label>
								<span>Public link <small>(optional)</small></span>
								<input type="url" name="source_url" value={data.submit_source_url} maxlength="2048" placeholder="https://…" inputmode="url"></input>
								<If cond={data.has_source_url_error}><small class="field-error">{data.source_url_error}</small></If>
							</label>
							<label>
								<span>What happened?</span>
								<textarea name="summary" maxlength="480" rows="4" placeholder="Player, team, status, and what changed…" required>{data.submit_summary}</textarea>
								<If cond={data.has_summary_error}><small class="field-error">{data.summary_error}</small></If>
							</label>
							<If cond={data.has_submit_error}>
								<p class="error-message" role="alert">{data.submit_error}</p>
							</If>
							<button class="button button--primary" type="submit">Transmit sighting</button>
						</form>
						<p class="wire-submit-note">Market sightings are human-entered. This app never logs into or scrapes PrizePicks—or any consumer account.</p>
					</If>
					<If cond={data.can_submit == false}>
						<p>Only signed-in league managers can add a sighting.</p>
						<a class="button button--primary" href="/login">Sign in with Google</a>
					</If>
				</section>

				<div class="data-note data-note--warning">
					<span>Scoring firewall</span>
					<p>
						A feed item, post, or league sighting can raise an alert; it can never award fantasy points. Corroboration raises visibility, not authority.
					</p>
					<small class="mono">{data.ignored_count} noise items ignored · {data.deleted_count} deletions honored</small>
				</div>
			</aside>
		</div>
	</main>
}
