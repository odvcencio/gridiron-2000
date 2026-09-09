# PX-1 evidence packet

Status: **OPEN — participant and owner review NOT RUN**

This packet inventories the evidence required by the Acceptance section of
spec.gridiron.manager-onboarding.v0.1. It separates executable receipts from
the human comprehension gate; neither category is silently substituted for
the other. It contains no participant identity, email address, invite link,
credential, mutable league value, or private deployment path.

Provenance for this preparation is the isolated Gridiron source snapshot
35ed7f8b92fce272b8ad88f7990df5cf7102fe58. The help corpus declares version
0.1 and records its own reviewed source receipt in
docs/px1_help_corpus.md. A source snapshot is evidence provenance, not a
production or release claim.

## What can close PX-1

The packet can close only when all of these have a receipt:

1. the complete corpus, topic, search, workflow/state, static, projection,
   route/link, and privacy inventory passes its automated checks;
2. required authenticated desktop/mobile accessibility traces and any
   framework-shaped upstream assessment or reviewed local exception are
   recorded;
3. remote relay, redirect, source/image, and release-identity drift is
   reconciled or explicitly marked with an owner disposition;
4. the six scenario rows in px1_comprehension-gate.md have real participant
   answers from isolated dynasty/redraft fixtures;
5. participants answer every critical question without private coaching;
6. an owner reviews the answer records and unresolved source differences; and
7. the existing Big Board decision and all projections are confirmed
   consistent, or the historical wording is corrected through the normal
   owner/documentation path.

The current status remains OPEN. The tables below deliberately contain
NOT RUN and OPEN values where no human receipt exists.

## Source authority and drift reconciliation

The source precedence in the onboarding contract makes executable runtime
truth authoritative for current roles, capabilities, deadlines, privacy, and
results. Product intent and accepted decisions explain the intended model;
tests and projections are evidence, not a replacement for that truth.

| Source | Exact claim or role in this packet | Status and boundary |
| --- | --- | --- |
| spec.gridiron.manager-onboarding.v0.1, Acceptance | Requires representative dynasty/redraft primary, co-manager, seatless, and commissioner-overlay comprehension with exact deadline, consequence/reversibility, Big Board truth, and recovery answers | Canonical draft acceptance contract; participant and owner receipts are still open |
| docs/decisions/0001-seat-scoped-big-board.md | Accepted decision 0001 (version 1.0, 2026-08-24): one private durable Big Board belongs to a team seat; primary and co-manager share it; AUTO takes the first available entry then best available; an outside commissioner sees readiness, presence, and gap/count only | Accepted repository decision; product evidence, not human comprehension |
| app/help/content.go | Executable topic, checklist, glossary, privacy, route, and current Big Board projection | Runtime/source projection; verify against the running fixture during the gate |
| docs/px1_manager-handbook.md | Generated manager projection of the executable corpus, including the shared-seat board copy | Static projection; it cannot establish participant understanding |
| docs/px1_help_corpus.md and docs/px1_operator-help-projection.md | Corpus/projection contracts, state vocabulary, source-only boundary, and executable check list | Static source receipts; not a production or human acceptance receipt |

The draft onboarding contract's Big Board section describes an older
per-account model and says shared-team-board copy is gated. The accepted
repository decision and executable projection now describe a seat-scoped
shared order. This packet uses the accepted decision and current executable
projection for the participant answer key, but records the historical
wording discrepancy as OPEN for owner/documentation reconciliation. No new
Big Board product decision is requested: decision 0001 is already accepted.
The discrepancy must not be treated as reconciled merely because the
generated handbook is internally consistent. A participant who receives
conflicting copy produces a P0 mismatch record, not a coached answer.

The corpus documents record reviewed source SHA
43c1c50dfbd493b01368d2c74959eb23de098749, while this preparation starts from
35ed7f8b92fce272b8ad88f7990df5cf7102fe58. That older receipt is retained by
the existing projection contract and is not presented here as a fresh human
or deployment verification.

## GoSX v0.56.1 framework assessment

