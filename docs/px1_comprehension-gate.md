# PX-1 comprehension gate

Status: **OPEN — NOT RUN**

This is the participant worksheet for the scripted comprehension gate in the
Acceptance section of spec.gridiron.manager-onboarding.v0.1. It is a test
script and an empty response record, not a claim that PX-1 has passed. It
must be run with isolated dynasty and redraft fixtures and real participants;
browser or unit automation cannot substitute for comprehension.

The worksheet uses the executable help corpus at app/help/content.go and the
current source snapshot 68966ea41dff0656437e7406a49a64e2a8e3264b as its
provenance. Runtime pages own mutable league names, deadlines, timezones,
phase, capabilities, locks, and data freshness. Do not paste those values,
personal identities, email addresses, invite links, credentials, tokens, or
private deployment paths into this repository.

## Running the gate

1. Prepare one isolated fixture for the scenario row. Verify its mode,
   admission state, team association, team seat, base role, and commissioner
   capability from the running page; do not infer them from the scenario
   label.
2. Give the participant only the scenario label and access to the normal
   /help entry point and the owning runtime route. The facilitator may
   explain how to navigate, but may not supply an answer, paraphrase a rule,
   point at a control, or privately coach the participant.
3. Ask the six prompts below in order. Record the participant's answer and
   the route or help topic they used. Record role labels and semantic
   outcomes, not names or other identifying values.
4. When a value is loading, stale, degraded, offline, unavailable, failed,
   locked, disabled, or not applicable, accept only an answer that names
   that state and follows the displayed recovery or boundary. A participant
   must not guess a date, score, permission, priority, or result.
5. Reset the fixture and response record before the next scenario. A response
   from one role, mode, or account cannot be reused for another row.

## Scenario matrix

These are fixture labels, not participant identities. They exercise every
base role in both supported modes and compose commissioner capability
independently of the team role.

| Scenario | Mode | Base role | Commissioner overlay | Fixture state the participant must recognize |
| --- | --- | --- | --- | --- |
| D-P-C | dynasty | primary manager | present | admitted, associated with a named team, responsible for that team's shared seat and roster, with a separate commissioner capability |
| D-C | dynasty | co-manager | absent | admitted, associated with the primary manager's team seat, sharing that seat's roster and permissions subject to the displayed predicates |
| D-S | dynasty | seatless member | absent | admitted, no team association and no team seat; league reading and activity may remain available |
| R-P | redraft | primary manager | absent | admitted, associated with a named team, responsible for that team's shared seat and roster |
| R-C-C | redraft | co-manager | present | admitted, associated with the primary manager's team seat, with commissioner capability shown as an independent overlay |
| R-S-C | redraft | seatless member | present | admitted and seatless, with commissioner capability; the overlay does not create or imply a commissioner-owned roster |

If a fixture cannot realize one of these states, mark that row blocked and
record the missing capability. Do not replace it with a fabricated role or
silently treat a different state as equivalent.

## Participant prompt script

Ask the following without leading language. The participant may reread the
page and use the linked help, but the facilitator must not correct an answer
until all six prompts are complete.

### 1. Identity, admission, and seat

“Using the product terms shown here, what is your identity state, admission
or membership state, team association, team seat, base team role, and
commissioner capability? Keep those concepts separate.”

Record whether the participant distinguishes:

- the person from the authenticated identity;
- admission or membership from team association;
- team association from a team seat and roster; and
- a primary manager or co-manager role from the orthogonal commissioner
  capability.

For a seatless row, a correct answer says that seatless membership is valid
and does not invent a roster or an assignment control. For a commissioner
row, a correct answer includes the base role and separately names the
overlay.

### 2. Next task and unavailable action

“What is your next applicable task? Which route owns it? Name one action
that is unavailable or not applicable to you right now, and explain the
runtime predicate that makes it so.”

The answer must follow the current checklist and capability/phase/data
state. It may name /help as the orientation route, but it must identify
the owning product surface for the task. A disabled action requires the
displayed role, phase, lock, capability, or data-state reason; a missing
feature is not a dead obligation.

### 3. Exact deadline

“What is the next material deadline relevant to this task? Read the exact
league-local date and time, timezone, and relative text as displayed, and
name the route that owns it. If no reliable deadline is available, what
state explains that?”

The answer key is the live rendered value, not a value in this worksheet.
The participant passes only if the date, time, timezone, relative text, and
owning route match the fixture capture, or if they correctly say that the
deadline is unavailable or not applicable and explain why.

### 4. Consequence and reversibility

