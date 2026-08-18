package blitz

func BlitzSlotRow(props any) Node {
	return <article class="board-row" data-picked={props.Slot.locked}>
		<div class="pool-player pool-player--photo stat-tip" tabindex="0">
			<If cond={props.Slot.has_headshot}>
				<img class="player-headshot" src={props.Slot.headshot} alt="" loading="lazy" />
			</If>
			<div class="pool-player__text">
				<strong>{props.Slot.name}</strong>
				<small>{props.Slot.detail}</small>
			</div>
			<div class="stat-tip__panel" role="tooltip" aria-hidden="true">
				<div class="stat-tip__head">
					<strong>{props.Slot.name}</strong>
					<span class="mono">{props.Slot.jersey}</span>
					<span class="mono stat-tip__team">{props.Slot.nfl_team}</span>
				</div>
				<If cond={props.Slot.has_breakdown}>
					<div class="stat-tip__rows">
						<Each of={props.Slot.breakdown} as="row">
							<div class="stat-tip__row" data-scored={row.scored}>
								<span>{row.label}</span>
								<span class="mono">{row.calc}</span>
								<b class="mono">{row.points}</b>
							</div>
						</Each>
						<div class="stat-tip__total">
							<span>Live score</span>
							<b class="mono">{props.Slot.breakdown_total}</b>
						</div>
					</div>
				</If>
				<If cond={props.Slot.has_breakdown == false}>
					<p class="stat-tip__empty">No live stats yet — this player has not kicked off.</p>
				</If>
			</div>
		</div>
		<span class="position-chip">{props.Slot.position}</span>
		<b class="mono">{props.Slot.points}</b>
		<div class="board-controls">
			<If cond={props.Slot.locked}>
				<b class="mono">LOCKED</b>
			</If>
			<If cond={props.Slot.locked == false}>
				<form method="post" action={props.RemoveAction} data-gosx-managed="true">
					<input type="hidden" name="csrf_token" value={props.CSRF}></input>
					<input type="hidden" name="slate" value={props.Slate}></input>
					<input type="hidden" name="player_id" value={props.Slot.id}></input>
					<button class="board-button board-button--cut" type="submit" aria-label={"Remove " + props.Slot.name}>✕</button>
				</form>
			</If>
		</div>
	</article>
}

func BlitzPoolRow(props any) Node {
	return <article class="pool-row" data-player-position={props.Player.position} data-gosx-filter-text={props.Player.search}>
		<span class="pool-rank mono">{props.Player.rank}</span>
		<div class="pool-player pool-player--photo stat-tip" tabindex="0">
			<If cond={props.Player.has_headshot}>
				<img class="player-headshot" src={props.Player.headshot} alt="" loading="lazy" />
			</If>
			<div class="pool-player__text">
				<strong>{props.Player.name}</strong>
				<If cond={props.Player.is_rookie}>
					<span class="badge-rookie">ROOKIE</span>
				</If>
				<If cond={props.Player.resting}>
					<span class="tag-resting">LIKELY TO REST</span>
				</If>
				<small>{props.Player.detail}</small>
				<small class="pre1-line" data-has-pre1={props.Player.has_pre1}>{props.Player.pre1_summary}</small>
				<If cond={props.Player.has_opponent}>
					<small class="mono">
						{props.Player.opponent}
						<If cond={props.Player.has_matchup}>
							·
							<span class="matchup-chip" data-matchup-tier={props.Player.matchup_tier}>{props.Player.matchup_chip}</span>
						</If>
					</small>
				</If>
			</div>
			<div class="stat-tip__panel" role="tooltip" aria-hidden="true">
				<div class="stat-tip__head">
					<strong>{props.Player.name}</strong>
					<span class="mono">{props.Player.jersey}</span>
					<span class="mono stat-tip__team">{props.Player.nfl_team}</span>
				</div>
				<If cond={props.Player.has_breakdown}>
					<div class="stat-tip__rows">
						<Each of={props.Player.breakdown} as="row">
							<div class="stat-tip__row" data-scored={row.scored}>
								<span>{row.label}</span>
								<span class="mono">{row.calc}</span>
								<b class="mono">{row.points}</b>
							</div>
						</Each>
						<div class="stat-tip__total">
							<span>Projection</span>
							<b class="mono">{props.Player.breakdown_total}</b>
						</div>
					</div>
				</If>
				<If cond={props.Player.has_breakdown == false}>
					<p class="stat-tip__empty">No projection detail for this position.</p>
				</If>
				<If cond={props.Player.has_matchup}>
					<p class="stat-tip__hist mono">{props.Player.matchup_detail}</p>
				</If>
			</div>
		</div>
		<span class="position-chip">{props.Player.position}</span>
		<b class="mono">{props.Player.projection}</b>
		<form method="post" action={props.AddAction} data-gosx-managed="true">
			<input type="hidden" name="csrf_token" value={props.CSRF}></input>
			<input type="hidden" name="slate" value={props.Slate}></input>
			<input type="hidden" name="player_id" value={props.Player.id}></input>
			<If cond={props.Open}>
				<button class="draft-button" type="submit">Add</button>
			</If>
			<If cond={props.Open == false}>
				<button class="draft-button" type="button" disabled="disabled">Locked</button>
			</If>
		</form>
	</article>
}

