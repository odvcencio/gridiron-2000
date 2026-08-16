package app

func Page() Node {
	return <main class="error-page" id="main-content">
		<span class="error-code mono">500 // SIGNAL LOST</span>
		<h1>
			THE BOOTH
			<br></br>
			WENT DARK.
		</h1>
		<p>
			The league system hit an unexpected error while rendering this page. The score feed and your saved state are separate, so a provider hiccup will not erase league data.
		</p>
		<a href="/" data-gosx-link class="button button--primary">Return to HQ</a>
	</main>
}
