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
				<span class="signal-label"><span class="signal-mark" aria-hidden="true"></span>COMMISSIONER HQ · READ ONLY</span>
				<h1>EVERY LEAGUE.<br></br>ONE READOUT.</h1>
				<p>One page for every league. Each league keeps its own record.<br></br>Every action stays on its owning league.</p>
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
				LEAGUE REPORT · {props.LeagueCount} LEAGUES · GENERATED {props.GeneratedAt}
			</p>
			<If cond={props.FederationEnabled == false}>
				<p class="demo-message"><strong>LOCAL-ONLY:</strong> this league is independent until commissioner peers are configured. This league stands alone until the commissioner adds another league.</p>
			</If>
			<section id="commissioner-attention-queue" class="commissioner-hq__queue" aria-labelledby="commissioner-attention-heading">
				<div class="commissioner-hq__subhead">
					<span class="signal-label">NEEDS ATTENTION</span>
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
								<span class="mono">{item.count} occurrence(s)</span>
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
								<span class="section-index">{card.name}</span>
								<h2>League unavailable</h2>
								</div><span class="position-chip">UNAVAILABLE</span>
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
									<p class="scoring-note">{card.name} · {card.draft_start_copy}</p>
								</div>
								<span class="position-chip">{card.draft_status}</span>
							</div>
							<section class="commissioner-hq__provenance" aria-label="Release metadata">
								<span>
									<strong>APP VERSION</strong>
									<span class="mono"><If cond={card.app_version != ""}>{card.app_version}</If><If cond={card.app_version == ""}>UNKNOWN</If></span>
								</span>
								<span>
									<strong>SOURCE GIT SHA</strong>
									<span class="mono"><If cond={card.git_sha != ""}>{card.git_sha}</If><If cond={card.git_sha == ""}>UNKNOWN</If></span>
								</span>
								<span>
									<strong>BUILD TIMESTAMP</strong>
									<If cond={card.build != ""}><time class="mono" datetime={card.build}>{card.build}</time></If>
									<If cond={card.build == ""}><span class="mono">UNKNOWN</span></If>
								</span>
								<span>
									<strong>FRAMEWORK VERSION</strong>
									<span class="mono"><If cond={card.framework_version != ""}>{card.framework_version}</If><If cond={card.framework_version == ""}>UNKNOWN</If></span>
								</span>
							</section>
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
									<p>Player list updated {card.pool_last_sync}</p>
								</section>
								<section class="commissioner-hq__detail">
									<h3>NFL DATA</h3>
									<ul class="commissioner-hq__data-list">
										<Each of={card.open_data} as="row"><li><strong>{row.label}</strong><span class="mono">{row.state} · {row.updated}</span></li></Each>
									</ul>
								</section>
								<section class="commissioner-hq__detail">
									<h3>PLAYOFF TRUTH · YEAR ONE PLAYOFFS</h3>
									<p><strong>{card.playoff_status_label}</strong> · <If cond={card.playoff_available}>PUBLISHED</If><If cond={card.playoff_available == false}>NOT PUBLISHED</If></p>
									<p>{card.playoff_note}</p>
									<If cond={card.playoff_source != ""}><p class="mono">SOURCE {card.playoff_source} · NEXT {card.playoff_next_matchups}</p></If>
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
			<If cond={props.HQV1Enabled}>
				<HQV1Portfolio {...props.HQV1}></HQV1Portfolio>
			</If>
		</If>
	</div>
}