“Choose one consequential action that is applicable to you. What league,
team, or object would it affect? Who can see the result? What is the
consequence, what part is reversible, and where would you find the local
result or recovery path? If no mutation is applicable, say so.”

The participant must distinguish a preview or read from a mutation, name
the affected object, state visibility, and identify the runtime boundary at
which reversal stops. They must not claim that a submission was saved merely
because a form was visited.

### 5. Big Board, AUTO, and privacy

“Whose Big Board are you seeing? Is its order shared with the other manager
of the team seat? Which order does AUTO consume? What can a commissioner
who is not a member of that seat see?”

For the current executable source and accepted seat decision, the expected
answer is one private durable order for the team seat; the primary manager
and co-manager share it; AUTO uses the first available entry and then the
best available player; and a commissioner outside that seat sees only
seat-level readiness, presence, and board gap/count signals. The participant
must use the current help/runtime copy, not a remembered rule.

This answer is intentionally recorded alongside the source-divergence note
in the evidence packet. The draft manager-onboarding specification describes
an older per-account board model, while the accepted repository decision and
executable projection describe the seat-scoped model. Do not coach toward
either answer; record any conflicting projection as an open P0 mismatch.

### 6. Unknown, stale, or unavailable recovery

“If the result of an attempted read or action is unknown, stale, or
unavailable, what do you do next? How do you keep your task context, which
route do you reread, when do you inspect Activity, and when would you retry?”

The expected recovery is to preserve the route, query, team/week, filter,
form, or focus context when safe; reread the owning surface and Activity
before retrying; avoid replaying a stale mutation; and use the displayed
retry or escalation path. The participant must distinguish unknown outcome
from a confirmed failure or saved result.

## Answer key and scoring

The following keys are semantic. Mutable values must be checked against the
fixture's captured runtime output at the time of the session.

| Prompt | Pass condition | Critical false answer |
| --- | --- | --- |
| Identity, admission, and seat | Separates identity, admission, association, seat, roster, base role, and commissioner overlay; matches the scenario's runtime predicates | Claims a sign-in is membership, gives a seatless participant a roster, or treats commissioner capability as a team seat |
| Next task and unavailable action | Names an applicable task and owning route; explains an unavailable action with its displayed predicate | Invents a capability, presents a non-applicable action as an obligation, or claims a role can bypass a gate |
| Exact deadline | Repeats the live date, time, timezone, and relative text, or correctly reports unavailable/not-applicable | Any invented, stale, wrong-timezone, or wrong-route deadline |
| Consequence and reversibility | Names affected object, visibility, effect, reversible boundary, and result/recovery location | Claims a preview is a mutation, a mutation is harmless or permanently reversible, or exposes private results |
| Big Board and AUTO | Matches the current executable projection and privacy boundary, including seat sharing and AUTO precedence | Any false board ownership, shared-order, AUTO, or commissioner-visibility rule |
| Recovery | Preserves context, rereads the owner route and Activity before retrying, and avoids replaying an unknown mutation | Retries blindly, discards context, or treats unknown/stale/unavailable as saved |

An omitted or uncertain answer is FAIL — follow-up, not a success. A false
role, admission, seat, deadline, timezone, privacy, consequence,
reversibility, Big Board, AUTO, or recovery answer is a **P0** acceptance
failure under the canonical Acceptance contract. Facilitator correction
after the prompt does not turn that answer into a pass.

## Run record

Copy this block for each scenario. Keep participant and fixture identifiers
out of committed documentation; use an internal receipt reference if the
owner has a private evidence store.

~~~text
Scenario label:
Mode/base role/commissioner overlay:
Fixture receipt reference:
Viewport and owning route:

Q1 identity/admission/team association/seat/base role/commissioner:
Q2 next task/owning route/unavailable action/reason:
Q3 exact deadline/timezone/relative text/owning route:
Q4 affected object/visibility/consequence/reversibility/result:
Q5 Big Board/AUTO/privacy:
Q6 unknown-stale-unavailable recovery:

No private coaching: NOT RECORDED / YES / NO
Facilitator corrections before final answer: NOT RECORDED / YES / NO
Critical-answer result: NOT RUN / PASS / FAIL — FOLLOW-UP / P0
Participant confidence (optional, participant-reported):
Owner review: OPEN
~~~

## Completion boundary

PX-1 remains open until all six scenario rows have real participant
responses that pass without private coaching, the owner reviews the packet,
and the Big Board source divergence is reconciled or explicitly accepted
with a recorded decision. Automated receipts listed in
px1_evidence-packet.md prove implementation projections only; they do not
fill this worksheet or provide a participant/owner sign-off.
