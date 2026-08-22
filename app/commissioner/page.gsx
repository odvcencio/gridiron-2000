package commissioner

func Page() Node {
	return <main class="page admin-page" id="main-content">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label"><span class="live-dot" aria-hidden="true"></span>COMMISSIONER HQ</span>
				<h1>EVERY LEAGUE.<br></br>ONE READOUT.</h1>
				<p>Fleet visibility without merging databases, sessions, or draft controls. Open the owning league when you need to act.</p>
			</div>
			<div class="draft-clock-panel">
				<span>Fleet status</span>
				<strong class="mono">{data.claimed_seats} / {data.total_seats} SEATS</strong>
				<div class="draft-clock-meta">
					<span>{data.league_count} LEAGUES</span>
					<span>{data.drafts_live} LIVE · {data.attention_count} FLAGS</span>
				</div>
			</div>
		</section>
		<If cond={data.is_commissioner == false}>
			<section class="player-pool"><div class="empty-tape">
				<strong>RESTRICTED</strong>
				<p>Fleet status is limited to the commissioner configured on this league.</p>
				<a href="/" data-gosx-link>Back to league home →</a>
			</div></section>
		</If>
		<If cond={data.is_commissioner}>
			<If cond={data.federation_enabled == false}>
				<p class="demo-message"><strong>LOCAL-ONLY:</strong> add configured commissioner peers to place other isolated leagues beside this one.</p>
			</If>
			<div class="admin-grid">
				<Each of={data.cards} as="card">
					<section class="player-pool">
						<If cond={card.available == false}>
							<div class="pool-toolbar"><div><span class="section-index">{card.peer_id}</span><h2>League unavailable</h2></div></div>
							<p class="error-message">{card.error}</p>
							<p class="scoring-note">The other leagues remain independent. Retry later or open that league directly.</p>
							<div class="draft-clock-meta">
								<a href={card.public_url}>Open {card.peer_id} directly →</a>
							</div>
						</If>
						<If cond={card.available}>
							<div class="pool-toolbar"><div>
								<span class="section-index">{card.short_code} // {card.mode}</span>
								<h2>{card.name}</h2>
							</div><span class="position-chip">{card.draft_status}</span></div>
							<div class="pool-stats">
								<div class="pool-stat"><span>SEATS · {card.ready_seats} READY</span><b>{card.claimed_seats} / {card.seats}</b></div>
								<div class="pool-stat"><span>DRAFT · {card.draft_at}</span><b>{card.picks} / {card.draft_slots}</b></div>
								<div class="pool-stat"><span>POOL · {card.pool_players} PLAYERS · {card.pool_cushion} CUSHION</span><b>{card.pool_coverage}</b></div>
								<div class="pool-stat"><span>RUNTIME · {card.version} · {card.git_sha}</span><b>{card.pool_mode}</b></div>
							</div>
							<If cond={card.has_attention}>
								<div class="notice-stack" aria-label="Commissioner attention">
									<Each of={card.attention} as="item"><p class="demo-message"><strong>{item.severity}:</strong> {item.message}</p></Each>
								</div>
							</If>
							<div class="draft-clock-meta">
								<a href={card.home_url}>Open league →</a>
								<a href={card.admin_url}>Open admin →</a>
								<a href={card.draft_url}>Open draft →</a>
							</div>
						</If>
					</section>
				</Each>
			</div>
		</If>
	</main>
}
