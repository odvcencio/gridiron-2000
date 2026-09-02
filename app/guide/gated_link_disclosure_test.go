package guide

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestGuideMarksEveryGatedLinkSignInRequired guards wave-6 item 8: /guide
// is a public, unauthenticated page (main.go requireLeagueSessionWithDemoMode's
// open allowlist), but links it to gated routes (every route not in that
// allowlist) rendered with no visible or accessible cue that following
// one, signed out, bounces to /login. Every gated link now carries a
// title and a visually-hidden "(sign-in required)" cue; /help and /
// (also public) carry neither.
func TestGuideMarksEveryGatedLinkSignInRequired(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)

	gated := []string{"/admin", "/board", "/commissioner", "/draft", "/pickem", "/players", "/scoring", "/team", "/trades", "/wire"}
	linkTag := regexp.MustCompile(`<a href="([^"]+)"[^>]*>`)
	for _, tag := range linkTag.FindAllString(body, -1) {
		href := linkTag.FindStringSubmatch(tag)[1]
		isGated := false
		for _, route := range gated {
			if href == route {
				isGated = true
				break
			}
		}
		if isGated {
			if !strings.Contains(tag, `title="Sign-in required"`) {
				t.Errorf("gated link to %q has no title=\"Sign-in required\": %s", href, tag)
			}
		} else if href == "/help" || href == "/" || href == "/login" {
			if strings.Contains(tag, "Sign-in required") {
				t.Errorf("public link to %q incorrectly marked sign-in required: %s", href, tag)
			}
		}
	}

	if count := strings.Count(body, `<span class="visually-hidden"> (sign-in required)</span>`); count != 19 {
		t.Errorf("guide page has %d visually-hidden sign-in-required cues, want 19 (one per gated link)", count)
	}
}
