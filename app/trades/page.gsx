package trades

func Page() Node {
	return <main class="page board-page" id="main-content">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="signal-mark" aria-hidden="true"></span>
					TRADE DESK
				</span>
				<h1>Trades</h1>
				<p>
					<strong>Make your move.</strong> Propose, counter, and settle trades with the rest of the league. Every executed deal posts to the transaction feed.
				</p>
			</div>
			<div class="draft-clock-panel">
				<strong class="mono">{data.veto_policy_label}</strong>
				<div class="draft-clock-meta">
					<div class="trades-veto-links">
						<a href="/activity" data-gosx-link>Transaction feed →</a>
						<If cond={data.can_edit}>
							<a href="/team" data-gosx-link>Team terminal →</a>
						</If>
						<If cond={data.can_edit == false}>
							<If cond={data.public_entry.can_claim || data.public_entry.action_href != "/join"}>
								<a href={data.public_entry.action_href} data-gosx-link>
									{data.public_entry.action_label}
								</a>
							</If>
						</If>
						<If cond={data.public_entry.is_commissioner}>
							<a href={data.public_entry.commissioner_href} data-gosx-link>{data.public_entry.commissioner_label}</a>
						</If>
					</div>
				</div>
			</div>
		</section>
		<div class="notice-stack" aria-live="polite">
			<If cond={data.demo_mode}>
				<p class="demo-message">
					<strong>REHEARSAL MODE:</strong>
					the console is open to everyone while demo mode is on.
				</p>
			</If>
			<If cond={data.has_notice}>
				<p class="flash-message">{data.notice}</p>
			</If>
			<If cond={data.has_trades_error}>
				<p class="error-message">{data.trades_error}</p>
			</If>
			<If cond={data.can_edit == false}>
				<p class="demo-message">
					<strong>{data.public_entry.state_label}:</strong>
					{data.public_entry.detail}
					<If cond={data.public_entry.can_claim || data.public_entry.action_href != "/join"}>
						<a href={data.public_entry.action_href} data-gosx-link>
							{data.public_entry.action_label}
						</a>
					</If>
				</p>
			</If>
			<If cond={data.trade_deadline_passed}>
				<p class="demo-message">
					<strong>TRADE DEADLINE CLOSED:</strong>
					New offers, counters, and acceptances are closed. The deadline passed
					{data.trade_deadline}
					(
					{data.trade_deadline_relative}
					). Existing offers can still be declined or withdrawn.
				</p>
			</If>
			<If cond={data.trade_deadline_configured && data.trade_deadline_passed == false}>
				<p class="demo-message">
					<strong>TRADE CREATION OPEN:</strong>
					New offers, counters, and acceptances close
					{data.trade_deadline}
					(
					{data.trade_deadline_relative}
					). Existing offers remain available for review, decline, or withdrawal.
				</p>
			</If>
		</div>
		<div
			id="trades-live-region"
			data-gosx-region
			data-gosx-region-url={data.trades_fragment_url}
			data-gosx-region-interval={data.trades_fragment_interval}
			data-gosx-region-signal="$trades.state.refresh"
			aria-label="Authoritative trade desk"
		>
			<TradeDeskRegion></TradeDeskRegion>
		</div>
		<p class="scoring-note lineup-sync-note" role="status" aria-live="polite">
			Trade Desk state refreshes automatically within 4 seconds after a managed trade result. If a refresh fails, use
			<button
				type="button"
				class="board-button"
				data-gosx-set="$trades.state.refresh"
				data-gosx-set-value="manual"
			>Refresh trades now</button>
			.
		</p>
	</main>
}
func TradeDeskRegion() Node {
	return <div>
		<section class="player-pool">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">01 // COMPOSE</span>
					<h2>Propose a trade</h2>
				</div>
			</div>
			<If cond={data.can_edit == false}>
				<div class="empty-tape">
					<strong>{data.public_entry.state_label}</strong>
					<p>
						{data.public_entry.detail}
					</p>
					<If cond={data.public_entry.can_claim || data.public_entry.action_href != "/join"}>
						<a href={data.public_entry.action_href} data-gosx-link>
							{data.public_entry.action_label}
						</a>
					</If>
				</div>
			</If>
			<If cond={data.can_compose}>
				<div class="position-filters" aria-label="Choose a trade partner">
					<Each of={data.counterparties} as="team">
						<a
							href={"/trades?counterparty=" + team.ID}
							data-gosx-link
							class="filter-button"
							aria-current={team.ID == data.compose_counterparty_id}
						>{team.Name}</a>
					</Each>
				</div>
				<If cond={data.counterparties_empty}>
					<div class="empty-tape">
						<strong>NO MANAGED TRADE PARTNERS YET</strong>
						<p>
							Other franchises appear here after their managers claim them.
						</p>
					</div>
				</If>
				<If cond={data.counterparties_empty == false}>
					<If cond={data.compose_active == false}>
						<div class="empty-tape">
							<strong>CHOOSE A TRADE PARTNER</strong>
							<p>
								Pick a managed team above to build an offer against its current roster.
							</p>
						</div>
					</If>
				</If>
				<If cond={data.compose_active}>
					<form
						method="post"
						action={actionPath("trade-propose")}
						data-gosx-managed="true"
						data-gosx-action-signal="$trades.state.refresh"
						class="trade-composer"
					>
						<input type="hidden" name="csrf_token" value={csrf.token}></input>
						<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
						<input type="hidden" name="to_team_id" value={data.compose_counterparty_id}></input>
						<input type="hidden" name="counterparty" value={data.compose_counterparty_id}></input>
						<div class="trade-composer__sides">
							<div class="trade-composer__side">
								<h3>You give</h3>
								<If cond={data.my_options_empty}>
									<p class="empty-tape">Your roster is empty.</p>
								</If>
								<Each of={data.my_options} as="opt">
									<label class="trade-composer__option">
										<input type="checkbox" name="give" value={opt.ID} checked={opt.Selected}></input>
										{opt.Label}
									</label>
								</Each>
							</div>
							<div class="trade-composer__side">
								<h3>
									{"You get from " + data.compose_counterparty_name}
								</h3>
								<If cond={data.compose_options_empty}>
									<p class="empty-tape">Their roster is empty.</p>
								</If>
								<Each of={data.compose_options} as="opt">
									<label class="trade-composer__option">
										<input type="checkbox" name="get" value={opt.ID} checked={opt.Selected}></input>
										{opt.Label}
									</label>
								</Each>
							</div>
						</div>
						<label class="trade-composer__note">
							Note (optional)
							<textarea
								name="note"
								maxlength={data.note_max}
								rows="2"
								placeholder="Add a note for the other manager..."
							>{data.compose_note}</textarea>
						</label>
						<p class="error-message form-error" data-gosx-field-error="offer_id" aria-live="polite"></p>
						<button class="draft-button" type="submit">Send offer</button>
					</form>
				</If>
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
						{data.empty_inbox_message}
					</p>
				</div>
			</If>
			<div class="pool-list">
				<Each of={data.inbox} as="offer">
					<article class="rank-row rank-row--wide">
						<div class="pool-player__text">
							<strong>{"From " + offer.FromTeam}</strong>
							<small>
								<Each of={offer.Give} as="p">
									{p.Name + " (" + p.Position + ") "}
								</Each>
								→ you send
								<Each of={offer.Get} as="p">
									{" " + p.Name + " (" + p.Position + ")"}
								</Each>
							</small>
							<If cond={offer.HasNote}>
								<small>
									"
									{offer.Note}
									"
								</small>
							</If>
							<If cond={offer.HasExpiry}>
								<If cond={offer.ExpiryState == "upcoming"}>
									<small>
										Offer expires
										{offer.Expiry}
										(
										{offer.ExpiryRelative}
										).
									</small>
								</If>
								<If cond={offer.ExpiryState == "overdue"}>
									<small>
										Offer expiry passed
										{offer.Expiry}
										(
										{offer.ExpiryRelative}
										); waiting for cleanup.
									</small>
								</If>
								<If cond={offer.ExpiryState == "unknown"}>
									<small>
										Offer expiry unknown; creation time is unavailable.
									</small>
								</If>
							</If>
						</div>
						<div class="board-controls">
							<If cond={offer.CanAccept}>
								<form
									method="post"
									action={actionPath("trade-accept")}
									data-gosx-managed="true"
									data-gosx-action-signal="$trades.state.refresh"
								>
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
									<input type="hidden" name="counterparty" value={data.compose_counterparty_id}></input>
									<input type="hidden" name="offer_id" value={offer.ID}></input>
									<details class="action-confirmation">
										<summary>Accept this trade</summary>
										<p>
											Accepting records your agreement. This either opens the league review window or executes immediately, depending on league policy. The roster change cannot be undone from this screen.
										</p>
										<label>
											<input type="checkbox" name="confirmation" value="accept-trade" required="required"></input>
											I understand this commits the offer.
										</label>
										<button class="draft-button" type="submit">Confirm acceptance</button>
									</details>
								</form>
							</If>
							<If cond={offer.CanDecline}>
								<form
									method="post"
									action={actionPath("trade-decline")}
									data-gosx-managed="true"
									data-gosx-action-signal="$trades.state.refresh"
								>
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
									<input type="hidden" name="counterparty" value={data.compose_counterparty_id}></input>
									<input type="hidden" name="offer_id" value={offer.ID}></input>
									<details class="action-confirmation">
										<summary>Decline this trade</summary>
										<p>
											Declining closes this offer permanently. The sender can send a new offer, but this one cannot be reopened or reconsidered from this screen.
										</p>
										<label>
											<input type="checkbox" name="confirmation" value="decline-trade" required="required"></input>
											I understand this offer cannot be reopened.
										</label>
										<button class="board-button board-button--cut" type="submit">Confirm decline</button>
									</details>
								</form>
							</If>
						</div>
						<If cond={offer.CanCounter}>
							<details class="trade-counter-details">
								<summary>Counter</summary>
								<form
									method="post"
									action={actionPath("trade-counter")}
									data-gosx-managed="true"
									data-gosx-action-signal="$trades.state.refresh"
									class="trade-composer"
								>
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
									<input type="hidden" name="offer_id" value={offer.ID}></input>
									<input type="hidden" name="counterparty" value={offer.FromTeamID}></input>
									<div class="trade-composer__sides">
										<div class="trade-composer__side">
											<h3>You give</h3>
											<If cond={offer.CounterGiveOptionsEmpty}>
												<p class="empty-tape">Your current roster is empty.</p>
											</If>
											<Each of={offer.CounterGiveOptions} as="opt">
												<label class="trade-composer__option">
													<input type="checkbox" name="give" value={opt.ID} checked={opt.Selected}></input>
													{opt.Label}
												</label>
											</Each>
										</div>
										<div class="trade-composer__side">
											<h3>{"You get from " + offer.FromTeam}</h3>
											<If cond={offer.CounterGetOptionsEmpty}>
												<p class="empty-tape">Their current roster is empty.</p>
											</If>
											<Each of={offer.CounterGetOptions} as="opt">
												<label class="trade-composer__option">
													<input type="checkbox" name="get" value={opt.ID} checked={opt.Selected}></input>
													{opt.Label}
												</label>
											</Each>
										</div>
									</div>
									<label class="trade-composer__note">
										Note (optional)
										<textarea name="note" maxlength={data.note_max} rows="2">{offer.CounterNote}</textarea>
									</label>
									<If cond={offer.HasCounterRecovery}>
										<small class="form-recovery" aria-live="polite">
											Your previous counter selections and note were kept. Review them before resending.
										</small>
									</If>
									<p class="error-message form-error" data-gosx-field-error="offer_id" aria-live="polite"></p>
									<button class="draft-button" type="submit">Send counter</button>
								</form>
							</details>
						</If>
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
						Offers you send show here, open or accepted and awaiting review.
					</p>
				</div>
			</If>
			<div class="pool-list">
				<Each of={data.outbox} as="offer">
					<article class="rank-row rank-row--wide">
						<div class="pool-player__text">
							<strong>
								{"To " + offer.ToTeam + " · " + offer.StatusLabel}
							</strong>
							<small>
								You send
								<Each of={offer.Give} as="p">
									{" " + p.Name + " (" + p.Position + ")"}
								</Each>
								for
								<Each of={offer.Get} as="p">
									{" " + p.Name + " (" + p.Position + ")"}
								</Each>
							</small>
							<If cond={offer.Status == "accepted"}>
								<small>
									Review window ends
									{offer.ReviewDeadline}
									·
									{offer.VetoesCount}
									of
									{offer.VetoesThreshold}
									vetoes filed
								</small>
							</If>
							<If cond={offer.HasExpiry}>
								<If cond={offer.ExpiryState == "upcoming"}>
									<small>
										Offer expires
										{offer.Expiry}
										(
										{offer.ExpiryRelative}
										).
									</small>
								</If>
								<If cond={offer.ExpiryState == "overdue"}>
									<small>
										Offer expiry passed
										{offer.Expiry}
										(
										{offer.ExpiryRelative}
										); waiting for cleanup.
									</small>
								</If>
								<If cond={offer.ExpiryState == "unknown"}>
									<small>
										Offer expiry unknown; creation time is unavailable.
									</small>
								</If>
							</If>
						</div>
						<div class="board-controls">
							<If cond={offer.CanWithdraw}>
								<form
									method="post"
									action={actionPath("trade-withdraw")}
									data-gosx-managed="true"
									data-gosx-action-signal="$trades.state.refresh"
								>
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
									<input type="hidden" name="counterparty" value={data.compose_counterparty_id}></input>
									<input type="hidden" name="offer_id" value={offer.ID}></input>
									<button class="board-button board-button--cut" type="submit">Withdraw</button>
								</form>
							</If>
						</div>
					</article>
				</Each>
			</div>
		</section>
		<section class="player-pool" id="pending-review">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">04 // PENDING REVIEW</span>
					<h2>Offers you accepted</h2>
				</div>
			</div>
			<If cond={data.pending_review_empty}>
				<div class="empty-tape">
					<strong>NOTHING PENDING</strong>
					<p>
						Offers you received and accepted show here until the league review window closes.
					</p>
				</div>
			</If>
			<div class="pool-list">
				<Each of={data.pending_review} as="offer">
					<article class="rank-row rank-row--wide">
						<div class="pool-player__text">
							<strong>
								{"From " + offer.FromTeam + " · " + offer.StatusLabel}
							</strong>
							<small>
								You get
								<Each of={offer.Give} as="p">
									{" " + p.Name + " (" + p.Position + ")"}
								</Each>
								· you sent
								<Each of={offer.Get} as="p">
									{" " + p.Name + " (" + p.Position + ")"}
								</Each>
							</small>
							<If cond={offer.HasReviewDeadline}>
								<small>
									Review window ends
									{offer.ReviewDeadline}
									·
									{offer.VetoesCount}
									of
									{offer.VetoesThreshold}
									vetoes filed
								</small>
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
						<span class="section-index">05 // COMMISSIONER REVIEW</span>
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
						<article class="rank-row rank-row--wide">
							<div class="pool-player__text">
								<strong>
									{offer.FromTeam + " ↔ " + offer.ToTeam}
								</strong>
								<small>
									{offer.FromTeam}
									sends
									<Each of={offer.Give} as="p">
										{" " + p.Name + " (" + p.Position + ")"}
									</Each>
									for
									<Each of={offer.Get} as="p">
										{" " + p.Name + " (" + p.Position + ")"}
									</Each>
								</small>
								<small>
									Review window ends
									{offer.ReviewDeadline}
									·
									{offer.VetoesCount}
									of
									{offer.VetoesThreshold}
									vetoes filed
								</small>
							</div>
							<div class="board-controls">
								<form
									method="post"
									action={actionPath("trade-approve")}
									data-gosx-managed="true"
									data-gosx-action-signal="$trades.state.refresh"
								>
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="counterparty" value={data.compose_counterparty_id}></input>
									<input type="hidden" name="offer_id" value={offer.ID}></input>
									<details class="action-confirmation">
										<summary>Approve and execute</summary>
										<p>
											Approval executes this accepted trade immediately and moves both rosters. This commissioner action cannot be undone from this screen.
										</p>
										<label>
											<input type="checkbox" name="confirmation" value="approve-trade" required="required"></input>
											I understand this executes the trade.
										</label>
										<button class="draft-button" type="submit">Confirm approval</button>
									</details>
								</form>
								<form
									method="post"
									action={actionPath("trade-veto-commissioner")}
									data-gosx-managed="true"
									data-gosx-action-signal="$trades.state.refresh"
								>
									<input type="hidden" name="csrf_token" value={csrf.token}></input>
									<input type="hidden" name="counterparty" value={data.compose_counterparty_id}></input>
									<input type="hidden" name="offer_id" value={offer.ID}></input>
									<details class="action-confirmation">
										<summary>Veto this trade</summary>
										<p>
											Vetoing rejects this accepted offer and prevents execution. This commissioner decision cannot be undone from this screen.
										</p>
										<label>
											<input type="checkbox" name="confirmation" value="veto-trade" required="required"></input>
											I understand this rejects the offer.
										</label>
										<button class="board-button board-button--cut" type="submit">Confirm veto</button>
									</details>
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
						<span class="section-index">06 // LEAGUE VOTE</span>
						<h2>Trades open for a veto vote</h2>
					</div>
				</div>
				<div class="pool-list">
					<Each of={data.vote_panel} as="offer">
						<article class="rank-row rank-row--wide">
							<div class="pool-player__text">
								<strong>
									{offer.FromTeam + " ↔ " + offer.ToTeam}
								</strong>
								<small>
									{offer.FromTeam}
									sends
									<Each of={offer.Give} as="p">
										{" " + p.Name + " (" + p.Position + ")"}
									</Each>
									for
									<Each of={offer.Get} as="p">
										{" " + p.Name + " (" + p.Position + ")"}
									</Each>
								</small>
								<small>
									{offer.VetoesCount}
									of
									{offer.VetoesThreshold}
									vetoes filed · window ends
									{offer.ReviewDeadline}
								</small>
							</div>
							<div class="board-controls">
								<If cond={offer.AlreadyVoted}>
									<span class="position-chip">VOTE RECORDED</span>
								</If>
								<If cond={offer.CanVote}>
									<form
										method="post"
										action={actionPath("trade-veto-vote")}
										data-gosx-managed="true"
										data-gosx-action-signal="$trades.state.refresh"
									>
										<input type="hidden" name="csrf_token" value={csrf.token}></input>
										<input type="hidden" name="team_id" value={data.viewer.team_id}></input>
										<input type="hidden" name="counterparty" value={data.compose_counterparty_id}></input>
										<input type="hidden" name="offer_id" value={offer.ID}></input>
										<button class="board-button board-button--cut" type="submit">File veto vote</button>
									</form>
								</If>
							</div>
						</article>
					</Each>
				</div>
			</section>
		</If>
		<section class="player-pool" id="history">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">07 // HISTORY</span>
					<h2>Trade history</h2>
				</div>
			</div>
			<If cond={data.history_empty}>
				<div class="empty-tape">
					<strong>NO TERMINAL TRADE HISTORY</strong>
					<p>
						Executed, declined, withdrawn, countered, vetoed, expired, and failed offers appear here for the participating seats.
					</p>
				</div>
			</If>
			<If cond={data.history_empty == false}>
				<div class="pool-list">
					<Each of={data.history} as="offer">
						<article class="rank-row rank-row--wide">
							<div class="pool-player__text">
								<strong>{offer.StatusLabel}</strong>
								<small>
									{offer.FromTeam + " ↔ " + offer.ToTeam}
								</small>
								<small>
									Created
									{offer.CreatedAt}
								</small>
								<If cond={offer.ResolvedAt != ""}>
									<small>
										Resolved
										{offer.ResolvedAt}
									</small>
								</If>
								<small>
									Give:
									<Each of={offer.Give} as="p">
										{" " + p.Name + " (" + p.Position + ")"}
									</Each>
									· Get:
									<Each of={offer.Get} as="p">
										{" " + p.Name + " (" + p.Position + ")"}
									</Each>
								</small>
								<If cond={offer.HasNote}>
									<small>
										Note: "
										{offer.Note}
										"
									</small>
								</If>
								<If cond={offer.Status == "failed"}>
									<small>
										Failure reason:
										{offer.FailReason}
									</small>
								</If>
							</div>
						</article>
					</Each>
				</div>
			</If>
		</section>
	</div>
}
