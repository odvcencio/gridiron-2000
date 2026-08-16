package scoring

func ScoringRow(props any) Node {
	return <div class="scoring-row">
		<div class="scoring-row__label">
			<strong>{props.rule.label}</strong>
			<If cond={props.rule.is_default == false}>
				<span class="position-chip">CUSTOM</span>
			</If>
		</div>
		<If cond={props.Editable == false}>
			<b class="mono">{props.rule.points}</b>
		</If>
		<If cond={props.Editable}>
			<form class="scoring-row__form" method="post" action={props.SetAction} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="key" value={props.rule.key}></input>
				<input type="text" name="points" value={props.rule.points} class="scoring-input"></input>
				<button class="board-button" type="submit">Set</button>
			</form>
		</If>
	</div>
}

func Page() Node {
	return <main class="page scoring-page" id="main-content">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					SCORING SYSTEM //
					{data.league_mode}
				</span>
				<h1>
					HOW WE
					<br></br>
					SCORE.
				</h1>
				<p>
					The commissioner can tune every value below until the season starts. After that the numbers lock for the year, so every manager plays under the same rules.
				</p>
			</div>
			<div class="draft-clock-panel">
				<If cond={data.locked == false}>
					<span>Editable until</span>
					<strong class="mono">{data.season_start}</strong>
				</If>
				<If cond={data.locked}>
					<span>Status</span>
					<strong class="mono">LOCKED</strong>
				</If>
				<div class="draft-clock-meta">
					<If cond={data.is_commissioner}>
						<a href="/admin" data-gosx-link>Admin console →</a>
					</If>
				</div>
			</div>
		</section>
		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_notice}>
				<p class="flash-message">{data.notice}</p>
			</If>
			<If cond={data.has_scoring_error}>
				<p class="error-message">{data.scoring_error}</p>
			</If>
			<If cond={data.locked}>
				<p class="demo-message">
					<strong>SEASON LOCK:</strong>
					scoring is frozen for the season.
				</p>
			</If>
		</div>
		<Each of={data.groups} as="group">
			<section class="player-pool">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">
							SCORING //
							{group.name}
						</span>
						<h2>{group.name}</h2>
					</div>
				</div>
				<div class="pool-list">
					<Each of={group.rules} as="rule">
						<ScoringRow
							rule={rule}
							Editable={data.editable}
							SetAction={actionPath("scoring-set")}
							CSRF={csrf.token}
						 />
					</Each>
				</div>
			</section>
		</Each>
		<If cond={data.editable}>
			<section class="player-pool admin-danger">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">RESET // SCORING</span>
						<h2>Danger zone</h2>
					</div>
				</div>
				<div class="danger-grid">
					<form method="post" action={actionPath("scoring-reset")} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<strong>Reset scoring</strong>
						<p>Restores every rule to the league defaults. Custom values are lost.</p>
						<input type="text" name="confirm" placeholder="type RESET" autocomplete="off"></input>
						<button class="button" type="submit">Reset scoring</button>
					</form>
				</div>
			</section>
		</If>
		<p class="scoring-note">{data.scoring_note}</p>
	</main>
}
