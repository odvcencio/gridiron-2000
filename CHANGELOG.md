# Changelog

All notable changes to Gridiron 2000 are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed
- The commissioner console's jump strip now stays on screen at every width: pinned to the top on a desktop, pinned above the bottom tab bar on a phone. The chip for the section in view is marked.
- Every jump-strip chip now reads the same word as the section it lands on, and every console section carries one number in one reading sequence instead of some sections showing a number and others showing the word "SEASON".
- The console's job list now includes running waivers, reviewing a trade, changing scoring, and reading the league log. Setting a lineup for a manager now renders as a boxed row like every other job.
- The jump strip is a same-page link now, not a full page reload, so a jump lands immediately instead of after a visible lurch.
- On a phone, the jump strip and the league notes move up ahead of the task board, so the console's Sunday jobs are reachable from the first screen.
- Seat rows in the console's readiness list now lead with the team name and manager; the two-letter seat code moves to a secondary chip, with one line explaining what the code is.
- League HQ now leads each league card with its week and close readiness, moves release and build details behind a closed "Build" disclosure, and reads the same clock the league's own console uses instead of the server's wall clock.
- The console's backup section now speaks in plain words: what is backed up, when the last automatic copy ran, how many are kept, and what to do if a backup will not open. The Danger Zone's reset warnings now link to it.
- The console's schedule card now hides its machine-generated redraw seed behind a closed "Redraw trail" disclosure.
- The console's pick-clock readout now shows duration and time remaining as minutes and seconds, matching the draft room's own clock, instead of raw seconds.
- The console's invites card now states that its count covers admitted addresses, not claimed seats, so it no longer reads as a second, contradicting seat count.
- An in-season console now opens with a "This week" card: the week and its close readiness, open waiver claims, trades in review, and one line naming what needs the commissioner today, each linking to its section.

### Fixed
- The Signal Wire's filter chips now show a real count for every category. A chip with nothing behind it reads disabled with a plain reason instead of a link into an empty page, and the empty state names the category, how far back the wire's window reaches, and the nearest chip that does have something on it.
- The Wire no longer tags an unrelated story "INJURY WATCH". A story with no real classification carries no label.
- The Wire now uses one vocabulary — "sources" for every upstream provider, "signals" for every item, "tip" for the one submission flow — instead of seven overlapping words for the same two ideas.
- The Wire's two refresh intervals (the page itself, and how often sources are checked) each carry their own clear label, and the source interval reads the real configured value instead of a hard-coded number.
- Every Wire item no longer carries an unexplained percentage next to its source type; the plain source-trust word stays on its own.
- The Wire's state words (LIVE, CACHED, STALE, DEGRADED, UNAVAILABLE) now match the words the Manager Guide documents. A failed source no longer reads as a quiet news day; it reads "Failed" with its reason and when it last succeeded. A source's "kept" count now says plainly that it is stories kept after filtering.
- A confirmation banner from the Signal Wire's own tip form no longer appears on an unrelated page a manager happens to open next.
- The transaction feed leads every roster-move line with the team's name; the division code now reads as a small secondary chip instead of repeating on every line. The feed's own refresh note states the cadence plainly and names the last real update instead of warning about failure.
- The transaction feed's playoff-bracket panel now reads as one quiet line until the postseason is actually the live phase, instead of outweighing the week's own moves in developer language every time.
- The Signal Wire and the transaction feed now use one league-local time format everywhere, with the zone and a relative phrase; a stray double space before the relative phrase on the transaction feed is gone.
- Six status chips on the console (schedule, roster, and playoff cards) no longer overflow or clip their own borders; they size to their own text and wrap instead.
- The console's pick-clock card no longer shows a blank "Deadline" cell when no pick is armed, or a blank "Duration source" cell; the Playoff Truth card no longer shows a blank "Source" cell before a bracket exists.
- The console's job list no longer renders as overlapping, ragged boxes on a phone; every row now measures and aligns correctly.

## [release-2026.09.08-88f7149-season2] — 2026-09-07

Scope: the second in-season batch: the team page's lineup grid (SLOT, PLAYER, OPPONENT, GAME, STATUS, PROJ, PTS, ACTION) with injuries on the row, a closed Swap disclosure, Start and Drop on bench rows, a current-matchup card, a truthful starters-only projection, and a shorter page; the console re-prioritised for the season with truthful week and forced-close copy, seat-release consequences, and a danger style on the one live control; open trade offers read as open; post-draft polish on the results page, the player pool (free agents by default), the Big Board, the login and help pages, the phone menu, the locker composer, and the footer. No schema change.

### Changed
- The team page shows a current-matchup card at the top: the opponent, the projected score, the win chance, and the first kickoff, with a link to the full matchup. It replaces the plain "View matchup" button.
- The team page's lock panel now sits inside the stat strip as a "Locks" line, with the exact time, the time zone, and a Details line for the locked-slot count. The panel no longer sits above the lineup.
- The starting lineup now shows a column header and a projected score and a live score on every row, next to a "Swap" control that opens in place instead of a form that is always open.
- Bench rows now offer a "Start" button that fills the best open slot, a "Swap with…" choice when no slot is open, and a "Drop" button with a confirm step, matching the player pool's own drop control.
- An injured player's status (Questionable, Doubtful, Out, IR) now shows on the row itself, with the source in a tip, instead of only inside the news panel.
- The team hero shows one row of facts (name, division, record, badge) once the season has started; the co-manager line and the customize link show only before the season starts.
- The draft-class callout on the team page disappears once the season has started.
- The Signal Watch panel on the team page is closed by default; open it to see scouting notes.
- The starting lineup and bench now share one explicit 8-column table on a wide screen: SLOT, PLAYER, OPPONENT, GAME, STATUS, PROJ, PTS, and ACTION each get their own column, so a name no longer clamps and the opponent and kickoff time no longer crowd into the name line. Details and the drafted round move into the row's own Details panel.
- On a phone, each starting and bench row now shows as a two-line block: slot, name, projection, and points on the first line; opponent, game, and status on the second, with Swap, Start, or Drop at the right.
- The league roster-shape legend and the "What does Set best lineup do?" note now collapse behind a closed summary line, so a manager sees a short line first and opens the detail only when they want it.
- The "Your draft class" callout now collapses behind a closed summary line instead of showing the full pick list.

