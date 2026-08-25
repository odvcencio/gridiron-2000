package app

// PrimaryNavigation is the single typed information architecture for every
// authenticated/demo navigation surface. Desktop rail, enhanced mobile
// dialog, and static no-JavaScript mobile disclosure all invoke this exact
// component so their groups, order, labels, and role gates cannot drift.
type PrimaryNavigationProps struct {
	SignedIn      bool
	HasSeat       bool
	SeatsOpen     bool
	CanClaimSeat  bool
	Commissioner  bool
	Initials      string
	TeamName      string
	CSRFToken     string
}

func PrimaryNavigation(props PrimaryNavigationProps) Node {
	return <div class="primary-navigation">
		<nav class="primary-navigation__groups" aria-label="Primary navigation">
			<div class="navigation-group" data-navigation-group="today">
				<p class="navigation-group__label mono">TODAY</p>
				<Link href="/" class="navigation-link">
					<span class="navigation-link__index mono">01</span>
					Home
				</Link>
				<Link href="/pickem" class="navigation-link navigation-link--hot">
					<span class="navigation-link__index mono">02</span>
					Pick'em
				</Link>
				<Link href="/matchups" class="navigation-link">
					<span class="navigation-link__index mono">03</span>
					Matchups
				</Link>
			</div>
			<div class="navigation-group" data-navigation-group="my-team">
				<p class="navigation-group__label mono">MY TEAM</p>
				<If cond={props.HasSeat}>
					<Link href="/team" class="navigation-link">
						<span class="navigation-link__index mono">04</span>
						Team terminal
					</Link>
				</If>
				<If cond={props.HasSeat == false && props.SeatsOpen && props.CanClaimSeat}>
					<Link href="/join" class="navigation-link navigation-link--hot">
						<span class="navigation-link__index mono">04</span>
						Join a team
					</Link>
				</If>
				<If cond={props.HasSeat == false && (props.SeatsOpen == false || props.CanClaimSeat == false)}>
					<Link href="/team" class="navigation-link">
						<span class="navigation-link__index mono">04</span>
						Team status
					</Link>
				</If>
				<Link href="/board" class="navigation-link">
					<span class="navigation-link__index mono">05</span>
					Draft board
				</Link>
				<If cond={props.HasSeat || props.SignedIn}>
					<Link href="/players" class="navigation-link">
						<span class="navigation-link__index mono">06</span>
						Player pool
					</Link>
				</If>
				<If cond={props.HasSeat || props.Commissioner}>
					<Link href="/trades" class="navigation-link">
						<span class="navigation-link__index mono">07</span>
						Trades
					</Link>
				</If>
			</div>
			<div class="navigation-group" data-navigation-group="game-day">
				<p class="navigation-group__label mono">GAME DAY</p>
				<Link href="/draft" class="navigation-link">
					<span class="navigation-link__index mono">08</span>
					Draft
				</Link>
				<Link href="/blitz" class="navigation-link">
					<span class="navigation-link__index mono">09</span>
					Preseason Blitz
				</Link>
			</div>
			<div class="navigation-group" data-navigation-group="league">
				<p class="navigation-group__label mono">LEAGUE</p>
				<Link href="/wire" class="navigation-link">
					<span class="navigation-link__index mono">10</span>
					Signal Wire
				</Link>
				<Link href="/activity" class="navigation-link">
					<span class="navigation-link__index mono">11</span>
					Activity
				</Link>
				<Link href="/scoring" class="navigation-link">
					<span class="navigation-link__index mono">12</span>
					Rules &amp; scoring
				</Link>
			</div>
			<div class="navigation-group" data-navigation-group="help">
				<p class="navigation-group__label mono">HELP</p>
				<Link href="/guide" class="navigation-link navigation-link--guide">
					<span class="navigation-link__index mono">13</span>
					Manager guide
				</Link>
				<Link href="/help" class="navigation-link navigation-link--guide">
					<span class="navigation-link__index mono">14</span>
					Help center
				</Link>
			</div>
			<If cond={props.Commissioner}>
				<div class="navigation-group" data-navigation-group="commissioner">
					<p class="navigation-group__label mono">COMMISSIONER</p>
					<Link href="/commissioner" class="navigation-link">
						<span class="navigation-link__index mono">15</span>
						All leagues
					</Link>
					<Link href="/admin" class="navigation-link">
						<span class="navigation-link__index mono">16</span>
						League settings
					</Link>
				</div>
			</If>
		</nav>
		<div class="navigation-account">
			<If cond={props.SignedIn}>
				<div class="user-badge">
					<span class="user-chip mono">{props.Initials}</span>
					<span class="user-name">{props.TeamName}</span>
				</div>
				<Link href="/settings" class="access-link">
					Notification settings
				</Link>
				<form method="post" action="/auth/logout" data-gosx-managed="true">
					<input type="hidden" name="csrf_token" value={props.CSRFToken}></input>
					<button class="access-link" type="submit">Sign out</button>
				</form>
			</If>
			<If cond={props.SignedIn == false}>
				<div class="user-badge">
					<span class="user-chip mono">{props.Initials}</span>
					<span class="user-name">Rehearsal seat</span>
				</div>
				<a href="/login" data-gosx-link class="access-link" aria-label="League access">
					<span class="signal-mark" aria-hidden="true"></span>
					League access
				</a>
			</If>
		</div>
	</div>
}

