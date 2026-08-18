package activity

func Page() Node {
	return <main class="page board-page" id="main-content" data-gosx-revalidate-interval="4s" data-gosx-revalidate-src="/api/league/version">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					TRANSACTION FEED
				</span>
				<h1>
					EVERY MOVE
					<br></br>
					ON THE RECORD.
				</h1>
				<p>
					Draft picks and every free-agent signing or drop, merged into one feed, newest first.
				</p>
			</div>
			<div class="draft-clock-panel">
				<span>Recorded moves</span>
				<strong class="mono">{data.transactions_count}</strong>
				<div class="draft-clock-meta">
					<a href="/players" data-gosx-link>Player pool →</a>
					<a href="/team" data-gosx-link>Team terminal →</a>
				</div>
			</div>
		</section>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">01 // LEAGUE WIRE</span>
					<h2>Every transaction</h2>
				</div>
			</div>
			<If cond={data.transactions_empty}>
				<div class="empty-tape">
					<strong>NO TRANSACTIONS YET</strong>
					<p>
						Draft picks and roster moves appear here as they happen.
					</p>
				</div>
			</If>
			<div class="activity-feed">
				<Each of={data.transactions} as="move">
					<div class="activity-item">
						<time class="mono">{move.Time}</time>
						<p>
							<strong>{move.Team}</strong>
							{move.Action}
							<b>{move.Player}</b>
						</p>
					</div>
				</Each>
			</div>
		</section>
	</main>
}