### Fixed
- A drafted round and pick no longer show as a chip on every starting-lineup row; the detail moved into the row's own Details panel.
- A closed Swap, Drop, roster-shape, or draft-class disclosure no longer showed its full form or list underneath its own summary line. An older rule on one of its child elements set a fixed display value that beat the browser's own closed-panel hiding; every closed panel now hides its content the way a closed panel should.
- The team page's "Locks" tile no longer shows "PLAYER LOCK TIMING UNAVAILABLE" at the same large size as a short number, wrapping across five lines. That message now reads at a smaller size, on one or two lines.
- The commissioner console's top line now states the week's true progress in plain words (for example "Week 1 in progress") instead of gluing the season phase and draft status into one raw line that read as "the season is over" during week 1. The draft deadline line shows only while a draft is still pending.
- Once the draft is complete, the console leads with the week's own open work — open waiver claims, trades in review, and seat readiness — and moves the draft-night seat and board details into a closed "Draft night (complete)" section.
- A forced week close now states what it actually closed with, for example "Week 1 closed with 0 of 16 games scored — 88 stat joins missed", instead of a bare success message. The results page shows a CLOSED EARLY warning when a week closed before its games finished. The force-close control now warns of the consequence before the click and uses the same destructive styling as other irreversible controls, instead of looking the same as its disabled neighbors.
- Invite emails sent after the draft state that the draft is finished and point the new manager to the current week's lineup, instead of telling them to build a draft board for a draft that already happened. The invite preview now addresses a real pending invite, or a plain placeholder, instead of a fake example address.
- Releasing a seat in season now names the roster, lineup, and matchup the release leaves in place. A manager whose seat was released sees a plain notice naming their released team and the date, instead of a first-time welcome page.
- The trade desk's empty-inbox message now calls an offer you sent "open," not "accepted," matching what the review sections actually show.
- The rules and scoring page now states that a rule change applies from the next open week and that closed weeks keep the scores they were closed with.

## [release-2026.09.08-cc0f473-season1] — 2026-09-07

Scope: the first in-season batch: the matchups page leads with projections (scorebug header, slot-aligned starter table with PROJ and PTS per side, benches disclosure, 1280 layout), the commissioner roster correction (drop and add on behalf of a team with a required reason, review-confirm, person-attributed audit event, a one-time notice for the manager), and the in-season truth fixes (live-scores label keyed to the poller, weeks end six hours after the last kickoff, every cascading lineup change reported, saves return to the changed row, dashes until the ledger posts, played weeks viewable, locked players disabled with a reason, one starters-only projection helper). No schema change.

### Added
- The commissioner can now correct a named team's roster on that team's own behalf from the admin console's Roster shape section: choose the team, choose a player to drop and a free agent to add (either side is optional), and give a required reason. The change is immediate, with a review step that restates the team and both players before a second, explicit confirm. The correction shows in the transaction feed, in the commissioner's own audit trail with the reason, and as a one-time notice on the affected team's next visit to their Team page.
### Fixed
- The draft results page now dates the draft by when it actually started, not by the scheduled meeting time; the scheduled time still shows as a second line when the two disagree. It ends with links to set your Week 1 lineup and browse free agents, and the finished draft room's own pick-history pane ends the same way, plus a link to the results page. The results page's value column now carries a plain-language legend and a "VS ADP" label on each value.
- After the draft, /players defaults to a "Free agents" filter instead of listing rostered players first; an "All players" link opts back into the full pool. The rostered-owner chip now shows the team's full name on a phone, not just the code.
- The draft room's ROSTER tab now lists your drafted players by slot, starters before bench, with a plain count of empty starting slots; the needs list itself now shows open slots first instead of alphabetically.
- The Big Board's rail panel now offers one "Clear drafted" button that removes every already-drafted entry at once, and dims a drafted row's name, rank, and news icon (not only the name). The standalone Big Board page gets the same bulk clear action, and once the draft is complete its own heading changes from draft-night copy to "Your watch list for waivers."
- The sign-in page now has one heading (Sign in) with nothing above it; the league name and the draft-event card render as plain text instead of a second and third heading.
- A help topic's primary button now names its destination ("Go to the league home", "Open sign-in", and so on) instead of "Open owning action," and the page's own source metadata now sits in a collapsed "Sources" disclosure below the answer instead of ahead of it.
- The Big Board's disabled "LOCKED" buttons on a seatless member's pool now name the player and point to the page's existing explanation, instead of fifty identical unnamed buttons.
- The phone navigation dialog now lays its destinations out in two columns, so every destination fits without scrolling.
- The Locker Room no longer shows two ways to post the same message on a phone.
- The footer's status now carries a plain "Status" label; the anonymous landing page no longer shows a playoff card that leads to the sign-in wall; a seatless member's rail badge now shows their own name instead of going blank.

