package activity

func Page() Node {
	return <main class="page board-page" id="main-content" data-gosx-revalidate-interval="4s" data-gosx-revalidate-src="/api/league/version">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="signal-mark" aria-hidden="true"></span>
					TRANSACTION FEED
				</span>
				<h1>
					EVERY MOVE
					<br></br>
					ON THE RECORD.
				</h1>
				<p>
					Draft picks, waiver and free-agent moves, and trades — one permanent league record, newest first.
				</p>
			</div>
			<div class="draft-clock-panel">
				<span>Recorded moves</span>
				<strong class="mono">{data.transactions_count}</strong>
				<div class="draft-clock-meta">
					<span class="mono">League time · {data.timezone}</span>
					<a href="/players" data-gosx-link>Player pool →</a>
					<a href="/team" data-gosx-link>Team terminal →</a>
				</div>
			</div>
		</section>
		<section class="score-command playoff-truth-card" aria-labelledby="activity-playoff-truth-heading">
			<header class="section-heading section-heading--split"><div><span class="section-index">POSTSEASON // ACTIVITY CONTEXT</span><h2 id="activity-playoff-truth-heading">{data.playoff_truth.headline}</h2></div><span class="position-chip">{data.playoff_truth.status_label}</span></header>
			<p>{data.playoff_truth.detail}</p>
			<If cond={data.playoff_truth.recovery != ""}><p class="scoring-note"><strong>RECOVERY:</strong> {data.playoff_truth.recovery}</p></If>
			<a href="/matchups" data-gosx-link class="access-link">Open persisted bracket truth →</a>
		</section>
		<div
			id="activity-feed-region"
			data-gosx-region
			data-gosx-region-url={data.activity_fragment_url}
			data-gosx-region-interval={data.activity_fragment_interval}
			data-gosx-region-signal="$players.state.refresh"
			aria-label="Authoritative transaction Activity"
		>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">01 // LEAGUE WIRE</span>
					<h2>Every transaction</h2>
				</div>
			</div>
			<form method="get" action="/activity" class="pool-search-bar">
				<label class="mono" for="activity-team">TEAM //</label>
				<select id="activity-team" name="team">
					<option value="">All teams</option>
					<Each of={data.teams} as="team">
						<option value={team} selected={team == data.team}>{team}</option>
					</Each>
				</select>
				<label class="mono" for="activity-search">SEARCH //</label>
				<input id="activity-search" type="search" name="q" value={data.query} placeholder="Player, move, or team" autocomplete="off"></input>
				<button class="filter-button" type="submit">Filter</button>
				<If cond={data.has_filters}>
					<a class="filter-button" href="/activity" data-gosx-link>Clear</a>
				</If>
			</form>
			<If cond={data.filtered_count > 0}>
				<p class="scoring-note" aria-live="polite">
					Showing {data.page_start}–{data.page_end} of {data.filtered_count} matching moves · {data.transactions_count} recorded overall
				</p>
			</If>
			<If cond={data.has_transactions == false}>
				<div class="empty-tape">
					<strong>NO TRANSACTIONS YET</strong>
					<p>
						Draft picks and roster moves appear here as they happen.
					</p>
				</div>
			</If>
			<If cond={data.has_transactions && data.transactions_empty}>
				<div class="empty-tape">
					<strong>NO MOVES MATCH</strong>
					<p>
						Try another team or query, or clear the filters to return to the full league record.
					</p>
					<a class="filter-button" href="/activity" data-gosx-link>Clear filters</a>
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
			<nav class="pool-pagination" aria-label="Transaction feed pages">
				<If cond={data.has_previous}>
					<a class="filter-button" href={data.previous_href} data-gosx-link rel="prev">← Previous</a>
				</If>
				<span class="mono">Page {data.page} / {data.pages}</span>
				<If cond={data.has_next}>
					<a class="filter-button" href={data.next_href} data-gosx-link rel="next">Next →</a>
				</If>
			</nav>
		</section>
		</div>
		<p class="scoring-note lineup-sync-note" role="status" aria-live="polite">
			Activity refreshes automatically within 4 seconds after a recorded add/drop result.
			If a refresh fails, use
			<button type="button" class="board-button" data-gosx-set="$players.state.refresh" data-gosx-set-value="manual">Refresh Activity now</button>.
		</p>
	</main>
}

// ActivityRegion is the server-rendered body of the Activity feed region.
// Filter, query, and page values arrive in the fragment URL, so convergence
// never resets a manager's current browse state or active input.
func ActivityRegion() Node {
	return <section class="player-pool">
		<div class="pool-toolbar"><div><span class="section-index">01 // LEAGUE WIRE</span><h2>Every transaction</h2></div></div>
		<form method="get" action="/activity" class="pool-search-bar">
			<label class="mono" for="activity-sync-team">TEAM //</label>
			<select id="activity-sync-team" name="team"><option value="">All teams</option><Each of={data.teams} as="team"><option value={team} selected={team == data.team}>{team}</option></Each></select>
			<label class="mono" for="activity-sync-search">SEARCH //</label>
			<input id="activity-sync-search" type="search" name="q" value={data.query} placeholder="Player, move, or team" autocomplete="off"></input>
			<If cond={data.page > 1}><input type="hidden" name="page" value={data.page}></input></If>
			<button class="filter-button" type="submit">Filter</button>
			<If cond={data.has_filters}><a class="filter-button" href="/activity" data-gosx-link>Clear</a></If>
		</form>
		<If cond={data.filtered_count > 0}><p class="scoring-note" aria-live="polite">Showing {data.page_start}–{data.page_end} of {data.filtered_count} matching moves · {data.transactions_count} recorded overall</p></If>
		<If cond={data.has_transactions == false}><div class="empty-tape"><strong>NO TRANSACTIONS YET</strong><p>Draft picks and roster moves appear here as they happen.</p></div></If>
		<If cond={data.has_transactions && data.transactions_empty}><div class="empty-tape"><strong>NO MOVES MATCH</strong><p>Try another team or query, or clear the filters.</p><a class="filter-button" href="/activity" data-gosx-link>Clear filters</a></div></If>
		<div class="activity-feed"><Each of={data.transactions} as="move"><div class="activity-item"><time class="mono">{move.Time}</time><p><strong>{move.Team}</strong>{move.Action}<b>{move.Player}</b></p></div></Each></div>
		<nav class="pool-pagination" aria-label="Transaction feed pages"><If cond={data.has_previous}><a class="filter-button" href={data.previous_href} data-gosx-link rel="prev">← Previous</a></If><span class="mono">Page {data.page} / {data.pages}</span><If cond={data.has_next}><a class="filter-button" href={data.next_href} data-gosx-link rel="next">Next →</a></If></nav>
	</section>
}
