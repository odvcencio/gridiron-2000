package app

func Page() Node {
	return <main class="error-page" id="main-content">
		<span class="error-code mono">404 // WRONG CHANNEL</span>
		<h1>We could not find that page.</h1>
		<p>
			This page doesn't exist, the commissioner moved it, or someone traded it for a future second.
		</p>
		<nav class="guide-actions" aria-label="Not found actions">
			<a href="/" data-gosx-link class="button button--primary">Back to Home</a>
			<a href="/help" data-gosx-link class="button button--ghost">Search help</a>
		</nav>
	</main>
}