## [release-2026.09.08-8b73e06-textflow] — 2026-09-07

Scope: the text-flow adoption: names, sentences, notices, and headlines on every non-live surface render through GoSX's text-layout substrate instead of CSS truncation, so long team and player names wrap or clamp at a line boundary instead of clipping. No schema change.

### Changed
- Notice and banner text, draft-room static copy, the admin and commissioner consoles, settings, the signed-out entry pages, the help center, scoring, the wire, the locker room, pick'em, and preseason blitz now flow long names and sentences to a real line boundary instead of clipping mid-word: team, manager, and league names; the commissioner announcement; the pool-status and practice-strip banners; the pre-draft checklist and admin runbook; invite, announcement, seat-ledger, and draft-order rows; the reset-panel consequence sentences; category names and ON/OFF state lines; help topic cards, glossary entries, and migration rows; the scoring format summary and section ledes; wire signal headlines and source names; locker post bodies and author names; pick'em game-row labels; and blitz entry and champion names. Toast notifications now wrap to two lines instead of clipping.
- /matchups shows projections first. Before kickoff, the featured card and every around-the-league card show one big PROJ number. During a game, the score shows big, with the projection small underneath. After the week, FINAL shows big. Every card also carries a win-probability meter and a plain sentence about starters still to play. The starter table now names each lineup slot and adds a PROJ column beside PTS, and ends in a totals row. A closed "Benches" disclosure lists both benches with their own projections. Each card names the manager's first name and record. The around-the-league layout no longer overlaps at 1280 pixels wide. On a phone, each slot shows as a two-column block, with the projection and points under each starter's name, instead of a squeezed row.

### Fixed
- The matchups page and the home page no longer say "Live scores on" when the live-scoring poller is off. They now read "Live scores off · weekly ledger only" and "Ledger posts after the games".
- A week's matchup status now moves past "in progress" once its last kickoff is more than six hours old, even when no game ever posts a Final flag, and reads "Games are over · fantasy results await week close". The masthead date/slate phrase now names a broadcast slate only while a game is actually inside its window.
- A lineup save now reports every slot it changed, not just the one the manager touched. Setting a player into a full slot names each starter the auto-fill cascade promoted or benched as a result.
- The team page's week selector now keeps a played (closed) week as a read-only option instead of refusing it with a false "not on the published schedule" reason. Opening one shows the accurate "Week N is closed" notice and that week's own lineup.
- A locked player now stays in a lineup slot's picker, disabled, with "locked, game started" instead of disappearing from the list with no explanation.
- The team page's PROJECTED figure now sums starters only, the same total the matchup card shows, instead of the whole roster including the bench.
- The team page's PTS column now reads "—" until the weekly ledger has posted, instead of a false "0.0", with the same "Weekly ledger (nflverse)" source line the matchups page uses.
- Saving a lineup slot or making a Pick'em pick now returns to the row that changed, on both a plain form submit and a JavaScript-managed one, instead of resetting the page to the top.

## [release-2026.09.05-fee1a4c-practice] — 2026-09-04

Scope: the practice draft rule change: a practice runs until the manager leaves or the real draft starts (no round cap, 12-hour sessions), and every practice entry point disappears once the real draft has started, with /draft/practice redirecting to the room. No schema change.

### Changed
- The practice draft has no round cap: a practice runs until the manager leaves, until the real draft starts, or until the sandbox draft reaches its final pick; "Practice complete" appears only at that final pick. A practice session now keeps for twelve idle hours, so a tab left open through draft weekend resumes where it stopped. Once the real draft has started, the practice draft is gone: no home card, no checklist item, no command-bar or phone-menu link, and `/draft/practice` and its actions redirect to `/draft`; an open practice ends with the "The real draft has started" line and its link, then the session is evicted. The strip reads "Practice draft · picks here do not count · leave whenever you like · the real draft starts …", and the lobby opens with "See what a live draft looks like before Sunday."

## [release-2026.09.04-972461d-uxpass] — 2026-09-04

Scope: the UX pass before the Sunday draft: six persona-journey audits on faithful copies of the live league (214 findings), the practice draft at /draft/practice, and 109 pre-draft fixes across the draft room, console, arrival, home, team, board, players, trades, settings, help, and scoring. No schema change.

### Added
- A practice draft at `/draft/practice`: a seated manager takes a few picks on the clock in a private copy of the draft room, in their real seat, against the other seats played by bots that draft from each seat's real Big Board. It uses the real pool, draft order, and pick clock, starts from round 1, 5, 10, or 15, runs three rounds, and saves nothing. Entry points sit on the home page, in the pre-draft checklist, and in /help.

### Changed
- /scoring states the league's scoring format, superflex, mode, draft rounds, and starter count in one line under the heading.

