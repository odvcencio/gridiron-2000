package trades

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVetoPolicyCardLinksWrapAsARowWithGap pins wave-2-verification item
// 10's layout half: .draft-clock-meta (shared by several pages, so left
// untouched) has no flex-wrap, so its default nowrap forced /trades'
// three links (Transaction feed, Team terminal or the seatless entry
// link, and — for a commissioner — Open Commissioner HQ) onto one row
// and shrank each into a narrow column that wrapped mid-word; the last
// link's trailing "→" ran into the card border. A new, page-scoped
// .trades-veto-links wrapper nested inside .draft-clock-meta instead
// lets the links wrap onto their own row(s) with a real gap.
func TestVetoPolicyCardLinksWrapAsARowWithGap(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(page)
	// The exact whitespace between the two wrapping <div>s is not load
	// bearing; what matters is that trades-veto-links nests inside
	// draft-clock-meta and precedes the three links it wraps.
	metaIndex := strings.Index(markup, `class="draft-clock-meta"`)
	wrapIndex := strings.Index(markup, `class="trades-veto-links"`)
	linkIndex := strings.Index(markup, `href="/activity" data-gosx-link>Transaction feed`)
	if metaIndex < 0 || wrapIndex < 0 || linkIndex < 0 || !(metaIndex < wrapIndex && wrapIndex < linkIndex) {
		t.Fatalf("trades-veto-links must wrap the veto card's links inside draft-clock-meta: %s", markup)
	}

	css, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	style := string(css)
	if !strings.Contains(style, ".trades-veto-links {\n  display: flex;\n  flex-wrap: wrap;\n  gap: var(--space-sm);\n}") {
		t.Error("trades-veto-links is missing or no longer wraps its links in a row with a gap")
	}
}

// TestVetoPolicyCardShowsOneMergedSentence covers wave-8 audit item 7:
// the card used to print a separate "Veto policy" label beside the bare
// config token ("commissioner"); it now reads league.tradeVetoPolicyLabel's
// one merged sentence ("Veto policy: commissioner review").
func TestVetoPolicyCardShowsOneMergedSentence(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(page)
	if !strings.Contains(markup, `<strong class="mono">{data.veto_policy_label}</strong>`) {
		t.Fatal("veto policy card is missing the merged veto_policy_label binding")
	}
	if strings.Contains(markup, `<span>Veto policy</span>`) {
		t.Fatal("veto policy card still carries the separate \"Veto policy\" label beside the raw veto_mode value")
	}
}

// TestTradeComposerOptionMeetsTouchFloor covers wave-8 audit item 9: the
// composer's roster checkboxes measured a bare 13×13 (the unstyled
// browser default) inside a 28px label row. Every label.trade-composer__
// option row is now a real 44px touch target with an explicit 24px
// checkbox — checked here in page.gsx (all four give/get rosters, the
// initial composer and the counter-offer form both use the identical
// label shape) and in public/styles.css (the sizing rule itself).
func TestTradeComposerOptionMeetsTouchFloor(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(page)
	if got := strings.Count(markup, `<label class="trade-composer__option">`); got < 4 {
		t.Fatalf("trade-composer__option labels = %d, want at least 4 (give/get, composer and counter-offer)", got)
	}
	css, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	style := string(css)
	for _, want := range []string{
		".trade-composer__option {\n  min-height: 2.75rem;\n}",
		".trade-composer__option input[type=\"checkbox\"] {\n  width: 1.5rem;\n  height: 1.5rem;",
	} {
		if !strings.Contains(style, want) {
			t.Errorf("styles.css missing %q", want)
		}
	}
}

// TestTradeSectionsRenumberAroundHiddenSections covers wave-8 audit item
// 11: COMMISSIONER REVIEW and LEAGUE VOTE bind their section-index chip
// to a computed field (league.tradeSectionIndexLabels) instead of the
// hardcoded "05"/"06" text, so HISTORY's own chip does the same rather
// than staying pinned at the literal "07" it used to skip to.
func TestTradeSectionsRenumberAroundHiddenSections(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(page)
	for _, want := range []string{
		`<span class="section-index">{data.section_review_index + " // COMMISSIONER REVIEW"}</span>`,
		`<span class="section-index">{data.section_vote_index + " // LEAGUE VOTE"}</span>`,
		`<span class="section-index">{data.section_history_index + " // HISTORY"}</span>`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("trade section header missing %q", want)
		}
	}
	for _, forbidden := range []string{
		`<span class="section-index">05 // COMMISSIONER REVIEW</span>`,
		`<span class="section-index">06 // LEAGUE VOTE</span>`,
		`<span class="section-index">07 // HISTORY</span>`,
	} {
		if strings.Contains(markup, forbidden) {
			t.Errorf("trade section header still carries the hardcoded index %q", forbidden)
		}
	}
}

// TestTradeDeskStatesTheVetoRuleAboveTheComposer is F20's UX-pass fix
// (comb audit J3): the masthead used to show only the bare "Veto policy:
// ..." fragment, with no sentence naming the review window or what
// happens after it. league.tradeVetoSummarySentence supplies one plain
// sentence (both config-driven modes are pinned at the data layer by
// TestTradeVetoSummarySentenceCoversEveryConfigValue, internal/league);
// this only pins that the composer's own masthead panel actually prints
// it, above the composer form itself.
func TestTradeDeskStatesTheVetoRuleAboveTheComposer(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(page)
	sentenceIndex := strings.Index(markup, "{data.veto_summary_sentence}")
	composeIndex := strings.Index(markup, `<h2>Propose a trade</h2>`)
	if sentenceIndex < 0 {
		t.Fatal("trade masthead lost the veto_summary_sentence line")
	}
	if composeIndex < 0 || sentenceIndex > composeIndex {
		t.Fatal("veto_summary_sentence must render above the compose section")
	}
}

// TestTradeAcceptGateNamesTheActualOutcome is F19's fix: the accept gate
// used to hedge ("This either opens the league review window or executes
// immediately, depending on league policy") even though the league's own
// veto policy is fixed and known. Both config-driven wordings are pinned
// at the data layer (TestTradeAcceptConsequenceSentenceCoversEveryConfigValue,
// internal/league); this pins that the gate reads that field instead of
// the old hedge string.
func TestTradeAcceptGateNamesTheActualOutcome(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(page)
	if !strings.Contains(markup, "{data.trade_accept_consequence}") {
		t.Fatal("accept gate lost the trade_accept_consequence line")
	}
	if strings.Contains(markup, "This either opens the league review window or executes immediately, depending on league policy.") {
		t.Fatal("accept gate still hedges instead of naming the actual outcome")
	}
}

// TestTradeOutboxAndPendingReviewBranchOnVetoMode is F11's fix: an
// accepted offer used to advertise "N of M vetoes filed" even in a
// commissioner-veto league, where no vote ever happens. Both the outbox
// (the proposer's own view) and pending review (the accepting manager's
// view) now branch on the configured veto mode.
func TestTradeOutboxAndPendingReviewBranchOnVetoMode(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(page)
	commissionerCopyCount := strings.Count(markup, "Waiting for the commissioner's review ·")
	if commissionerCopyCount != 2 {
		t.Fatalf(`"Waiting for the commissioner's review ·" appears %d times, want 2 (outbox and pending review)`, commissionerCopyCount)
	}
	voteGuardCount := strings.Count(markup, `<If cond={data.veto_mode != "commissioner"}>`)
	if voteGuardCount != 2 {
		t.Fatalf(`veto_mode != "commissioner" guard appears %d times, want 2 (outbox and pending review)`, voteGuardCount)
	}
}
