package app

// Layout renders the persistent chrome around every route's <Slot/>.
//
// Primary navigation is a left rail on desktop and a full-height drawer on
// mobile (see .site-rail / .drawer-* in styles.css for the breakpoint).
// The drawer's open/closed state is a checkbox (#nav-drawer). Rail links
// keep data-gosx-link for soft (client-side) navigation, including inside
// the drawer: verified empirically in the smoke server that the nav
// runtime re-renders this whole layout region on every navigation, soft
// or full, so #nav-drawer is a fresh, unchecked node afterward - the
// drawer never stays open. The same re-render is what lets aria-current
// (set by the nav runtime against data-gosx-link hrefs) highlight the
// active rail-link after a soft navigation.
func Layout() Node {
	return <div class="app-shell">
		<a class="skip-link" href="#main-content">Skip to league content</a>
		<div class="ambient-grid" aria-hidden="true"></div>

		<If cond={data.viewer.signed_in || data.viewer.demo}>
			<input type="checkbox" id="nav-drawer" class="drawer-check" aria-hidden="true"></input>

			<header class="mobile-bar">
				<a href="/" data-gosx-link class="mobile-brand" aria-label={data.league.name + " league home"}>
					<span class="brand-badge">{data.league.short_code}</span>
					<strong>{data.league.name}</strong>
				</a>
				<label for="nav-drawer" class="drawer-open" aria-label="Open menu">
					<span aria-hidden="true"></span>
				</label>
			</header>

			<label for="nav-drawer" class="drawer-backdrop" aria-hidden="true"></label>

			<aside class="site-rail">
				<div class="rail-head">
					<a href="/" data-gosx-link class="site-brand" aria-label={data.league.name + " league home"}>
						<span class="brand-badge">{data.league.short_code}</span>
						<span class="brand-copy">
							<strong>{data.league.name}</strong>
							<small>{data.league.tagline}</small>
						</span>
					</a>
					<label for="nav-drawer" class="drawer-close" aria-label="Close menu">
						<span aria-hidden="true">&#10005;</span>
					</label>
				</div>
				<nav class="rail-nav" aria-label="Primary navigation">
					<a href="/" data-gosx-link class="rail-link"><span class="rail-index mono">01</span>HQ</a>
					<a href="/matchups" data-gosx-link class="rail-link"><span class="rail-index mono">02</span>Live</a>
					<a href="/wire" data-gosx-link class="rail-link"><span class="rail-index mono">03</span>Wire</a>
					<a href="/team" data-gosx-link class="rail-link"><span class="rail-index mono">04</span>Team</a>
					<a href="/board" data-gosx-link class="rail-link"><span class="rail-index mono">05</span>Board</a>
					<a href="/scoring" data-gosx-link class="rail-link"><span class="rail-index mono">06</span>Rules</a>
					<a href="/pickem" data-gosx-link class="rail-link"><span class="rail-index mono">07</span>Pick'em</a>
					<a href="/blitz" data-gosx-link class="rail-link"><span class="rail-index mono">08</span>Blitz</a>
					<a href="/draft" data-gosx-link class="rail-link rail-link--hot"><span class="rail-index mono">09</span>Draft</a>
				</nav>
				<div class="rail-foot">
					<If cond={data.viewer.signed_in}>
						<div class="user-badge">
							<span class="user-chip mono">{data.viewer.initials}</span>
							<span class="user-name">{data.viewer.team_name}</span>
						</div>
						<div class="rail-foot-links">
							<a href="/team" data-gosx-link class="access-link">Team</a>
							<If cond={data.viewer.is_commissioner}>
								<a href="/admin" data-gosx-link class="access-link">Admin</a>
							</If>
							<form method="post" action="/auth/logout" data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<button class="access-link" type="submit">Sign out</button>
							</form>
						</div>
					</If>
					<If cond={data.viewer.signed_in == false}>
						<div class="user-badge">
							<span class="user-chip mono">{data.viewer.initials}</span>
							<span class="user-name">Rehearsal seat</span>
						</div>
						<a href="/login" data-gosx-link class="access-link" aria-label="League access">
							<span class="access-light" aria-hidden="true"></span>
							League access
						</a>
					</If>
				</div>
			</aside>
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
				<a href="/login" data-gosx-link class="access-link" aria-label="League access">
					<span class="access-light" aria-hidden="true"></span>
					League access
				</a>
			</header>
		</If>

		<div class="site-frame">
			<Slot />
		</div>
		<footer class="site-footer">
			<div>
				<p>
					<strong>{data.league.name}</strong>
					<If cond={data.league.has_footer_line}>
						// {data.league.footer_line}
					</If>
				</p>
				<nav aria-label="Legal">
					<a href="/privacy" data-gosx-link>Privacy</a>
					<a href="/terms" data-gosx-link>Terms</a>
				</nav>
			</div>
			<div class="footer-status">
				<span class="live-dot" aria-hidden="true"></span>
					20 SEC SIGNAL CHECK · LOCAL OPEN-DATA CACHE
			</div>
		</footer>
	</div>
}
