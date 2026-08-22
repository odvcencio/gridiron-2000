package league

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"gridiron-2000/internal/identity"
)

// reconcileIdentityState folds explicitly configured authentication aliases
// into the internal ownership keys. It is deliberately a state migration,
// not a lookup convenience: after a successful pass, every future request,
// notification, and audit row uses the canonical person key.
//
// Admission invites are intentionally excluded. They are policy inputs and
// remain raw provider emails so an alias cannot silently widen or change the
// league login allowlist.
func reconcileIdentityState(state *PersistedState, resolver identity.Resolver) (bool, error) {
	if state == nil || !resolver.Enabled() {
		return false, nil
	}
	before := cloneState(*state)
	if err := reconcileMembers(state, resolver); err != nil {
		return false, err
	}
	if err := reconcileCoInvites(state, resolver); err != nil {
		return false, err
	}
	if err := reconcileBoards(state, resolver); err != nil {
		return false, err
	}
	if err := reconcilePickems(state, resolver); err != nil {
		return false, err
	}
	if err := reconcileBlitzEntries(state, resolver); err != nil {
		return false, err
	}
	if err := reconcileNotifyPrefs(state, resolver); err != nil {
		return false, err
	}
	for i := range state.Announcements {
		state.Announcements[i].PostedBy = resolver.Resolve(state.Announcements[i].PostedBy)
	}
	reconcileSentLog(state, resolver)
	return !reflect.DeepEqual(before, *state), nil
}

type identityMemberRecord struct {
	key      string
	rawEmail string
	member   Member
}

func reconcileMembers(state *PersistedState, resolver identity.Resolver) error {
	groups := make(map[string][]identityMemberRecord, len(state.Members))
	for key, member := range state.Members {
		raw := member.Email
		if strings.TrimSpace(raw) == "" {
			raw = key
		}
		canonical := resolver.Resolve(raw)
		member.Email = canonical
		groups[canonical] = append(groups[canonical], identityMemberRecord{
			key: key, rawEmail: raw, member: member,
		})
	}
	out := make(map[string]Member, len(groups))
	for canonical, records := range groups {
		sort.SliceStable(records, func(i, j int) bool {
			iPreferred := preferredIdentityRecord(records[i], canonical)
			jPreferred := preferredIdentityRecord(records[j], canonical)
			if iPreferred != jPreferred {
				return iPreferred
			}
			return strings.ToLower(strings.TrimSpace(records[i].key)) <
				strings.ToLower(strings.TrimSpace(records[j].key))
		})
		merged := records[0].member
		for _, record := range records[1:] {
			candidate := record.member
			if merged.TeamID != "" && candidate.TeamID != "" &&
				merged.TeamID != candidate.TeamID {
				return fmt.Errorf("identity alias migration: %q has conflicting team seats %q and %q",
					canonical, merged.TeamID, candidate.TeamID)
			}
			if merged.Role != candidate.Role && (merged.Role != "" || candidate.Role != "") {
				return fmt.Errorf("identity alias migration: %q has conflicting member roles %q and %q",
					canonical, merged.Role, candidate.Role)
			}
			if merged.TeamID == "" {
				merged.TeamID = candidate.TeamID
			}
			if merged.Role == "" {
				merged.Role = candidate.Role
			}
			if merged.Name == "" {
				merged.Name = candidate.Name
			}
		}
		merged.Email = canonical
		out[canonical] = merged
	}
	state.Members = out
	return nil
}

func preferredIdentityRecord(record identityMemberRecord, canonical string) bool {
	return normalizeIdentityKey(record.key) == canonical ||
		normalizeIdentityKey(record.rawEmail) == canonical
}

func normalizeIdentityKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func reconcileCoInvites(state *PersistedState, resolver identity.Resolver) error {
	out := make(map[string]string, len(state.CoInvites))
	for raw, teamID := range state.CoInvites {
		email := resolver.Resolve(raw)
		if previous, ok := out[email]; ok && previous != teamID {
			return fmt.Errorf("identity alias migration: pending co-manager %q has conflicting teams %q and %q",
				email, previous, teamID)
		}
		out[email] = teamID
	}
	state.CoInvites = out
	return nil
}

