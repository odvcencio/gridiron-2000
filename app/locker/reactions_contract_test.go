package locker

import (
	"os"
	"strings"
	"testing"
)

// TestLockerReactionsContract pins the reaction row: one managed form per
// emoji under a post and under a reply, posting post_id, emoji, and the
// next state to the locker-react action with the page's CSRF token, the
// viewer's own mark as aria-pressed, and the action registered against
// league.Service.ReactToLockerPost.
func TestLockerReactionsContract(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, want := range []string{
		`<If cond={post.CanReact}>`, `<Each of={post.Reactions} as="reaction">`,
		`action={data.locker_react_action}`,
		`name="post_id" value={post.ID}`, `name="emoji" value={reaction.Emoji}`, `name="on" value={reaction.ToggleValue}`,
		`aria-pressed={reaction.Mine}`, `{reaction.Emoji}`, `{reaction.Count}`,
		`<If cond={reply.CanReact}>`, `<Each of={reply.Reactions} as="reaction">`, `name="post_id" value={reply.ID}`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("page.gsx is missing the reactions piece %q", want)
		}
	}
	server, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"locker-react": func(ctx *action.Context) error {`,
		`ReactToLockerPost(ctx.Request, ctx.FormData["post_id"], ctx.FormData["emoji"], ctx.FormData["on"] == "1")`,
		`data["locker_react_action"]`,
	} {
		if !strings.Contains(string(server), want) {
			t.Errorf("page.server.go is missing %q", want)
		}
	}
}