Assessment status: REVIEWED against the m31labs.dev/gosx v0.56.1 module
required by this snapshot's go.mod and go.sum. The source and framework
contract tests named below were read at that pinned version. This is a
compatibility and ownership receipt for the PX-1 help surfaces; it is not a
GoSX release sign-off, an accessibility receipt, or a participant/owner
comprehension receipt.

Pin receipt: go.mod requires m31labs.dev/gosx v0.56.1; go.sum records module
checksum h1:uVG0qWVzMiGWpagWoJyunJKChcJFT1dDKJCnN8BafXY= and go.mod checksum
h1:Ug3rYHb8JX3agdTwc55OgGsKgHZ1U+qflXrFsN9Prbg=. The assessment is tied to
that exact dependency, not to an unpinned or locally replaced framework.

| Surface | Reviewed GoSX contract | Gridiron use and explicit local boundary |
| --- | --- | --- |
| Route, query, and render | route/route.go exposes RouteContext.Param, Query, ActionPath, ActionState, and ActionStates. route/filesystem.go provides AddDir and DefaultFileRenderer; route/fileprogram.go lowers file-based components, including TextBlock. | app/help/page.server.go and app/help/_topic_id/page.server.go own topic lookup, runtime data, state guidance, and field projections. The app supplies league/help data to the framework route and renderer; it does not reimplement URL parsing or file rendering. |
| data-gosx-link and runtime handles | server/navigation_contract.go defines NavigationLinkAttr. client/runtime/host/navigation.ts resolves managed links through isManagedNavigationLink, shouldHandleLink, and closestLink, with same-origin checks and native opt-out behavior. route/filesystem_test.go covers file-rendered link lowering and managed-link state. | Help templates use data-gosx-link for same-origin topic and owning-action links. GoSX owns the managed navigation lifecycle and handle discovery; Gridiron does not duplicate a client navigation registry or soft-navigation implementation. |
| TextBlock and semantic as values | server/textblock.go provides TextBlockProps, TextBlock, TextBlockAttrs, and bootstrap/native modes. route/fileprogram.go maps the GSX as or tag attribute to TextBlockProps.Tag. server/textblock_test.go covers source, hints, native mode, and bootstrap attributes. | app/help/page.gsx and app/help/_topic_id/page.gsx use TextBlock, including TextBlock as="dfn" for glossary terms. app/help textflow tests verify the rendered contract. Text measurement, text-layout bootstrap, and semantic tag lowering remain framework-owned; no local text-layout clone is claimed. |
| Action validation, feedback, and return targets | action/action.go provides Result, Validation, View, ReturnTargetField, and Context.RedirectBackWithMessage; route.RouteContext exposes ActionState and ActionStates. action/return_target_test.go covers native and managed POST-redirect/JSON behavior, fallback/root safety, and explicit redirects. | Gridiron maps domain failures to safe topic/field wording and uses internal/navigation.SafeActionReturnPath for contextual help. That validator additionally enforces UTF-8 validity, decoded leading-slash checks, double-encoded ambiguity rejection, authentication/action-route denial, fragment rules, and a 1024-byte cap. GoSX rootRelativeTarget is an internal helper, not a public drop-in for this policy; a generic public sanitizer is only a candidate and is not implemented or counted here. |
| Action and component registries | action/action.go provides action.Registry with Register, Invoke, Has, List, and ServeHTTP. components/registry.go provides registry-backed component Register, Lookup, Render, and Bindings with stable names. | Gridiron uses GoSX action registration/dispatch for route actions. The help TopicCorpus, Search, ChecklistFor, and ContextualFieldHelp values are league-domain data and intentionally remain in app/help/content.go rather than being misrepresented as framework registry entries. |
| App-domain generator | GoSX does not own Gridiron's topic corpus or PX-1 projections. | cmd/helpdocs/main.go projects app/help/content.go into the tracked manager, commissioner, and operator documents. Those outputs retain the generated “do not edit by hand” contract; the generator and its domain vocabulary are an explicit app-local exception, not a framework gap. |

