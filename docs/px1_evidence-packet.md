# PX-1 evidence packet

Status: **OPEN — participant and owner review NOT RUN**

This packet inventories the evidence required by the Acceptance section of
spec.gridiron.manager-onboarding.v0.1. It separates executable receipts from
the human comprehension gate; neither category is silently substituted for
the other. It contains no participant identity, email address, invite link,
credential, mutable league value, or private deployment path.

Provenance for this preparation is the isolated Gridiron source snapshot
68966ea41dff0656437e7406a49a64e2a8e3264b. The help corpus declares version
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
68966ea41dff0656437e7406a49a64e2a8e3264b. That older receipt is retained by
the existing projection contract and is not presented here as a fresh human
or deployment verification.

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
| A-12 | docs/px1_operator-help-projection.md projection checks; GoSX help/field/glossary surfaces named by app/help/content.go | Identifies the upstream projection owner and the framework-shaped surfaces that require an upstream receipt or reviewed local exception | OPEN — no dedicated released/pinned GoSX assessment receipt or local-exception record is attached to this packet |
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
