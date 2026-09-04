package team

type BreakdownRow struct {
	Scored bool
	Label  string
	Calc   string
	Points string
}

type RosterRowProps struct {
	Position       string
	HasHeadshot    bool
	Headshot       string
	Name           string
	NFLTeam        string
	Opponent       string
	HasOpponent    bool
	HasMatchup     bool
	MatchupTier    string
	MatchupChip    string
	MatchupDetail  string
	Jersey         string
	HasBreakdown   bool
	Breakdown      []BreakdownRow
	BreakdownTotal string
	HasHist        bool
	Hist           string
	HistLabel      string
	Status         string
	Projection     string
	Points         string
	// Kickoff/Bye (wave 7 item 4) render unconditionally — before lock,
	// not only once a slot has locked — as the row's own second line:
	// "CIN · vs SF · SUN 4:25 PM · BYE 5". See league.addScheduleLabels.
	HasKickoff bool
	Kickoff    string
	HasBye     bool
	Bye        string
	// DraftedLabel (wave 7 item 5) is a compact "R3 · P28" chip for a
	// drafted roster player; empty (HasDraftedLabel false) for a
	// free-agency add. Sourced from playerMap's own is_drafted/
	// drafted_label fields (league.draftedByPlayerID).
	HasDraftedLabel bool
	DraftedLabel    string
	// GroupHeader (wave 7 item 1) renders once, on the first bench row of
	// each new position group (QB, RB, WR, TE, K, DST) — blank on every
	// other row. See league.addBenchGroupHeaders.
	HasGroupHeader bool
	GroupHeader    string
	// HasNews/News/HasInjury/Injury/HasHouseRank/HouseRank (wave-8 audit
	// item 5) are playerMap's own news/injury/house-rank fields, already
	// carried by benchRows (playerMapsWithScoring) but never rendered on
	// this page before: /players and /board showed a 📰 news tip and an
	// "H###" house rank chip for the same player /team showed neither
	// for.
	HasNews      bool
	News         string
	HasInjury    bool
	Injury       string
	HasHouseRank bool
	HouseRank    string
}

// BadgeCellProps is one cell of the team-badge picker grid: a free
// motif's cell is a small submit form (csrf_token, team_id, motif,
// redirect_to), a taken-by-another-team cell is a dimmed, non-clickable
// preview labeled with that team's abbreviation, and this team's own
// claim is a highlighted preview. Free/Mine/TakenByOther are precomputed,
// mutually exclusive booleans (not "Claimed && ..." expressions) because a
// strict <If> cond must be a plain bool props field, or a bool props
// field compared with "== false" — see page.server.go's badgeGridProps
// doc comment for how the three are derived. CSRF/TeamID/RedirectTo
// repeat on every cell (rather than living once on the grid container)
// because a strict component call accepts exactly one spread attribute
// and no other attributes.
type BadgeCellProps struct {
	Slug          string
	Name          string
	MaskHref      string
	Free          bool
	Mine          bool
	TakenByOther  bool
	ClaimedByAbbr string
	CSRF          string
	TeamID        string
	RedirectTo    string
}

component BadgeCell(props: BadgeCellProps) {
	return <div class="badge-option-wrap">
		<If cond={props.Free}>
			<form method="post" action="/avatar/badge" data-gosx-managed="false" class="badge-option-form">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="team_id" value={props.TeamID}></input>
				<input type="hidden" name="motif" value={props.Slug}></input>
				<input type="hidden" name="redirect_to" value={props.RedirectTo}></input>
				<button type="submit" class="badge-option" title={props.Name} data-badge-state="available" aria-label={"Choose " + props.Name + " badge (available)"}>
					<span class="badge-option__art" style={"mask-image:url(" + props.MaskHref + ");-webkit-mask-image:url(" + props.MaskHref + ");"} aria-hidden="true"></span>
					<small>AVAILABLE · {props.Name}</small>
				</button>
			</form>
		</If>
		<If cond={props.Mine}>
			<div class="badge-option badge-option--mine" role="img" title={props.Name} data-badge-state="current" aria-label={"Current badge: " + props.Name} aria-current="true">
				<span class="badge-option__art" style={"mask-image:url(" + props.MaskHref + ");-webkit-mask-image:url(" + props.MaskHref + ");"} aria-hidden="true"></span>
				<small>CURRENT · {props.Name}</small>
			</div>
		</If>
		<If cond={props.TakenByOther}>
			<div class="badge-option badge-option--taken" role="img" title={props.Name} data-badge-state="taken" aria-label={props.Name + " badge is taken by " + props.ClaimedByAbbr} aria-disabled="true">
				<span class="badge-option__art" style={"mask-image:url(" + props.MaskHref + ");-webkit-mask-image:url(" + props.MaskHref + ");"} aria-hidden="true"></span>
				<small>TAKEN · {props.ClaimedByAbbr}</small>
			</div>
		</If>
	</div>
}

