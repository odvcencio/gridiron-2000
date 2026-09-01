// Package setupwizard is the first-boot setup wizard's engine: the
// configFile-shaped draft, the one shared validation path
// (league.LoadConfigBytes — "the same path leaguecheck uses, no parallel
// validation"), step ordering/status, and draft persistence. It has no
// HTTP or rendering concerns; main.go's wizard_*.go files are the thin
// GoSX plumbing on top of this package.
//
// Steps 1-9 and 13 of the design's 13-step flow are in scope for this
// slice (identity, teams, scoring, roster, draft, waivers, trades,
// membership, commissioner, review/confirm). Steps 10-12 (email, Google
// OAuth, data feed) are optional Tier 2/3 additions a later slice adds.
package setupwizard

// Step names one wizard step.
type Step struct {
	Slug  string
	Title string
}

// Steps is the wizard's fixed order (design section 4.1, slice-3 subset).
var Steps = []Step{
	{"identity", "League identity"},
	{"teams", "Teams and divisions"},
	{"scoring", "Scoring format"},
	{"roster", "Roster"},
	{"draft", "Draft meeting"},
	{"waivers", "Waivers"},
	{"trades", "Trades"},
	{"membership", "Membership and invites"},
	{"commissioner", "Commissioner account"},
	{"review", "Review and confirm"},
}

// StepIndex returns slug's position in Steps, or -1 when slug is unknown.
func StepIndex(slug string) int {
	for i, step := range Steps {
		if step.Slug == slug {
			return i
		}
	}
	return -1
}

// ValidStep reports whether slug names a real wizard step.
func ValidStep(slug string) bool {
	return StepIndex(slug) >= 0
}

// FirstStepSlug is the wizard's entry step.
func FirstStepSlug() string {
	return Steps[0].Slug
}

// NextStepSlug returns the step after slug, or "" at the end.
func NextStepSlug(slug string) string {
	i := StepIndex(slug)
	if i < 0 || i+1 >= len(Steps) {
		return ""
	}
	return Steps[i+1].Slug
}

// PrevStepSlug returns the step before slug, or "" at the start.
func PrevStepSlug(slug string) string {
	i := StepIndex(slug)
	if i <= 0 {
		return ""
	}
	return Steps[i-1].Slug
}

// StepState is one of the design's typed step statuses (section 4.4):
// "DONE / CURRENT / TODO — words, not color." Stale is this package's
// addition for a DONE step whose saved value a later edit has
// invalidated, per "marks dependent steps STALE when a dependency
// changed."
type StepState string

const (
	StepTodo    StepState = "TODO"
	StepCurrent StepState = "CURRENT"
	StepDone    StepState = "DONE"
	StepStale   StepState = "STALE"
)

// StatusMap tracks every step's persisted status (DONE/STALE only; TODO is
// the implicit default and CURRENT is computed per-request from the URL,
// so neither is ever stored).
type StatusMap map[string]StepState

// StatusFor reports slug's status given which step is currently being
// viewed.
func (m StatusMap) StatusFor(slug, currentSlug string) StepState {
	if slug == currentSlug {
		return StepCurrent
	}
	if m != nil {
		if status, ok := m[slug]; ok && status != "" {
			return status
		}
	}
	return StepTodo
}

// MarkDone records slug as DONE, clearing any earlier STALE mark.
func (m StatusMap) MarkDone(slug string) {
	m[slug] = StepDone
}

// MarkStale records slug as STALE: it was DONE, but a dependency changed.
// Marking a step that was never visited (still TODO) is a no-op — a step
// with no prior value cannot go stale, it is simply not yet reached.
func (m StatusMap) MarkStale(slug string) {
	if m[slug] == StepDone {
		m[slug] = StepStale
	}
}

// FirstIncompleteStep returns the earliest step that is not DONE — the
// resume point after a restart, or the redirect target from the bare
// /setup root (design section 4.4: "A container restart resumes at the
// first incomplete step").
func (m StatusMap) FirstIncompleteStep() string {
	for _, step := range Steps {
		if m == nil || m[step.Slug] != StepDone {
			return step.Slug
		}
	}
	return Steps[len(Steps)-1].Slug
}

// AllDone reports whether every step through "review" is DONE — the
// review step's own gate: design section 4.2, "The review step requires a
// clean pass with no placeholders."
func (m StatusMap) AllDone() bool {
	for _, step := range Steps {
		if step.Slug == "review" {
			continue
		}
		if m == nil || m[step.Slug] != StepDone {
			return false
		}
	}
	return true
}
