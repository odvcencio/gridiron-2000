package trades

func Page() Node {
	return <main class="page board-page" id="main-content" data-gosx-revalidate-interval="4s" data-gosx-revalidate-src="/api/league/version">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					TRADE DESK
				</span>
				<h1>
					MAKE YOUR
					<br></br>
					MOVE.
				</h1>
				<p>
					Propose, counter, and settle trades with the rest of the league. Every executed deal posts to the transaction feed.
				</p>
			</div>
			<div class="draft-clock-panel">
				<span>Veto policy</span>
				<strong class="mono">{data.veto_mode}</strong>
				<div class="draft-clock-meta">
					<a href="/activity" data-gosx-link>Transaction feed →</a>
					<a href="/team" data-gosx-link>Team terminal →</a>
				</div>
			</div>
		</section>
		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_notice}>
				<p class="flash-message">{data.notice}</p>
			</If>
			<If cond={data.has_trades_error}>
				<p class="error-message">{data.trades_error}</p>
			</If>
			<If cond={data.can_edit == false}>
				<p class="demo-message">
					<strong>SIGN IN REQUIRED:</strong>
					use League access to propose or respond to trades for your seat.
				</p>
			</If>
		</div>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">01 // COMPOSE</span>
					<h2>Propose a trade</h2>
				</div>
			</div>
			<div class="position-filters" aria-label="Choose a trade partner">
				<Each of={data.counterparties} as="team">
					<a href={"/trades?counterparty=" + team.id} data-gosx-link class="filter-button" aria-pressed={team.id == data.compose_counterparty_id}>{team.name}</a>
				</Each>
			</div>
			<If cond={data.compose_active == false}>
				<div class="empty-tape">
					<strong>CHOOSE A TRADE PARTNER</strong>
					<p>
						Pick a team above to build an offer against their roster.
					</p>
				</div>
			</If>
			<If cond={data.compose_active}>
				<form method="post" action={actionPath("trade-propose")} data-gosx-managed="true" class="trade-composer">
					<input type="hidden" name="csrf_token" value={csrf.token}></input>
					<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
					<input type="hidden" name="to_team_id" value={data.compose_counterparty_id}></input>
					<div class="trade-composer__sides">
						<div class="trade-composer__side">
							<h3>You give</h3>
							<If cond={data.my_options_empty}>
								<p class="empty-tape">Your roster is empty.</p>
							</If>
							<Each of={data.my_options} as="opt">
								<label class="trade-composer__option">
									<input type="checkbox" name="give" value={opt.id}></input>
									{opt.label}
								</label>
							</Each>
						</div>
						<div class="trade-composer__side">
							<h3>{"You get from " + data.compose_counterparty_name}</h3>
							<If cond={data.compose_options_empty}>
								<p class="empty-tape">Their roster is empty.</p>
							</If>
							<Each of={data.compose_options} as="opt">
								<label class="trade-composer__option">
									<input type="checkbox" name="get" value={opt.id}></input>
									{opt.label}
								</label>
							</Each>
						</div>
					</div>
					<label class="trade-composer__note">
						Note (optional)
						<textarea name="note" maxlength={data.note_max} rows="2" placeholder="Add a note for the other manager..."></textarea>
					</label>
					<button class="draft-button" type="submit">Send offer</button>
				</form>
			</If>
		</section>
		<section class="player-pool" id="inbox">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">02 // INBOX</span>
					<h2>Offers waiting on you</h2>
				</div>
			</div>
			<If cond={data.inbox_empty}>
				<div class="empty-tape">
					<strong>NO INCOMING OFFERS</strong>
					<p>
						Nothing waiting on your response right now.
					</p>
				</div>
			</If>
			<div class="pool-list">
				<Each of={data.inbox} as="offer">
					<article class="pool-row trade-row">
						<div class="pool-player__text">
							<strong>{"From " + offer.from_team}</strong>
							<small>
								<Each of={offer.give} as="p">
									{p.name + " (" + p.position + ") "}
								</Each>
								→ you send
								<Each of={offer.get} as="p">
									{" " + p.name + " (" + p.position + ")"}
								</Each>
							</small>
							<If cond={offer.has_note}>
								<small>"{offer.note}"</small>
							</If>
						</div>
						<div class="board-controls">
							<form method="post" action={actionPath("trade-accept")} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
								<input type="hidden" name="offer_id" value={offer.id}></input>
								<button class="draft-button" type="submit">Accept</button>
							</form>
							<form method="post" action={actionPath("trade-decline")} data-gosx-managed="true">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
								<input type="hidden" name="offer_id" value={offer.id}></input>
								<button class="board-button board-button--cut" type="submit">Decline</button>
							</form>
						</div>
						<details class="trade-counter-details">
							<summary>Counter</summary>
							<form method="post" action={actionPath("trade-counter")} data-gosx-managed="true" class="trade-composer">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
								<input type="hidden" name="offer_id" value={offer.id}></input>
								<div class="trade-composer__sides">
									<div class="trade-composer__side">
										<h3>You give</h3>
										<Each of={data.my_options} as="opt">
											<label class="trade-composer__option">
												<input type="checkbox" name="give" value={opt.id}></input>
												{opt.label}
											</label>
										</Each>
									</div>
									<div class="trade-composer__side">
										<h3>{"You get from " + offer.from_team}</h3>
										<Each of={offer.give} as="p">
											<label class="trade-composer__option">
												<input type="checkbox" name="get" value={p.id}></input>
												{p.name + " (" + p.position + ")"}
											</label>
										</Each>
									</div>
								</div>
								<label class="trade-composer__note">
									Note (optional)
									<textarea name="note" maxlength={data.note_max} rows="2"></textarea>
								</label>
								<button class="draft-button" type="submit">Send counter</button>
							</form>
						</details>
					</article>
				</Each>
			</div>
		</section>
		<section class="player-pool" id="outbox">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">03 // OUTBOX</span>
					<h2>Your offers</h2>
				</div>
			</div>
			<If cond={data.outbox_empty}>
				<div class="empty-tape">
					<strong>NO OPEN OR PENDING OFFERS</strong>
					<p>
						Offers you send, and offers under commissioner or league review, show here.
					</p>
				</div>
			</If>
			<div class="pool-list">
				<Each of={data.outbox} as="offer">
					<article class="pool-row trade-row">
						<div class="pool-player__text">
							<strong>{"To " + offer.to_team + " · " + offer.status}</strong>
							<small>
								You send
								<Each of={offer.give} as="p">
									{" " + p.name + " (" + p.position + ")"}
								</Each>
								for
								<Each of={offer.get} as="p">
									{" " + p.name + " (" + p.position + ")"}
								</Each>
							</small>
							<If cond={offer.status == "accepted"}>
								<small>Review window ends {offer.review_deadline} · {offer.vetoes_count} of {offer.vetoes_threshold} vetoes filed</small>
							</If>
						</div>
						<div class="board-controls">
							<If cond={offer.can_withdraw}>
								<form method="post" action={actionPath("trade-withdraw")} data-gosx-managed="true">
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
									<input type="hidden" name="offer_id" value={offer.id}></input>
									<button class="board-button board-button--cut" type="submit">Withdraw</button>
								</form>
							</If>
						</div>
					</article>
				</Each>
			</div>
		</section>
		<If cond={data.is_commissioner}>
			<section class="player-pool" id="review">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">04 // COMMISSIONER REVIEW</span>
						<h2>Trades awaiting a decision</h2>
					</div>
				</div>
				<If cond={data.review_empty}>
					<div class="empty-tape">
						<strong>NOTHING TO REVIEW</strong>
						<p>
							No accepted trade is currently in its review window.
						</p>
					</div>
				</If>
				<div class="pool-list">
					<Each of={data.review} as="offer">
						<article class="pool-row trade-row">
							<div class="pool-player__text">
								<strong>{offer.from_team + " ↔ " + offer.to_team}</strong>
								<small>
									{offer.from_team} sends
									<Each of={offer.give} as="p">
										{" " + p.name + " (" + p.position + ")"}
									</Each>
									for
									<Each of={offer.get} as="p">
										{" " + p.name + " (" + p.position + ")"}
									</Each>
								</small>
								<small>Review window ends {offer.review_deadline} · {offer.vetoes_count} of {offer.vetoes_threshold} vetoes filed</small>
							</div>
							<div class="board-controls">
								<form method="post" action={actionPath("trade-approve")} data-gosx-managed="true">
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="offer_id" value={offer.id}></input>
									<button class="draft-button" type="submit">Approve</button>
								</form>
								<form method="post" action={actionPath("trade-veto-commissioner")} data-gosx-managed="true">
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="offer_id" value={offer.id}></input>
									<button class="board-button board-button--cut" type="submit">Veto</button>
								</form>
							</div>
						</article>
					</Each>
				</div>
			</section>
		</If>
		<If cond={data.vote_panel_empty == false}>
			<section class="player-pool" id="vote">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">05 // LEAGUE VOTE</span>
						<h2>Trades open for a veto vote</h2>
					</div>
				</div>
				<div class="pool-list">
					<Each of={data.vote_panel} as="offer">
						<article class="pool-row trade-row">
							<div class="pool-player__text">
								<strong>{offer.from_team + " ↔ " + offer.to_team}</strong>
								<small>
									{offer.from_team} sends
									<Each of={offer.give} as="p">
										{" " + p.name + " (" + p.position + ")"}
									</Each>
									for
									<Each of={offer.get} as="p">
										{" " + p.name + " (" + p.position + ")"}
									</Each>
								</small>
								<small>{offer.vetoes_count} of {offer.vetoes_threshold} vetoes filed · window ends {offer.review_deadline}</small>
							</div>
							<div class="board-controls">
								<If cond={offer.already_voted}>
									<span class="position-chip">VOTE RECORDED</span>
								</If>
								<If cond={offer.can_vote}>
									<form method="post" action={actionPath("trade-veto-vote")} data-gosx-managed="true">
										<input type="hidden" name="csrf_token" value={csrf.token}></input>
										<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
										<input type="hidden" name="offer_id" value={offer.id}></input>
										<button class="board-button board-button--cut" type="submit">File veto vote</button>
									</form>
								</If>
							</div>
						</article>
					</Each>
				</div>
			</section>
		</If>
	</main>
}