The reviewed split is therefore: GoSX owns route/query/render primitives,
managed same-origin link handles, text-flow lowering, and action/result
transport; Gridiron owns league-specific topics, search, role/checklist
predicates, safe contextual-help policy, domain validation wording, and the
helpdocs projection. The source review found no reason to duplicate a GoSX
runtime primitive in the PX-1 corpus. Required accessibility, route/link
completeness, release/source-image, remote-relay, human-comprehension, and
owner-review receipts remain separately open below.

## Automated evidence inventory

These entries are exact source/test receipts. They prove the behavior named
in the third column; they do not prove that a participant understood it or
that an owner reviewed the result.

| ID | Exact source or command | What it proves | Human acceptance status |
| --- | --- | --- | --- |
| A-01 | app/help/content_test.go: TestCorpusValidatesAndContainsStableTopicInventory; TestSearchGoldensAreDeterministic; TestSearchFindsThisLeagueSignatureRules | Topic inventory, deterministic search, and the required search fixtures | Automated only |
| A-02 | app/help/content_test.go: TestRoleChecklistComposesCommissionerOverlay; TestMigrationAndGlossaryStayPIIFreeAndRuntimeOwned; TestTopicSummariesUseTheFreshnessVocabulary | Base-role/commissioner composition, privacy/runtime ownership, and freshness vocabulary | Automated only |
| A-03 | app/help/projection_test.go: TestHelpDocsInventoryMatchesExecutableCorpus; TestVerifiedSourceSHARecordsReviewedOriginSnapshot; TestChecklistProjectionUsesViewerRoleAndOrthogonalCommissioner; TestFailedGuidanceNamesUnknownOutcomeAndReread | Static projection inventory, reviewed-source receipt, role projection, and unknown-outcome recovery copy | Automated only |
| A-04 | app/help/_topic_id/page_render_test.go: TestTopicRouteRendersStateSemanticsValidationAndOwningTopic; cmd/helpdocs checks listed in docs/px1_operator-help-projection.md | Topic route state/validation wiring and generated projection drift/link/privacy checks | Automated only |
| A-05 | help_return_context_browser_test.go: TestBrowserContextualHelpReturnsToTeamAndMatchupsTask at 390x844 and 1440x900 | Authenticated simulated browser can return from contextual help to Team and Matchups context | Browser automation only; not comprehension |
| A-06 | internal/league/board_clear_drafted_test.go: TestBoardClearDraftedRemovesOnlyTakenEntries; internal/league/draftclock_test.go: TestAutopickPrecedence | Board cleanup and AUTO precedence at the service layer | Service automation only; not participant privacy comprehension |
| A-07 | docs/decisions/0001-seat-scoped-big-board.md | Accepted product decision for current team-seat board ownership and commissioner visibility | Owner decision receipt exists; PX-1 owner reconciliation still OPEN |
| A-08 | docs/px1_operator-help-projection.md, section “Evidence status” | Explicit source-only boundary for the operator projection | Source-only; no runtime or participant claim |
| A-09 | README.md and docs/README.md documentation tables; docs/px1_help_corpus.md and docs/px1_operator-help-projection.md local references | Locator, corpus, projection, and owning-route links are discoverable; generated projection links can be checked with the helpdocs command | Static/link evidence only; the complete Acceptance route/link report is NOT RUN |
| A-10 | app/help/content.go and app/help/content_test.go; app/help/projection_test.go; app/help/_topic_id/page_render_test.go | Workflow/state vocabulary, role predicates, privacy, recovery, topic routes, and static projection checks | Automated source evidence only; the complete mode/phase/workflow-state matrix is NOT RUN |
| A-11 | help_return_context_browser_test.go: TestBrowserContextualHelpReturnsToTeamAndMatchupsTask at 390x844 and 1440x900 | Existing authenticated browser trace for contextual help return and task context | Existing contextual trace only; the required full desktop/mobile accessibility trace for the comprehension packet is NOT RUN |
| A-12 | docs/px1_operator-help-projection.md projection checks; “GoSX v0.56.1 framework assessment” above; GoSX action, route, navigation, TextBlock, and registry surfaces named there | Identifies the upstream projection owner, records the pinned framework contract, and names the reviewed app-local exceptions for domain corpus, contextual return policy, validation wording, and helpdocs generation | REVIEWED against the go.mod/go.sum v0.56.1 pin; this does not close PX-1, accessibility, participant, owner, or release gates |
| A-13 | cmd/statrelay/relay_test.go; deploy/k8s/http-redirect.yaml; deploy/k8s/sk/http-redirect.yaml; docs/launch-checklist.md; app/commissioner/release_metadata_test.go | Exact source locations for relay behavior, HTTP redirects, release/image identity, and deployment drift evidence | OPEN — no fresh relay/redirect/source-image drift reconciliation is claimed in this PX-1 packet |