// Layout renders the persistent chrome around every route's <Slot/>.
func Layout() Node {
	return <div class="app-shell">
		<a class="skip-link" href="#main-content">Skip to league content</a>
		<div class="ambient-grid" aria-hidden="true"></div>
		<div
			class="toast-stack"
			data-gosx-toast-host
			aria-live="polite"
			aria-relevant="additions"
			aria-label="Action notifications"
		></div>
		<If cond={data.viewer.signed_in || data.viewer.demo}>
			<aside
				class="site-rail navigation-surface navigation-surface--desktop"
				data-navigation-surface="desktop"
			>
				<div class="rail-head">
					<a href="/" data-gosx-link class="site-brand" aria-label={data.league.name + " league home"}>
						<span class="brand-badge">{data.league.short_code}</span>
						<span class="brand-copy">
							<strong>{data.league.name}</strong>
							<small>{data.league.tagline}</small>
						</span>
					</a>
				</div>
				<PrimaryNavigation
					SignedIn={data.viewer.signed_in}
					HasSeat={data.viewer.has_seat}
					SeatsOpen={data.league.fantasy_seats_open}
					CanClaimSeat={data.viewer.seat_claim_eligible}
					Commissioner={data.viewer.is_commissioner}
					Initials={data.viewer.initials}
					TeamName={data.viewer.team_name}
					CSRFToken={csrf.token}
				></PrimaryNavigation>
			</aside>
			<header class="mobile-navigation-enhanced" data-navigation-surface="mobile-enhanced-bar">
				<a href="/" data-gosx-link class="mobile-brand" aria-label={data.league.name + " league home"}>
					<span class="brand-badge">{data.league.short_code}</span>
					<strong>{data.league.name}</strong>
				</a>
				<button
					type="button"
					class="mobile-navigation-open"
					aria-label="Open league navigation"
					aria-controls="primary-navigation-dialog"
					aria-expanded="false"
					data-gosx-disclosure-target="#primary-navigation-dialog"
				>
					<span aria-hidden="true"></span>
				</button>
			</header>
			<div
				class="mobile-navigation-backdrop"
				data-gosx-disclosure-backdrop="#primary-navigation-dialog"
				hidden
				aria-hidden="true"
			></div>
			<aside
				id="primary-navigation-dialog"
				class="mobile-navigation-dialog"
				data-navigation-surface="mobile-enhanced-dialog"
				data-gosx-disclosure
				data-gosx-disclosure-modal
				hidden
				role="dialog"
				aria-modal="true"
				aria-labelledby="primary-navigation-title"
			>
				<div class="mobile-navigation-dialog__head">
					<div>
						<span class="brand-badge">{data.league.short_code}</span>
						<h2 id="primary-navigation-title">League navigation</h2>
					</div>
					<button
						type="button"
						class="mobile-navigation-close"
						aria-label="Close league navigation"
						data-gosx-disclosure-close="#primary-navigation-dialog"
						data-gosx-disclosure-initial-focus
					>
						<span aria-hidden="true">&#10005;</span>
					</button>
				</div>
				<PrimaryNavigation
					SignedIn={data.viewer.signed_in}
					HasSeat={data.viewer.has_seat}
					SeatsOpen={data.league.fantasy_seats_open}
					CanClaimSeat={data.viewer.seat_claim_eligible}
					Commissioner={data.viewer.is_commissioner}
					Initials={data.viewer.initials}
					TeamName={data.viewer.team_name}
					CSRFToken={csrf.token}
				></PrimaryNavigation>
			</aside>
			<details class="mobile-navigation-static" data-navigation-surface="mobile-static">
				<summary>
					<span class="mobile-brand">
						<span class="brand-badge">{data.league.short_code}</span>
						<strong>{data.league.name}</strong>
					</span>
					<span class="mobile-navigation-static__label mono">MENU</span>
				</summary>
				<PrimaryNavigation
					SignedIn={data.viewer.signed_in}
					HasSeat={data.viewer.has_seat}
					SeatsOpen={data.league.fantasy_seats_open}
					CanClaimSeat={data.viewer.seat_claim_eligible}
					Commissioner={data.viewer.is_commissioner}
					Initials={data.viewer.initials}
					TeamName={data.viewer.team_name}
					CSRFToken={csrf.token}
				></PrimaryNavigation>
			</details>
		</If>
		<If cond={(data.viewer.signed_in || data.viewer.demo) == false}>
			<header class="minimal-bar">
				<a href="/" data-gosx-link class="site-brand" aria-label={data.league.name + " league home"}>
					<span class="brand-badge">{data.league.short_code}</span>
					<span class="brand-copy">
						<strong>{data.league.name}</strong>
						<small>{data.league.tagline}</small>
					</span>
				</a>
				<nav class="minimal-actions" aria-label="Public navigation">
					<a href="/guide" data-gosx-link class="access-link access-link--guide">Manager guide</a>
					<a href="/login" data-gosx-link class="access-link" aria-label="League access">
						<span class="signal-mark" aria-hidden="true"></span>
						League access
					</a>
				</nav>
			</header>
		</If>
		<If cond={(data.viewer.signed_in || data.viewer.demo) && data.league.latest_announcement.has}>
			<div class="announcement-banner" role="status">
				<span class="announcement-banner__label mono">COMMISSIONER NOTE</span>
				<p>{data.league.latest_announcement.body}</p>
				<span class="announcement-banner__time mono">
					{data.league.latest_announcement.posted_at}
				</span>
			</div>
		</If>
		<div class="site-frame">
			<Slot />
		</div>
		<footer class="site-footer">
			<div>
				<p>
					<strong>{data.league.name}</strong>
					<If cond={data.league.has_footer_line}>
						//
						{data.league.footer_line}
					</If>
				</p>
				<nav aria-label="Footer">
					<a href="/privacy" data-gosx-link>Privacy</a>
					<a href="/terms" data-gosx-link>Terms</a>
					<a href="/open-source" data-gosx-link>Run your own league →</a>
				</nav>
			</div>
			<div class="footer-status">
				<If cond={data.league.matchup_footer_live}>
					<span class="live-dot" aria-hidden="true"></span>
				</If>
				{data.league.matchup_footer_label}
			</div>
		</footer>
	</div>
}