func HQV1Portfolio(props hqV1PortfolioProps) Node {
	return <section class="commissioner-hq__v1" aria-labelledby="commissioner-hq-v1-heading">
		<div class="commissioner-hq__subhead">
			<span class="signal-label">PRIVATE HQ V1 · READ ONLY</span>
			<h2 id="commissioner-hq-v1-heading">Operations across the fleet</h2>
			<p>Provider snapshots remain isolated by league. This surface never provisions seats, changes boards, or executes league actions.</p>
			<button type="button" class="board-button" data-gosx-set="$commissioner.hq.refresh" data-gosx-set-value="manual">Refresh operations</button>
		</div>
		<div class="commissioner-hq__v1-summary">
			<span class="mono">{props.Total} LEAGUES · {props.Live} LIVE · {props.Stale} STALE · {props.Warnings} NEED REVIEW</span>
			<span class="mono">GENERATED {props.GeneratedAt}</span>
		</div>
		<If cond={props.Total == 0}>
			<p class="commissioner-hq__empty">No HQ v1 connections are configured on this host.</p>
		</If>
		<div class="commissioner-hq__v1-rows">
			<Each of={props.Rows} as="row">
				<article class="commissioner-hq__v1-row" data-freshness={row.Freshness} data-connection={row.ConnectionResult}>
					<div class="pool-toolbar commissioner-hq__card-header">
						<div>
							<span class="section-index">{row.ShortCode} · {row.LeagueID}</span>
							<h3>{row.Name}</h3>
						</div>
						<span class="position-chip">{row.Freshness} · {row.ConnectionResult}</span>
					</div>
					<If cond={row.Available == false}>
						<p class="error-message">This league snapshot is unavailable ({row.Diagnostic}).</p>
						<p class="scoring-note">Other league rows remain independently readable.</p>
						<a href={row.PublicURL}>Open league directly →</a>
					</If>
					<If cond={row.Available}>
						<div class="commissioner-hq__v1-grid">
							<section class="commissioner-hq__detail">
								<h4>PHASE / DEADLINE</h4>
								<p><strong>{row.Phase}</strong></p>
								<If cond={row.HasDeadline}>
									<p>{row.Deadline} · <span class="mono">{row.DeadlineAt}</span></p>
									<If cond={row.DeadlineHref != ""}><a href={row.DeadlineHref}>Open owning control →</a></If>
								</If>
								<If cond={row.HasDeadline == false}><p class="scoring-note">No upcoming deadline reported.</p></If>
							</section>
							<section class="commissioner-hq__detail">
								<h4>SEATS / READINESS</h4>
								<p><strong>{row.ClaimedSeats} / {row.Seats}</strong> claimed · {row.OpenSeats} open · {row.PendingInvites} pending invites</p>
								<p>{row.ReadyTeams} ready · {row.BoardGaps} board gaps</p>
								<p class="scoring-note">{row.Readiness}</p>
							</section>
							<section class="commissioner-hq__detail">
								<h4>LINEUP / WAIVERS</h4>
								<p><strong>{row.LineupIssues}</strong> lineup issues · next lock <span class="mono">{row.LineupLock}</span></p>
								<p>{row.WaiverMode} · {row.OpenClaims} open claims · next run <span class="mono">{row.WaiverRun}</span></p>
							</section>
							<section class="commissioner-hq__detail">
								<h4>TRADES / PICK'EM</h4>
								<p>{row.TradePending} pending trades · {row.TradeDecisions} commissioner decisions</p>
								<p>WEEK {row.PickemWeek} · {row.PickemUnpicked} unpicked · deadline <span class="mono">{row.PickemDeadline}</span></p>
							</section>
							<section class="commissioner-hq__detail">
								<h4>RELEASE / HEALTH</h4>
								<p class="mono">{row.ReleaseSHA} · {row.ReleaseBuiltAt}</p>
								<p>{row.Quality} · {row.SourceState} · as of <span class="mono">{row.DataAsOf}</span></p>
								<p>Last successful collection <span class="mono">{row.LastSuccess}</span></p>
								<p>Last attempt <span class="mono">{row.LastAttempt}</span> · provider produced <span class="mono">{row.ProviderProduced}</span></p>
							</section>
						</div>
						<nav class="commissioner-hq__links" aria-label="HQ v1 league links">
							<If cond={row.LeagueURL != ""}><a href={row.LeagueURL}>Open league →</a></If>
							<If cond={row.CommissionerURL != ""}><a href={row.CommissionerURL}>Commissioner controls →</a></If>
						</nav>
					</If>
				</article>
			</Each>
		</div>
		<section class="commissioner-hq__queue" aria-labelledby="commissioner-hq-v1-attention-heading">
			<div class="commissioner-hq__subhead"><h3 id="commissioner-hq-v1-attention-heading">Fleet attention</h3></div>
			<Each of={props.Attention} as="item">
				<article class="commissioner-hq__attention" data-severity={item.Severity}>
					<div class="commissioner-hq__attention-copy"><span class="section-index">{item.Severity} · {item.League}</span><strong>{item.Title}</strong><span>{item.Summary}</span><span class="mono">due {item.Due}</span></div>
					<If cond={item.HasHref}><a href={item.Href}>Open owning league →</a></If>
				</article>
			</Each>
		</section>
		<section class="commissioner-hq__queue" aria-labelledby="commissioner-hq-v1-deadlines-heading">
			<div class="commissioner-hq__subhead"><h3 id="commissioner-hq-v1-deadlines-heading">Upcoming deadlines</h3></div>
			<Each of={props.Deadlines} as="item">
				<article class="commissioner-hq__attention"><div class="commissioner-hq__attention-copy"><span class="section-index">{item.Category} · {item.League}</span><strong>{item.Title}</strong><span class="mono">{item.When} · {item.Relative}</span></div><If cond={item.HasHref}><a href={item.Href}>Open →</a></If></article>
			</Each>
		</section>
		<section class="commissioner-hq__queue" aria-labelledby="commissioner-hq-v1-activity-heading">
			<div class="commissioner-hq__subhead"><h3 id="commissioner-hq-v1-activity-heading">Recent activity</h3></div>
			<Each of={props.Activity} as="item"><article class="commissioner-hq__attention"><div class="commissioner-hq__attention-copy"><span class="section-index">{item.Category} · {item.League}</span><strong>{item.Summary}</strong><span class="mono">{item.When}</span></div><If cond={item.HasHref}><a href={item.Href}>Open →</a></If></article></Each>
		</section>
	</section>
}

func Page() Node {
	return <main class="page admin-page commissioner-hq" id="main-content">
		<If cond={data.is_commissioner == false}>
			<FleetReadout {...data.fleet}></FleetReadout>
		</If>
		<If cond={data.is_commissioner}>
			<div class="commissioner-hq__fleet-region" data-gosx-region data-gosx-region-url="/commissioner/fragment" data-gosx-region-interval="15s" data-gosx-region-signal="$commissioner.hq.refresh">
				<FleetReadout {...data.fleet}></FleetReadout>
			</div>
		</If>
	</main>
}