component RosterRow(props: RosterRowProps) {
	return <div class="roster-row">
		<If cond={props.HasGroupHeader}>
			<h4 class="roster-group-header mono">{props.GroupHeader}</h4>
		</If>
		<div class="position-chip">
			{props.Position}
			<If cond={props.HasHouseRank}>
				<small class="house-rank">{props.HouseRank}</small>
			</If>
		</div>
		<span class="pool-player-cell">
		<details class="player-identity stat-tip">
			<summary class="stat-tip__summary">
			<If cond={props.HasHeadshot}>
				<img class="player-avatar player-avatar--photo" src={props.Headshot} alt="" loading="lazy" />
			</If>
			<If cond={props.HasHeadshot == false}>
				<span class="player-avatar" aria-hidden="true">{props.NFLTeam}</span>
			</If>
			<span class="player-identity__text">
				<strong>{props.Name}</strong>
				<If cond={props.HasDraftedLabel}>
					<span class="drafted-chip mono">{props.DraftedLabel}</span>
				</If>
				<small class="roster-row__schedule mono">
					{props.NFLTeam}
					<If cond={props.HasOpponent}>
						·
						{props.Opponent}
					</If>
					<If cond={props.HasKickoff}>
						·
						{props.Kickoff}
					</If>
					<If cond={props.HasMatchup}>
						·
						<span class="matchup-chip" data-matchup-tier={props.MatchupTier}>{props.MatchupChip}</span>
					</If>
					<If cond={props.HasBye}>
						·
						{props.Bye}
					</If>
				</small>
			</span>
			</summary>
			<div class="stat-tip__panel">
				<div class="stat-tip__head">
					<strong>{props.Name}</strong>
					<span class="mono">{props.Jersey}</span>
					<span class="mono stat-tip__team">{props.NFLTeam}</span>
				</div>
				<If cond={props.HasBreakdown}>
					<div class="stat-tip__rows">
						<Each of={props.Breakdown} as="row">
							<div class="stat-tip__row" data-scored={row.Scored}>
								<span>{row.Label}</span>
								<span class="mono">{row.Calc}</span>
								<b class="mono">{row.Points}</b>
							</div>
						</Each>
						<div class="stat-tip__total">
							<span>League scoring</span>
							<b class="mono">{props.BreakdownTotal}</b>
						</div>
					</div>
				</If>
				<If cond={props.HasBreakdown == false}>
					<p class="stat-tip__empty">No projection detail for this position.</p>
				</If>
				<If cond={props.HasMatchup}>
					<p class="stat-tip__hist mono">{props.MatchupDetail}</p>
				</If>
				<If cond={props.HasHist}>
					<p class="stat-tip__hist mono">{props.Hist}</p>
					<p class="stat-tip__hist-note">{props.HistLabel}</p>
				</If>
			</div>
		</details>
		<If cond={props.HasNews}>
			<details class="stat-tip stat-tip--news">
				<summary class="stat-tip__summary stat-tip__summary--news" aria-label={"News for " + props.Name}>📰</summary>
				<div class="stat-tip__panel">
					<p class="stat-tip__news"><span class="stat-tip__label">NEWS</span> {props.News}</p>
					<If cond={props.HasInjury}>
						<p class="stat-tip__hist-note">{props.Injury}</p>
					</If>
				</div>
			</details>
		</If>
		</span>
		<div class="game-state">
			<span class="signal-mark" aria-hidden="true"></span>
			{props.Status}
		</div>
		<div class="player-number mono">
			<small>PROJ</small>
			{props.Projection}
		</div>
		<div class="player-number mono">
			<small>PTS</small>
			{props.Points}
		</div>
	</div>
}

