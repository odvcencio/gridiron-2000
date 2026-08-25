#!/usr/bin/env bash
set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

echo "[qa1] canonical league matrix: 588 hermetic rows"
go test ./internal/league -run '^TestQA1AcceptanceMatrix$' -count=1

echo "[qa1] existing public-entry render matrix: dynasty/redraft × identity"
go test ./app/login -run '^TestPublicEntryRenderMatrixKeepsActionsTruthful$' -count=1

echo "[qa1] existing landing/home/matchup render fixtures"
go test ./app -run '^(TestPublicLandingPreservesConfiguredModeAndEventTruth|TestHomepageMatchupPreviewOnlyShowsLiveIndicatorsInProgress|TestHomepagePendingCoManagerInviteRendersTruthfully|TestHomepageStandingsPendingStateRendersExplicitly|TestHomepageStandingsRendersFinalizedScheduleData)$' -count=1
go test ./app/matchups -run '^TestMatchupsPagePreseasonAndScheduledCopyIsNotLive$' -count=1

echo "[qa1] existing source-health render fixtures"
go test ./app/wire -run '^(TestWireModeLabelsCoverServiceVocabulary|TestWirePresentationMarksPartialFeedOutageAndRetainsSignals|TestWirePageAndPulseExposePartialSourceIssue)$' -count=1

echo "[qa1] matrix complete; no browser evidence is asserted by this harness"
