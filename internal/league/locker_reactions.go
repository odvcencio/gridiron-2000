package league

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// lockerReactionEmojis is the fixed reaction set, in render order. A
// closed set keeps the row one line and the ledger small; the Locker Room
// is trash talk, not a chat client.
var lockerReactionEmojis = []string{"👍", "🔥", "😂"}

// LockerReactionView is one emoji's tally on a post as the board renders
// it: the count, whether the viewer left it, and the value the toggle
// form posts to flip it.
type LockerReactionView struct {
	Emoji       string
	Count       int
	Mine        bool
	ToggleValue string
	Label       string
}

func lockerReactionAllowed(emoji string) bool {
	for _, allowed := range lockerReactionEmojis {
		if emoji == allowed {
			return true
		}
	}
	return false
}

// SetLockerReaction leaves (on) or lifts one member's mark of one emoji on
// one live post. Lifting a mark never left is a no-op. The post must
// exist and must not be removed; the emoji must be in the fixed set.
func (s *Store) SetLockerReaction(postID, emoji, email string, on bool, now time.Time) error {
	postID = strings.TrimSpace(postID)
	emoji = strings.TrimSpace(emoji)
	email = strings.ToLower(strings.TrimSpace(email))
	if postID == "" || email == "" {
		return errors.New("sign in and choose a post to react to")
	}
	if !lockerReactionAllowed(emoji) {
		return fmt.Errorf("choose one of the offered reactions")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	post, ok := s.lockerPostByIDLocked(postID)
	if !ok {
		return errors.New("that post no longer exists")
	}
	if !post.RemovedAt.IsZero() {
		return errors.New("that post was removed")
	}
	if s.state.LockerReactions == nil {
		s.state.LockerReactions = map[string]map[string]map[string]time.Time{}
	}
	byEmoji := s.state.LockerReactions[postID]
	if !on {
		if byEmoji == nil {
			return nil
		}
		byEmail := byEmoji[emoji]
		if _, marked := byEmail[email]; !marked {
			return nil
		}
		delete(byEmail, email)
		if len(byEmail) == 0 {
			delete(byEmoji, emoji)
		}
		if len(byEmoji) == 0 {
			delete(s.state.LockerReactions, postID)
		}
		return s.persistLocked(colScalars)
	}
	if byEmoji == nil {
		byEmoji = map[string]map[string]time.Time{}
		s.state.LockerReactions[postID] = byEmoji
	}
	byEmail := byEmoji[emoji]
	if byEmail == nil {
		byEmail = map[string]time.Time{}
		byEmoji[emoji] = byEmail
	}
	if _, marked := byEmail[email]; marked {
		return nil
	}
	byEmail[email] = now.UTC()
	return s.persistLocked(colScalars)
}

// lockerReactionViews renders the fixed set for one post, in order, with
// the viewer's own mark. Every emoji is present even at zero so the row
// is the same shape on every post.
func lockerReactionViews(state PersistedState, postID, viewerEmail string) []LockerReactionView {
	viewerEmail = strings.ToLower(strings.TrimSpace(viewerEmail))
	byEmoji := state.LockerReactions[postID]
	out := make([]LockerReactionView, 0, len(lockerReactionEmojis))
	for _, emoji := range lockerReactionEmojis {
		byEmail := byEmoji[emoji]
		_, mine := byEmail[viewerEmail]
		view := LockerReactionView{Emoji: emoji, Count: len(byEmail), Mine: mine, ToggleValue: "1", Label: "React " + emoji}
		if mine {
			view.ToggleValue = "0"
			view.Label = "Remove your " + emoji + " reaction"
		}
		out = append(out, view)
	}
	return out
}

// ReactToLockerPost leaves or lifts the signed-in member's mark. The same
// writer gate as posting decides who may react.
func (s *Service) ReactToLockerPost(r *http.Request, postID, emoji string, on bool) (string, error) {
	email, _, _, err := s.lockerRequireWriter(r)
	if err != nil {
		return "", err
	}
	if err := s.store.SetLockerReaction(postID, emoji, email, on, s.clock()); err != nil {
		return "", err
	}
	if on {
		return "Reaction added.", nil
	}
	return "Reaction removed.", nil
}