### Fixed
- The draft room's RK column is wide enough for the house-rank code, so the code no longer paints over the player name at desktop widths.
- Before the draft, the command bar and the phone menu carry a "Practice the draft room" link, so the practice draft is visible without opening the checklist.
- `/settings` states a category's ON/OFF wording honestly: an OFF category now says it will not send even after email is set up, instead of claiming it still sends.
- `/settings` states "email is not configured" at most twice instead of fourteen times, and adds one link asking the commissioner to turn on email.
- Saving a notification category on `/settings` returns you to that category, not the top of the page, and names the category and its new state in the confirmation.
- The session-expired page after a stale form submission now says the form expired, not the session, and names the page its back link returns to. A form posted to a page that does not accept one gets its own short 405 page instead of the same message.
- The Preseason Blitz notice's action button no longer paints its top border across the sentence above it.
- Every "back to home" link across `/settings`, `/terms`, `/open-source`, `/privacy`, and the 404 page uses the same label and destination; `/settings`'s sign-in link is named for what it does.
- The Help Center glossary shows its 75 terms, definitions, and topic links instead of 75 blank cards linking to a 404.
- The Help Center's "coming from another app" table shows its nine rows instead of nine blank rows, and a long unbroken run of text in that table wraps instead of pushing the page into a sideways scroll.
- /scoring's "editable until" deadline shows only to the commissioner; a manager reads a true sentence about when the rules become final instead.
- /scoring and /matchups now name the same season-opening kickoff instead of disagreeing by a day; scoring locks at the earlier of the configured start and the schedule's real week-1 kickoff.
- The 404 page leads with a plain sentence and adds links back to Home and to Search help, alongside its existing joke.
- /terms no longer calls itself the commissioner edition for every manager, and now carries a last-updated date like /privacy does.
- The draft room now updates itself while you keep it open: the DRAFT button, the "you're up" pill, the page heading, the paused-clock state, and the phone pick bar all refresh live instead of freezing at the state the page had when you loaded it. Live updates now default to the mode that refetches these regions on every pick, clock change, and state change; the faster fetchless mode stays available as an override for anyone who needs it.
- The draft room's screen-reader announcement now names the team on the clock ("Los Delfines del Norte on the clock") instead of an internal seat code ("AQ2 on the clock").
- The home page marks a waiting trade offer URGENT, matching the count the attention chip already shows.
- The home page tells a manager waivers are open and names the next run, even before a claim is filed.
- A waiver claim locked on a normal kickoff reads "Locked until waivers run" instead of "Resolution degraded."
- Every page that names a waiver's resolve time — the team page, the player pool, and the claim card — now prints the same sentence: the event, the league-local run time, the zone, and a relative phrase.
- The team page explains the "H###" house-rank code the same way the player pool and Big Board already do.
- Pick'em marks your own pick with the words "Your pick," not color alone, on an open game as well as a locked one.
- A saved lineup, a filed claim, and every other result message announce once instead of twice.
- The matchups page's previous/next week arrows carry a real accessible name.
- The matchups page's freshness clock drops literal seconds and adds a relative phrase.
- League HQ's sidebar numbers every destination and shows "Draft results," matching the league settings console.
- The waiver claim form states the roster consequence and asks for confirmation before a claim that would drop a player, matching the free-agent add form.
- An accepted trade in a commissioner-veto league now says the commissioner is reviewing it, with the review deadline, instead of a league-vote count that never applies.
- The trade desk states the veto rule and review window above the offer form, and the accept confirmation names the actual outcome instead of hedging between two policies.
- The player pool's position filter chips show and work at desktop width; the phone filter rail still collapses to one row.
- The player pool's add-and-drop confirmation panel no longer overlaps the row's status chip at desktop width.
- The trade inbox's offer text no longer prints over the Accept and Decline buttons on a phone.
- A long player name on the team page shows in full at phone width instead of clipping to a few characters.
- Opening the player pool's phone filter panel no longer jumps the page or draws the panel outside its card.
- The player pool's roster-full error names the player you tried to add or claim.
- The draft room's pre-draft checklist checks a manager in directly; the check-in and autopick items post the same controls the Room tab uses, at every width, instead of a link to a hidden panel.
- One name for the draft check-in everywhere it appears: "Check in for the draft" to act, "Undo check-in" to reverse it.
- The anonymous landing page keeps "Sign in to enter." as its headline at every seat count; a full league states the fact in the detail line instead of the headline.
- The home page's sign-in button sits above the two explanatory paragraphs at every width, and the headline never grows taller than the viewport allows.
- The home Action Center sorts cards by time remaining to their deadline, so the nearer event leads regardless of its priority label.
- The rail's attention chip wraps to two clean lines instead of three, with no stray separator on its own line.
- The draft room tells a member with no seat "You are watching this draft" and, once a seat cannot be claimed, "Ask your commissioner for a seat" instead of "Get your seat ready."
- Signing in with a saved destination names the destination ("the Draft room") instead of "the page you requested."
- An unconfigured Google sign-in reads "Google sign-in is not set up on this server yet" instead of "Sign-in is not open yet," which read as a league policy.
- On a phone, a live draft no longer tells you the room has not opened yet: the pick bar now shows your own on-clock prompt while the draft is live, and a link to results once it is complete.
- The draft pool's player names stay readable once the draft goes live, on both phone and desktop, instead of collapsing to one or two characters once the value-versus-ADP column appears.
- The phone pick bar's Draft button no longer breaks its own label into three stacked letters.
- "Your pick in N" is reachable from the phone MENU sheet even when you are not on the clock.
- The MENU control's own label no longer renders upside down while its sheet is open.
- The paused pick clock reads "Paused · 2:00 left" instead of "PAUSED OF 2:00".
- A pick's confirmation toast no longer covers the pick bar on a phone or the room's own status line on desktop.
- The Big Board rail keeps player names readable at 1280 and 1440 instead of collapsing them to one or two characters.
- Drafting a player from the pool now asks you to confirm before it posts: the button opens to "Confirm <player>" on the first tap and posts on the second. Drafting a kicker, punter, or defense before the last three rounds also asks "Specialists usually go late. Draft anyway?"
- The Teams and Draft grid panels in the room now show their own heading instead of "Pick history" on every tab.
- The pick tape no longer breaks a pick number across two lines.
- The room's Draft and Add-to-board buttons now announce the player's name to a screen reader instead of a bare "Draft"/"+ RANK" repeated on every row.
- A refused pick now reads as a plain sentence ("That pick is not allowed: <reason>.") instead of a raw, lower-case error fragment.
- `/board` rows keep the same shape from row to row, and the move-up/move-down buttons meet the 44px touch-target floor on a phone.
- The room's pick-history tabs ("Picks", "Draft grid", "Teams") stay on one line at desktop width instead of wrapping one letter per line.
- The room flags the on-clock team as "Not in the room" when that seat has not been seen recently.
- The co-manager welcome flash on sign-in names the seat's primary manager, not the invitee reading it.
- A freshly bound co-manager sees a first-session panel on the home page naming the team and the primary manager by first name, and a CO-MANAGER chip beside the team name in the rail and phone menu.
- Public pages, sign-in, and the seatless states on the team page and the Big Board name the commissioner by first name and team when one is seated, instead of a generic, unreachable "ask the commissioner."
- The claim page's headline matches the member's true admission state instead of always promising a claim.
- The claim page's primary button no longer renders a doubled arrow.
- A member with no franchise seat sees the rail's team group renamed "TEAM," with Team Terminal and Big Board shown as disabled items naming "Needs a franchise seat"; Player Pool stays enabled, and the phone tab bar's Team entry names the same reason.
- The rail's numbered list no longer skips a number when Trades is hidden for a seatless member.
- Reordering or removing a ranked player on the Big Board with no JavaScript returns to the ranked panel instead of scrolling past it to the player pool, and the panel clears the sticky phone header on landing.
- Renaming the team with JavaScript on now shows a visible confirmation naming the new team name.
- A franchise still named its configured default gets an Action Center card prompting a rename, and the setup checklist stops marking personalization done until it is renamed.
- The team page's pre-draft empty-starter warning collapses to one line instead of naming each empty slot and telling the manager to sign a player before the draft has run.
- The commissioner drawer's seat coverage grid no longer squeezes into four overlapping columns at desktop width; it stays one column inside the drawer at every viewport.
- On a phone, the commissioner drawer's "Force current pick now" and "Undo last pick" controls no longer sit off-screen in hidden side columns.
- Undoing a pick from the draft room's commissioner drawer returns to the room instead of throwing the commissioner onto the console's Danger Zone.
- The activity feed and the pick tape name who actually made a pick: an autopick reads "Autopick for `<team>` selects `<player>`" and a commissioner's forced pick reads "Commissioner picks `<player>` for `<team>`", instead of both reading like the manager's own pick.
- A seat on the 20-second not-seen safety clock shows why its clock is short, in the room's command bar and in the commissioner drawer, instead of the room implying the seat still has the full two minutes.
- The commissioner drawer states the clock's true state ("Paused · 1:44 left" or "Running") instead of always claiming the draft is running, and makes the one action actually available (Pause or Resume) the bright button, with the other disabled and its reason named.
- Extend Pick defaults to 60 seconds instead of an empty field, and its error names the seconds field with a worked example instead of a bare lowercase sentence. Error toasts now read with a distinct color and an "Error:" label instead of matching a success toast's plain panel.
- The commissioner drawer's clock and seat actions return with the drawer already open, instead of closing it after every action.
- The undo record names the pick it removed ("undid pick 42: In Shedeur Time / Bucky Irving") instead of only "undid the last pick," and the drawer's own undo confirmation names the pick before asking for the typed confirmation.
- The console's reschedule-the-draft time field shows its full value instead of clipping to "09/06" with the year, hour, and AM/PM cut off.
- The console's readiness card names who has not checked in, by first name and team, with a link that opens the draft room to check them in, instead of reporting a bare ready count with no way to act.
- The console's readiness rows show each manager's own name and a plain-language presence sentence instead of a bare seat code and "no room heartbeat since this server started."
- The console's outreach control sends an already-seated manager a "please check in for the draft" reminder with the room link, instead of inviting them to a seat they already hold.
- The commissioner's own pre-draft task ("Start the draft Sunday · N of 8 checked in · open the runbook") now leads the home page's Action Center while the draft has not started, instead of a manager's own pick'em review.
- The console shows the live pick deadline in league-local time with a relative phrase, instead of a raw UTC timestamp.
- The draft-night runbook's fourth step reads without a stray space before its comma.
- Commissioner HQ's heading wraps between "Commissioner" and "HQ" instead of splitting "Commissioner" itself mid-word.
- The draft room's pre-draft checklist points commissioners to the draft-night runbook.
- The console's published draft order shows each seat's real pick number and marks the viewer's own seat.

