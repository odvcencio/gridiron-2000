package app

func Layout() Node {
	return <div class="app-shell">
		<a class="skip-link" href="#main-content">Skip to league content</a>
		<div class="ambient-grid" aria-hidden="true"></div>
		<header class="site-header">
			<a href="/" data-gosx-link class="site-brand" aria-label="GRIDIRON 2000 league home">
				<span class="brand-badge">G2K</span>
				<span class="brand-copy">
					<strong>GRIDIRON 2000</strong>
					<small>Pacific Fantasy System</small>
				</span>
			</a>
			<nav class="site-nav" aria-label="Primary navigation">
				<a href="/" data-gosx-link class="site-link">
					<span>01</span>
					HQ
				</a>
					<a href="/matchups" data-gosx-link class="site-link">
						<span>02</span>
						Live
					</a>
					<a href="/wire" data-gosx-link class="site-link">
						<span>03</span>
						Wire
					</a>
					<a href="/team" data-gosx-link class="site-link">
						<span>04</span>
						Team
					</a>
					<a href="/board" data-gosx-link class="site-link">
						<span>05</span>
						Board
					</a>
					<a href="/draft" data-gosx-link class="site-link site-link--hot">
						<span>06</span>
						Draft
				</a>
			</nav>
			<a href="/login" data-gosx-link class="access-link" aria-label="League access">
				<span class="access-light" aria-hidden="true"></span>
				League access
			</a>
		</header>
		<div class="site-frame">
			<Slot />
		</div>
		<footer class="site-footer">
			<div>
				<p>
					<strong>GRIDIRON 2000</strong>
					// Eight seats. One trophy. Permanent group-chat evidence.
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