func reconcileBoards(state *PersistedState, resolver identity.Resolver) error {
	groups := make(map[string][]string, len(state.Boards))
	keys := make([]string, 0, len(state.Boards))
	for raw := range state.Boards {
		keys = append(keys, raw)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := resolver.Resolve(keys[i]), resolver.Resolve(keys[j])
		if (normalizeIdentityKey(keys[i]) == left) != (normalizeIdentityKey(keys[j]) == right) {
			return normalizeIdentityKey(keys[i]) == left
		}
		return normalizeIdentityKey(keys[i]) < normalizeIdentityKey(keys[j])
	})
	for _, raw := range keys {
		owner := resolver.Resolve(raw)
		for _, playerID := range state.Boards[raw] {
			if !containsString(groups[owner], playerID) {
				groups[owner] = append(groups[owner], playerID)
			}
		}
	}
	for owner, board := range groups {
		if len(board) > boardLimit {
			return fmt.Errorf("identity alias migration: merged board for %q exceeds %d players",
				owner, boardLimit)
		}
	}
	state.Boards = groups
	return nil
}

func reconcilePickems(state *PersistedState, resolver identity.Resolver) error {
	out := make(map[string]map[string]string, len(state.Pickems))
	for raw, picks := range state.Pickems {
		owner := resolver.Resolve(raw)
		if out[owner] == nil {
			out[owner] = map[string]string{}
		}
		for gameID, team := range picks {
			if previous, ok := out[owner][gameID]; ok && previous != team {
				return fmt.Errorf("identity alias migration: pick'em owner %q has conflicting picks for game %q",
					owner, gameID)
			}
			out[owner][gameID] = team
		}
	}
	state.Pickems = out
	return nil
}

func reconcileBlitzEntries(state *PersistedState, resolver identity.Resolver) error {
	out := make(map[string]map[string]BlitzEntry, len(state.BlitzEntries))
	for raw, entries := range state.BlitzEntries {
		owner := resolver.Resolve(raw)
		if out[owner] == nil {
			out[owner] = map[string]BlitzEntry{}
		}
		for slate, entry := range entries {
			previous, exists := out[owner][slate]
			if !exists {
				out[owner][slate] = cloneBlitzEntry(entry)
				continue
			}
			if !sameBlitzPlayers(previous, entry) {
				return fmt.Errorf("identity alias migration: Blitz owner %q has conflicting entries for slate %q",
					owner, slate)
			}
			if entry.UpdatedAt.After(previous.UpdatedAt) {
				out[owner][slate] = cloneBlitzEntry(entry)
			}
		}
	}
	state.BlitzEntries = out
	return nil
}

func cloneBlitzEntry(entry BlitzEntry) BlitzEntry {
	entry.Players = append([]string(nil), entry.Players...)
	return entry
}

func sameBlitzPlayers(left, right BlitzEntry) bool {
	return reflect.DeepEqual(left.Players, right.Players)
}

func reconcileNotifyPrefs(state *PersistedState, resolver identity.Resolver) error {
	out := make(map[string]map[string]bool, len(state.NotifyPrefs))
	for raw, prefs := range state.NotifyPrefs {
		email := resolver.Resolve(raw)
		if out[email] == nil {
			out[email] = map[string]bool{}
		}
		for category, enabled := range prefs {
			if previous, ok := out[email][category]; ok && previous != enabled {
				return fmt.Errorf("identity alias migration: notification preference for %q/%q conflicts",
					email, category)
			}
			out[email][category] = enabled
		}
	}
	state.NotifyPrefs = out
	return nil
}

func reconcileSentLog(state *PersistedState, resolver identity.Resolver) {
	out := make(map[string]time.Time, len(state.SentLog))
	for key, sentAt := range state.SentLog {
		canonicalKey := key
		if index := strings.LastIndex(key, ":"); index >= 0 {
			suffix := key[index+1:]
			if canonical := resolver.Resolve(suffix); canonical != suffix {
				canonicalKey = key[:index+1] + canonical
			}
		}
		if previous, ok := out[canonicalKey]; !ok || sentAt.Before(previous) {
			out[canonicalKey] = sentAt
		}
	}
	state.SentLog = out
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func identityStateCollections() []collectionID {
	return []collectionID{
		colMembers,
		colCoInvites,
		colBoards,
		colPickems,
		colBlitzEntries,
		colNotifyPrefs,
		colAnnouncements,
		colSentLog,
	}
}
