package league

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// LockerPostView is one Locker Room post or reply as Page() and the
// LockerRegion fragment read it (GC-4). Body is empty and RemovedLabel is
// set once Removed is true — a tombstone, never the removed text.
type LockerPostView struct {
	ID           string
	ParentID     string
	Body         string
	AuthorLabel  string
	TimeLabel    string
	Removed      bool
	RemovedLabel string
	CanRemove    bool
	Replies      []LockerPostView
}

// lockerViewerIdentity resolves the Locker Room viewer's canonical email,
// display name, and seated team ID ("" seatless) from state.Members —
// admission is membership, not seat ownership, the same rule
// pickemViewerKeyForState already enforces for Pick'em. The demo guest is
// the one deliberate synthetic exception for READ access; PostLockerPost
// and RemoveLockerPost refuse every demo-mode write regardless of this
// result (GC-4: "Demo mode is read-only").
func (s *Service) lockerViewerIdentity(r *http.Request, state PersistedState) (email, name, teamID string, admitted bool) {
	user, signedIn := s.CurrentUser(r)
	if !signedIn {
		if s.demoMode {
			return "demo-guest", "Guest Coach", "", true
		}
		return "", "", "", false
	}
	email = s.identityResolver.Resolve(user.Email)
	if email == "" {
		return "", "", "", false
	}
	member, exists := memberByEmail(state.Members, email)
	if !exists {
		return "", "", "", false
	}
	name = strings.TrimSpace(user.Name)
	if name == "" {
		name = strings.Split(email, "@")[0]
	}
	return email, name, member.TeamID, true
}

// lockerRequireWriter is PostLockerPost's admission boundary. Demo mode is
// always read-only, even when a local test happens to attach an auth
// provider to a demo request (the same unconditional-refusal shape
// SetNotificationPreference already uses). A signed-in identity with no
// persisted Member — including one with a still-pending co-manager
// invitation — is told exactly why, mirroring pickemOwner's own three
// branches.
func (s *Service) lockerRequireWriter(r *http.Request) (email, name, teamID string, err error) {
	if s.demoMode {
		return "", "", "", fmt.Errorf("Locker Room posting is read-only in demo mode. Sign in to post.")
	}
	user, signedIn := s.CurrentUser(r)
	if !signedIn {
		return "", "", "", fmt.Errorf("Google sign-in is required for the Locker Room")
	}
	email = s.identityResolver.Resolve(user.Email)
	if email == "" {
		return "", "", "", fmt.Errorf("Google sign-in is required for the Locker Room")
	}
	state := s.store.Snapshot()
	member, exists := memberByEmail(state.Members, email)
	if !exists {
		if pendingTeamID, pending := state.CoInvites[email]; pending {
			return "", "", "", fmt.Errorf("complete the pending co-manager invitation for %s before posting in the Locker Room", s.TeamLabel(pendingTeamID))
		}
		return "", "", "", fmt.Errorf("league admission is not recorded for this Google account; ask the commissioner to verify this identity before posting")
	}
	name = strings.TrimSpace(user.Name)
	if name == "" {
		name = strings.Split(email, "@")[0]
	}
	return email, name, member.TeamID, nil
}

// PostLockerPost records a new top-level Locker Room post, or — when
// parentID names an existing top-level post — a one-level-flat reply,
// under the acting request's own admitted, non-demo identity. A
// successful commit calls emitLockerChanged so the locker-live hub
// broadcasts at once — no interval poll ever discovers a post.
func (s *Service) PostLockerPost(r *http.Request, parentID, body string) (LockerPost, error) {
	email, name, teamID, err := s.lockerRequireWriter(r)
	if err != nil {
		return LockerPost{}, err
	}
	post, err := s.store.PostLocker(parentID, body, email, name, teamID, s.clock())
	if err != nil {
		return LockerPost{}, err
	}
	s.emitLockerChanged()
	return post, nil
}

