// Wizard draft persistence (setup_draft, design section 4.4) and Tier 0
// invite-link auth (invite_links, design section 6.2). Both are
// deliberately independent of PersistedState's collection/shadow-diff
// system (sqlstore.go): setup_draft is wizard-only scratch state that never
// becomes league state, and invite_links' single-use consume is a direct
// conditional UPDATE under the store's one-writer discipline
// (sqlstore.go's MaxOpenConns(1) already serializes it), not a
// snapshot-diffed collection.
package league

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// DefaultInviteLinkTTL is the invite link's expiry window when the caller
// does not name one (owner decision: 7 days — "share in group chat this
// week", not "tonight"). Still single-use, still revocable, still audited.
const DefaultInviteLinkTTL = 7 * 24 * time.Hour

// setupTokenEncoding is the lowercase, unpadded base32 alphabet every
// one-time token in this design shares (design section 3.3): unambiguous to
// read aloud or copy-paste, no '=' padding noise.
var setupTokenEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newRandomToken returns n bytes of crypto/rand entropy, lowercase base32
// encoded. 32 bytes (the only size this file uses) is 256 bits.
func newRandomToken(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return strings.ToLower(setupTokenEncoding.EncodeToString(raw)), nil
}

// hashToken returns the hex SHA-256 digest of a raw token, the only form
// ever persisted (design section 6.4: "Database theft: hashes only; no
// token is recoverable").
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ConstantTimeTokenEqual compares two raw tokens without leaking timing
// information. Exported for the setup-boot-token check (package main),
// which mints and compares its own token the same way but is not itself
// league domain state.
func ConstantTimeTokenEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// ---------------------------------------------------------------------
// setup_draft: resumable wizard progress (design section 4.4).
// ---------------------------------------------------------------------

// SetupDraft is the setup_draft table's one row: the non-secret configFile
// draft plus a step-status map, both opaque JSON blobs to the store.
type SetupDraft struct {
	DraftJSON      []byte
	StepStatusJSON []byte
	UpdatedAt      time.Time
}

// LoadSetupDraft reads the persisted wizard draft, if any. found is false on
// a database with no saved progress (a fresh SETUP boot, or one where every
// step still holds only defaults).
func (s *Store) LoadSetupDraft() (draft SetupDraft, found bool, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return SetupDraft{}, false, nil
	}
	var draftJSON, stepStatusJSON, updatedAt string
	row := s.db.QueryRow(`SELECT draft_json, step_status_json, updated_at FROM setup_draft WHERE id = 1`)
	switch err := row.Scan(&draftJSON, &stepStatusJSON, &updatedAt); {
	case err == sql.ErrNoRows:
		return SetupDraft{}, false, nil
	case err != nil:
		return SetupDraft{}, false, fmt.Errorf("%w: read setup_draft: %w", ErrInternal, err)
	}
	parsed, _ := time.Parse(time.RFC3339, updatedAt)
	return SetupDraft{DraftJSON: []byte(draftJSON), StepStatusJSON: []byte(stepStatusJSON), UpdatedAt: parsed}, true, nil
}