## [release-2026.09.04-b96bb85-gosx0552] — 2026-09-04

Scope: GoSX v0.55.2 adoption, plus a phone-first pass over every route (anonymous, manager, commissioner)
in the seated, live-draft, and season states, plus the stylesheet and font
delivery path. Rolled as revision 104; no schema change.

### Changed
- The framework is GoSX v0.55.2. Comments inside `.gsx` markup compile away on the released line instead of a maintenance prerelease, and soft navigation reconciles the body in place instead of replacing it.
- The three type families are self-hosted under `public/fonts` and preloaded; the stylesheet no longer imports fonts.googleapis.com, and the Content Security Policy drops both Google font hosts.
- The server serves `styles.css` with its source comments stripped and its font URLs content-addressed (about 39 KB gzipped instead of 135 KB); the `?v=` hash now covers the served bytes.
- The anonymous header reads "Guide" and "Sign in" on one row at phone width, and public pages no longer reserve space for a fixed bar they never render, so the landing's sign-in action sits in the first phone viewport.
- Mastheads on phones lead with less space above the eyebrow, drop the second-eyebrow reserve, and set their lede at body size, so each route's first state card lands in the first viewport.

### Fixed
- The browser harness drops one known chromedp log line about `@starting-style` telemetry, measures compact density at desktop width where the toggle applies, and reports where a replay-score stall happened.
- Draft-room pool rows at phone width show the full player name instead of three characters: the rank chip carries only the active-sort rank and the detail line moves under the name.
- The phone action bar shows on the same tier as the tab bar, so tablet widths between 609 px and 899 px no longer reserve 56 px of empty bottom padding.
- Compact density keeps the 13 px small-text floor on touch widths.
- The pre-draft room title reads "opens TBD" instead of "opens TBD ·".
- The trade desk's veto policy sentence reads at text size instead of scorebug size.
- The matchup status chip hides until a live state has text, instead of rendering an empty pill in the preseason.
- The dead `--rail-breakpoint` token is gone.