func BlitzGameRow(props any) Node {
	return <article class="pickem-row">
		<small class="mono">{props.Game.kickoff_display}</small>
		<strong>{props.Game.label}</strong>
		<div class="pickem-status">
			<b class="mono">{props.Game.status}</b>
		</div>
	</article>
}

func BlitzLeaderRow(props any) Node {
	return <article class="board-row">
		<span class="pool-rank mono">{props.Entry.rank}</span>
		<div class="pool-player">
			<strong>{props.Entry.name}</strong>
			<span class="position-chip">{props.Entry.team}</span>
		</div>
		<b class="mono">{props.Entry.total}</b>
		<div class="blitz-chip-row">
			<Each of={props.Entry.players} as="chip">
				<div class="pool-player stat-tip" tabindex="0">
					<If cond={chip.revealed}>
						<small class="mono">{chip.position}</small>
						<span>{chip.name}</span>
					</If>
					<If cond={chip.revealed == false}>
						<small class="mono">— · —</small>
						<span>Hidden</span>
					</If>
					<b class="mono">{chip.points}</b>
					<div class="stat-tip__panel" role="tooltip" aria-hidden="true">
						<div class="stat-tip__head">
							<strong>{chip.name}</strong>
							<span class="mono stat-tip__team">{chip.team}</span>
						</div>
						<If cond={chip.has_breakdown}>
							<div class="stat-tip__rows">
								<Each of={chip.breakdown} as="row">
									<div class="stat-tip__row" data-scored={row.scored}>
										<span>{row.label}</span>
										<span class="mono">{row.calc}</span>
										<b class="mono">{row.points}</b>
									</div>
								</Each>
								<div class="stat-tip__total">
									<span>Live score</span>
									<b class="mono">{chip.breakdown_total}</b>
								</div>
							</div>
						</If>
						<If cond={chip.has_breakdown == false}>
							<p class="stat-tip__empty">Locks at this player's kickoff.</p>
						</If>
					</div>
				</div>
			</Each>
		</div>
	</article>
}

func BlitzArchiveRow(props any) Node {
	return <article class="board-row">
		<span class="pool-rank mono">{props.Entry.rank}</span>
		<div class="pool-player">
			<strong>{props.Entry.name}</strong>
			<span class="position-chip">{props.Entry.team}</span>
		</div>
		<b class="mono">{props.Entry.total}</b>
	</article>
}