// Page's avatar-upload form posts to /avatar/upload as a plain, unmanaged
// (data-gosx-managed="false") full-page submission. GoSX v0.50.0 supports
// File/Files and MaxActionBodyBytes for managed actions, but this native route
// remains until the production consumer adopts a bounded-multipart contract;
// its outer middleware applies the complete-envelope limit before sessions
// and CSRF parsing (see avatar_handlers.go in the repo root).
func Page() Node {
	return <main class="page team-page" id="main-content">
		<If cond={data.has_seat == false}>
			<section class="no-franchise tone-lime">
				<div class="signal-label">
					<span class="signal-mark" aria-hidden="true"></span>
					MANAGER TERMINAL // NO FRANCHISE
				</div>
				<h1>NO TEAM YET.</h1>
				<p>
					<strong>{data.public_entry.state_label}:</strong>
					{data.public_entry.detail}
				</p>
				<a href={data.public_entry.action_href} data-gosx-link class="button button--primary">{data.public_entry.action_label}</a>
			</section>
		</If>
		<If cond={data.has_seat}>
		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_notice}>
				<p class="flash-message">{data.notice}</p>
			</If>
			<If cond={data.has_avatar_error}>
				<p class="error-message">{data.avatar_error}</p>
			</If>
			<If cond={data.has_lineup_error}>
				<p class="error-message">{data.lineup_error}</p>
			</If>
			<If cond={data.has_rename_error}>
				<p class="error-message" role="alert">{data.rename_error}</p>
			</If>
			<If cond={data.has_co_error}>
				<p class="error-message" role="alert">{data.co_error}</p>
			</If>
			<If cond={data.viewer.demo}>
				<p class="demo-message">
					<strong>REHEARSAL MODE:</strong>
					the console is open to everyone while demo mode is on.
				</p>
			</If>
		</div>
		<If cond={data.lineup_intervention}>
			<section class="lineup-intervention-banner" aria-labelledby="lineup-intervention-title">
				<span class="section-index">COMMISSIONER // LINEUP CONTROL</span>
				<strong id="lineup-intervention-title">LINEUP INTERVENTION · {data.team.name}</strong>
				<p>You are editing this claimed franchise for week {data.week}. Only lineup controls are enabled here; identity, ownership, badge, roster transactions, ready status, and autopick remain with the franchise manager.</p>
			</section>
		</If>
		<section class={"team-hero tone-" + data.team.tone} id="team-identity-hero">
			<div class="team-hero__identity">
				<span class="team-monogram">
					<If cond={data.team.has_avatar_image}>
						<img class="avatar-mark__photo" src={data.team.avatar_image_large_url} alt={data.team.name} loading="lazy" />
					</If>
					<If cond={data.team.has_avatar_image == false}>
						{data.team.abbreviation}
					</If>
				</span>
				<div>
					<span class="section-index">
						MANAGER TERMINAL //
						{data.viewer.initials}
					</span>
					<h1>{data.team.name}</h1>
					<small class="mono">
						{data.team.division}
						DIVISION
					</small>
					<If cond={data.lineup_intervention == false}>
					<If cond={data.team.claimed && data.co_manager.has_co}>
						<p>
							Operated by
							{data.team.manager}
							· with
							{data.co_manager.co_name}
						</p>
					</If>
					<If cond={data.team.claimed && data.co_manager.has_co == false}>
						<p>
							Operated by
							{data.team.manager}
						</p>
					</If>
						<If cond={data.team.claimed == false}>
							<p>
								Awaiting a manager — sign in to claim this seat.
							</p>
						</If>
						<a href="/team?identity=edit#team-identity" data-gosx-link class="button button--ghost button--compact team-identity-link">Customize franchise</a>
						</If>
					</div>
				</div>
			<div class="team-hero__record">
				<span>Season</span>
				<strong class="mono">{data.team.record}</strong>
				<small>
					{data.team.points_for}
					points scored
					<If cond={data.has_team_streak}>
						·
						{data.team.streak}
					</If>
					</small>
				</div>
			</section>
			<div
				class="team-lineup-sync"
				data-gosx-region
				data-gosx-region-url={data.lineup_fragment_url}
				data-gosx-region-interval={data.lineup_fragment_interval}
				data-gosx-region-signal="$team.lineup.refresh"
				data-gosx-region-on="scores:changed"
				aria-label="Authoritative team lineup"
			>
				<TeamLineupRegion></TeamLineupRegion>
			</div>
			<p class="scoring-note lineup-sync-note" role="status" aria-live="polite">
				Lineup state refreshes automatically within 4 seconds after a manager saves.
				If a refresh fails, use
				<button type="button" class="board-button" data-gosx-set="$team.lineup.refresh" data-gosx-set-value="manual">Refresh lineup now</button>.
			</p>
			<If cond={data.is_commissioner}>
				<section class="lineup-target-switcher" aria-label="Commissioner lineup target">
					<div>
						<span class="section-index">COMMISSIONER HQ // CLAIMED FRANCHISES</span>
						<strong>Set lineup for another franchise</strong>
					</div>
					<form method="get" action="/team" class="lineup-target-switcher__form">
						<input type="hidden" name="week" value={data.week}></input>
						<select name="team" aria-label="Choose a claimed franchise lineup">
							<Each of={data.lineup_target_options} as="option">
								<option value={option.id} selected={option.selected}>{option.label}</option>
							</Each>
						</select>
						<button class="button button--compact" type="submit">Open lineup</button>
					</form>
					<If cond={data.lineup_intervention}>
						<a href={data.lineup_intervention_exit_href} data-gosx-link class="lineup-target-switcher__exit">Return to my lineup →</a>
					</If>
				</section>
			</If>
			<If cond={data.playoff_truth.season_phase == "preseason"}>
				<section class="score-command playoff-truth-card playoff-truth-card--compact" aria-labelledby="team-playoff-truth-heading">
					<p id="team-playoff-truth-heading"><span class="position-chip">{data.playoff_truth.status_label}</span> {data.playoff_truth.headline} — bracket truth opens after the regular season. <a href="/matchups" data-gosx-link class="access-link">Open bracket truth →</a></p>
				</section>
			</If>
			<If cond={data.playoff_truth.season_phase != "preseason"}>
				<section class="score-command playoff-truth-card" aria-labelledby="team-playoff-truth-heading">
					<header class="section-heading section-heading--split"><div><span class="section-index">POSTSEASON // TEAM VIEW</span><h2 id="team-playoff-truth-heading">{data.playoff_truth.headline}</h2></div><span class="position-chip">{data.playoff_truth.status_label}</span></header>
					<p>{data.playoff_truth.detail}</p>
					<If cond={data.playoff_truth.has_bracket}><p class="scoring-note">Your team appears in the persisted bracket only when published; tie explanations and bye states remain attached to the matchup.</p></If>
					<If cond={data.playoff_truth.recovery != ""}><p class="demo-message"><strong>RECOVERY:</strong> {data.playoff_truth.recovery}</p></If>
					<a href="/matchups" data-gosx-link class="access-link">Open bracket truth →</a>
				</section>
			</If>
			<If cond={data.lineup_intervention == false}>
			<details class="team-identity-settings" id="team-identity" open={data.identity_expanded}>
				<summary>
					<span class="team-identity-settings__summary-copy">
						<span class="section-index">FRANCHISE IDENTITY</span>
						<strong>Customize your team</strong>
						<small>Name · co-manager · image · league badge</small>
					</span>
					<span class="team-identity-settings__summary-action mono">OPEN EDITOR</span>
				</summary>
				<div class="team-identity-settings__body">
					<section class="team-identity-settings__panel" aria-labelledby="team-profile-settings-title">
						<header>
							<span class="section-index">PROFILE</span>
							<h2 id="team-profile-settings-title">Manager and team details</h2>
						</header>
						<If cond={data.co_manager.has_pending}>
							<p class="status-card__line">
								Co-manager invite pending:
								{data.co_manager.pending_email}
							</p>
						</If>
						<If cond={data.co_manager.can_invite}>
							<label class="team-identity-settings__field" for="co-manager-email">Co-manager email</label>
							<form method="post" action={actionPath("co-invite")} data-gosx-managed="true" class="co-manager-form">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name="team_id" value={data.team.id}></input>
								<input type="hidden" name={data.team_return_target_field} value={data.team_return_target}></input>
								<input id="co-manager-email" type="email" name="email" value={data.co_manager.invite_email} placeholder="co-manager@example.com" autocomplete="email" inputmode="email" enterkeyhint="done" required="required" aria-invalid={data.has_co_error} aria-describedby="co-manager-email-error"></input>
								<p id="co-manager-email-error" class="error-message form-error" data-gosx-field-error="email" role="alert">{data.co_error}</p>
								<button class="button button--compact" type="submit">Invite co-manager</button>
							</form>
						</If>
						<If cond={data.co_manager.can_detach}>
							<form method="post" action={actionPath("co-detach")} data-gosx-managed="true" class="co-manager-form">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name="team_id" value={data.team.id}></input>
								<input type="hidden" name={data.team_return_target_field} value={data.team_return_target}></input>
								<button class="button button--compact button--ghost" type="submit">Detach co-manager</button>
							</form>
						</If>
						<label class="team-identity-settings__field" for="team-name-input">Team name</label>
						<form method="post" action={actionPath("team-rename")} data-gosx-managed="true" class="team-rename-form">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<input type="hidden" name="team_id" value={data.team.id}></input>
							<input type="hidden" name={data.team_return_target_field} value={data.team_return_target}></input>
							<input id="team-name-input" type="text" name="name" value={data.team_name_value} maxlength="40" enterkeyhint="done" aria-invalid={data.has_rename_error} aria-describedby="team-name-error"></input>
							<p id="team-name-error" class="error-message form-error" data-gosx-field-error="name" role="alert">{data.rename_error}</p>
							<button class="button button--compact" type="submit">Rename</button>
						</form>
						<If cond={data.team.has_custom_name}>
							<form method="post" action={actionPath("team-name-reset")} data-gosx-managed="true" class="team-rename-form">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name="team_id" value={data.team.id}></input>
								<input type="hidden" name={data.team_return_target_field} value={data.team_return_target}></input>
								<button class="button button--secondary button--compact" type="submit">Reset to configured name</button>
							</form>
						</If>
						<If cond={data.identity_available}>
							<label class="team-identity-settings__field" for="team-avatar-upload">Custom team image</label>
							<form method="post" action="/avatar/upload" enctype="multipart/form-data" data-gosx-managed="false" class="avatar-upload-form">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name="team_id" value={data.team.id}></input>
								<input type="hidden" name="redirect_to" value="/team?identity=edit#team-identity"></input>
								<input
									id="team-avatar-upload"
									type="file"
									name="avatar"
									accept="image/png,image/jpeg"
									aria-describedby="team-avatar-upload-help"
									required="required"
								></input>
								<button class="button button--compact" type="submit">Upload image</button>
							</form>
							<p class="scoring-note" id="team-avatar-upload-help">PNG or JPEG, 10 MB maximum, from 64×64 through 4096×4096 pixels. Images are center-cropped and resized to a 512×512 PNG with metadata removed. Uploading a custom image releases this seat’s claimed badge so another team can use it.</p>
						</If>
					</section>
					<section class="team-identity-settings__panel" aria-labelledby="team-badge-settings-title">
						<header>
							<span class="section-index">LEAGUE BADGE</span>
							<h2 id="team-badge-settings-title">Choose one shared badge</h2>
						</header>
						<If cond={data.identity_available}>
							<p class="scoring-note">Available badges are exclusive to one team. Choosing one replaces this seat’s custom image; release it to use the league standard or your team letters.</p>
							<div class="badge-picker" style={"--badge-tone: " + data.badge_tone_hex + ";"}>
								<Each of={data.badge_grid} as="badge">
									<BadgeCell {...badge}></BadgeCell>
								</Each>
							</div>
							<If cond={data.has_badge_claim}>
								<form method="post" action="/avatar/badge" data-gosx-managed="false" class="badge-release-form">
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="team_id" value={data.team.id}></input>
									<input type="hidden" name="motif" value=""></input>
									<input type="hidden" name="action" value="release"></input>
									<input type="hidden" name="redirect_to" value="/team?identity=edit#team-identity"></input>
									<button class="button button--compact" type="submit">Release badge</button>
								</form>
							</If>
						</If>
						<If cond={data.identity_available == false}>
							<p class="error-message" role="status">{data.identity_error}</p>
						</If>
					</section>
				</div>
			</details>
			</If>
		</If>
	</main>
}

