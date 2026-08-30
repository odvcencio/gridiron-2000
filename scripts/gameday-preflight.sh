#!/bin/sh
# gameday-preflight.sh: read-only Sep 10 2026 game-day preflight.
#
# Run this before the T-30m flag flip. It only reads state: it never
# applies a manifest, patches a Secret, or rolls a pod. It checks:
#   1. /api/health on the target host (appVersion, frameworkVersion,
#      stateSchema.compatible).
#   2. The shared relay is reachable and reports a budget, through a
#      read-only kubectl port-forward (the relay Service has no public
#      route and both app images are distroless, so no in-pod curl
#      exists to reach it any other way).
#   3. The live-scoring flag's current value in the tracked Deployment
#      manifest.
#   4. It prints the exact flip and kill-switch-drill commands from
#      docs/launch-checklist.md step 13.5 and
#      docs/season-operations.md#kill-switch-procedure.
#
# See docs/launch-checklist.md step 13.5 for the drill this script
# prepares, and docs/season-operations.md#kill-switch-procedure for the
# full drill steps and the LEDGER-not-PAUSED finding
# (sim_gameday_test.go's TestSimGameDayTimeline is the harness evidence).
#
# Host and namespace default to this repository's flagship instance;
# override with the environment variables below for another instance.
set -eu

HOST="${GAMEDAY_HOST:-gridiron.draco.quest}"
NAMESPACE="${GAMEDAY_NAMESPACE:-gridiron}"
DEPLOYMENT="${GAMEDAY_DEPLOYMENT:-gridiron-2000}"
DEPLOYMENT_MANIFEST="${GAMEDAY_DEPLOYMENT_MANIFEST:-deploy/k8s/deployment.yaml}"
RELAY_NAMESPACE="${GAMEDAY_RELAY_NAMESPACE:-gridiron}"
RELAY_SERVICE="${GAMEDAY_RELAY_SERVICE:-statrelay}"
RELAY_LOCAL_PORT="${GAMEDAY_RELAY_LOCAL_PORT:-18090}"
SEASON="${GAMEDAY_SEASON:-2026}"
WEEK="${GAMEDAY_WEEK:-1}"

fail=0

section() {
	printf '\n== %s ==\n' "$1"
}

require_command() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "FAIL: required command '$1' is not on PATH" >&2
		exit 1
	fi
}

require_command curl
require_command jq
require_command kubectl

section "1/4 /api/health (https://${HOST})"
health_body="$(curl --fail-with-body -sS "https://${HOST}/api/health" 2>&1)" && health_ok=1 || health_ok=0
if [ "$health_ok" -ne 1 ]; then
	echo "FAIL: curl https://${HOST}/api/health failed: ${health_body}" >&2
	fail=1
else
	compatible="$(printf '%s' "$health_body" | jq -r '.stateSchema.compatible // "null"')" || compatible=""
	printf '  appVersion:             %s\n' "$(printf '%s' "$health_body" | jq -r '.appVersion // "?"')"
	printf '  gitSHA:                 %s\n' "$(printf '%s' "$health_body" | jq -r '.gitSHA // "?"')"
	printf '  buildDate:              %s\n' "$(printf '%s' "$health_body" | jq -r '.buildDate // "?"')"
	printf '  frameworkVersion:       %s\n' "$(printf '%s' "$health_body" | jq -r '.frameworkVersion // "?"')"
	printf '  stateSchema.compatible: %s\n' "$compatible"
	if [ "$compatible" != "true" ]; then
		echo "FAIL: stateSchema.compatible is not true" >&2
		fail=1
	fi
fi

section "2/4 relay reachable + budget header (svc/${RELAY_SERVICE} -n ${RELAY_NAMESPACE}, read-only port-forward)"
pf_log="$(mktemp)"
pf_pid=""
cleanup_port_forward() {
	if [ -n "$pf_pid" ]; then
		kill "$pf_pid" >/dev/null 2>&1 || true
		wait "$pf_pid" 2>/dev/null || true
	fi
	rm -f "$pf_log"
}
trap cleanup_port_forward EXIT INT TERM HUP

kubectl -n "$RELAY_NAMESPACE" port-forward "svc/${RELAY_SERVICE}" "${RELAY_LOCAL_PORT}:80" >"$pf_log" 2>&1 &
pf_pid=$!

# Poll for the forward to report ready rather than a fixed sleep; bounded
# at 5 seconds (20 tries * 0.25s) so an unreachable cluster fails fast.
ready=0
tries=0
while [ "$tries" -lt 20 ]; do
	if grep -q "Forwarding from" "$pf_log" 2>/dev/null; then
		ready=1
		break
	fi
	if ! kill -0 "$pf_pid" 2>/dev/null; then
		break
	fi
	sleep 0.25
	tries=$((tries + 1))
