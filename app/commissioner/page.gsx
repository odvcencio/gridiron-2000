package commissioner

type FleetReadoutProps struct {
	IsCommissioner    bool
	FederationEnabled bool
	Cards             []map[string]any
	AttentionQueue    []map[string]any
	LeagueCount       int
	ClaimedSeats      int
	TotalSeats        int
	DraftsLive        int
	AttentionCount    int
	CriticalCount     int
	WarningCount      int
	GeneratedAt       string
	GeneratedAtISO    string
}

func FleetReadout(props FleetReadoutProps) Node {
	return <div class="commissioner-hq__readout">
		<section class="draft-masthead commissioner-hq__masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label"><span class="live-dot" aria-hidden="true"></span>COMMISSIONER HQ · READ ONLY</span>
				<h1>EVERY LEAGUE.<br></br>ONE READOUT.</h1>
				<p>Fleet visibility without merged databases.<br></br>Every action stays on its owning league.</p>
			</div>
			<div class="draft-clock-panel commissioner-hq__fleet-total" aria-label="Fleet totals">
				<span>Fleet status</span>
				<strong class="mono">{props.LeagueCount} LEAGUES</strong>
				<div class="draft-clock-meta">
					<span>{props.ClaimedSeats} / {props.TotalSeats} SEATS CLAIMED</span>
					<span>{props.DraftsLive} DRAFTS LIVE</span>
				</div>
				<div class="commissioner-hq__severity mono">
					<span>{props.CriticalCount} CRITICAL</span>
					<span>{props.WarningCount} WARNING</span>
				</div>
				<time class="commissioner-hq__generated mono" datetime={props.GeneratedAtISO}>GENERATED {props.GeneratedAt}</time>
			</div>
		</section>
		<If cond={props.IsCommissioner == false}>
			<section class="player-pool"><div class="empty-tape">
				<strong>RESTRICTED</strong>
				<p>Fleet status is limited to the commissioner configured on this league.</p>
				<a href="/" data-gosx-link>Back to league home →</a>
			</div></section>
		</If>
		<If cond={props.IsCommissioner}>
			<p class="commissioner-hq__refresh-status mono" role="status" aria-live="polite" aria-atomic="true">
				FLEET SNAPSHOT · {props.LeagueCount} LEAGUES · GENERATED {props.GeneratedAt}
			</p>
			<If cond={props.FederationEnabled == false}>
				<p class="demo-message"><strong>LOCAL-ONLY:</strong> this league is independent until commissioner peers are configured. No shared session or data plane is created.</p>
			</If>
			<section id="commissioner-attention-queue" class="commissioner-hq__queue" aria-labelledby="commissioner-attention-heading">
				<div class="commissioner-hq__subhead">
					<span class="signal-label">OPERATIONS QUEUE</span>
					<h2 id="commissioner-attention-heading">Attention by league</h2>
					<p>Sorted by severity and count.<br></br>Open the owning league section.</p>
				</div>
				<If cond={props.AttentionCount == 0}>
					<p class="commissioner-hq__empty">No open flags. Every configured league is clear.</p>
				</If>
				<div class="commissioner-hq__queue-list">
					<Each of={props.AttentionQueue} as="item">
						<article class="commissioner-hq__attention" data-severity={item.severity} data-attention-code={item.code}>
							<div class="commissioner-hq__attention-copy">
								<span class="section-index">{item.severity} · {item.league}</span>
								<strong>{item.message}</strong>
								<span class="mono">{item.area} · {item.count} occurrence(s)</span>
							</div>
							<a href={item.owner_url}>{item.owner_text} →</a>
						</article>
					</Each>
				</div>
			</section>
			<div class="admin-grid commissioner-hq__cards" aria-label="League readouts">
				<Each of={props.Cards} as="card">
					<section class="player-pool commissioner-hq__card" data-peer-id={card.peer_id}>
						<If cond={card.available == false}>
							<div class="pool-toolbar commissioner-hq__card-header"><div>
								<span class="section-index">{card.peer_id}</span>
								<h2>League unavailable</h2>
								</div><span class="position-chip">DEGRADED</span>
							</div>
							<p class="error-message">{card.error}</p>
							<p class="scoring-note">This readout is independent; the owning league remains available directly.</p>
							<a href={card.public_url}>Open {card.peer_id} directly →</a>
						</If>
						<If cond={card.available}>
							<div class="pool-toolbar commissioner-hq__card-header">
								<div>
									<span class="section-index">{card.short_code} // {card.mode} · SEASON {card.season}</span>
									<h2>{card.name}</h2>
									<p class="scoring-note">{card.host_label} · {card.draft_start_copy}</p>
								</div>
								<span class="position-chip">{card.draft_status}</span>
							</div>
							<div class="commissioner-hq__provenance">
								<span><strong>RUNTIME</strong> {card.runtime_ready} · {card.app_version}</span>
								<span><strong>FRAMEWORK</strong> {card.framework_version}</span>
								<span><strong>RELEASE</strong> {card.git_sha} · {card.build}</span>
								<time class="mono" datetime={card.generated_at_iso}>SNAPSHOT {card.generated_at}</time>
							</div>
							<div class="commissioner-hq__details">
								<section id={"commissioner-"+card.peer_id+"-seats"} class="commissioner-hq__detail">
									<h3>SEATS · LEDGER</h3>
									<p><strong>{card.claimed_seats} / {card.seats}</strong> claimed · {card.ready_seats} ready</p>
									<ol class="commissioner-hq__ledger">
										<Each of={card.seat_ledger} as="seat">
											<li>SEAT {seat.seat} · <If cond={seat.claimed}>CLAIMED</If><If cond={seat.claimed == false}>OPEN</If> · <If cond={seat.ready}>READY</If><If cond={seat.ready == false}>NOT READY</If></li>
										</Each>
									</ol>
								</section>
								<section class="commissioner-hq__detail">
									<h3>DRAFT CONTROL</h3>
									<p><strong>{card.draft_status}</strong> · <If cond={card.draft_started}>STARTED</If><If cond={card.draft_started == false}>NOT STARTED</If></p>
									<p>{card.draft_start_copy}</p>
									<p class="mono"><time datetime={card.draft_at_iso}>{card.draft_at}</time> · {card.draft_order} · {card.clock_text}</p>
								</section>
								<section class="commissioner-hq__detail">
									<h3>SCHEDULE / WEEK CLOSE</h3>
									<p><strong>{card.season_phase}</strong> · week {card.current_week} · {card.schedule_range}</p>
									<p>{card.schedule_text}</p>
									<p class="mono">WEEK {card.week_close_week} · {card.week_close_badge} · {card.week_close_games}/{card.week_close_total} GAMES FINAL</p>
									<If cond={card.week_close_waiting}><p class="scoring-note">{card.week_close_waiting_reason}</p></If>
									<p>{card.schedule_final_text} · stats <If cond={card.week_close_stats}>FRESH</If><If cond={card.week_close_stats == false}>WAITING</If></p>
								</section>
								<section class="commissioner-hq__detail">
									<h3>PLAYER POOL</h3>
									<p><strong>{card.pool_mode}</strong> · {card.pool_actual} actual · {card.pool_roster_capacity} roster minimum · {card.pool_target} planning target</p>
									<p class="mono">ACTUAL {card.pool_actual_coverage} · TARGET {card.pool_target_coverage} · CUSHION {card.pool_cushion}</p>
									<p>Last sync {card.pool_last_sync}</p>
								</section>
								<section class="commissioner-hq__detail">
									<h3>OPEN DATA STATES</h3>
									<ul class="commissioner-hq__data-list">
										<Each of={card.open_data} as="row"><li><strong>{row.label}</strong><span class="mono">{row.state} · {row.updated}</span></li></Each>
									</ul>
								</section>
								<section class="commissioner-hq__detail">
									<h3>YEAR ONE PLAYOFFS</h3>
									<p><If cond={card.playoff_available}>AVAILABLE</If><If cond={card.playoff_available == false}>NOT AVAILABLE</If> · {card.playoff_note}</p>
								</section>
							</div>
							<If cond={card.has_attention}>
								<div class="notice-stack commissioner-hq__inline-attention" aria-label="League attention">
									<Each of={card.attention} as="item"><p class="demo-message"><strong>{item.severity}:</strong> {item.message} <a href={item.owner_url}>{item.owner_text} →</a></p></Each>
								</div>
							</If>
							<nav class="commissioner-hq__links" aria-label="League links">
								<a href={card.home_url}>Open league →</a>
								<a href={card.admin_draft_url}>Draft controls →</a>
								<a href={card.admin_schedule_url}>Schedule →</a>
								<a href={card.admin_data_url}>Data →</a>
							</nav>
						</If>
					</section>
				</Each>
			</div>
		</If>
	</div>
}

func Page() Node {
	return <main class="page admin-page commissioner-hq" id="main-content">
		<If cond={data.is_commissioner == false}>
			<FleetReadout {...data.fleet}></FleetReadout>
		</If>
		<If cond={data.is_commissioner}>
			<div class="commissioner-hq__fleet-region" data-gosx-region data-gosx-region-url="/commissioner/fragment" data-gosx-region-interval="30s">
				<FleetReadout {...data.fleet}></FleetReadout>
			</div>
		</If>
	</main>
}
