package wire

// SignalCardProps structurally mirrors page.server.go's WireSignalCard
// (the loader's actual type): the same-file schema rule requires a strict
// component's props to be declared in this .gsx file, so this needs only
// to share shape with the loader's converter, not identity.
type SignalCardProps struct {
	ID                 string
	Category           string
	Label              string
	Text               string
	Source             string
	ReportedBy         string
	HasReporter        bool
	Evidence           string
	Trust              string
	Time               string
	URL                string
	HasURL             bool
	Rule               string
	Confidence         string
	Corroborations     int
	HasCorroboration   bool
	CorroborationLabel string
	Retained           bool
}

component SignalCard(props: SignalCardProps) {
	return <article class={"wire-event wire-event--" + props.Category} data-wire-event={props.ID} data-wire-category={props.Category}>
		<header>
			<div class="wire-event__heading">
				<span class="wire-event__label">{props.Label}</span>
				<span class="wire-event__evidence">{props.Evidence}</span>
			</div>
			<span class="wire-event__trust mono">{props.Trust} · {props.Confidence}%</span>
		</header>
		<p>{props.Text}</p>
		<footer>
			<span class="mono">{props.Source}</span>
			<If cond={props.HasReporter}>
				<span class="mono">VIA {props.ReportedBy}</span>
			</If>
			<If cond={props.HasCorroboration}>
				<span class="wire-event__corroboration mono">{props.CorroborationLabel}</span>
			</If>
			<If cond={props.Retained}>
				<span class="wire-event__retained mono">RETAINED · AS OF</span>
			</If>
			<time class="mono">{props.Time}</time>
			<If cond={props.HasURL}>
				<a href={props.URL} target="_blank" rel="noreferrer">Read the report ↗</a>
			</If>
		</footer>
	</article>
}

type WireEmptyStateProps struct {
	WireConfigured bool
	WireIssue      string
}

// WireEmptyState is the "no signals yet" panel shown inside the wire feed
// region — both on the page's own first render and inside the
// data-gosx-region fragment /wire/fragment answers when a later poll finds
// zero signals, so the two paths render byte-identical markup (see
// FeedFragment in page.server.go).
component WireEmptyState(props: WireEmptyStateProps) {
	return <div class="wire-empty" data-wire-empty>
		<span class="mono">NO SIGNALS YET</span>
		<h3>Your wire is quiet—not broken.</h3>
		<If cond={props.WireConfigured == false}>
			<p>{props.WireIssue}</p>
			<p>Ask the commissioner to add news sources.</p>
		</If>
		<If cond={props.WireConfigured}>
			<p>Relevant feed items and league sightings appear here, and stay provisional until the official stats catch up.</p>
		</If>
	</div>
}

func Page() Node {
	return <main class="page wire-page" id="main-content" data-wire-root data-wire-last={data.last_event_id} data-gosx-live-src="/api/wire/pulse" data-gosx-live-interval="20s">
		<header class="page-masthead wire-masthead">
			<div>
				<span class="signal-label">
					<span class="signal-mark" aria-hidden="true"></span>
					PRIVATE LEAGUE NEWS
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
						<span class="section-index">NEWS DESK</span>
						<h2>Fantasy-relevant dispatches</h2>
					</div>
					<div class="sync-state" role="status" aria-live="polite">
						<span class="live-dot live-dot--bound" aria-hidden="true" data-gosx-live-bind="indicator">{data.wire_indicator}</span>
						<span data-wire-status data-gosx-live-bind="status">{data.wire_health}</span>
						<If cond={data.wire_source_issue != ""}>
							<small class="wire-source-issue">{data.wire_source_issue}</small>
						</If>
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
						<WireEmptyState {...data.wire_empty}></WireEmptyState>
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
						<span class="signal-mark" aria-hidden="true"></span>
						<div>
							<strong>Public feeds</strong>
							<small>{data.feed_ready}/{data.feed_count} ready · updates every 2 min</small>
						</div>
					</div>
					<If cond={data.bluesky_count > 0}>
						<div class="wire-system-row">
							<span class="signal-mark" aria-hidden="true"></span>
							<div>
								<strong>Bluesky event wire</strong>
								<small>{data.wire_mode} · {data.bluesky_count} tracked accounts</small>
							</div>
						</div>
					</If>
					<div class="wire-system-row">
						<span class="signal-mark signal-mark--cyan" aria-hidden="true"></span>
						<div>
							<strong>Schedule data</strong>
							<small>{data.schedule_state} · {data.schedule_rows} games · {data.schedule_updated}</small>
						</div>
					</div>
					<div class="wire-system-row">
						<span class="signal-mark signal-mark--hot" aria-hidden="true"></span>
						<div>
							<strong>{data.season} player ledger</strong>
							<small>{data.player_state} · {data.player_rows} players · {data.player_updated}</small>
						</div>
					</div>
					<div class="wire-system-row">
						<span class="signal-mark signal-mark--cyan" aria-hidden="true"></span>
						<div>
							<strong>{data.season} injury ledger</strong>
							<small>{data.injury_state} · {data.injury_rows} reports · {data.injury_updated}</small>
						</div>
					</div>
					<p class="wire-license">Stat source: public NFL data</p>
				</section>

				<section class="wire-source-panel">
					<header>
						<span class="section-index">NEWS SOURCES</span>
						<b>Sources</b>
					</header>
					<div class="wire-source-list">
						<Each of={data.feeds} as="feed">
							<div>
								<a href={feed.url} target="_blank" rel="noreferrer"><strong>{feed.name} ↗</strong></a>
								<small class="mono">{feed.evidence} · {feed.state} · {feed.accepted} kept</small>
								<small class="mono">LAST CHECK · {feed.checked} · LAST PUBLISHED · {feed.published}</small>
								<If cond={feed.has_error}>
									<small class="mono">ERROR · {feed.last_error}</small>
								</If>
							</div>
						</Each>
						<Each of={data.sources} as="source">
							<div>
								<strong>Bluesky · @{source.name}</strong>
								<small class="mono">CURATED SOCIAL</small>
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
						<p class="wire-submit-note">Market sightings are human-entered. The league never reads your accounts elsewhere.</p>
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