## [release-2026.09.03-1828a8e-sweep6] — 2026-09-03

Scope: the sixth sweep release — the re-audit residue on the manager loop and
the console. Rolled as revision 103; no schema change.

### Fixed
- /board's header row shares the row tracks and no longer widens the page on desktop.
- /matchups starter meta lines end cleanly on desktop; the win-probability bar is an accessible meter; the featured card's manager names wrap instead of clipping at 1440.
- /pickem's "Back to current week" sits on its own row within the phone viewport.
- /team's phone action bar no longer covers the stat strip on first paint; a DST row shows its house rank.
- /draft/results names the league in its masthead; /settings says "On · sends once email is configured".


## [release-2026.09.03-38f998b-sweep5] — 2026-09-03

Scope: the fifth sweep release — the draft room's residue from the re-audit.
Rolled as revision 102; no schema change.

### Fixed
- The draft room's pool search hides non-matching rows and reports the true count, and works as a plain form without JavaScript.
- The phone countdown shows the full time to the draft; the notice banner keeps one line with a Details disclosure.
- Before the draft starts, the live region, the grid's next-pick cell, and the commissioner drawer all say the draft has not started.
- The rank shows on phones as a chip before the name; the two ranks are separated; the RK header describes the active sort.
- Draft grid team headers ellipsize; the commissioner drawer reads in plain words.
- /players shows five rows per phone screen with the injury note in the stat tip; news icons are 44 px targets on every surface; /board rows share one height.
- A stylesheet merge that dropped a closing brace is guarded by a brace-balance test.


## [release-2026.09.03-577fd49-sweep4] — 2026-09-03

Scope: the fourth sweep release — the manager loop surfaces from the comb
audits, plus the draft room's pre-draft checklist as a disclosure that
never collapses the pane grid. Rolled as revision 100; no schema change.

### Fixed
- /trades partner chips and headings use the league's real team names.
- /matchups shows each side's projection and win probability before kickoff instead of dashes; the ledger reads in plain words with one freshness clause.
- /team rows carry the news tip and house rank like the other pool surfaces, one schedule line per row, and the VIEW MATCHUP button sits under the stat strip instead of inside its scroller.
- Action Center chips read "On track" / "Needs you"; the trade veto policy is a sentence; trade composer options meet the 44 px floor; trade sections number sequentially.
- The draft room's pre-draft checklist is a closed-by-default disclosure whose open state overlays the panes; the pane grid keeps one geometry in both states at every width.
- Personal names removed from two source comments (privacy contract).


## [release-2026.09.03-4cf0542-sweep3] — 2026-09-03

Scope: the third sweep release — the commissioner and results surfaces from
the comb audits. Rolled as revision 97; no schema change.

### Fixed
- /draft/results renders the full app shell and the league's identity for signed-in members instead of the anonymous bar with a blank masthead; an unknown `?team=` code says so.
- /admin: pending invites exclude people who already hold a seat; the draft date and seat presence read as words, not raw values; the invite preview wraps instead of widening the page; the draft-night runbook marks each step done, next, or later from the league's real state.
- /admin and /commissioner report one pool-coverage figure; the commissioner page names each seat's team.
- /help: the mapping table keeps its headers on phones, the source hash is short, and topic mastheads wrap.
- Anonymous header links meet the 44 px floor; a failed avatar image no longer paints its alt text over its neighbours.


## [release-2026.09.03-e1baaa1-sweep2] — 2026-09-03

Scope: the second sweep release, from the fine-toothed-comb audits run on
faithful copies of the live league. Rolled as revision 96; no schema change.

### Fixed
- /activity's team filter lists the league's real team names, and an unknown team code says so instead of filtering silently.
- The attention chip counts only the pick'em games the viewer has not called, so it no longer shows "1 URGENT" to a manager who has picked every game.
- /scoring's jump strip is a single opaque row on phones and at most two rows on desktop; /pickem's Prev and Next stay pinned at the strip's edges.
- /blitz no longer prints empty champion labels; /settings says when email delivery is not configured; /wire says "player stat ledger" and its status dot renders.
- /players collapses its filter rail to one row on phones (seven rows visible instead of two) and, with /board, gains column headers and a legend for RK and H.
- The desktop rail fits every link at 1440×900 and 1280×800; the home status line and the rail footer wrap instead of clipping.
- Player and pool counts pluralize correctly.


## [release-2026.09.03-19e370f-sweep1] — 2026-09-03

Scope: the first release of the whole-repo sweep before the Sunday draft,
verified on faithful copies of the live league. Rolled as revision 95; no
schema change.

