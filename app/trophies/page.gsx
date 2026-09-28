package trophies

// TrophyRowView structurally mirrors internal/league's TrophyRowView and is
// TrophyItem's own props type, the same shape-sharing the Pick'em page's
// LeaderboardRow uses. Manager is empty on the by-manager view (the page
// heading already names them). The rule is visible text, not a tooltip, so
// phone and keyboard readers get the tie rules too.
type TrophyRowView struct {
	Title       string
	Meta        string
	Manager     string
	ManagerHref string
	HasManager  bool
	Value       string
	Rule        string
}

// TrophyManagerOption mirrors internal/league's TrophyManagerOption.
type TrophyManagerOption struct {
	Name     string
	Href     string
	Count    int
	Selected bool
}

// TrophyCountView mirrors internal/league's TrophyCountView.
type TrophyCountView struct {
	Title string
	Count int
}

component TrophyItem(props: TrophyRowView) {
	return <li class="pickem-trophy" data-awarded="true">
		<span class="section-index">
			{props.Title}
			·
			{props.Meta}
		</span>
		<div class="pickem-trophy__body">
			<If cond={props.HasManager}>
				<strong class="pickem-trophy__names">
					<a class="pickem-manager-link" href={props.ManagerHref} data-gosx-link>{props.Manager}</a>
				</strong>
			</If>
			<b class="mono pickem-trophy__value">{props.Value}</b>
		</div>
		<p class="scoring-note pickem-trophy__rule">{props.Rule}</p>
	</li>
}

component RuleItem(props: TrophyRowView) {
	return <li class="pickem-trophy">
		<span class="section-index">
			{props.Title}
			·
			{props.Meta}
		</span>
		<p class="scoring-note pickem-trophy__rule">{props.Rule}</p>
	</li>
}

component ManagerChip(props: TrophyManagerOption) {
	return <li>
		<a href={props.Href} data-gosx-link class="board-button trophy-manager-chip" aria-current={props.Selected}>
			{props.Name}
			<span class="mono">{props.Count}</span>
		</a>
	</li>
}

component CountItem(props: TrophyCountView) {
	return <li class="trophy-count">
		<span>{props.Title}</span>
		<b class="mono">
			×
			{props.Count}
		</b>
	</li>
}

func Page() Node {
	return <main class="page pickem-page trophies-page" id="main-content">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="signal-mark" aria-hidden="true"></span>
					TROPHY CASE
				</span>
				<h1>Trophies</h1>
			</div>
			<div class="draft-clock-panel">
				<span>How to read it</span>
				<div class="draft-clock-meta">
					<a href="/pickem" data-gosx-link>Pick'em →</a>
					<a href="/matchups" data-gosx-link>Matchups →</a>
				</div>
			</div>
		</section>

		<nav class="pickem-weeknav" aria-label="Trophy case view">
			<a href={data.by_week_href} data-gosx-link class="board-button" aria-current={data.is_week}>By week</a>
			<a href="#by-manager" data-gosx-link class="board-button" aria-current={data.is_manager}>By manager</a>
		</nav>

		<If cond={data.has_awards == false}>
			<div class="empty-tape">
				<strong>NO TROPHIES YET</strong>
				<p>
					Trophies are awarded once a week closes. Check back after the first slate settles.
				</p>
			</div>
		</If>

		<If cond={data.is_week}>
			<section class="player-pool" id="by-week">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">
							WEEK
							{data.week}
						</span>
						<h2>This week's trophies</h2>
					</div>
				</div>
				<If cond={data.has_weeks}>
					<div class="pickem-weeknav">
						<If cond={data.has_prev_week}>
							<a href={data.prev_week_href} data-gosx-link class="board-button" rel="prev">← Prev</a>
						</If>
						<form method="get" action="/trophies" class="lineup-week-form">
							<select name="week" class="board-button" aria-label="Select week">
								<Each of={data.week_options} as="wk">
									<option value={wk.value} selected={wk.selected}>{wk.label}</option>
								</Each>
							</select>
							<button class="board-button" type="submit">Go</button>
						</form>
						<If cond={data.has_next_week}>
							<a href={data.next_week_href} data-gosx-link class="board-button" rel="next">Next →</a>
						</If>
					</div>
				</If>
				<If cond={data.has_week_rows == false}>
					<div class="empty-tape">
						<strong>NO AWARDS THIS WEEK</strong>
						<p>
							Nothing has been awarded for week
							{data.week}
							yet. Weeks award once every game and matchup settles.
						</p>
					</div>
				</If>
				<ul class="pickem-trophies" aria-label="Trophies awarded this week">
					<Each of={data.week_rows} as="row">
						<TrophyItem {...row}></TrophyItem>
					</Each>
				</ul>
			</section>
		</If>

		<section class="player-pool" id="by-manager">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">BY MANAGER</span>
					<h2>
						<If cond={data.is_manager}>
							{data.manager_name}
						</If>
						<If cond={data.is_week}>
							Pick a manager
						</If>
					</h2>
				</div>
			</div>
			<If cond={data.has_manager_options}>
				<ul class="trophy-manager-list" aria-label="Managers, with trophy counts">
					<Each of={data.manager_options} as="opt">
						<ManagerChip {...opt}></ManagerChip>
					</Each>
				</ul>
			</If>
			<If cond={data.manager_unknown}>
				<div class="empty-tape">
					<strong>MANAGER NOT FOUND</strong>
					<p>
						That link does not match a manager in this league. Pick one above.
					</p>
				</div>
			</If>
			<If cond={data.is_manager}>
				<If cond={data.manager_known}>
					<If cond={data.has_manager_rows == false}>
						<div class="empty-tape">
							<strong>NO TROPHIES YET</strong>
							<p>
								{data.manager_name}
								has not won a trophy this season.
							</p>
						</div>
					</If>
					<If cond={data.has_manager_rows}>
						<p class="scoring-note">
							<strong>{data.manager_total}</strong>
							trophies this season.
						</p>
						<ul class="trophy-counts" aria-label="Trophies won, by kind">
							<Each of={data.manager_counts} as="count">
								<CountItem {...count}></CountItem>
							</Each>
						</ul>
						<ul class="pickem-trophies" aria-label="Every trophy won, with its week">
							<Each of={data.manager_rows} as="row">
								<TrophyItem {...row}></TrophyItem>
							</Each>
						</ul>
					</If>
				</If>
			</If>
		</section>

		<section class="player-pool" id="season-trophies">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">SEASON</span>
					<h2>Season trophies</h2>
				</div>
			</div>
			<If cond={data.has_season_rows == false}>
				<div class="empty-tape">
					<strong>NOT AWARDED YET</strong>
					<p>
						Season trophies show the current leader once a week closes.
					</p>
				</div>
			</If>
			<ul class="pickem-trophies" aria-label="Season trophies, current leaders">
				<Each of={data.season_rows} as="row">
					<TrophyItem {...row}></TrophyItem>
				</Each>
			</ul>
		</section>

		<section class="player-pool" id="trophy-rules">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">RULES</span>
					<h2>Every trophy and how ties work</h2>
				</div>
			</div>
			<ul class="pickem-trophies" aria-label="Trophy rules">
				<Each of={data.trophy_defs} as="def">
					<RuleItem {...def}></RuleItem>
				</Each>
			</ul>
		</section>
	</main>
}
