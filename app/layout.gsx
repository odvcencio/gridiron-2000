package app

// PrimaryNavigation is the single typed information architecture for every
// authenticated/demo navigation surface. Desktop rail, enhanced mobile
// dialog, and static no-JavaScript mobile disclosure all invoke this exact
// component so their groups, order, labels, and role gates cannot drift.
//
// Every link's title attribute (wave-6 audit item 10) repeats its own
// visible label. body:has(.draft-shell) .site-rail:not(:hover,
// :focus-within) (public/styles.css) collapses the rail to its 4rem icon
// column at rest, hiding every navigation-link's text and leaving only its
// two-digit index visible — title gives that collapsed state a native
// hover/focus tooltip naming the destination without touching the
// existing hover/focus expansion rule that already restores the full
// label on approach.
type PrimaryNavigationProps struct {
	SignedIn      bool
	HasSeat       bool
	SeatsOpen     bool
	CanClaimSeat  bool
	Commissioner  bool
	Initials      string
	TeamName      string
	CSRFToken     string
	// PickemHot/TradesHot and their AttentionText counterparts (build item
	// 2, rail-dot leftover) come pre-shaped from
	// data.league.attention.{pickem,trades}_hot/_attention_text
	// (internal/league/service.go attentionMap): PrimaryNavigation is a
	// legacy (non-island) GoSX component, and the Phase 4
	// filter()/startsWith() expression forms only lower for //gosx:island
	// bytecode, never the server-rendered legacy runtime — so the
	// route-prefix match against attention.items happens once in Go, not
	// per render here.
	PickemHot           bool
	PickemAttentionText string
	TradesHot           bool
	TradesAttentionText string
	// DraftComplete (wave 7, item 6) comes from data.league.draft_complete
	// (leagueMap, internal/league/service.go) — the same "one map every
	// page's data function already includes" property PickemHot/TradesHot
	// rely on — and gates the "Draft results" destination in the game-day
	// group below: truthful state, never shown before the draft is
	// actually complete.
	DraftComplete bool
}

