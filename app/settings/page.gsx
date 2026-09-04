package settings

// OnAndReady/OnAndNoTransport/OffAndReady/OffAndNoTransport (item 6,
// 2026-09-02 audit) are pre-combined so NotificationRow's strict cond
// exprs stay bare bool-field reads: this component's own strict-render
// rules reject a compound expression like "props.Enabled &&
// props.DeliveryReady" (gosx's lower pass rejects anything but a bare
// bool field, or that field compared with "== false"), so page.server.go
// computes the four exclusive state combinations once, server-side.
type NotificationRowProps struct {
	Category           string
	Label              string
	Description        string
	Delivery           string
	State              string
	Enabled            bool
	CurrentOn          bool
	CurrentOff         bool
	CanEdit            bool
	Planned            bool
	OnAndReady         bool
	OnAndNoTransport   bool
	OffAndReady        bool
	OffAndNoTransport  bool
	Action             string
	CSRF               string
}

component NotificationRow(props: NotificationRowProps) {
	return <fieldset class="notification-preference" id={"notify-" + props.Category} data-notification-category={props.Category}>
		<legend>{props.Label}</legend>
		<div class="notification-preference__body">
			<div>
				<p>{props.Description}</p>
				<small>{props.Delivery}</small>
			</div>
			<If cond={props.CanEdit}>
				<div class="notification-choice-group" role="group" aria-label={props.Label + " delivery setting"}>
					<form method="post" action={props.Action} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<input type="hidden" name="category" value={props.Category}></input>
						<input type="hidden" name="enabled" value="true"></input>
						<button class="notification-choice" type="submit" aria-pressed={props.CurrentOn} data-current={props.CurrentOn}>On</button>
						<If cond={props.CurrentOn}>
							<span class="notification-choice__current"><span aria-hidden="true">✓</span> CURRENT</span>
						</If>
					</form>
					<form method="post" action={props.Action} data-gosx-managed="true">
						<input type="hidden" name="csrf_token" value={props.CSRF}></input>
						<input type="hidden" name="category" value={props.Category}></input>
						<input type="hidden" name="enabled" value="false"></input>
						<button class="notification-choice" type="submit" aria-pressed={props.CurrentOff} data-current={props.CurrentOff}>Off</button>
						<If cond={props.CurrentOff}>
							<span class="notification-choice__current"><span aria-hidden="true">✓</span> CURRENT</span>
						</If>
					</form>
				</div>
			</If>
			<If cond={props.CanEdit == false}>
				<span class="notification-preference__readonly">{props.State} · READ ONLY</span>
			</If>
		</div>
		<If cond={props.OnAndReady}>
			<span class="notification-preference__state">Current state: ON</span>
		</If>
		<If cond={props.OnAndNoTransport}>
			<span class="notification-preference__state">On · sends once email is set up</span>
		</If>
		<If cond={props.OffAndReady}>
			<span class="notification-preference__state">Current state: OFF</span>
		</If>
		<If cond={props.OffAndNoTransport}>
			<span class="notification-preference__state">Off · will not send, even after email is set up</span>
		</If>
	</fieldset>
}

