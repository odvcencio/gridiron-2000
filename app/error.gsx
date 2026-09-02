package app

func Page() Node {
	return <main class="error-page" id="main-content">
		<span class="error-code mono">500 // SIGNAL LOST</span>
		<h1>
			THE BOOTH
			{" "}
			WENT DARK.
		</h1>
		<p>
			Something broke loading this page. Your league data is safe and nothing was lost.
		</p>
		<a href="/" data-gosx-link class="button button--primary">Return to HQ</a>
	</main>
}