// TeamLineupRegion is the smallest authoritative Team fragment affected by
// lineup mutations. It intentionally uses the page bindings (data, csrf,
// actionPath) so the initial page and /team/fragment share the exact same
// server-rendered controls and native-form fallback.
//
// Roster-shape slot title (wave-7 re-audit, item 5): the title attribute
// only renders when slot.eligible is non-empty. An unconditional
// title={slot.eligible} printed a bare title="" on every slot with no
// eligible positions of its own (8 per roster, per the audit finding).
// The fix needs two branches, not one attribute expression: GSX has no
// ternary or "omit when falsy" attribute syntax, so an empty title has
// to come from never rendering the attribute at all, not from rendering
// it with an empty value.
func TeamLineupRegion() Node {
	return <div class="team-lineup-region">
				<div class="team-command-strip">
				<div>
					<span>Projected</span>
					<strong class="mono">{data.projected}</strong>
				</div>
				<div>
					<span>Starters</span>
					<strong class="mono">
						{data.starters_filled}
						/
						{data.starters_total}
					</strong>
				</div>
				<div>
					<span>Roster</span>
					<strong class="mono">{data.team_terminal_roster_count} / {data.team_terminal_roster_capacity}</strong>
				</div>
				<div>
					<span>Division</span>
					<strong class="mono">{data.team.division}</strong>
				</div>
				<div>
					<span>League</span>
					<strong class="mono">{data.league_mode}</strong>
				</div>
			</div>
			<If cond={data.lineup_intervention == false}>
			<a href="/matchups" data-gosx-link class="team-command-strip__action button button--primary button--compact">View matchup</a>
			</If>
			<If cond={data.predraft_visible && data.lineup_intervention == false}>
				<section class="predraft-progress" aria-labelledby="predraft-progress-title">
					<header class="predraft-progress__header">
						<div>
							<span class="section-index">PRE-DRAFT // YOUR SETUP</span>
							<h2 id="predraft-progress-title">Get this franchise draft-ready</h2>
						</div>
						<p>The scheduled time is the room’s meeting point—not an automatic start. The commissioner intentionally starts the draft after managers check in.</p>
					</header>
					<div class="checklist predraft-progress__list">
						<div class="checklist-item">
							<span class="checklist-mark checklist-mark--complete mono" aria-hidden="true">✓</span>
							<div class="checklist-item__text">
								<strong>Claim and personalize your franchise</strong>
								<small>Your seat is secured. Team name, image, badge, and co-manager controls live in Customize your team.</small>
							</div>
							<a href="/team?identity=edit#team-identity" data-gosx-link class="board-button">Customize team →</a>
						</div>
						<div class="checklist-item">
							<If cond={data.predraft_has_board}>
								<span class="checklist-mark checklist-mark--complete mono" aria-hidden="true">✓</span>
							</If>
							<If cond={data.predraft_has_board == false}>
								<span class="checklist-mark mono" aria-hidden="true">02</span>
							</If>
							<div class="checklist-item__text">
								<strong>Rank your draft targets</strong>
								<If cond={data.predraft_has_board}>
									<small>{data.predraft_board_count} players ranked. Keep refining—the board drives your draft-room shortlist and autopick order.</small>
								</If>
								<If cond={data.predraft_has_board == false}>
									<small>No players ranked yet. Add targets in the order you would want them drafted.</small>
								</If>
							</div>
							<a href="/board" data-gosx-link class="board-button">Open board →</a>
						</div>
						<div class="checklist-item">
							<If cond={data.predraft_ready}>
								<span class="checklist-mark checklist-mark--complete mono" aria-hidden="true">✓</span>
							</If>
							<If cond={data.predraft_ready == false}>
								<span class="checklist-mark mono" aria-hidden="true">03</span>
							</If>
							<div class="checklist-item__text">
								<strong>Confirm your room status</strong>
								<If cond={data.predraft_ready}>
									<small>You are marked ready. You can change that status any time before the commissioner starts.</small>
								</If>
								<If cond={data.predraft_ready == false}>
									<small>You are not marked ready. Check the room details, then tell the commissioner you are present.</small>
								</If>
							</div>
							<a href="/draft#ready-toggle" data-gosx-link class="board-button">Open draft room →</a>
						</div>
					</div>
				</section>
			</If>
			<div class="team-layout">
				<section class="roster-panel" id="lineup">
					<header class="section-heading section-heading--split">
						<div>
							<span class="section-index">01 // STARTING LINEUP</span>
							<h2>
								Week
								{data.week}
								lineup
							</h2>
						</div>
						<span class="lineup-lock">
							<span class="signal-mark" aria-hidden="true"></span>
							<b class="mono">{data.starters_filled}</b>
							/
							{data.starters_total}
							STARTERS
						</span>
					</header>
					<If cond={data.starters_empty}>
						<p class="error-message lineup-starters-warning" role="status">{data.starters_empty_label}</p>
					</If>
					<If cond={data.has_week_notice}>
						<p class="error-message lineup-week-notice" role="status">{data.week_notice}</p>
					</If>
					<details class="pool-legend">
						<summary>What does H### mean?</summary>
						<p>H### — house rank: this league's own superflex-aware value order (your scoring and roster rules), shown beside every lineup and bench slot. <a href="/help#glossary" data-gosx-link>More terms in the glossary →</a></p>
					</details>
					<section class="lineup-deadline" aria-live="polite" aria-label="Lineup lock timing">
						<div class="lineup-deadline__heading">
							<span class="section-index">WEEK {data.lineup_deadline.week} // LOCK WINDOW</span>
							<strong>{data.lineup_deadline.headline}</strong>
						</div>
						<If cond={data.lineup_deadline.has_deadline}>
							<p class="lineup-deadline__exact">
								<span class="mono">NEXT PLAYER LOCK</span>
								<b>{data.lineup_deadline.exact}</b>
							</p>
							<p class="lineup-deadline__relative mono">{data.lineup_deadline.relative} · {data.lineup_deadline.timezone}</p>
						</If>
						<p>{data.lineup_deadline.detail}</p>
						<If cond={data.lineup_deadline.is_no_schedule}>
							<p class="mono">Do not treat an unavailable timestamp as an unlocked deadline.</p>
						</If>
						<If cond={data.lineup_deadline.is_degraded}>
							<p class="mono">Schedule refresh required before this lock window is authoritative.</p>
						</If>
					</section>
					<div class="roster-shape" aria-label="League roster shape">
						<Each of={data.roster_shape} as="slot">
							<span class="roster-shape__slot-wrap">
								<If cond={slot.has_eligible}>
									<span class="roster-shape__slot mono" title={slot.eligible}>{slot.label}</span>
									<small class="roster-shape__eligible mono">ELIGIBLE: {slot.eligible}</small>
								</If>
								<If cond={slot.has_eligible == false}>
									<span class="roster-shape__slot mono">{slot.label}</span>
								</If>
							</span>
						</Each>
						<p class="roster-shape__summary mono">{data.shape_summary}</p>
						<div class="roster-shape__depth" role="list" aria-label={"Roster positional depth: " + data.positional_depth}>
							<Each of={data.positional_depth_chips} as="chip">
								<span class="roster-shape__depth-chip mono" role="listitem">{chip.label}</span>
							</Each>
						</div>
					</div>
					<If cond={data.team_terminal_roster_complete && data.draft_class_teaser_empty == false}>
						<section class="draft-class-callout" aria-labelledby="draft-class-callout-title">
							<div>
								<span class="section-index">DRAFT COMPLETE // YOUR CLASS</span>
								<strong id="draft-class-callout-title">Your draft class</strong>
							</div>
							<ul class="draft-class-callout__list">
								<Each of={data.draft_class_teaser} as="pick">
									<li>
										<span class="position-chip">{pick.position}</span>
										{pick.name}
										<small class="mono">{pick.label}</small>
									</li>
								</Each>
							</ul>
							<a href={data.draft_class_href} data-gosx-link class="access-link">View your full draft class →</a>
						</section>
					</If>
					<div class="lineup-toolbar">
						<form method="get" action="/team" class="lineup-week-form">
							<If cond={data.lineup_intervention}>
								<input type="hidden" name="team" value={data.lineup_target_id}></input>
							</If>
							<select name="week" aria-label="Select week">
								<Each of={data.week_options} as="wk">
									<option value={wk.value} selected={wk.selected}>{wk.label}</option>
								</Each>
							</select>
							<button class="board-button" type="submit">Go</button>
						</form>
						<If cond={data.team_terminal_roster_complete}>
							<form id="lineup-auto-form" method="post" action={actionPath("lineup-auto")} data-gosx-managed="true" class="lineup-auto-form">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name="team_id" value={data.team.id}></input>
								<input type="hidden" name="week" value={data.week}></input>
								<button class="button button--compact lineup-auto-form__button" type="submit">Set best lineup</button>
							</form>
							<p class="scoring-note lineup-action-note">Set best lineup rewrites every currently unlocked starter slot using your roster, highest projection first. Locked slots stay exactly where they are; run it again any time before those players kick off.</p>
						</If>
					</div>
					<If cond={data.team_terminal_pre_draft && data.lineup_intervention == false}>
						<div class="empty-tape roster-lifecycle-state roster-lifecycle-state--predraft">
							<strong>{data.team_terminal_label}</strong>
							<p>{data.team_terminal_detail}</p>
							<div class="hero-actions">
								<a href={data.team_terminal_primary_href} data-gosx-link class="button button--primary">{data.team_terminal_primary_label} →</a>
								<a href={data.team_terminal_secondary_href} data-gosx-link class="button button--ghost">{data.team_terminal_secondary_label} →</a>
							</div>
						</div>
					</If>
					<If cond={data.lineup_intervention && data.team_terminal_pre_draft}>
						<div class="empty-tape roster-lifecycle-state roster-lifecycle-state--predraft">
							<strong>ROSTER PREVIEW · DRAFT PENDING</strong>
							<p>This claimed franchise has no players yet. Starting slots remain empty until the commissioner starts the draft and picks are recorded.</p>
							<a href="/draft" data-gosx-link class="button button--primary">Open the draft room →</a>
						</div>
					</If>
					<If cond={data.team_terminal_draft_live && data.lineup_intervention == false}>
						<div class="empty-tape roster-lifecycle-state roster-lifecycle-state--live">
							<strong>{data.team_terminal_label}</strong>
							<p>{data.team_terminal_detail}</p>
							<div class="hero-actions">
								<a href={data.team_terminal_primary_href} data-gosx-link class="button button--primary">{data.team_terminal_primary_label} →</a>
								<a href={data.team_terminal_secondary_href} data-gosx-link class="button button--ghost">{data.team_terminal_secondary_label} →</a>
							</div>
						</div>
					</If>
					<If cond={data.has_matchup_source}>
						<details class="matchup-rank-glossary">
							<summary>What do the matchup ranks mean?</summary>
							<p class="demo-message">
								<strong>MATCHUP RANKS:</strong>
								ranked from the {data.matchup_source_label}. A higher "-toughest" number is a softer matchup; a lower one is tougher.
							</p>
						</details>
					</If>
					<div class="lineup-slot-list">
							<Each of={data.starters} as="slot">
								<div class="lineup-slot">
									<div class="lineup-slot__id mono">
										{slot.slot_id}
										<If cond={slot.has_house_rank}>
											<small class="house-rank">{slot.house_rank}</small>
										</If>
									</div>
									<If cond={slot.has_player}>
									<span class="pool-player-cell">
										<details class="player-identity stat-tip">
											<summary class="stat-tip__summary">
											<If cond={slot.has_headshot}>
												<img class="player-avatar player-avatar--photo" src={slot.headshot} alt="" loading="lazy" />
											</If>
											<If cond={slot.has_headshot == false}>
												<span class="player-avatar" aria-hidden="true">{slot.nfl_team}</span>
											</If>
											<span class="player-identity__text">
												<strong>{slot.name}</strong>
												<If cond={slot.is_drafted}>
													<span class="drafted-chip mono">{slot.drafted_label}</span>
												</If>
												<small class="roster-row__schedule mono">
													{slot.position}
													·
													{slot.nfl_team}
													<If cond={slot.has_opponent}>
														·
														{slot.opponent}
													</If>
													<If cond={slot.has_kickoff_label}>
														·
														{slot.kickoff_label}
													</If>
													<If cond={slot.has_matchup}>
														·
														<span class="matchup-chip" data-matchup-tier={slot.matchup_tier}>{slot.matchup_chip}</span>
													</If>
													<If cond={slot.has_bye_label}>
														·
														{slot.bye_label}
													</If>
												</small>
											</span>
											</summary>
											<div class="stat-tip__panel">
												<div class="stat-tip__head">
													<strong>{slot.name}</strong>
													<span class="mono">{slot.jersey}</span>
													<span class="mono stat-tip__team">{slot.nfl_team}</span>
												</div>
												<If cond={slot.has_breakdown}>
													<div class="stat-tip__rows">
														<Each of={slot.breakdown} as="row">
															<div class="stat-tip__row" data-scored={row.scored}>
																<span>{row.label}</span>
																<span class="mono">{row.calc}</span>
																<b class="mono">{row.points}</b>
															</div>
														</Each>
														<div class="stat-tip__total">
															<span>League scoring</span>
															<b class="mono">{slot.breakdown_total}</b>
														</div>
													</div>
												</If>
												<If cond={slot.has_breakdown == false}>
													<p class="stat-tip__empty">No projection detail for this position.</p>
												</If>
												<If cond={slot.has_matchup}>
													<p class="stat-tip__hist mono">{slot.matchup_detail}</p>
												</If>
											</div>
										</details>
										<If cond={slot.has_news}>
											<details class="stat-tip stat-tip--news">
												<summary class="stat-tip__summary stat-tip__summary--news" aria-label={"News for " + slot.name}>📰</summary>
												<div class="stat-tip__panel">
													<p class="stat-tip__news"><span class="stat-tip__label">NEWS</span> {slot.news}</p>
													<If cond={slot.has_injury}>
														<p class="stat-tip__hist-note">{slot.injury}</p>
													</If>
												</div>
											</details>
										</If>
									</span>
									</If>
									<If cond={slot.has_player == false}>
										<If cond={data.team_terminal_roster_complete}>
											<div class="slot-empty mono">EMPTY</div>
										</If>
										<If cond={data.team_terminal_roster_complete == false}>
											<div class="slot-empty mono">AWAITING DRAFT</div>
										</If>
									</If>
									<div class="lineup-slot__chips">
										<If cond={slot.has_player}>
											<span class="position-chip lineup-slot__position">{slot.position}</span>
										</If>
										<If cond={slot.auto_filled}>
											<span class="position-chip" title="Filled automatically by SET BEST LINEUP" aria-label="Filled automatically by SET BEST LINEUP">AUTO</span>
										</If>
										<If cond={slot.has_warning}>
											<span class="position-chip position-chip--warn">{slot.warning_label}</span>
										</If>
										<If cond={slot.locked}>
											<span class="position-chip position-chip--locked">{slot.lock_label}</span>
										</If>
										<If cond={slot.has_possession}>
											<span class="possession-chip">{slot.possession_label}</span>
										</If>
									</div>
									<If cond={data.team_terminal_roster_complete && slot.locked == false}>
										<form method="post" action={actionPath("lineup-set")} data-gosx-managed="true" class="lineup-slot__form">
											<input type="hidden" name="csrf_token" value={csrf.token}></input>
											<input type="hidden" name="team_id" value={data.team.id}></input>
											<input type="hidden" name="week" value={data.week}></input>
											<input type="hidden" name="slot" value={slot.slot_id}></input>
											<select name="player_id" aria-label={"Assign a player to " + slot.slot_id}>
												<Each of={slot.options} as="opt">
													<option value={opt.id} selected={opt.selected}>{opt.label}</option>
												</Each>
											</select>
											<button class="board-button" type="submit">Set</button>
										</form>
									</If>
								</div>
							</Each>
						</div>
						<h3 class="lineup-bench-title">Bench</h3>
						<If cond={data.bench_empty}>
							<If cond={data.team_terminal_roster_complete}>
								<p class="stat-tip__empty">No bench players.</p>
							</If>
							<If cond={data.team_terminal_roster_complete == false}>
								<p class="stat-tip__empty">Bench capacity: {data.bench_capacity} open until the draft fills it.</p>
							</If>
						</If>
						<If cond={data.bench_empty == false}>
							<div class="roster-labels mono" aria-hidden="true">
								<span>POS</span>
								<span>PLAYER</span>
								<span>GAME</span>
								<span>PROJ</span>
								<span>PTS</span>
							</div>
							<div class="roster-list">
								<Each of={data.bench} as="player">
									<RosterRow {...player}></RosterRow>
								</Each>
							</div>
							<If cond={data.has_reserve && data.lineup_intervention == false}>
								<h3 class="lineup-bench-title">
									Reserve
									<small class="mono">{data.reserve_capacity}</small>
								</h3>
								<If cond={data.reserve_occupants_empty}>
									<p class="stat-tip__empty">No one is on reserve.</p>
								</If>
								<If cond={data.reserve_occupants_empty == false}>
									<p class="scoring-note zone-action-note">Reserve keeps the player on your roster but removes them from starting and bench eligibility. Activate returns them to the general pool; neither action changes the draft or roster count.</p>
									<div class="roster-list">
										<Each of={data.reserve_occupants} as="occ">
											<div class="roster-row">
												<div class="position-chip">{occ.position}</div>
												<div>
													<strong>{occ.name}</strong>
													<small>
														{occ.nfl_team}
														<If cond={occ.has_opponent}>
															·
															{occ.opponent}
															<If cond={occ.has_matchup}>
																·
																<span class="matchup-chip" data-matchup-tier={occ.matchup_tier}>{occ.matchup_chip}</span>
															</If>
														</If>
													</small>
												</div>
												<form method="post" action={actionPath("reserve-activate")} data-gosx-managed="true">
													<input type="hidden" name="csrf_token" value={csrf.token}></input>
													<input type="hidden" name="team_id" value={data.team.id}></input>
													<input type="hidden" name="week" value={data.week}></input>
													<input type="hidden" name="player_id" value={occ.id}></input>
													<button class="button button--compact" type="submit">Activate</button>
												</form>
											</div>
										</Each>
									</div>
								</If>
								<If cond={data.reserve_place_empty == false}>
									<p class="scoring-note zone-action-note">Place on reserve is reversible: the player leaves the lineup/bench pool until you activate them again. Your roster capacity does not increase.</p>
									<form method="post" action={actionPath("reserve-place")} data-gosx-managed="true" class="lineup-toolbar">
										<input type="hidden" name="csrf_token" value={csrf.token}></input>
										<input type="hidden" name="team_id" value={data.team.id}></input>
										<input type="hidden" name="week" value={data.week}></input>
										<select name="player_id" aria-label="Place a player on reserve">
											<Each of={data.reserve_place_options} as="opt">
												<option value={opt.id}>{opt.label}</option>
											</Each>
										</select>
										<button class="board-button" type="submit">Place on reserve</button>
									</form>
								</If>
							</If>
							<If cond={data.has_ir && data.lineup_intervention == false}>
								<h3 class="lineup-bench-title">
									<abbr title="injured reserve" aria-label="injured reserve">IR</abbr>
									<small class="mono">{data.ir_capacity}</small>
								</h3>
								<If cond={data.ir_occupants_empty}>
									<p class="stat-tip__empty">No one is on IR.</p>
								</If>
								<If cond={data.ir_occupants_empty == false}>
									<p class="scoring-note zone-action-note">IR removes an injured player from the counted roster while the designation qualifies. Activate returns them to the roster; if full, choose a drop—the drop is permanent for this transaction and cannot be undone here.</p>
									<div class="roster-list">
										<Each of={data.ir_occupants} as="occ">
											<div class="roster-row">
												<div class="position-chip">{occ.position}</div>
												<div>
													<strong>{occ.name}</strong>
													<small>
														{occ.nfl_team}
														<If cond={occ.has_opponent}>
															·
															{occ.opponent}
															<If cond={occ.has_matchup}>
																·
																<span class="matchup-chip" data-matchup-tier={occ.matchup_tier}>{occ.matchup_chip}</span>
															</If>
														</If>
													</small>
													<If cond={occ.healed}>
														<p class="error-message">
															Off the injury report — activate before
															{occ.deadline_label}
															or the league drops him automatically.
														</p>
													</If>
												</div>
												<form method="post" action={actionPath("ir-activate")} data-gosx-managed="true">
													<input type="hidden" name="csrf_token" value={csrf.token}></input>
													<input type="hidden" name="team_id" value={data.team.id}></input>
													<input type="hidden" name="week" value={data.week}></input>
													<input type="hidden" name="player_id" value={occ.id}></input>
													<select name="drop_id" aria-label="Optional drop to make room">
														<option value="">— no drop —</option>
														<Each of={data.ir_drop_options} as="opt">
															<option value={opt.id}>{opt.label}</option>
														</Each>
													</select>
													<button class="button button--compact" type="submit">Activate</button>
												</form>
											</div>
										</Each>
									</div>
								</If>
								<If cond={data.ir_place_empty == false}>
									<p class="scoring-note zone-action-note">Place on IR is reversible while the player remains eligible. It frees a roster slot for the season; activate later, or review the injury designation before dropping anyone.</p>
									<form method="post" action={actionPath("ir-place")} data-gosx-managed="true" class="lineup-toolbar">
										<input type="hidden" name="csrf_token" value={csrf.token}></input>
										<input type="hidden" name="team_id" value={data.team.id}></input>
										<input type="hidden" name="week" value={data.week}></input>
										<select name="player_id" aria-label="Place a player on IR">
											<Each of={data.ir_place_options} as="opt">
												<option value={opt.id}>{opt.label}</option>
											</Each>
										</select>
										<button class="board-button" type="submit">Place on IR</button>
									</form>
								</If>
							</If>
						</If>
				</section>
				<aside class="scout-panel">
					<header>
						<span class="section-index">{data.radar_kicker}</span>
						<h2>{data.radar_title}</h2>
					</header>
					<If cond={data.scouting_empty}>
						<div class="empty-tape scout-empty-state">
							<strong>{data.scouting_empty_title}</strong>
							<p>{data.scouting_empty_detail}</p>
						</div>
					</If>
					<If cond={data.scouting_empty == false}>
						<div class="scout-list">
							<Each of={data.scouting} as="player">
								<article class="scout-row">
									<span class="position-chip">{player.position}</span>
									<div class="scout-row__copy">
										<strong>{player.name}</strong>
										<small>{player.team} · {player.signal}</small>
										<If cond={player.has_resolution}>
											<small class="mono scout-row__resolution">{player.resolution}</small>
										</If>
										<If cond={player.has_link}>
											<a href={player.href} data-gosx-link class="scout-row__link">{player.link_label} →</a>
										</If>
									</div>
									<b class="mono">{player.status}</b>
								</article>
							</Each>
						</div>
					</If>
					<div class="scout-callout">
						<span>Radar note</span>
						<p>{data.radar_description}</p>
						<a href={data.radar_link_href} data-gosx-link>{data.radar_link_label} →</a>
						<If cond={data.is_commissioner}>
							<a href="/admin" data-gosx-link>Commissioner console →</a>
						</If>
					</div>
				</aside>
			</div>
	</div>
}