// PrimaryNavigation's sign-out form posts to /auth/logout as a plain,
// unmanaged (data-gosx-managed="false") full-page submission — the same
// shape team/page.gsx's own avatar-upload form uses. POST /auth/logout is a
// raw http.HandlerFunc that always answers a plain 303 to /login with a
// rotated session cookie, never JSON. GoSX's managed-form runtime only
// performs a soft navigation when the parsed JSON result carries a
// "redirect" field (client/runtime/host/navigation.ts); a managed fetch
// instead followed the redirect itself, received HTML it could not parse as
// JSON, and left the browser on the current page with a generic "Action
// completed." toast and no URL change. Opting out lets the browser submit
// the form and follow the 303 natively.
func PrimaryNavigation(props PrimaryNavigationProps) Node {
	return <div class="primary-navigation">
		<nav class="primary-navigation__groups" aria-label="Primary navigation">
			<div class="navigation-group" data-navigation-group="today">
				<p class="navigation-group__label mono">TODAY</p>
				<Link href="/" class="navigation-link" title="Home">
					<span class="navigation-link__index mono">01</span>
					Home
				</Link>
				<If cond={props.HasSeat && props.PickemHot}>
					<Link href="/pickem" class="navigation-link navigation-link--hot" title="Pick'em">
						<span class="navigation-link__index mono">02</span>
						Pick'em
						<span class="visually-hidden">{props.PickemAttentionText}</span>
					</Link>
				</If>
				<If cond={(props.HasSeat && props.PickemHot) == false}>
					<Link href="/pickem" class="navigation-link" title="Pick'em">
						<span class="navigation-link__index mono">02</span>
						Pick'em
					</Link>
				</If>
				<Link href="/matchups" class="navigation-link" title="Matchups">
					<span class="navigation-link__index mono">03</span>
					Matchups
				</Link>
			</div>
			<div class="navigation-group" data-navigation-group="my-team">
				<p class="navigation-group__label mono">MY TEAM</p>
				<If cond={props.HasSeat}>
					<Link href="/team" class="navigation-link" title="Team terminal">
						<span class="navigation-link__index mono">04</span>
						Team terminal
					</Link>
				</If>
				<If cond={props.HasSeat == false && props.SeatsOpen && props.CanClaimSeat}>
					<Link href="/join" class="navigation-link navigation-link--hot" title="Join a team">
						<span class="navigation-link__index mono">04</span>
						Join a team
					</Link>
				</If>
				<If cond={props.HasSeat == false && (props.SeatsOpen == false || props.CanClaimSeat == false)}>
					<Link href="/team" class="navigation-link" title="Team terminal">
						<span class="navigation-link__index mono">04</span>
						Team terminal
					</Link>
				</If>
				<Link href="/board" class="navigation-link" title="Big Board">
					<span class="navigation-link__index mono">05</span>
					Big Board
				</Link>
				<If cond={props.HasSeat || props.SignedIn}>
					<Link href="/players" class="navigation-link" title="Player pool">
						<span class="navigation-link__index mono">06</span>
						Player pool
					</Link>
				</If>
				<If cond={(props.HasSeat || props.Commissioner) && props.HasSeat && props.TradesHot}>
					<Link href="/trades" class="navigation-link navigation-link--hot" title="Trades">
						<span class="navigation-link__index mono">07</span>
						Trades
						<span class="visually-hidden">{props.TradesAttentionText}</span>
					</Link>
				</If>
				<If cond={(props.HasSeat || props.Commissioner) && (props.HasSeat && props.TradesHot) == false}>
					<Link href="/trades" class="navigation-link" title="Trades">
						<span class="navigation-link__index mono">07</span>
						Trades
					</Link>
				</If>
			</div>
			<div class="navigation-group" data-navigation-group="game-day">
				<p class="navigation-group__label mono">GAME DAY</p>
				<Link href="/draft" class="navigation-link" title="Draft">
					<span class="navigation-link__index mono">08</span>
					Draft
				</Link>
				<If cond={props.DraftComplete}>
					<Link href="/draft/results" class="navigation-link" title="Draft results">
						<span class="navigation-link__index mono">09</span>
						Draft results
					</Link>
				</If>
				<Link href="/blitz" class="navigation-link" title="Preseason Blitz">
					<span class="navigation-link__index mono">10</span>
					Preseason Blitz
				</Link>
			</div>
			<div class="navigation-group" data-navigation-group="league">
				<p class="navigation-group__label mono">LEAGUE</p>
				<Link href="/wire" class="navigation-link" title="Signal Wire">
					<span class="navigation-link__index mono">11</span>
					Signal Wire
				</Link>
				<Link href="/activity" class="navigation-link" title="Activity">
					<span class="navigation-link__index mono">12</span>
					Activity
				</Link>
				<Link href="/locker" class="navigation-link" title="Locker Room">
					<span class="navigation-link__index mono">13</span>
					Locker Room
				</Link>
				<Link href="/scoring" class="navigation-link" title="Rules & scoring">
					<span class="navigation-link__index mono">14</span>
					Rules &amp; scoring
				</Link>
			</div>
			<div class="navigation-group" data-navigation-group="help">
				<p class="navigation-group__label mono">HELP</p>
				<Link href="/guide" class="navigation-link navigation-link--guide" title="Manager guide">
					<span class="navigation-link__index mono">15</span>
					Manager guide
				</Link>
				<Link href="/help" class="navigation-link navigation-link--guide" title="Help center">
					<span class="navigation-link__index mono">16</span>
					Help center
				</Link>
			</div>
			<If cond={props.Commissioner}>
				<div class="navigation-group" data-navigation-group="commissioner">
					<p class="navigation-group__label mono">COMMISSIONER</p>
					<Link href="/commissioner" class="navigation-link" title="All leagues">
						<span class="navigation-link__index mono">17</span>
						All leagues
					</Link>
					<Link href="/admin" class="navigation-link" title="League settings">
						<span class="navigation-link__index mono">18</span>
						League settings
					</Link>
				</div>
			</If>
		</nav>
		<div class="navigation-account">
			<If cond={props.SignedIn}>
				<div class="user-badge">
					<span class="user-chip mono">{props.Initials}</span>
					<span class="user-name">{props.TeamName}</span>
				</div>
				<Link href="/settings" class="access-link">Notification settings</Link>
				<form method="post" action="/auth/logout" data-gosx-managed="false">
					<input type="hidden" name="csrf_token" value={props.CSRFToken}></input>
					<button class="access-link" type="submit">Sign out</button>
				</form>
			</If>
			<If cond={props.SignedIn == false}>
				<div class="user-badge">
					<span class="user-chip mono">{props.Initials}</span>
					<span class="user-name">Rehearsal seat</span>
				</div>
				<a href="/login" data-gosx-link class="access-link" aria-label="League access">
					<span class="signal-mark" aria-hidden="true"></span>
					League access
				</a>
			</If>
		</div>
	</div>
}