func Page() Node {
	return <main class="page blitz-page" id="main-content" data-gosx-revalidate-interval="4s" data-gosx-revalidate-src="/api/league/version">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					PRESEASON BLITZ //
					{data.slate_label}
				</span>
				<h1>
					FIVE PICKS.
					<br></br>
					TWO WEEKS.
				</h1>
				<p>
					Play money. Bragging rights. Nothing else.
				</p>
			</div>
			<div class="draft-clock-panel">
				<span>Entries this slate</span>
				<strong class="mono">{data.entry_count}</strong>
				<div class="draft-clock-meta">
					<a href={"/blitz?slate=" + data.other_slate} data-gosx-link>
						{data.other_slate_label}
						→
					</a>
				</div>
			</div>
		</section>
		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_notice}>
				<p class="flash-message">{data.notice}</p>
			</If>
			<If cond={data.has_blitz_error}>
				<p class="error-message">{data.blitz_error}</p>
			</If>
			<If cond={data.can_enter == false}>
				<p class="demo-message">
					<strong>SIGN IN REQUIRED:</strong>
					use League access to enter the blitz tied to your seat.
				</p>
			</If>
			<If cond={data.feed_offline}>
				<p class="demo-message">
					<strong>FEED OFFLINE:</strong>
					the live preseason feed is not configured on this run. Schedules and live scores stay empty until it is.
				</p>
			</If>
			<If cond={data.slate_closed}>
				<p class="demo-message">
					<strong>SLATE CLOSED:</strong>
					every game in this slate is final. The leaderboard is frozen.
				</p>
			</If>
			<If cond={data.has_matchup_source}>
				<p class="demo-message">
					<strong>MATCHUP RANKS:</strong>
					ranked from the {data.matchup_source_label} — regular-season data used as a proxy for this preseason matchup. A higher "-toughest" number is a softer matchup; a lower one is tougher.
				</p>
			</If>
		</div>

		<If cond={data.archived == false}>
			<section class="player-pool">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">01 // ENTRY BUILDER</span>
						<h2>Your five</h2>
					</div>
				</div>
				<p class="stat-tip__empty">
					Starters may not play. Pool players only — practice-squad heroes are not selectable.
				</p>
				<If cond={data.slots_empty}>
					<div class="empty-tape">
						<strong>NO PLAYERS ENTERED</strong>
						<p>Add up to 5 players from the eligible list below. Max 2 per NFL team.</p>
					</div>
				</If>
				<div class="pool-list">
					<Each of={data.slots} as="slot">
						<BlitzSlotRow
							Slot={slot}
							RemoveAction={actionPath("blitz-remove")}
							CSRF={csrf.token}
							Slate={data.slate}
						 />
					</Each>
				</div>
				<div class="pool-toolbar">
					<div>
						<span class="section-index">ELIGIBLE PLAYERS</span>
						<h2>Add to your entry</h2>
					</div>
				</div>
				<div class="pool-search-bar">
					<label class="mono" for="blitz-search">QUERY //</label>
					<input
						id="blitz-search"
						type="search"
						data-gosx-filter="blitz-pool-rows"
						placeholder="Search player, team, or position"
						autocomplete="off"
					 />
				</div>
				<If cond={data.eligible_empty}>
					<div class="empty-tape">
						<strong>NO ELIGIBLE PLAYERS YET</strong>
						<p>The slate schedule has not synced. Check back once the feed is online.</p>
					</div>
				</If>
				<div class="pool-list pool-list--tall" id="blitz-pool-rows">
					<Each of={data.eligible} as="player">
						<BlitzPoolRow
							Player={player}
							AddAction={actionPath("blitz-add")}
							CSRF={csrf.token}
							Slate={data.slate}
							Open={data.entry_open}
						 />
					</Each>
				</div>
			</section>

			<section class="player-pool">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">02 // SLATE SCHEDULE</span>
						<h2>{data.slate_label}</h2>
					</div>
				</div>
				<If cond={data.games_empty}>
					<div class="empty-tape">
						<strong>NO GAMES YET</strong>
						<p>The slate schedule syncs from the live preseason feed.</p>
					</div>
				</If>
				<div class="pool-list">
					<Each of={data.games} as="game">
						<BlitzGameRow Game={game} />
					</Each>
				</div>
			</section>
		</If>

		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">03 // LEADERBOARD</span>
					<h2>{data.slate_label}</h2>
				</div>
			</div>
			<If cond={data.leaderboard_empty}>
				<div class="empty-tape">
					<strong>NO ENTRIES YET</strong>
					<p>The leaderboard fills in as members build their five.</p>
				</div>
			</If>
			<div class="pool-list">
				<Each of={data.leaderboard} as="entry">
					<BlitzLeaderRow Entry={entry} />
				</Each>
			</div>
		</section>

		<If cond={data.archived}>
			<section class="player-pool">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">ARCHIVE // THE BLITZ IS OVER</span>
						<h2>Final standings</h2>
					</div>
				</div>
				<div class="empty-tape">
					<strong>OVERALL CHAMPION: {data.archive.overall_champion}</strong>
					<p>
						Preseason Week 2 champion: {data.archive.pre2_champion}. Preseason Week 3 champion: {data.archive.pre3_champion}.
					</p>
				</div>
				<h3>Preseason Week 2 — final</h3>
				<If cond={data.archive.pre2_leaderboard_empty}>
					<p class="stat-tip__empty">No entries were made for this slate.</p>
				</If>
				<div class="pool-list">
					<Each of={data.archive.pre2_leaderboard} as="entry">
						<BlitzArchiveRow Entry={entry} />
					</Each>
				</div>
				<h3>Preseason Week 3 — final</h3>
				<If cond={data.archive.pre3_leaderboard_empty}>
					<p class="stat-tip__empty">No entries were made for this slate.</p>
				</If>
				<div class="pool-list">
					<Each of={data.archive.pre3_leaderboard} as="entry">
						<BlitzArchiveRow Entry={entry} />
					</Each>
				</div>
			</section>
		</If>
	</main>
}