// RemoveLockerPost soft-deletes one post: its author may remove their own
// post, and the commissioner may remove any post (GC-4). Demo mode is
// always read-only, the same unconditional refusal PostLockerPost applies.
// confirmation is wave-6 item 9's server-side enforcement of the page's
// gated <details> disclosure: removal is permanent from this screen, the
// same irreversibility class DropPlayer's playerDropConfirmation gate
// already covers.
func (s *Service) RemoveLockerPost(r *http.Request, id, confirmation string) error {
	if s.demoMode {
		return fmt.Errorf("Locker Room moderation is read-only in demo mode. Sign in to remove a post.")
	}
	user, signedIn := s.CurrentUser(r)
	if !signedIn {
		return fmt.Errorf("Google sign-in is required for the Locker Room")
	}
	email := s.identityResolver.Resolve(user.Email)
	state := s.store.Snapshot()
	post, ok := lockerPostByID(state.LockerPosts, id)
	if !ok {
		return fmt.Errorf("that post no longer exists")
	}
	role := ""
	switch {
	case email != "" && strings.EqualFold(strings.TrimSpace(post.AuthorEmail), email):
		role = "author"
	case s.IsCommissioner(r):
		role = "commissioner"
	default:
		return fmt.Errorf("you may remove only your own posts")
	}
	if err := requireMutationConfirmation(lockerRemoveConfirmation, confirmation); err != nil {
		return err
	}
	if err := s.store.RemoveLockerPost(id, role, s.clock()); err != nil {
		return err
	}
	s.emitLockerChanged()
	return nil
}

// lockerPostByID finds a post by ID in a read snapshot (never touches
// s.mu; unlike Store.lockerPostByIDLocked, callers here already hold their
// own independent Snapshot()).
func lockerPostByID(posts []LockerPost, id string) (LockerPost, bool) {
	for _, post := range posts {
		if post.ID == id {
			return post, true
		}
	}
	return LockerPost{}, false
}

// lockerTopLevelPosts returns every post with no parent, newest first —
// the same "newest first" convention the Activity feed and the
// Announcements panel already use.
func lockerTopLevelPosts(posts []LockerPost) []LockerPost {
	out := make([]LockerPost, 0, len(posts))
	for _, post := range posts {
		if post.ParentID == "" {
			out = append(out, post)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].PostedAt.After(out[j].PostedAt) })
	return out
}

// lockerRepliesFor returns parentID's one level of flat replies, oldest
// first — natural reading order underneath the post they answer.
func lockerRepliesFor(posts []LockerPost, parentID string) []LockerPost {
	out := make([]LockerPost, 0)
	for _, post := range posts {
		if post.ParentID == parentID {
			out = append(out, post)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].PostedAt.Before(out[j].PostedAt) })
	return out
}

// lockerRemovedLabel is the tombstone's whole visible truth: who removed a
// post, never what it said.
func lockerRemovedLabel(role string) string {
	switch role {
	case "author":
		return "Removed by the author."
	case "commissioner":
		return "Removed by the commissioner."
	default:
		return "Removed."
	}
}

// lockerAuthorLabel is the canonical identity's display name, plus its
// team display when the post's author held a seat at post time (GC-4:
// "author = canonical identity plus team display when seated"). AuthorTeamID
// is resolved live through TeamLabel, so a later team rename still reads
// correctly on an old post; the author's own display name is the snapshot
// PostLocker took at post time.
func (s *Service) lockerAuthorLabel(post LockerPost) string {
	name := strings.TrimSpace(post.AuthorName)
	if name == "" {
		name = "A league member"
	}
	teamID := strings.TrimSpace(post.AuthorTeamID)
	if teamID == "" {
		return name
	}
	return name + " (" + s.TeamLabel(teamID) + ")"
}