### Fixed
- Draft room: the pool is a real table with aligned headers and a fixed info column; the pre-draft checklist no longer hides the pool; position chips include punters and reflect the active filter; VS ADP is hidden before pick 1; the pool orders by house rank (superflex value) by default with an ADP toggle; the phone pill no longer overflows or shrinks the page; the collapsed rail no longer overprints the command bar; pre-draft copy says the draft has not started and shows the start control on phones.
- The player pool is frozen while a draft is in progress, and a resync can never drop a rostered player.
- Team defenses have projections and house ranks; manual picks apply the same scarcity guard as autopick; the projection request sends the league's scoring values.
- /players and /activity render one region for the page and the 4-second refresh, so the drop confirmation survives; the matchup stat query pages past the 1,000-row cap.
- Big Board rows no longer collapse player names beside the news icon.
- Simulator: an existing-seats rehearsal mode and a punter fallback in the bot.
- Code health: dead symbols removed, comment drift fixed, `.claude/` ignored, `/favicon.ico` served, /admin and the session-expired page have headings.


## [release-2026.09.03-eaa98d1-draftweek] — 2026-09-03

Scope: draft-week fixes from the commissioner's own use of the live site.
Rolled to the flagship as revision 94; no schema change.

### Fixed
- Punters are in the live draft pool again: the pool keeps a per-position floor (teams × slots + 4) that the ADP cut cannot remove, so a roster with a P slot can always fill it.
- A custom team avatar stored with a writable file mode no longer returns 404: the read path repairs the mode after a hash match, and a boot sweep repairs the whole store.
- A player's news headline no longer stretches Big Board, player pool, and draft rows: the row detail stays one line and the headline opens from a newspaper icon with its own detail panel.
- A managed save keeps your scroll position: runtime requests drop the section anchor from the redirect, native form posts keep it.
- Developer comments inside `.gsx` markup no longer render as page text (GoSX v0.53.11, pinned by pseudo-version).
- Two tests that depended on the wall clock or on an incidental element count are deterministic.


## [release-2026.09.02-1d2b7e4-wave7] — 2026-09-02

Scope: draft results and roster clarity, plus the mobile pass across every
page. Rolled to the flagship as revision 93; no schema change.

### Added
- `/draft/results`: the draft by team (viewer first), by round, and as a grid with sticky headers; vs-ADP on every pick; a CSV link; a home card and a nav entry after the draft completes.
- Draft round and pick (`R3 · P28`) on /players rows, the activity feed, and every /team roster row; a "Your draft class" callout on /team.
- /team: bench grouped by position with sticky headers, a positional depth line, visible FLEX eligibility, kickoff and bye on every row before lock.
- A phone action bar that keeps each page's primary verb under the thumb (/team submits SET BEST LINEUP from it); a web app manifest and home-screen icons.

### Changed
- The 44 px touch floor and 16 px inputs now key on `pointer: coarse` and `hover: none`, so landscape phones keep them; press feedback on touch; toasts anchor above the tab bar.
- The draft room's command bar collapses to a 56 px "on the clock" pill on phones with a bottom sheet for sound, League, autopick, and force-pick; the Draft grid tab is reachable on phones with "Jump to my picks".
- /matchups shows scores first on game day with a sticky week strip; /players, /activity, and /board keep search and filters sticky; /wire collapses long feeds; long documents and the console get sticky section strips.
- `viewport-fit=cover` with safe-area padding on every fixed bar; `100dvh`; the scanline overlay is off on touch devices.

### Fixed
- Sticky round and team headers on both draft grids no longer drift off-screen.
- /players no longer shows a finished draft's clock panel above the pool; the notice stack collapses on phones.
- Keyboard hints (`enterkeyhint`, `inputmode`) and email autocomplete on forms; empty `title` attributes removed; the sign-in page's heading is in the first viewport.


## [release-2026.09.02-ee12ed7-wave6] — 2026-09-02

Scope: a cumulative wave 1-6 UX and accessibility audit pass across every
league page, plus a free-tier live-scoring profile. Rolling to the flagship
now.

### Added
- A four-slot mobile bottom tab bar (Home, Team, Matchups, More) for signed-in managers.
- A shared rail attention chip and a login seat meter, both readable as text, not color alone.
- A commissioner event audit trail that attributes every admin mutation to the person who acted.
- A free-tier `LIVE_PROFILE=free` bundle sized for Tank01's free quota.
- ARIA table semantics on the starter comparison grids, for screen-reader users.

### Changed
- Review-confirm gates now cover clearing the Big Board, removing a Locker Room post, and declining a trade, matching the existing drop and trade-accept gates.
- Danger-zone reset copy uses league nouns instead of Go field names, and states plainly that only a backup restores the data.
- Every league page carries a plain sentence-case heading naming the page, replacing a slogan.
- `relativeTime` now reads a still-future instant as "in N minutes" instead of "just now".
- Sign-out on `/login` is a native page navigation again, so the signed-out state always shows.

### Fixed
- A completed draft's live pick counter no longer exceeds the final pick.
- A CSRF rejection now shows a plain "session expired" recovery page instead of a bare 403.
- A blank team-name submission shows a plain-language error instead of silently resetting the name.
- Team abbreviation normalization was restored for LAR/WSH/JAC, fixing their kickoff clock and waiver lock.
- The attention chip no longer names trades or Pick'em games to a signed-out demo-league visitor.

## [release-2026.09.01-3f78a44-adoption-wave] — 2026-09-01

Scope: the first-boot setup wizard, backup and restore, and a one-command
self-host deploy path.