// PageActionBarProps is the strict prop boundary PageActionBar (below)
// reads; Layout builds it straight from data.primary_action (any page's
// own Load()-returned data map may set that key — see PageActionBar's own
// doc comment for the full contract Layout imposes on it).
type PageActionBarProps struct {
	Label string
	Href  string
	Kind  string
	Form  string
	Tone  string
}

// PageActionBar is the phone-only thumb-zone slot for a page's single most
// important action (wave 7b mobile-foundation audit, item 11 — larch). A
// page opts in by setting a "primary_action" key on its OWN Load()-
// returned data map; Layout (below) only reads that key and never sets it
// itself. /team is the first page to wire it (elm — internal/league/
// service.go's teamPrimaryAction), gated behind data.primary_action.label:
// a missing "primary_action" key altogether (every other page, still, as
// of this wave), a nil value, and teamPrimaryAction's own explicit "no
// verb yet" map{} all read back with an empty label the same way, so all
// three collapse to the identical "no bar" outcome with no special-casing
// required here — label doubles as the presence flag on purpose, rather
// than a separate boolean field a producer could set inconsistently with
// the rest of the map.
//
// The primary_action contract:
//
//	primary_action: {
//	  label: string             // the control's own visible text — "" (or
//	                              // the key/value absent entirely) means
//	                              // "no primary action on this page," and
//	                              // renders no bar at all
//	  href:  string              // required when kind == "link"; ignored
//	                              // otherwise
//	  kind:  "link" | "submit"   // "link" renders <a data-gosx-link>, a
//	                              // plain same-document navigation; "submit"
//	                              // renders <button type="submit" form="...">
//	  form:  string               // required when kind == "submit": the id
//	                              // of a <form> element ELSEWHERE on the
//	                              // page (this bar renders outside any
//	                              // page-owned <form>, being shared chrome).
//	                              // The HTML form="" attribute is a native,
//	                              // browser-resolved association — it works
//	                              // with a GoSX-managed form exactly like an
//	                              // unmanaged one, and with no JavaScript at
//	                              // all, since the browser (not a script)
//	                              // wires the submit to that form by id.
//	  tone:  "primary" | "neutral" // presentation only, no behavior change
//	}
//
// Fixed, 56px tall with a 44px control inside it (the mobile touch floor —
// see public/styles.css item 1/2), positioned directly above .app-tabbar
// (never stacked under or over it) and env(safe-area-inset-bottom)-aware
// through that same tab bar's own reserved height; public/styles.css's own
// body:has(.page-action-bar) rule grows .site-frame's bottom padding by
// this bar's own height so page content already scrolls clear of it,
// exactly as it already does for .app-tabbar alone. Hidden entirely above
// the phone breakpoint: a mouse/keyboard viewer already has the page's own
// inline call to action in easy reach and needs no thumb-zone duplicate.
func PageActionBar(props PageActionBarProps) Node {
	return <div class={"page-action-bar page-action-bar--" + props.Tone}>
		<If cond={props.Kind == "submit"}>
			<button form={props.Form} type="submit" class="page-action-bar__link">{props.Label}</button>
		</If>
		<If cond={props.Kind != "submit"}>
			<a href={props.Href} data-gosx-link class="page-action-bar__link">{props.Label}</a>
		</If>
	</div>
}

