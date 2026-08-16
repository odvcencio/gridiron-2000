package privacy

func Page() Node {
	return <main class="legal-page" id="main-content">
		<span class="signal-label">
			<span class="live-dot" aria-hidden="true"></span>
			DOCUMENT // PRIVACY
		</span>
		<p class="page-kicker">Last updated August 8, 2026</p>
		<h1>
			PRIVACY
			<br></br>
			PLAYBOOK.
		</h1>
		<section>
			<h2>What this league stores</h2>
			<p>
				When Google OAuth is configured, GRIDIRON 2000 stores the name, email address, and provider identifier returned by Google so one account can be mapped to one league seat. It also stores league actions such as draft readiness and picks.
			</p>
			<h2>Where it lives</h2>
			<p>
				The starter stores league state in the server-side JSON file configured by
				<span class="inline-code">DATA_FILE</span>
				. Authentication state lives in an encrypted, HTTP-only cookie. Google client secrets remain server-side environment variables.
			</p>
			<h2>Football data</h2>
			<p>
				The server reads the commissioner-approved public RSS, Atom, and Bluesky sources and caches open nflverse schedules, injury reports, and weekly player statistics. Source text is kept only in rewriteable current state; an upstream social deletion removes that text. The metadata journal retains provenance, hashes, rules, trust tiers, and timestamps so the league can audit its own classifier.
			</p>
			<p>
				A signed-in manager may submit a short summary, source name, optional link, and sighting type. Their display name is retained with the sighting inside this private league. Local data exports require a private bearer token. GRIDIRON 2000 does not store NFL+ or PrizePicks credentials and does not authenticate to betting or paid sports-data services.
			</p>
			<h2>Your controls</h2>
			<p>
				Ask the commissioner to remove your league assignment and stored identity. Signing out destroys your current authenticated session.
			</p>
		</section>
		<a href="/login" data-gosx-link class="button button--primary">Return to league access</a>
	</main>
}