### Added
- A first-boot setup wizard: draft engine, HTTP surface, review-then-commit flow, and hybrid restart.
- A boot state machine with three truthful outcomes: configured, setup, or a fail-closed operator-error page.
- Tier 0 invite-link sign-in.
- One-click league backup, nightly snapshots, and an offline restore path.
- A one-command self-host deploy for Docker Compose and Fly.io.

### Changed
- The session cookie's max age rose to 180 days.
- `/api/health` reports the configured boot state.

## [release-2026.09.01-460d658-gap-closure-wave1] — 2026-09-01

Scope: the three-layer live-scoring engine, the draft war room, the sim and
replay harness, and gap-closure fixes across scoring, waivers, and punters.

### Added
- A three-layer live-scoring design: a scoreboard tick gates a change-triggered box-score fetch, with a wire-signal trigger for fast plays.
- A draft war room: shell panes, a tape cursor, queue reorder, round-grouped pick history, and a CSV ledger export.
- The Locker Room league board.
- House rank VORP ranking beside market ADP, and an autopick guard against scarce-position starvation.
- Punter rankings from the league's embedded prior-season rescoring.
- A harness-only `/test/*` route surface and a draft rehearsal command for browser evidence.

### Fixed
- Four gap-closure items in scoring, projection, and image sizing.
- The waiver penalty floor, deferral expiry, and force-run wiring.
- Matchup scores and team lines now read honestly instead of guessing.
- The live poller's shutdown, ETag matching, and health reporting.

### Docs
- Documented the three-layer live-scoring design and the verified Tank01 quota.

## [release-2026.08.29-7fdd84f] — 2026-08-29

Scope: read-only commissioner live-operations dashboards, the postseason
bracket, and a public help center.

### Added
- Read-only live-operations dashboards for admins and commissioners, plus a scoped Trade Desk refresh region.
- A persisted postseason bracket lifecycle.
- A versioned help center with a searchable topic corpus.
- The QA-1 acceptance matrix harness for release evidence.

### Fixed
- The draft live room is event-driven instead of polling.
- Open seats are excluded from draft readiness status.
- Console and matchup layouts no longer overflow their container.

## [release-2026.08.25-36719b3] — 2026-08-25

Scope: avatar upload limits.

### Changed
- Raised the avatar upload size limit, resolution, and concurrency controls.

## [release-2026.08.25-82c8d8c] / [release-2026.08.25-90c8935] — 2026-08-25

Scope: two release names for the same commit. Draft readiness, roster
capacity, and admission-gate hardening.

### Added
- Explicit `SetReady` draft-seat readiness with claim validation.
- A roster-capacity breakdown by zone in the player pool.
- Native reorder controls for the seat board, preserving query context.

### Fixed
- Removed a false claim of automatic dynasty roster rollover.
- Waiver claims no longer resolve before their filed time during historical catch-up.
- Cross-week roster mutations are locked under store authority.

## [release-2026.08.24-ea3dcae] — 2026-08-24

Scope: the Commissioner HQ v1 federation and the fleet configuration
compiler.

### Added
- Commissioner HQ v1: a signed, bounded fleet-registry provider and consumer.
- A strict fleet configuration compiler (`fleetconfig`) with origin and image validation.
- `fleetgen`, which publishes deterministic, reviewable fleet bundles.

## [release-2026.08.24-7586755] — 2026-08-24

Scope: release provenance evidence, commissioner lineup intervention,
truthful live-state indicators, and a broad confirmation-gate pass.

### Added
- Schema-aware release compatibility evidence, shown as release provenance on commissioner fleet cards.
- Commissioner lineup intervention and authoritative starter ledgers on matchup scores.
- Private waiver receipts with claim reordering, and trade history with failure receipts.
- A stage-aware action center on the home dashboard and team terminal.
- A 44px mobile touch-target baseline across content controls.

### Changed
- Destructive admin actions now require explicit typed confirmation.
- Live indicators reflect authoritative state instead of an optimistic guess.
- Notification delivery status is reported accurately instead of assumed.

### Fixed
- Pick'em now treats an unavailable market as a void pick, not a broken submission.
- The live week API is protected behind league access.
- Scoring rejects non-finite point values and reverts a failed write.
- Trades close to new offers at the deadline but stay open to execute an accepted one.

## [release-2026.08.23-3d72967-lifecycle-ready] — 2026-08-23

Scope: draft lifecycle completion.

### Fixed
- The draft lifecycle now completes correctly and enforces roster-safe picks.

## [release-2026.08.23-e8cfcc4-responsive-sweep] — 2026-08-23

Scope: responsive layout fixes.

### Fixed
- Team and Pick'em layouts no longer overflow on narrow viewports.

## [release-2026.08.23-9178977-mobile-ready] — 2026-08-23

Scope: mobile masthead fix.

### Fixed
- The masthead no longer overflows beside the navigation rail.

## [release-2026.08.23-7e68ba5-mobile-ready] — 2026-08-23

Scope: draft check-in.

### Added
- A commissioner control to explicitly set a seat's draft readiness.

### Changed
- Clearer draft check-in guidance and state styling.

## [release-2026.08.22-91378de-season-ready] — 2026-08-22

Scope: draft order and commissioner switching.

### Added
- A commissioner league switcher in the admin console.

### Fixed
- The draft clock applies a NOT SEEN cap for an unresponsive seat.
- The draft order and schedule publish exactly once.

## [release-2026.08.22-3def224-season-ready] — 2026-08-22

Scope: the first tagged release. The core league app: draft room with a
live pick clock and autopick, scoring and playoffs, Pick'Em, waivers,
trades, notifications, and the commissioner console.