Recommended focused commands for a fresh automated receipt are:

    go test ./app/help -count=1
    go test ./app/help/_topic_id -count=1
    go test ./cmd/helpdocs -count=1
    go vet ./cmd/helpdocs
    go run ./cmd/helpdocs check --out docs
    go test ./internal/league -run 'TestBoardClearDraftedRemovesOnlyTakenEntries|TestAutopickPrecedence' -count=1

The contextual browser receipt must be recorded separately from the human
session. It establishes navigation/rendering only; it cannot establish
confidence, comprehension, or the absence of private coaching.

## Human comprehension and owner receipts

The participant script is in px1_comprehension-gate.md. These rows are
intentionally blank until a real session occurs:

| Receipt | Required run | Status |
| --- | --- | --- |
| H-01 | D-P-C: dynasty primary manager with commissioner overlay | NOT RUN |
| H-02 | D-C: dynasty co-manager | NOT RUN |
| H-03 | D-S: dynasty seatless member | NOT RUN |
| H-04 | R-P: redraft primary manager | NOT RUN |
| H-05 | R-C-C: redraft co-manager with commissioner overlay | NOT RUN |
| H-06 | R-S-C: redraft seatless member with commissioner overlay | NOT RUN |
| H-07 | A no-private-coaching check and verbatim critical-answer review for each row | NOT RUN |
| H-08 | Owner review of participant records, automated receipts, and source drift | OPEN |

The six rows ask for identity/admission/team-seat state, next task and
unavailable action, exact runtime deadline/timezone/relative text,
consequence/reversibility/privacy, Big Board/AUTO truth, and recovery. A
successful browser trace cannot populate H-01 through H-07.

## Response summary

Use semantic labels and internal receipt references only. Do not commit
participant names, emails, team names, timestamps, invite URLs, or private
screenshots.

| Scenario | Participant receipt | Critical answers | No private coaching | Result |
| --- | --- | --- | --- | --- |
| D-P-C | NOT RECORDED | NOT RUN | NOT RECORDED | NOT RUN |
| D-C | NOT RECORDED | NOT RUN | NOT RECORDED | NOT RUN |
| D-S | NOT RECORDED | NOT RUN | NOT RECORDED | NOT RUN |
| R-P | NOT RECORDED | NOT RUN | NOT RECORDED | NOT RUN |
| R-C-C | NOT RECORDED | NOT RUN | NOT RECORDED | NOT RUN |
| R-S-C | NOT RECORDED | NOT RUN | NOT RECORDED | NOT RUN |

## Unresolved risks

- The draft specification and accepted/runtime Big Board models need an
  explicit owner reconciliation before this row can close.
- The existing corpus source receipt is an older reviewed SHA than this
  preparation snapshot; no fresh corpus review is claimed here.
- Automated browser, unit, and generated-projection receipts do not measure
  human comprehension, confidence, or private coaching.
- Mutable deadlines, timezones, permissions, phase, locks, data states, and
  team association must be captured from each isolated runtime session; this
  packet intentionally contains no fixed answer for them.

## Current conclusion

PX-1 is **not complete**. This packet is ready for a real, no-coaching
participant run and an owner review, but it does not fabricate either one.
