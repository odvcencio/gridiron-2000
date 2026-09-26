package draft

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
	"m31labs.dev/gosx/route"
)

func TestDraftSearchKeepsAccessibleNameWhenQueryIsFilled(t *testing.T) {
	program, err := route.LoadFileProgram("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	for _, room := range []string{"/draft", "/draft/practice"} {
		t.Run(room, func(t *testing.T) {
			body, err := route.RenderProgramComponent(program, "DraftAvailableHead", route.ProgramRenderEnv{Values: map[string]any{
				"props": map[string]any{"RoomPath": room, "Query": "Allen", "SearchPlaceholder": "Search 200 available", "Position": "QB", "Sort": "adp"},
			}})
			if err != nil {
				t.Fatal(err)
			}
			z := html.NewTokenizer(strings.NewReader(body))
			for {
				kind := z.Next()
				if kind == html.ErrorToken {
					t.Fatal("draft search input was not rendered")
				}
				if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
					continue
				}
				token := z.Token()
				attrs := map[string]string{}
				for _, attr := range token.Attr {
					attrs[attr.Key] = attr.Val
				}
				if token.Data == "input" && attrs["id"] == "draft-search" {
					if attrs["aria-label"] != "Search available players" || attrs["value"] != "Allen" {
						t.Fatalf("filled search input lost its accessible name or query: %v", attrs)
					}
					return
				}
			}
		})
	}
}
