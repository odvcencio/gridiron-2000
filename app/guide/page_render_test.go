package guide

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func renderGuidePage(t *testing.T, data map[string]any) string {
	t.Helper()
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	previous := loadPublicGuideData
	loadPublicGuideData = func() map[string]any { return data }
	t.Cleanup(func() { loadPublicGuideData = previous })

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (guide page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func guideConfig(name, mode, timezone, domain string, teams, rosterSpots int) league.Config {
	cfg := league.DefaultConfig()
	cfg.Name = name
	cfg.ModeLabel = mode
	cfg.Timezone = timezone
	cfg.DraftAt = time.Date(2026, time.August, 29, 20, 0, 0, 0, time.UTC)
	cfg.Membership.AllowedDomain = domain
	cfg.Teams = make([]league.TeamSeed, teams)
	cfg.Roster = league.RosterPreset{Slots: map[string]int{"QB": rosterSpots}}
	return cfg
}

func TestPublicGuideDataReflectsDynastyAndRedraftConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  league.Config
		want map[string]any
	}{
		{
			name: "dynasty invite posture",
			cfg:  guideConfig("Gridiron House", "dynasty", "America/New_York", "", 8, 17),
			want: map[string]any{"league_format_summary": "8 teams · DYNASTY", "roster_capacity": 136, "pool_target": 340, "pool_target_cushion": 204, "membership_label": "OPEN AFTER SIGN-IN"},
		},
		{
			name: "redraft domain posture",
			cfg:  guideConfig("Stable Kernel League", "redraft", "America/New_York", "stablekernel.com", 14, 17),
			want: map[string]any{"league_format_summary": "14 teams · REDRAFT", "roster_capacity": 238, "pool_target": 595, "pool_target_cushion": 357, "membership_label": "DOMAIN OR INVITE"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := publicGuideData(tc.cfg)
			for key, want := range tc.want {
				if got[key] != want {
					t.Errorf("publicGuideData[%q] = %#v, want %#v", key, got[key], want)
				}
			}
		})
	}
}

func TestGuidePageRendersSeasonReadyManagerPath(t *testing.T) {
	body := renderGuidePage(t, publicGuideData(guideConfig("Stable Kernel League", "redraft", "America/New_York", "stablekernel.com", 14, 17)))
	for _, want := range []string{
		"MANAGER GUIDE",
		"Manager Guide · Stable Kernel League",
		"14 teams · REDRAFT",
		"Sat, Aug 29, 2026 · 4:00 PM EDT",
		"America/New_York",
		"DOMAIN OR INVITE",
		"Accounts at the configured work domain or with an explicit invitation may enter.",
		"FIVE-MINUTE START",
		"COMMISSIONER OPENING CHECKLIST",
		"What is intentionally different.",
		"draft readiness and draft start are commissioner-controlled",
		"Big Board",
		"Autopick",
		"Waivers",
		"Lineups",
		"Pick&#39;em",
		"Signal Wire",
		"LIVE is fresh from the source",
		"CACHED, STALE, or DEGRADED",
		"OFFLINE or UNAVAILABLE",
		"exact last-success time",
		"enough players to draft from",
		"The pool holds more players than the draft needs.",
		"Commissioner HQ",
		"No automated migration exists.",
		"Practical manual migration checklist",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("guide page omitted %q: %s", want, body)
		}
	}
	for _, href := range []string{
		"href=\"/draft\"",
		"href=\"/board\"",
		"href=\"/scoring\"",
		"href=\"/players\"",
		"href=\"/trades\"",
		"href=\"/team\"",
		"href=\"/pickem\"",
		"href=\"/wire\"",
	} {
		if !strings.Contains(body, href) {
			t.Errorf("guide page omitted route entry %q: %s", href, body)
		}
	}
	if strings.Contains(body, "class=\"error-page\"") {
		t.Fatalf("guide page rendered the GoSX error page: %s", body)
	}
	for _, obsolete := range []string{"G2K", "SKL", "Toggle ready", "toggle ready", "DOMAIN + INVITES", "Access comes from an individual invite", "CURRENT, SAVED, or BUILT-IN", "whether the pool is live"} {
		if strings.Contains(body, obsolete) {
			t.Errorf("guide page retained obsolete or league-specific claim %q", obsolete)
		}
	}
}

func TestGuidePageRendersDynastyVariantWithoutDomain(t *testing.T) {
	body := renderGuidePage(t, publicGuideData(guideConfig("Gridiron House", "dynasty", "America/Los_Angeles", "", 8, 17)))
	for _, want := range []string{
		"Manager Guide · Gridiron House",
		"8 teams · DYNASTY",
		"OPEN AFTER SIGN-IN",
		"Any authenticated Google account may enter while league setup is open.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dynasty guide omitted %q", want)
		}
	}
	for _, wrong := range []string{"Stable Kernel League", "@stablekernel.com", "INVITES / ALLOWLIST", "Access comes from an individual invite"} {
		if strings.Contains(body, wrong) {
			t.Errorf("dynasty guide leaked redraft/domain claim %q", wrong)
		}
	}
}

func TestGuideStylesCoverInformationAndNarrowNavigation(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatalf("read guide styles: %v", err)
	}
	css := string(styles)
	for _, selector := range []string{
		".minimal-actions",
		".guide-toc",
		".guide-section",
		".guide-steps",
		".guide-checklist",
		".guide-card-grid--three",
		".guide-compare",
		".guide-callout--alert",
		".guide-formula",
		"@media (max-width: 38rem)",
		"@media (max-width: 20rem)",
		".minimal-actions .access-link",
	} {
		if !strings.Contains(css, selector) {
			t.Errorf("guide stylesheet omitted %q", selector)
		}
	}
}
