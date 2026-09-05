package locker

// Page is the /locker route (GC-4): every admitted member — seatless
// managers included — can read and post. The compose form lives OUTSIDE
// the auto-refreshing board region below, so a hub-triggered refetch from
// another member's post never clobbers a manager's own in-progress text; a
// reply or remove form has no such choice (it must sit next to its own
// post ID, with no bespoke JavaScript to relocate it), so those live
// inside the region and accept the same "a live refetch can happen while
// you are mid-reply" tradeoff pickem's picks region already does.
func Page() Node {
	return <main class="page board-page" id="main-content">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="signal-mark" aria-hidden="true"></span>
					LOCKER ROOM
				</span>
				<h1>Locker Room</h1>
				<p>
					<strong>Talk to your league.</strong> Post league business, trash talk, and updates. Every admitted member can read and post here — seatless managers included.
				</p>
			</div>
			<div class="draft-clock-panel">
				<span>Posts on record</span>
				<strong class="mono">{data.posts_count}</strong>
				<div class="draft-clock-meta">
					<span class="mono">League time · {data.timezone}</span>
				</div>
			</div>
		</section>

		<div class="locker-notice">
			<If cond={data.demo_mode}>
				<p class="demo-message">
					<strong>REHEARSAL MODE:</strong>
					posting and moderation are read-only while demo mode is on. Sign in to participate.
				</p>
			</If>
			<If cond={data.has_notice}><p class="flash-message">{data.notice}</p></If>
			<If cond={data.has_locker_error}><p class="error-message">{data.locker_error}</p></If>
		</div>

		<section class="wire-submit-panel" id="locker-composer">
			<header>
				<span class="section-index">LOCKER ROOM // NEW POST</span>
				<b>Say something to the league</b>
			</header>
			<If cond={data.can_post}>
				<form id="locker-post-form" method="post" action={data.locker_post_action} data-gosx-managed="true">
					<input type="hidden" name="csrf_token" value={data.csrf_token}></input>
					<If cond={data.page > 1}><input type="hidden" name="page" value={data.page}></input></If>
					<label>
						<span>What's on your mind?</span>
						<textarea name="body" maxlength="1000" rows="4" placeholder="Talk trash, plan a trade, keep the league honest…" required="required"></textarea>
					</label>
					<button class="button button--primary" type="submit">Post</button>
				</form>
			</If>
			<If cond={data.can_post == false}>
				<p>{data.read_only_reason}</p>
				<If cond={data.demo_mode}>
					<a class="button button--primary" href="/login">Sign in with Google</a>
				</If>
			</If>
		</section>

		<div
			id="locker-board-region"
			data-gosx-region
			data-gosx-region-url={data.locker_fragment_url}
			data-gosx-region-on="locker:changed"
			aria-label="Locker Room posts"
		>
			<LockerBoard></LockerBoard>
		</div>
	</main>
}

// LockerBoard is the server-rendered body of the board region: the
// paginated, newest-first post list with one level of flat replies. Page()
// and the /locker/fragment handler share it verbatim, the same pattern
// app/activity's ActivityRegion already establishes.
func LockerBoard() Node {
	return <section class="player-pool">
		<div class="pool-toolbar">
			<div>
				<span class="section-index">01 // THE BOARD</span>
				<h2>Every post</h2>
			</div>
		</div>
		<If cond={data.has_posts == false}>
			<div class="empty-tape">
				<strong>NO POSTS YET</strong>
				<p>Be the first to say something. Every admitted member can read and post here.</p>
			</div>
		</If>
		<div class="locker-feed">
			<Each of={data.posts} as="post">
				<div class="locker-post" id={post.ID}>
					<If cond={post.Removed}>
						<p class="scoring-note">{post.RemovedLabel}</p>
					</If>
					<If cond={post.Removed == false}>
						<p class="locker-post__meta mono">
							<strong>{post.AuthorLabel}</strong>
							<time>{post.TimeLabel}</time>
						</p>
						<p class="locker-post__body">{post.Body}</p>
						<If cond={post.CanRemove}>
							<form method="post" action={data.locker_remove_action} data-gosx-managed="true" class="locker-post__remove">
								<input type="hidden" name="csrf_token" value={data.csrf_token}></input>
								<input type="hidden" name="post_id" value={post.ID}></input>
								<If cond={data.page > 1}><input type="hidden" name="page" value={data.page}></input></If>
								<details class="action-confirmation">
									<summary>Remove post</summary>
									<p>
										Removing replaces this post with a removal notice for the whole league. This cannot be undone from this screen.
									</p>
									<label>
										<input type="checkbox" name="confirmation" value="remove-locker-item" required="required"></input>
										I understand this post cannot be restored.
									</label>
									<button class="board-button" type="submit">Confirm remove</button>
								</details>
							</form>
						</If>
					</If>
					<If cond={data.can_post}>
						<form method="post" action={data.locker_post_action} data-gosx-managed="true" class="locker-post__reply-form">
							<input type="hidden" name="csrf_token" value={data.csrf_token}></input>
							<input type="hidden" name="parent_id" value={post.ID}></input>
							<If cond={data.page > 1}><input type="hidden" name="page" value={data.page}></input></If>
							<textarea name="body" maxlength="1000" rows="2" placeholder="Reply…" required="required"></textarea>
							<button class="filter-button" type="submit">Reply</button>
						</form>
					</If>
					<div class="locker-replies">
						<Each of={post.Replies} as="reply">
							<div class="locker-reply" id={reply.ID}>
								<If cond={reply.Removed}>
									<p class="scoring-note">{reply.RemovedLabel}</p>
								</If>
								<If cond={reply.Removed == false}>
									<p class="locker-post__meta mono">
										<strong>{reply.AuthorLabel}</strong>
										<time>{reply.TimeLabel}</time>
									</p>
									<p class="locker-post__body">{reply.Body}</p>
									<If cond={reply.CanRemove}>
										<form method="post" action={data.locker_remove_action} data-gosx-managed="true" class="locker-post__remove">
											<input type="hidden" name="csrf_token" value={data.csrf_token}></input>
											<input type="hidden" name="post_id" value={reply.ID}></input>
											<If cond={data.page > 1}><input type="hidden" name="page" value={data.page}></input></If>
											<details class="action-confirmation">
												<summary>Remove reply</summary>
												<p>
													Removing replaces this reply with a removal notice for the whole league. This cannot be undone from this screen.
												</p>
												<label>
													<input type="checkbox" name="confirmation" value="remove-locker-item" required="required"></input>
													I understand this reply cannot be restored.
												</label>
												<button class="board-button" type="submit">Confirm remove</button>
											</details>
										</form>
									</If>
								</If>
							</div>
						</Each>
					</div>
				</div>
			</Each>
		</div>
		<nav class="pool-pagination" aria-label="Locker Room pages">
			<If cond={data.has_previous}><a class="filter-button" href={data.previous_href} data-gosx-link rel="prev">← Previous</a></If>
			<span class="mono">Page {data.page} / {data.pages}</span>
			<If cond={data.has_next}><a class="filter-button" href={data.next_href} data-gosx-link rel="next">Next →</a></If>
		</nav>
	</section>
}
