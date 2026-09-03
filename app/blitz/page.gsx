package blitz

type BlitzSlotRowProps struct {
	Slot         map[string]any
	RemoveAction string
	CSRF         string
	Slate        string
	ReturnTargetField string
	ReturnTarget      string
}

func BlitzSlotRow(props BlitzSlotRowProps) Node {
	return <article class="board-row" data-picked={props.Slot.locked}>
		<details class="stat-tip">
			<summary class="pool-player pool-player--photo stat-tip__summary">
			<If cond={props.Slot.has_headshot}>
				<img class="player-headshot" src={props.Slot.headshot} alt="" loading="lazy" />
			</If>
			<span class="pool-player__text">
				<strong>{props.Slot.name}</strong>
				<small>{props.Slot.detail}</small>
			</span>
			</summary>
			<div class="stat-tip__panel">
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
							<span>Recorded score</span>
							<b class="mono">{props.Slot.breakdown_total}</b>
						</div>
					</div>
				</If>
				<If cond={props.Slot.has_breakdown == false}>
					<p class="stat-tip__empty">No recorded scoring stats yet.</p>
				</If>
			</div>
		</details>
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
					<input type="hidden" name={props.ReturnTargetField} value={props.ReturnTarget}></input>
					<button class="board-button board-button--cut" type="submit" aria-label={"Remove " + props.Slot.name}>✕</button>
				</form>
			</If>
		</div>
	</article>
}

type BlitzPoolRowProps struct {
	Player   map[string]any
	AddAction string
	CSRF     string
	Slate    string
	Open     bool
	ReturnTargetField string
	ReturnTarget      string
}

func BlitzPoolRow(props BlitzPoolRowProps) Node {
	return <article class="pool-row" data-player-position={props.Player.position} data-gosx-filter-text={props.Player.search}>
		<span class="pool-rank mono">{props.Player.rank}</span>
		<details class="stat-tip">
			<summary class="pool-player pool-player--photo stat-tip__summary">
			<If cond={props.Player.has_headshot}>
				<img class="player-headshot" src={props.Player.headshot} alt="" loading="lazy" />
			</If>
			<span class="pool-player__text">
				<strong>{props.Player.name}</strong>
				<If cond={props.Player.is_rookie}>
					<span class="badge-rookie">ROOKIE</span>
				</If>
				<If cond={props.Player.resting}>
					<span class="tag-resting">LIKELY TO REST</span>
				</If>
				<If cond={props.Player.locked}>
					<span class="tag-locked">{props.Player.lock_label}</span>
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
			</span>
			</summary>
			<div class="stat-tip__panel">
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
		</details>
		<span class="position-chip">{props.Player.position}</span>
		<b class="mono">{props.Player.projection}</b>
		<form method="post" action={props.AddAction} data-gosx-managed="true">
			<input type="hidden" name="csrf_token" value={props.CSRF}></input>
			<input type="hidden" name="slate" value={props.Slate}></input>
			<input type="hidden" name="player_id" value={props.Player.id}></input>
			<input type="hidden" name={props.ReturnTargetField} value={props.ReturnTarget}></input>
			<If cond={props.Open}>
				<button class="draft-button" type="submit">Add</button>
			</If>
			<If cond={props.Open == false}>
				<button class="draft-button" type="button" disabled="disabled">Locked</button>
			</If>
		</form>
	</article>
}

type BlitzGameRowProps struct {
	Game map[string]any
}

func BlitzGameRow(props BlitzGameRowProps) Node {
	return <article class="pickem-row">
		<small class="mono">{props.Game.kickoff_display}</small>
		<strong>{props.Game.label}</strong>
		<div class="pickem-status">
			<b class="mono">{props.Game.status}</b>
		</div>
	</article>
}

type BlitzLeaderRowProps struct {
	Entry map[string]any
}

func BlitzLeaderRow(props BlitzLeaderRowProps) Node {
	return <article class="board-row">
		<span class="pool-rank mono">{props.Entry.rank}</span>
		<div class="pool-player">
			<strong>{props.Entry.name}</strong>
			<span class="position-chip">{props.Entry.team}</span>
		</div>
		<b class="mono">{props.Entry.total}</b>
		<div class="blitz-chip-row">
			<Each of={props.Entry.players} as="chip">
				<details class="stat-tip">
					<summary class="pool-player stat-tip__summary">
					<If cond={chip.revealed}>
						<small class="mono">{chip.position}</small>
						<span>{chip.name}</span>
					</If>
					<If cond={chip.revealed == false}>
						<small class="mono">— · —</small>
						<span>Hidden</span>
					</If>
					<b class="mono">{chip.points}</b>
					</summary>
					<div class="stat-tip__panel">
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
									<span>Recorded score</span>
									<b class="mono">{chip.breakdown_total}</b>
								</div>
							</div>
						</If>
						<If cond={chip.has_breakdown == false}>
							<p class="stat-tip__empty">Locks at this player's kickoff.</p>
						</If>
					</div>
				</details>
			</Each>
		</div>
	</article>
}

type BlitzArchiveRowProps struct {
	Entry map[string]any
}