// SaveSetupDraft persists the wizard's current non-secret draft and
// step-status map, replacing whatever was saved before. Called after every
// successful step POST (design section 4.4), so a container restart resumes
// at the first incomplete step. Secrets never reach this method — callers
// keep those in the bound session only (design section 4.3).
func (s *Store) SaveSetupDraft(draftJSON, stepStatusJSON []byte, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.db == nil {
		return fmt.Errorf("%w: no database open", ErrInternal)
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO setup_draft (id, draft_json, step_status_json, updated_at) VALUES (1, ?, ?, ?)`,
		string(draftJSON), string(stepStatusJSON), now.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("%w: write setup_draft: %w", ErrInternal, err)
	}
	return nil
}

// ---------------------------------------------------------------------
// invite_links: Tier 0 auth (design section 6.2).
// ---------------------------------------------------------------------

// InviteLink is one invite_links row.
type InviteLink struct {
	ID            int64
	Email         string
	CreatedBy     string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	ConsumedAt    time.Time
	ConsumedEmail string
	RevokedAt     time.Time
}

// InviteLinkState names one of the four typed states design section 6.2
// pins: "UNUSED / CONSUMED (when, from where) / EXPIRED / REVOKED" — words,
// not color.
type InviteLinkState string

const (
	InviteLinkUnused   InviteLinkState = "unused"
	InviteLinkConsumed InviteLinkState = "consumed"
	InviteLinkExpired  InviteLinkState = "expired"
	InviteLinkRevoked  InviteLinkState = "revoked"
)

// StateAt reports link's typed state as of now. Consumed is checked first:
// once a link has been used, that fact is permanent and outranks a later
// revoke or the token's own expiry.
func (l InviteLink) StateAt(now time.Time) InviteLinkState {
	switch {
	case !l.ConsumedAt.IsZero():
		return InviteLinkConsumed
	case !l.RevokedAt.IsZero():
		return InviteLinkRevoked
	case !now.Before(l.ExpiresAt):
		return InviteLinkExpired
	default:
		return InviteLinkUnused
	}
}

func scanInviteLink(row interface{ Scan(...any) error }) (InviteLink, error) {
	var link InviteLink
	var createdAt, expiresAt, consumedAt, revokedAt string
	if err := row.Scan(&link.ID, &link.Email, &link.CreatedBy, &createdAt, &expiresAt, &consumedAt, &link.ConsumedEmail, &revokedAt); err != nil {
		return InviteLink{}, err
	}
	link.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	link.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	if consumedAt != "" {
		link.ConsumedAt, _ = time.Parse(time.RFC3339, consumedAt)
	}
	if revokedAt != "" {
		link.RevokedAt, _ = time.Parse(time.RFC3339, revokedAt)
	}
	return link, nil
}

const inviteLinkColumns = `id, email, created_by, created_at, expires_at, consumed_at, consumed_email, revoked_at`

// MintInviteLink creates a new Tier 0 invite link for email only — it does
// not touch the plain admission invite list (state.Invites). Most callers
// want MintInviteLinkWithAdmission instead (design section 6.2: "extends
// AddInvite; one action records the invite AND mints the link"); this
// lower-level entry point exists for re-minting a link to an email that is
// already admitted some other way (a persisted member, a domain match, or
// an already-recorded invite), where adding it to state.Invites again
// would be a redundant write. Returns the raw token exactly once — only
// its SHA-256 hash is ever stored (design section 6.4: "Database theft:
// hashes only; no token is recoverable"). Minting supersedes (revokes)
// every earlier unused link already minted for the same email (design
// section 6.2). ttl<=0 uses DefaultInviteLinkTTL.
func (s *Store) MintInviteLink(email, createdBy string, ttl time.Duration, now time.Time) (token string, link InviteLink, err error) {
	email = admissionEmail(email)
	if email == "" || !strings.Contains(email, "@") {
		return "", InviteLink{}, fmt.Errorf("enter a valid email address")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return "", InviteLink{}, err
	}
	return s.mintInviteLinkLocked(email, createdBy, ttl, now)
}

// MintInviteLinkWithAdmission is the real mint entry point (design section
// 6.2): it records email on the plain admission invite list
// (state.Invites, the same list Store.AddInvite maintains and
// Service.EmailAllowed reads) and mints the Tier 0 invite link, in the
// same locked section — "one action records the invite AND mints the
// link." Every real caller (the wizard's membership step and atomic
// commit; a later /admin mint action) uses this, not the lower-level
// MintInviteLink alone, so a freshly minted link always admits the email
// it targets.
func (s *Store) MintInviteLinkWithAdmission(email, createdBy string, ttl time.Duration, now time.Time) (token string, link InviteLink, err error) {
	email = admissionEmail(email)
	if email == "" || !strings.Contains(email, "@") {
		return "", InviteLink{}, fmt.Errorf("enter a valid email address")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return "", InviteLink{}, err
	}
	alreadyInvited := false
	for _, existing := range s.state.Invites {
		if existing == email {
			alreadyInvited = true
			break
		}
	}
	if !alreadyInvited {
		s.state.Invites = append(s.state.Invites, email)
		if err := s.persistLocked(colInvites); err != nil {
			return "", InviteLink{}, err
		}
	}
	return s.mintInviteLinkLocked(email, createdBy, ttl, now)
}

// mintInviteLinkLocked is MintInviteLink/MintInviteLinkWithAdmission's
// shared core. Callers hold s.mu and have already run writeErrorLocked.
func (s *Store) mintInviteLinkLocked(email, createdBy string, ttl time.Duration, now time.Time) (token string, link InviteLink, err error) {
	if ttl <= 0 {
		ttl = DefaultInviteLinkTTL
	}
	token, err = newRandomToken(32)
	if err != nil {
		return "", InviteLink{}, err
	}
	if s.db == nil {
		return "", InviteLink{}, fmt.Errorf("%w: no database open", ErrInternal)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", InviteLink{}, fmt.Errorf("%w: begin invite_links tx: %w", ErrInternal, err)
	}
	defer func() { _ = tx.Rollback() }()
	nowText := now.UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`UPDATE invite_links SET revoked_at = ? WHERE email = ? AND consumed_at = '' AND revoked_at = ''`, nowText, email); err != nil {
		return "", InviteLink{}, fmt.Errorf("%w: revoke prior invite links: %w", ErrInternal, err)
	}
	expiresAt := now.Add(ttl)
	result, err := tx.Exec(`INSERT INTO invite_links (email, token_hash, created_by, created_at, expires_at, consumed_at, consumed_email, revoked_at)
		VALUES (?, ?, ?, ?, ?, '', '', '')`, email, hashToken(token), createdBy, nowText, expiresAt.UTC().Format(time.RFC3339))
	if err != nil {
		return "", InviteLink{}, fmt.Errorf("%w: insert invite_links: %w", ErrInternal, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return "", InviteLink{}, fmt.Errorf("%w: invite_links insert id: %w", ErrInternal, err)
	}
	if err := tx.Commit(); err != nil {
		return "", InviteLink{}, fmt.Errorf("%w: commit invite_links: %w", ErrInternal, err)
	}
	link = InviteLink{
		ID: id, Email: email, CreatedBy: createdBy,
		CreatedAt: now.UTC(), ExpiresAt: expiresAt.UTC(),
	}
	return token, link, nil
}

// LookupInviteLinkByToken performs a read-only lookup for the GET
// confirmation page (design section 6.2): "a read-only lookup and renders a
// typed confirmation." ok is false only when no invite link was ever minted
// with this token — an unrecognized token must render the one generic
// invalid page, revealing no email (design section 6.4).
func (s *Store) LookupInviteLinkByToken(token string) (InviteLink, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return InviteLink{}, false, nil
	}
	row := s.db.QueryRow(`SELECT `+inviteLinkColumns+` FROM invite_links WHERE token_hash = ?`, hashToken(token))
	link, err := scanInviteLink(row)
	switch {
	case err == sql.ErrNoRows:
		return InviteLink{}, false, nil
	case err != nil:
		return InviteLink{}, false, fmt.Errorf("%w: read invite_links: %w", ErrInternal, err)
	}
	return link, true, nil
}

// ConsumeInviteLink atomically claims token for consumedEmail (the
// canonical identity signing in), exactly once. The single conditional
// UPDATE (design section 6.2) is the entire race-free contract: under the
// store's one-writer SQLite pool (sqlstore.go's MaxOpenConns(1)) a replay or
// a concurrent double-submit both resolve to "RowsAffected != 1", and
// exactly one caller ever observes ok == true for a given token. ok is
// false for any invalid/expired/consumed/revoked/unknown token; the caller
// should re-run LookupInviteLinkByToken to render the specific truthful
// state.
func (s *Store) ConsumeInviteLink(token, consumedEmail string, now time.Time) (ok bool, err error) {
	consumedEmail = admissionEmail(consumedEmail)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return false, err
	}
	if s.db == nil {
		return false, fmt.Errorf("%w: no database open", ErrInternal)
	}
	result, err := s.db.Exec(`UPDATE invite_links SET consumed_at = ?, consumed_email = ?
		WHERE token_hash = ? AND consumed_at = '' AND revoked_at = '' AND expires_at > ?`,
		now.UTC().Format(time.RFC3339), consumedEmail, hashToken(token), now.UTC().Format(time.RFC3339))
	if err != nil {
		return false, fmt.Errorf("%w: consume invite_links: %w", ErrInternal, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("%w: invite_links rows affected: %w", ErrInternal, err)
	}
	return rows == 1, nil
}

// RevokeInviteLink marks id revoked as of now. Revoking an already
// consumed or already revoked link is a harmless no-op (idempotent audit
// action): the WHERE clause only ever changes an unused link's row.
func (s *Store) RevokeInviteLink(id int64, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.db == nil {
		return fmt.Errorf("%w: no database open", ErrInternal)
	}
	_, err := s.db.Exec(`UPDATE invite_links SET revoked_at = ? WHERE id = ? AND consumed_at = '' AND revoked_at = ''`, now.UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("%w: revoke invite_links: %w", ErrInternal, err)
	}
	return nil
}

// InviteLinksForEmail lists every invite link ever minted for email, newest
// first — /admin's per-member history (slice 4) and the wizard's own review
// step read this.
func (s *Store) InviteLinksForEmail(email string) ([]InviteLink, error) {
	email = admissionEmail(email)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT `+inviteLinkColumns+` FROM invite_links WHERE email = ? ORDER BY id DESC`, email)
	if err != nil {
		return nil, fmt.Errorf("%w: query invite_links: %w", ErrInternal, err)
	}
	defer rows.Close()
	return collectInviteLinks(rows)
}

// InviteLinks lists every invite link ever minted, newest first.
func (s *Store) InviteLinks() ([]InviteLink, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT ` + inviteLinkColumns + ` FROM invite_links ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("%w: query invite_links: %w", ErrInternal, err)
	}
	defer rows.Close()
	return collectInviteLinks(rows)
}

func collectInviteLinks(rows *sql.Rows) ([]InviteLink, error) {
	var links []InviteLink
	for rows.Next() {
		link, err := scanInviteLink(rows)
		if err != nil {
			return nil, fmt.Errorf("%w: scan invite_links: %w", ErrInternal, err)
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: iterate invite_links: %w", ErrInternal, err)
	}
	return links, nil
}
