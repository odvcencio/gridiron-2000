package opensource

func Page() Node {
	return <main class="legal-page" id="main-content">
		<span class="signal-label">
			<span class="signal-mark" aria-hidden="true"></span>
			DOCUMENT // OPEN SOURCE
		</span>
		<p class="page-kicker">For developers, not managers</p>
		<h1>
			OPEN
			<br></br>
			SOURCE.
		</h1>
		<div class="oss-invite">
			<span class="section-index">RUN YOUR OWN</span>
			<p>
				This whole league room is open source: GoSX server, snake draft, {data.league.format_blurb}, team-seat big boards, a player pool with optional live data, and a commissioner console. One Go binary, your own data, MIT licensed.
			</p>
			<div class="hero-actions">
				<a href="https://github.com/odvcencio/gridiron-2000" rel="noreferrer" class="button button--compact">Get the source ↗</a>
				<a href="https://github.com/odvcencio/gosx" rel="noreferrer" class="button button--compact">Built with GoSX ↗</a>
			</div>
		</div>
		<section>
			<h2>Three cost tiers</h2>
			<p>
				The corrected nflverse ledger gives next-morning truth for $0, with no live-data account at all. Tank01's free tier adds a $0 slow-live feed, about every 30 minutes on game days. Tank01 Ultra adds the 10-second live experience for about $25 a month for the whole league. The live feed only ever affects freshness — it never decides a closed week's score.
			</p>
		</section>
		<section>
			<h2>Data attribution</h2>
			<p>
				Player stats, schedules, and injury reports come from {data.attribution_name}, licensed {data.attribution_license}.
			</p>
			<a href={data.attribution_url} target="_blank" rel="noreferrer" class="wire-license">
				{data.attribution_name} · {data.attribution_license} ↗
			</a>
		</section>
		<a href="/" data-gosx-link class="button button--primary">Return to the league</a>
	</main>
}