// Layout renders the persistent chrome around every route's <Slot/>.
//
// The rail-head and mobile-navigation-enhanced attention chips (gap-audit
// item 6, wave 4 — linden) are a distinct region from sycamore's own new
// mobile bottom bar elsewhere in this file — see that work for the
// four-slot bar itself. Both chips are gated three ways: has_seat (a
// seatless visitor has no team-scoped task this list could name),
// attention.has_items (an honest empty state renders no chip at all,
// never "0 URGENT"), and demo == false (wave-6 audit item 1: an
// anonymous visitor on a DEMO_MODE deployment gets has_seat: true from
// Viewer's own signed-out branch — internal/league/service.go — purely so
// the rehearsal dashboard has something to show; that visitor has no real
// Action Center to visit, so the chip must key off demo specifically, not
// has_seat alone). data.league.attention is internal/league/
// service.go's leagueMap() addition — see that function's own doc
// comment for why it is league-wide, not per-viewer. Both chips link to
// "/#home-action-center-heading" (the Action Center's existing heading
// id, app/page.gsx) rather than a new "#action-center" id: this wave's
// app/page.gsx scope is the playoff-card block only, so the chip targets
// the id already in the DOM instead of adding one there. That heading's
// own scroll-margin-top (public/styles.css) is what keeps the deep link
// from landing behind the masthead.
func Layout() Node {
	return <div class="app-shell">
		<a class="skip-link" href="#main-content">Skip to league content</a>
		<div class="ambient-grid" aria-hidden="true"></div>
		<div
			class="toast-stack"
			data-gosx-toast-host
			aria-live="polite"
			aria-relevant="additions"
			aria-label="Action notifications"
		></div>
		<If cond={data.viewer.signed_in || data.viewer.demo}>
			<aside
				class="site-rail navigation-surface navigation-surface--desktop"
				data-navigation-surface="desktop"
			>
				<div class="rail-head">
					<a href="/" data-gosx-link class="site-brand" aria-label={data.league.name + " league home"}>
						<span class="brand-badge">{data.league.short_code}</span>
						<span class="brand-copy">
							<strong>{data.league.name}</strong>
							<small>{data.league.tagline}</small>
						</span>
					</a>
					<If cond={data.viewer.has_seat && data.league.attention.has_items && data.viewer.demo == false}>
						<a
							href="/#home-action-center-heading"
							data-gosx-link
							class="rail-attention-chip"
							aria-label={data.league.attention.chip_label}
						>
							<span class="signal-mark" aria-hidden="true"></span>
							ACTION CENTER · {data.league.attention.urgent_count} URGENT
						</a>
					</If>
				</div>
				<PrimaryNavigation
					SignedIn={data.viewer.signed_in}
					HasSeat={data.viewer.has_seat}
					SeatsOpen={data.league.fantasy_seats_open}
					CanClaimSeat={data.viewer.seat_claim_eligible}
					Commissioner={data.viewer.is_commissioner}
					Initials={data.viewer.initials}
					TeamName={data.viewer.team_name}
					CSRFToken={csrf.token}
					PickemHot={data.league.attention.pickem_hot}
					PickemAttentionText={data.league.attention.pickem_attention_text}
					TradesHot={data.league.attention.trades_hot}
					TradesAttentionText={data.league.attention.trades_attention_text}
					DraftComplete={data.league.draft_complete}
				></PrimaryNavigation>
			</aside>
			<header class="mobile-navigation-enhanced" data-navigation-surface="mobile-enhanced-bar">
				<a href="/" data-gosx-link class="mobile-brand" aria-label={data.league.name + " league home"}>
					<span class="brand-badge">{data.league.short_code}</span>
					<strong>{data.league.name}</strong>
				</a>
				<If cond={data.viewer.has_seat && data.league.attention.has_items && data.viewer.demo == false}>
					<a
						href="/#home-action-center-heading"
						data-gosx-link
						class="rail-attention-chip rail-attention-chip--mobile"
						aria-label={data.league.attention.chip_label}
					>
						<span class="signal-mark" aria-hidden="true"></span>
						{data.league.attention.urgent_count} URGENT
					</a>
				</If>
				<button
					type="button"
					class="mobile-navigation-open"
					aria-label="Open league navigation"
					aria-controls="primary-navigation-dialog"
					aria-expanded="false"
					data-gosx-disclosure-target="#primary-navigation-dialog"
				>
					<span aria-hidden="true"></span>
				</button>
			</header>
			<div
				class="mobile-navigation-backdrop"
				data-gosx-disclosure-backdrop="#primary-navigation-dialog"
				hidden
				aria-hidden="true"
			></div>
			<aside
				id="primary-navigation-dialog"
				class="mobile-navigation-dialog"
				data-navigation-surface="mobile-enhanced-dialog"
				data-gosx-disclosure
				data-gosx-disclosure-modal
				hidden
				role="dialog"
				aria-modal="true"
				aria-labelledby="primary-navigation-title"
			>
				<div class="mobile-navigation-dialog__head">
					<div>
						<span class="brand-badge">{data.league.short_code}</span>
						<h2 id="primary-navigation-title">League navigation</h2>
					</div>
					<button
						type="button"
						class="mobile-navigation-close"
						aria-label="Close league navigation"
						data-gosx-disclosure-close="#primary-navigation-dialog"
						data-gosx-disclosure-initial-focus
					>
						<span aria-hidden="true">&#10005;</span>
					</button>
				</div>
				<PrimaryNavigation
					SignedIn={data.viewer.signed_in}
					HasSeat={data.viewer.has_seat}
					SeatsOpen={data.league.fantasy_seats_open}
					CanClaimSeat={data.viewer.seat_claim_eligible}
					Commissioner={data.viewer.is_commissioner}
					Initials={data.viewer.initials}
					TeamName={data.viewer.team_name}
					CSRFToken={csrf.token}
					PickemHot={data.league.attention.pickem_hot}
					PickemAttentionText={data.league.attention.pickem_attention_text}
					TradesHot={data.league.attention.trades_hot}
					TradesAttentionText={data.league.attention.trades_attention_text}
					DraftComplete={data.league.draft_complete}
				></PrimaryNavigation>
			</aside>
			<details class="mobile-navigation-static" data-navigation-surface="mobile-static">
				<summary>
					<span class="mobile-brand">
						<span class="brand-badge">{data.league.short_code}</span>
						<strong>{data.league.name}</strong>
					</span>
					<span class="mobile-navigation-static__label mono">MENU</span>
				</summary>
				<PrimaryNavigation
					SignedIn={data.viewer.signed_in}
					HasSeat={data.viewer.has_seat}
					SeatsOpen={data.league.fantasy_seats_open}
					CanClaimSeat={data.viewer.seat_claim_eligible}
					Commissioner={data.viewer.is_commissioner}
					Initials={data.viewer.initials}
					TeamName={data.viewer.team_name}
					CSRFToken={csrf.token}
					PickemHot={data.league.attention.pickem_hot}
					PickemAttentionText={data.league.attention.pickem_attention_text}
					TradesHot={data.league.attention.trades_hot}
					TradesAttentionText={data.league.attention.trades_attention_text}
					DraftComplete={data.league.draft_complete}
				></PrimaryNavigation>
			</details>
			<nav class="app-tabbar" aria-label="Quick navigation" data-navigation-surface="mobile-tabbar">
				<Link href="/" class="app-tabbar__tab">
					<span class="app-tabbar__icon" aria-hidden="true">&#8962;</span>
					Home
				</Link>
				<If cond={data.viewer.has_seat}>
					<Link href="/team" class="app-tabbar__tab">
						<span class="app-tabbar__icon" aria-hidden="true">&#9689;</span>
						Team
					</Link>
				</If>
				<If cond={data.viewer.has_seat == false && data.league.fantasy_seats_open && data.viewer.seat_claim_eligible}>
					<Link href="/join" class="app-tabbar__tab">
						<span class="app-tabbar__icon" aria-hidden="true">&#9689;</span>
						Team
					</Link>
				</If>
				<If cond={data.viewer.has_seat == false && (data.league.fantasy_seats_open == false || data.viewer.seat_claim_eligible == false)}>
					<Link href="/team" class="app-tabbar__tab">
						<span class="app-tabbar__icon" aria-hidden="true">&#9689;</span>
						Team
					</Link>
				</If>
				<Link href="/matchups" class="app-tabbar__tab">
					<span class="app-tabbar__icon" aria-hidden="true">&#9917;</span>
					Matchups
				</Link>
				<button
					type="button"
					class="app-tabbar__tab"
					aria-label="More"
					aria-controls="primary-navigation-dialog"
					aria-expanded="false"
					data-gosx-disclosure-target="#primary-navigation-dialog"
				>
					<span class="app-tabbar__icon" aria-hidden="true">&#8942;</span>
					More
				</button>
			</nav>
		</If>
		<If cond={(data.viewer.signed_in || data.viewer.demo) == false}>
			<header class="minimal-bar">
				<a href="/" data-gosx-link class="site-brand" aria-label={data.league.name + " league home"}>
					<span class="brand-badge">{data.league.short_code}</span>
					<span class="brand-copy">
						<strong>{data.league.name}</strong>
						<small>{data.league.tagline}</small>
					</span>
				</a>
				<nav class="minimal-actions" aria-label="Public navigation">
					<a href="/guide" data-gosx-link class="access-link access-link--guide">Manager guide</a>
					<a href="/login" data-gosx-link class="access-link" aria-label="League access">
						<span class="signal-mark" aria-hidden="true"></span>
						League access
					</a>
				</nav>
			</header>
		</If>
		<If cond={(data.viewer.signed_in || data.viewer.demo) && data.league.latest_announcement.has}>
			<div class="announcement-banner" role="status">
				<span class="announcement-banner__label mono">COMMISSIONER NOTE</span>
				<p>{data.league.latest_announcement.body}</p>
				<span class="announcement-banner__time mono">
					{data.league.latest_announcement.posted_at}
				</span>
			</div>
		</If>
		<div class="site-frame">
			<Slot />
		</div>
		<footer class="site-footer">
			<div>
				<p>
					<strong>{data.league.name}</strong>
					<If cond={data.league.has_footer_line}>
						//
						{data.league.footer_line}
					</If>
				</p>
				<nav aria-label="Footer">
					<a href="/privacy" data-gosx-link>Privacy</a>
					<a href="/terms" data-gosx-link>Terms</a>
					<a href="/open-source" data-gosx-link>Run your own league →</a>
				</nav>
			</div>
			<div class="footer-status">
				<If cond={data.league.matchup_footer_live}>
					<span class="live-dot" aria-hidden="true"></span>
				</If>
				{data.league.matchup_footer_label}
			</div>
		</footer>
		<If cond={data.primary_action.label != ""}>
			<PageActionBar
				Label={data.primary_action.label}
				Href={data.primary_action.href}
				Kind={data.primary_action.kind}
				Form={data.primary_action.form}
				Tone={data.primary_action.tone}
			></PageActionBar>
		</If>
	</div>
}