func Page() Node {
	return <main class="page notification-settings-page" id="main-content">
		<section class="draft-masthead notification-settings-masthead">
			<div class="draft-masthead__copy">
				<span class="signal-label">
					<span class="signal-mark" aria-hidden="true"></span>
					ACCOUNT // EMAIL SETTINGS
				</span>
				<h1>Notification settings</h1>
				<p class="page-subhead">Control the signal.</p>
				<p>
					Choose which live league emails reach your manager account. These controls affect future delivery only; they do not replay messages already sent.
				</p>
				<If cond={data.has_email && data.delivery_ready}>
					<p class="notification-settings-account">
						Delivering to <strong>{data.email}</strong>
					</p>
				</If>
				<If cond={data.has_email && data.delivery_ready == false}>
					<p class="notification-settings-account">
						Preferences saved for <strong>{data.email}</strong>.
					</p>
				</If>
			</div>
			<div class="draft-clock-panel notification-settings-summary">
				<span>Delivery status</span>
				<strong class="mono notification-settings-summary__status">
					<If cond={data.delivery_ready}>EMAIL READY</If>
					<If cond={data.delivery_ready == false}>EMAIL NOT CONFIGURED</If>
				</strong>
				<div class="draft-clock-meta">
					<span>{data.live_category_count} LIVE CATEGORIES</span>
					<span>EMAIL ONLY // SMS NOT SUPPORTED</span>
				</div>
			</div>
		</section>

		<div class="notice-stack" aria-live="polite">
			<p class="notification-settings-delivery" role="status">{data.delivery_message}</p>
			<If cond={data.delivery_ready == false}>
				<p class="notification-settings-ask">
					<a href="/locker" data-gosx-link>Ask the commissioner to turn on email</a>
				</p>
			</If>
			<If cond={data.has_notice}>
				<p class="flash-message">{data.notice}</p>
			</If>
			<If cond={data.has_settings_error}>
				<p class="error-message">{data.settings_error}</p>
			</If>
			<If cond={data.read_only}>
				<p class="demo-message">
					<strong>{data.read_only_reason}</strong>
				</p>
			</If>
			<If cond={data.viewer.demo}>
				<p class="demo-message">
					<strong>REHEARSAL MODE:</strong>
					the console is open to everyone while demo mode is on.
				</p>
			</If>
		</div>

		<section class="player-pool notification-settings-panel" aria-labelledby="density-settings-heading">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">00 // DISPLAY</span>
					<h2 id="density-settings-heading">Data density</h2>
				</div>
			</div>
			<p class="notification-settings-note">
				Comfortable keeps every number and label at an easy-to-read size. Compact shrinks data text to fit more rows on screen, for a manager who wants the old dense view back.
			</p>
			<fieldset class="notification-preference" data-notification-category="density">
				<legend>Data text size</legend>
				<div class="notification-preference__body">
					<div>
						<p>Comfortable is the default across every page. Compact applies everywhere until you switch back.</p>
					</div>
					<div class="notification-choice-group" role="group" aria-label="Data density setting">
						<form method="post" action={data.density_action} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<input type="hidden" name="density" value="comfortable"></input>
							<button class="notification-choice" type="submit" aria-pressed={data.density_comfortable} data-current={data.density_comfortable}>Comfortable</button>
							<If cond={data.density_comfortable}>
								<span class="notification-choice__current"><span aria-hidden="true">✓</span> CURRENT</span>
							</If>
						</form>
						<form method="post" action={data.density_action} data-gosx-managed="true">
							<input type="hidden" name="csrf_token" value={csrf.token}></input>
							<input type="hidden" name="density" value="compact"></input>
							<button class="notification-choice" type="submit" aria-pressed={data.density_compact} data-current={data.density_compact}>Compact</button>
							<If cond={data.density_compact}>
								<span class="notification-choice__current"><span aria-hidden="true">✓</span> CURRENT</span>
							</If>
						</form>
					</div>
				</div>
			</fieldset>
		</section>

		<section class="player-pool notification-settings-panel" aria-labelledby="notification-settings-heading">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">01 // LIVE DELIVERY</span>
					<h2 id="notification-settings-heading">Manager notifications</h2>
				</div>
				<span class="mono notification-settings-count">{data.live_category_count} AVAILABLE NOW</span>
			</div>
			<p class="notification-settings-note">
				Turn a category on or off with a native form. The server checks your signed-in identity and accepts only the categories listed here.
			</p>
			<If cond={data.delivery_ready == false}>
				<p class="notification-settings-note">
					Email delivery is not configured on this league; these preferences apply once it is.
				</p>
			</If>
			<div class="notification-preference-groups">
				<section class="notification-preference-group" aria-labelledby="draft-notifications">
					<div class="notification-preference-group__heading">
						<h3 id="draft-notifications">Draft day</h3>
						<p>Preparation, live-room timing, and the final recap.</p>
					</div>
					<div class="notification-preference-list">
						<Each of={data.draft_preferences} as="preference">
							<NotificationRow {...preference}></NotificationRow>
						</Each>
					</div>
				</section>
				<section class="notification-preference-group" aria-labelledby="weekly-notifications">
					<div class="notification-preference-group__heading">
						<h3 id="weekly-notifications">Weekly play</h3>
						<p>Pick'em, matchup recaps, roster movement, lineup deadlines, and IR status.</p>
					</div>
					<div class="notification-preference-list">
						<Each of={data.weekly_preferences} as="preference">
							<NotificationRow {...preference}></NotificationRow>
						</Each>
					</div>
				</section>
				<section class="notification-preference-group" aria-labelledby="league-notifications">
					<div class="notification-preference-group__heading">
						<h3 id="league-notifications">League</h3>
						<p>Access milestones, scoring changes, season kickoff, and commissioner announcements.</p>
					</div>
					<div class="notification-preference-list">
						<Each of={data.league_preferences} as="preference">
							<NotificationRow {...preference}></NotificationRow>
						</Each>
					</div>
				</section>
			</div>
		</section>

		<If cond={data.planned_category_count > 0}>
		<section class="player-pool notification-settings-panel notification-settings-panel--planned" aria-labelledby="planned-notification-heading">
			<div class="pool-toolbar">
				<div>
					<span class="section-index">02 // DELIVERY ROADMAP</span>
					<h2 id="planned-notification-heading">Planned categories</h2>
				</div>
				<span class="mono notification-settings-count">{data.planned_category_count} PLANNED // NOT ACTIVE</span>
			</div>
			<p class="notification-settings-note">
				These catalog entries are documented for the future, but they do not have an active delivery path yet. There is no switch to flip today.
			</p>
			<p class="notification-settings-note">This setting is not active yet.</p>
			<div class="notification-preference-list">
				<Each of={data.planned_preferences} as="preference">
					<fieldset class="notification-preference notification-preference--planned" data-notification-category={preference.Category} aria-disabled="true">
						<legend>{preference.Label}</legend>
						<div class="notification-preference__body">
							<div>
								<p>{preference.Description}</p>
								<small>{preference.Delivery}</small>
							</div>
							<span class="notification-preference__readonly">PLANNED</span>
						</div>
					</fieldset>
				</Each>
			</div>
		</section>
		</If>

		<nav class="notification-settings-footer" aria-label="Account settings navigation">
			<a href="/" data-gosx-link class="button button--ghost">Back to Home</a>
			<a href="/login" data-gosx-link class="button button--compact">Sign-in and account</a>
		</nav>
	</main>
}
