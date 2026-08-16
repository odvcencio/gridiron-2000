package board

func BoardRow(props any) Node {
	return <article class="board-row" data-picked={props.player.picked}>
		<span class="pool-rank mono">{props.player.board_rank}</span>
		<div class="pool-player pool-player--photo">
			<If cond={props.player.has_headshot}>
				<img class="player-headshot" src={props.player.headshot} alt="" loading="lazy" />
			</If>
			<div class="pool-player__text">
				<strong>{props.player.name}</strong>
				<small>{props.player.detail}</small>
			</div>
		</div>
		<span class="position-chip">{props.player.position}</span>
		<b class="mono">{props.player.projection}</b>
		<div class="board-controls">
			<form method="post" action={props.MoveAction} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="player_id" value={props.player.id}></input>
				<input type="hidden" name="direction" value="up"></input>
				<button class="board-button" type="submit" aria-label={"Move " + props.player.name + " up"}>▲</button>
			</form>
			<form method="post" action={props.MoveAction} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="player_id" value={props.player.id}></input>
				<input type="hidden" name="direction" value="down"></input>
				<button class="board-button" type="submit" aria-label={"Move " + props.player.name + " down"}>▼</button>
			</form>
			<form method="post" action={props.RemoveAction} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
				<input type="hidden" name="csrf_token" value={props.CSRF}></input>
				<input type="hidden" name="player_id" value={props.player.id}></input>
				<button class="board-button board-button--cut" type="submit" aria-label={"Remove " + props.player.name}>✕</button>
			</form>
		</div>
	</article>
}

func Page() Node {
	return <main class="page board-page" id="main-content">
		<section class="draft-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="live-dot" aria-hidden="true"></span>
					PERSONAL BIG BOARD
				</span>
				<h1>
					RANK IT
					<br></br>
					YOUR WAY.
				</h1>
				<p>
					Your private draft order. The draft room surfaces your top available names when you are on the clock.
				</p>
			</div>
			<div class="draft-clock-panel">
				<span>Players on your board</span>
				<strong class="mono">{data.board_count}</strong>
				<div class="draft-clock-meta">
					<a href="/draft" data-gosx-link>Draft room →</a>
					<If cond={data.is_commissioner}>
						<a href="/admin" data-gosx-link>Admin console →</a>
					</If>
				</div>
			</div>
		</section>
		<div class="notice-stack" aria-live="polite">
			<If cond={data.has_notice}>
				<p class="flash-message">{data.notice}</p>
			</If>
			<If cond={data.has_board_error}>
				<p class="error-message">{data.board_error}</p>
			</If>
			<If cond={data.can_edit == false}>
				<p class="demo-message">
					<strong>SIGN IN REQUIRED:</strong>
					use League access to build a board tied to your seat.
				</p>
			</If>
			<If cond={data.pool_live == false}>
				<p class="demo-message">
					<strong>OFFLINE POOL:</strong>
					player ranks are approximate until the live pool syncs.
				</p>
			</If>
		</div>
		<div class="board-workspace">
			<section class="player-pool">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">01 // YOUR BOARD</span>
						<h2>Ranked queue</h2>
					</div>
					<If cond={data.board_count > 0}>
						<form method="post" action={actionPath("board-clear")} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<button class="filter-button" type="submit">Clear board</button>
						</form>
					</If>
				</div>
				<If cond={data.board_count == 0}>
					<div class="empty-tape">
						<strong>NO PLAYERS RANKED</strong>
						<p>
							Add players from the pool on the right. Your order is saved to your seat.
						</p>
					</div>
				</If>
				<div class="pool-list">
					<Each of={data.board} as="entry">
						<BoardRow
							player={entry}
							MoveAction={actionPath("board-move")}
							RemoveAction={actionPath("board-remove")}
							CSRF={csrf.token}
						 />
					</Each>
				</div>
			</section>
			<section class="player-pool">
				<div class="pool-toolbar">
					<div>
						<span class="section-index">02 // PLAYER POOL</span>
						<h2>Add to board</h2>
					</div>
				</div>
				<div class="pool-search-bar">
					<label class="mono" for="board-search">QUERY //</label>
					<input
						id="board-search"
						type="search"
						data-pool-search="true"
						placeholder="Search player, team, or position"
						autocomplete="off"
					 />
				</div>
				<div class="pool-list pool-list--tall">
					<Each of={data.available} as="player">
						<article class="pool-row" data-player-position={player.position} data-search={player.search}>
							<span class="pool-rank mono">{player.rank}</span>
							<div class="pool-player pool-player--photo">
								<If cond={player.has_headshot}>
									<img class="player-headshot" src={player.headshot} alt="" loading="lazy" />
								</If>
								<div class="pool-player__text">
									<strong>{player.name}</strong>
									<small>{player.detail}</small>
								</div>
							</div>
							<span class="position-chip">{player.position}</span>
							<b class="mono">{player.projection}</b>
							<form method="post" action={actionPath("board-add")} data-gosx-form="true" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form">
								<input type="hidden" name="csrf_token" value={csrf.token}></input>
								<input type="hidden" name="player_id" value={player.id}></input>
								<If cond={data.can_edit}>
									<button class="draft-button" type="submit">Add</button>
								</If>
								<If cond={data.can_edit == false}>
									<button class="draft-button" type="button" disabled="disabled">Locked</button>
								</If>
							</form>
						</article>
					</Each>
				</div>
			</section>
		</div>
	</main>
}