done

if [ "$ready" -ne 1 ]; then
	echo "FAIL: kubectl port-forward to svc/${RELAY_SERVICE} -n ${RELAY_NAMESPACE} did not become ready" >&2
	cat "$pf_log" >&2
	fail=1
else
	# curl's own transport outcome, the relay's HTTP status, and the
	# budget-header check are kept apart (item 5): a relay outage (curl
	# fails, or the relay answers with a non-200) must FAIL with its own
	# distinct message, not read as "no budget configured."
	relay_headers="$(curl -sS -D - -o /dev/null -w 'HTTPSTATUS:%{http_code}\n' \
		"http://127.0.0.1:${RELAY_LOCAL_PORT}/getNFLGamesForWeek?week=${WEEK}&seasonType=reg&season=${SEASON}" \
		2>&1)" && curl_ok=1 || curl_ok=0
	relay_headers="$(printf '%s' "$relay_headers" | tr -d '\r')"
	http_status="$(printf '%s\n' "$relay_headers" | sed -n 's/^HTTPSTATUS://p')"
	if [ "$curl_ok" -ne 1 ]; then
		echo "FAIL: curl to the relay failed: ${relay_headers}" >&2
		fail=1
	elif [ "$http_status" != "200" ]; then
		echo "FAIL: the relay responded with HTTP ${http_status:-?} (not 200); check svc/${RELAY_SERVICE} -n ${RELAY_NAMESPACE}" >&2
		fail=1
	else
		remaining="$(printf '%s\n' "$relay_headers" | grep -i '^X-Statrelay-Budget-Remaining:' | awk '{print $2}')"
		if [ -z "$remaining" ]; then
			echo "FAIL: the relay responded 200 with no X-Statrelay-Budget-Remaining header (a budget must be set: STATRELAY_DAILY_BUDGET)" >&2
			fail=1
		else
			printf '  X-Statrelay-Budget-Remaining: %s\n' "$remaining"
		fi
	fi
fi
cleanup_port_forward
trap - EXIT INT TERM HUP

section "3/4 live-scoring flag (${DEPLOYMENT_MANIFEST})"
if [ -f "$DEPLOYMENT_MANIFEST" ]; then
	flag_line="$(grep -A1 'name: LIVE_SCORING_ENABLED' "$DEPLOYMENT_MANIFEST" | grep 'value:' | head -1 | sed -e 's/^ *value: *//' -e 's/"//g')"
	if [ -z "$flag_line" ]; then
		echo "FAIL: LIVE_SCORING_ENABLED not found in ${DEPLOYMENT_MANIFEST}" >&2
		fail=1
	else
		printf '  LIVE_SCORING_ENABLED (tracked manifest): %s\n' "$flag_line"
	fi
else
	echo "FAIL: ${DEPLOYMENT_MANIFEST} not found (run this from the repository root)" >&2
	fail=1
fi

section "4/4 flip and kill-switch-drill commands"
cat <<EOF
  Flip ON, 30 minutes before kickoff (launch-checklist.md step 13.5):
    Edit ${DEPLOYMENT_MANIFEST}: set LIVE_SCORING_ENABLED to "true", then:
      kubectl apply -f ${DEPLOYMENT_MANIFEST}
      kubectl -n ${NAMESPACE} rollout status deployment/${DEPLOYMENT} --timeout=5m

  Kill-switch drill, one hour after kickoff (a starter's game must be
  in progress; season-operations.md#kill-switch-procedure):
    Edit ${DEPLOYMENT_MANIFEST}: set LIVE_SCORING_ENABLED to "false", then:
      kubectl apply -f ${DEPLOYMENT_MANIFEST}
      kubectl -n ${NAMESPACE} rollout status deployment/${DEPLOYMENT} --timeout=5m
    Within 60 s, confirm the Matchups status line reads LEDGER
    ("Weekly ledger (nflverse)"), NOT PAUSED: a boot-time-disabled
    poller has no in-progress game history to pause on (the finding
    behind this doc fix; see TestSimGameDayTimeline).
    Then edit ${DEPLOYMENT_MANIFEST} again: set LIVE_SCORING_ENABLED
    back to "true", apply it, and confirm the status line reads LIVE
    within 60 s.

  Log the drill in docs/launch-checklist.md's "Kill-switch drill log" table.
EOF

if [ "$fail" -ne 0 ]; then
	echo
	echo "gameday-preflight: one or more read-only checks failed; see FAIL lines above." >&2
	exit 1
fi
echo
echo "gameday-preflight: all read-only checks passed."