// lockerPostView converts one stored post (and its already-resolved
// replies) into the page's strict-component view. Only a top-level call
// passes replies; lockerPostView recurses with nil so the one flat level
// GC-4 specifies can never grow a second.
func (s *Service) lockerPostView(post LockerPost, replies []LockerPost, viewerEmail string, commissioner bool, location *time.Location) LockerPostView {
	removed := !post.RemovedAt.IsZero()
	canRemove := !removed && (commissioner || (viewerEmail != "" && strings.EqualFold(strings.TrimSpace(post.AuthorEmail), viewerEmail)))
	view := LockerPostView{
		ID:          post.ID,
		ParentID:    post.ParentID,
		Body:        post.Body,
		AuthorLabel: s.lockerAuthorLabel(post),
		TimeLabel:   post.PostedAt.In(location).Format("Jan 2, 3:04 PM MST"),
		Removed:     removed,
		CanRemove:   canRemove,
	}
	if removed {
		view.RemovedLabel = lockerRemovedLabel(post.RemovedByRole)
	}
	replyViews := make([]LockerPostView, 0, len(replies))
	for _, reply := range replies {
		replyViews = append(replyViews, s.lockerPostView(reply, nil, viewerEmail, commissioner, location))
	}
	view.Replies = replyViews
	return view
}

// lockerPageHref returns a stable GET link for a paginated board page. A
// first page omits page=1 so copied links stay compact.
func lockerPageHref(page int) string {
	if page <= 1 {
		return "/locker"
	}
	values := url.Values{}
	values.Set("page", strconv.Itoa(page))
	return "/locker?" + values.Encode()
}

// LockerData assembles the /locker page and its board region: the
// paginated (50 top-level posts per page, poolPageSize) newest-first
// thread list, each carrying its own one-level-flat replies, plus the
// viewer's admission, commissioner, and demo/read-only posture. It
// performs no reconciliation writes, so LockerDataReadOnly is a direct
// alias, the same shape ActivityDataReadOnly already uses.
func (s *Service) LockerData(r *http.Request) map[string]any {
	state := s.store.Snapshot()
	_, _, _, admitted := s.lockerViewerIdentity(r, state)
	viewerEmail := s.viewerKey(r)
	commissioner := s.IsCommissioner(r)
	readOnly := s.demoMode
	top := lockerTopLevelPosts(state.LockerPosts)
	pagination := newPoolPagination(len(top), r.URL.Query().Get("page"))
	pageStart := 0
	if pagination.Total > 0 {
		pageStart = pagination.Start + 1
	}
	pageTop := top[pagination.Start:pagination.End]
	location := s.matchupLocation()
	views := make([]LockerPostView, 0, len(pageTop))
	for _, post := range pageTop {
		replies := lockerRepliesFor(state.LockerPosts, post.ID)
		views = append(views, s.lockerPostView(post, replies, viewerEmail, commissioner, location))
	}
	readOnlyReason := ""
	if readOnly {
		readOnlyReason = "Demo mode is read-only. Sign in to post in the Locker Room."
	} else if !admitted {
		readOnlyReason = "League admission is required to post in the Locker Room."
	}
	return map[string]any{
		"viewer":           s.Viewer(r),
		"league":           s.leagueMap(),
		"timezone":         FriendlyTimezoneLabel(location.String()),
		"admitted":         admitted,
		"is_commissioner":  commissioner,
		"demo_mode":        s.demoMode,
		"read_only":        readOnly,
		"can_post":         admitted && !readOnly,
		"read_only_reason": readOnlyReason,
		"posts":            views,
		"has_posts":        len(top) > 0,
		"posts_empty":      pagination.Total == 0,
		"posts_count":      len(top),
		"page":             pagination.Page,
		"pages":            pagination.Pages,
		"page_start":       pageStart,
		"page_end":         pagination.End,
		"has_previous":     pagination.HasPrevious,
		"has_next":         pagination.HasNext,
		"previous_href":    lockerPageHref(pagination.Page - 1),
		"next_href":        lockerPageHref(pagination.Page + 1),
	}
}

// LockerDataReadOnly names the polling boundary explicitly (ActivityDataReadOnly's
// precedent): safe for repeated cross-client fragment GETs, since LockerData
// itself performs no reconciliation writes.
func (s *Service) LockerDataReadOnly(r *http.Request) map[string]any {
	return s.LockerData(r)
}

// LockerVersion is the locker-live hub's version source
// (app/locker/live.go): the current in-memory Locker Room commit counter.
func (s *Service) LockerVersion() int64 {
	return s.store.LockerGeneration()
}
