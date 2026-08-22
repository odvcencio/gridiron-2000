package admin

import (
	"os"
	"strings"
	"testing"
)

func TestCommissionerTaskMapTargetsRealAdminSections(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)

	targets := []string{
		"admin-draft-runbook",
		"admin-seats",
		"admin-invites",
		"admin-draft-order",
		"admin-data-feed",
		"admin-draft-clock",
		"admin-announcements",
	}
	for _, target := range targets {
		if got := strings.Count(source, `href="#`+target+`"`); got != 1 {
			t.Errorf("task href #%s count = %d, want 1", target, got)
		}
		if got := strings.Count(source, `id="`+target+`"`); got != 1 {
			t.Errorf("section id %s count = %d, want 1", target, got)
		}
	}
	if got := strings.Count(source, `href="#admin-draft-start"`); got != 1 {
		t.Errorf("pre-draft start href count = %d, want 1", got)
	}
	if got := strings.Count(source, `id="admin-draft-start"`); got != 1 {
		t.Errorf("draft start form id count = %d, want 1", got)
	}
}

func TestCommissionerTaskMapKeepsDraftStartIntentional(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)

	for _, want := range []string{
		"Start draft intentionally",
		"Scheduled time does not start it",
		"The scheduled time never opens the room",
		"Type START below when you intentionally begin pick one.",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("admin copy missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"the clock arms the first pick on its own",
		"the clock starts automatically at draft time",
		"No action is needed",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("admin copy still contains false automatic-start promise %q", forbidden)
		}
	}
}

func TestCommissionerTaskMapCSSIsResponsiveAndReachable(t *testing.T) {
	cssBytes, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)

	for _, want := range []string{
		".commissioner-task-map",
		"position: sticky",
		".commissioner-task-links",
		"repeat(6, minmax(0, 1fr))",
		"min-height: 5rem",
		"scroll-margin-top: 20rem",
		"@media (max-width: 78rem)",
		".admin-page .draft-masthead",
		"position: static",
		"@media (max-width: 38rem)",
		"grid-template-columns: 1fr",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("task map CSS missing %q", want)
		}
	}
}