func BlitzArchiveRow(props BlitzArchiveRowProps) Node {
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
					<span class="signal-mark" aria-hidden="true"></span>
					PRESEASON BLITZ //
					{data.slate_label}
				</span>
				<h1>Preseason Blitz</h1>
				<p class="page-subhead">Five picks. Two weeks.</p>
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
					<strong>{data.public_entry.state_label}:</strong>
					{data.public_entry.detail}
					<a class="filter-button" href={data.public_entry.action_href} data-gosx-link>{data.public_entry.action_label}</a>
				</p>
			</If>
			<If cond={data.feed_offline}>
				<p class="demo-message">
					<strong>PRESEASON SCORES NOT OPEN:</strong>
					live preseason scores are not available yet. Schedules and live scores stay empty until it is.
				</p>
			</If>
			<If cond={data.blitz_loading}>
				<p class="demo-message">
					<strong>PRESEASON SOURCE CHECKING:</strong>
					we're checking the preseason feed now. Empty schedules and zero scores are not verified yet.
				</p>
			</If>
			<If cond={data.blitz_recovery}>
				<p class="demo-message">
					<strong>PRESEASON SOURCE DEGRADED:</strong>
					retained scores stay visible as of {data.blitz_as_of}; terminal copy stays provisional until the source confirms complete, final inputs.
				</p>
			</If>
			<If cond={data.archive_blocked}>
				<p class="demo-message">
					<strong>ARCHIVE VERIFICATION PENDING:</strong>
					the contest clock has elapsed, but final standings wait for complete, final source data. We will retry automatically.
				</p>
			</If>
			<If cond={data.pre1_partial}>
				<p class="demo-message">
					<strong>WEEK 1 EVIDENCE PARTIAL:</strong>
					available preseason-week-1 lines are provisional; players without a fetched line are not confirmed to have no snaps.
				</p>
			</If>
			<If cond={data.slate_closed}>
				<p class="demo-message">
					<strong>SLATE CLOSED:</strong>
					every game in this slate is final. The leaderboard is frozen.
				</p>
			</If>
			<If cond={data.has_locked_eligible}>
				<p class="demo-message">
					<strong>KICKOFF LOCK:</strong>
					{data.locked_eligible_label} below are locked. Their games have started, so you can no longer add them. They stay listed with their week 1 lines.
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
			<section class="player-pool" id="blitz-entry">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">01 // ENTRY BUILDER</span>
						<If cond={data.can_enter}>
							<h2>Your five</h2>
						</If>
						<If cond={data.can_enter == false}>
							<h2>Seat entry locked</h2>
						</If>
					</div>
				</div>
				<p class="stat-tip__empty">
					Starters may not play. Pool players only — practice-squad heroes are not selectable.
				</p>
				<If cond={data.slots_empty}>
					<If cond={data.can_enter}>
						<div class="empty-tape">
							<strong>NO PLAYERS ENTERED</strong>
							<p>Add up to 5 players from the eligible list below. Max 2 per NFL team.</p>
						</div>
					</If>
					<If cond={data.can_enter == false}>
						<div class="empty-tape">
							<strong>BROWSE THE ELIGIBLE POOL</strong>
							<p>Eligible players remain visible below. Entry controls unlock only for an identity that manages a franchise seat.</p>
						</div>
					</If>
				</If>
				<div class="pool-list">
					<Each of={data.slots} as="slot">
						<BlitzSlotRow
							Slot={slot}
							RemoveAction={actionPath("blitz-remove")}
							CSRF={csrf.token}
							Slate={data.slate}
							ReturnTargetField={data.blitz_return_target_field}
							ReturnTarget={data.blitz_return_target}
						 />
					</Each>
				</div>
				<div class="pool-toolbar">
					<div>
						<span class="section-index">ELIGIBLE PLAYERS</span>
						<If cond={data.can_enter}>
							<h2>Add to your entry</h2>
						</If>
						<If cond={data.can_enter == false}>
							<h2>Browse eligible players</h2>
						</If>
					</div>
				</div>
				<div class="pool-search-bar">
					<label class="mono" for="blitz-search">SEARCH //</label>
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
						<If cond={data.blitz_unknown}>
							<strong>WAITING FOR A VERIFIED SLATE</strong>
							<p>The source has not confirmed this slate yet. We will retry automatically.</p>
						</If>
						<If cond={data.blitz_unknown == false}>
							<strong>NO ELIGIBLE PLAYERS YET</strong>
							<p>The preseason slate is not published yet.</p>
						</If>
					</div>
				</If>
				<div class="pool-list pool-list--tall" id="blitz-pool-rows">
					<Each of={data.eligible} as="player">
						<BlitzPoolRow
							Player={player}
							AddAction={actionPath("blitz-add")}
							CSRF={csrf.token}
							Slate={data.slate}
							Open={player.can_add}
							ReturnTargetField={data.blitz_return_target_field}
							ReturnTarget={data.blitz_return_target}
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
						<If cond={data.blitz_unknown}>
							<strong>SCHEDULE NOT VERIFIED</strong>
							<p>The source has not confirmed zero games. We will retry automatically.</p>
						</If>
						<If cond={data.blitz_unknown == false}>
							<strong>NO GAMES YET</strong>
							<p>The preseason slate is not published yet.</p>
						</If>
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
					<If cond={data.archive.overall_champion != ""}>
						<strong>OVERALL CHAMPION: {data.archive.overall_champion}</strong>
					</If>
					<If cond={data.archive.overall_champion == ""}>
						<strong>OVERALL CHAMPION: no entries were scored</strong>
					</If>
					<p>
						<If cond={data.archive.pre2_champion != ""}>Preseason Week 2 champion: {data.archive.pre2_champion}. </If>
						<If cond={data.archive.pre2_champion == ""}>Preseason Week 2 — no champion recorded. </If>
						<If cond={data.archive.pre3_champion != ""}>Preseason Week 3 champion: {data.archive.pre3_champion}.</If>
						<If cond={data.archive.pre3_champion == ""}>Preseason Week 3 — no champion recorded.</If>
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
