package topic

import (
	"html"
	"net/url"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
	helpcontent "gridiron-2000/app/help"
)

func topicRequestWithReturn(raw string) string {
	values := url.Values{}
	values.Set(helpcontent.ReturnToQuery, raw)
	values.Set("state", "unavailable")
	values.Set("field", "validation")
	return "/lineups-locks-matchups-and-scoring?" + values.Encode()
}

func renderedTopicAnchorHrefs(t *testing.T, body string) []string {
	t.Helper()
	document, err := xhtml.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse rendered topic: %v", err)
	}
	var hrefs []string
	var visit func(*xhtml.Node)
	visit = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && node.Data == "a" {
			for _, attr := range node.Attr {
				if attr.Key == "href" {
					hrefs = append(hrefs, attr.Val)
					break
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(document)
	return hrefs
}

func TestTopicRouteRoundTripsSafeReturnTargetAndKeepsOwningAction(t *testing.T) {
	rawReturn := "/matchups?team=team-2&week=3#main-content"
	body := renderTopicRoute(t, topicRequestWithReturn(rawReturn))

	wantReturnHref := "href=\"/matchups?team=team-2&amp;week=3#main-content\""
	if got := strings.Count(body, wantReturnHref); got != 1 {
		t.Fatalf("safe return CTA href rendered %d times, want once: %s", got, body)
	}
	if !strings.Contains(body, "Return to Matchups →") {
		t.Fatalf("safe return CTA did not name the Matchups destination: %s", body)
	}
	if !strings.Contains(body, "href=\"/team\"") {
		t.Fatalf("topic's existing owning action disappeared while adding return CTA: %s", body)
	}

	nested := helpcontent.ContextualTopicURL("lineups-locks-matchups-and-scoring", url.Values{"state": []string{"unavailable"}}, rawReturn)
	if !strings.Contains(body, "href=\""+html.EscapeString(nested)+"\"") {
		t.Fatalf("state help link dropped its safe return context: %s", body)
	}
	nestedField := helpcontent.ContextualTopicURL("lineups-locks-matchups-and-scoring", nil, rawReturn)
	if !strings.Contains(body, "href=\""+html.EscapeString(nestedField)+"\"") {
		t.Fatalf("field help link dropped its safe return context: %s", body)
	}
}

func TestTopicRouteRejectsUnsafeReturnTargetWithoutNestedContext(t *testing.T) {
	for _, raw := range []string{
		"https://evil.example/steal",
		"//evil.example/steal",
		"/login#again",
		"/auth/google/start#again",
		"/team/__actions/save#again",
	} {
		t.Run(raw, func(t *testing.T) {
			body := renderTopicRoute(t, topicRequestWithReturn(raw))
			if strings.Contains(body, "href=\"/matchups?") {
				t.Fatalf("unsafe return target %q rendered a Matchups return CTA: %s", raw, body)
			}
			if strings.Contains(body, "Return to Matchups") {
				t.Fatalf("unsafe return target %q rendered a labeled return CTA: %s", raw, body)
			}
			for _, href := range renderedTopicAnchorHrefs(t, body) {
				if strings.Contains(href, helpcontent.ReturnToQuery+"=") {
					t.Fatalf("unsafe return target %q leaked into contextual anchor %q: %s", raw, href, body)
				}
			}
		})
	}
}

func TestTopicRouteAllowsExplicitRootReturnTarget(t *testing.T) {
	body := renderTopicRoute(t, topicRequestWithReturn("/"))
	if !strings.Contains(body, "href=\"/\"") {
		t.Fatalf("explicit root return target omitted its CTA href: %s", body)
	}
	if !strings.Contains(body, "Return to the league home →") {
		t.Fatalf("explicit root return target got the wrong CTA label: %s", body)
	}
}
