package league

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // format sniffer + decoder for image.DecodeConfig / image.Decode
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
)

// Avatar dimension and size limits (design decision 1). AvatarOutputSize is
// the square thumbnail every stored avatar is normalized to.
const (
	AvatarMinDimension = 64
	AvatarMaxDimension = 4096
	AvatarOutputSize   = 512
	// AvatarMaxBytes is the uploaded file's own size ceiling. This is
	// checked on the decoded upload body itself, independent of whatever
	// envelope (multipart boundaries, other fields) the HTTP request wraps
	// it in — see avatarUploadHandler in the main package for the request
	// side of this limit.
	AvatarMaxBytes = 10 * 1024 * 1024

	// avatarDecodeConcurrency bounds the number of uploads that may hold a
	// fully decoded source image and Catmull-Rom scratch at once. A 4096x4096
	// 16-bit PNG decode can occupy roughly 128 MiB and scaling to 512x512 may
	// add roughly 64 MiB, so one slot preserves headroom in a 512 MiB pod.
	avatarDecodeConcurrency = 1
)

// Exact validation messages (brief's security/validation matrix). Every
// caller — the HTTP handler and every test — surfaces err.Error() as-is, so
// these strings are the contract, not the Go error variable names.
var (
	ErrAvatarWrongType     = errors.New("upload a PNG or JPEG image")
	ErrAvatarTooLarge      = errors.New("image must be 10MB or smaller")
	ErrAvatarBadDimensions = errors.New("image must be between 64x64 and 4096x4096")
	ErrAvatarForbidden     = errors.New("only the seat's manager or the commissioner can set this avatar")
)

// avatarDecodeGate is a small counting semaphore kept local to the upload
// pipeline. Keeping it as a type makes the capacity contract directly
// testable without replacing the process-wide gate used by production.
type avatarDecodeGate chan struct{}

func newAvatarDecodeGate(capacity int) avatarDecodeGate {
	if capacity < 1 {
		panic("avatar decode gate capacity must be positive")
	}
	return make(avatarDecodeGate, capacity)
}

func (gate avatarDecodeGate) acquire() func() {
	gate <- struct{}{}
	return func() { <-gate }
}

var avatarDecodeSlots = newAvatarDecodeGate(avatarDecodeConcurrency)

// AvatarUploadResult describes the durable identity transition completed by
// UploadAvatar. Ref is the immutable content address used by the serving
// route; BadgeReleased tells the UI whether the previous badge reservation
// was returned to the catalog in the same Store transaction.
type AvatarUploadResult struct {
	Ref           string
	BadgeReleased bool
}

type seatActor struct {
	email        string
	commissioner bool
	demo         bool
}

func (s *Service) seatActor(r *http.Request) seatActor {
	actor := seatActor{commissioner: s.IsCommissioner(r), demo: s.demoMode}
	if user, ok := s.CurrentUser(r); ok {
		actor.email = user.Email
	}
	return actor
}

// defaultBadgeCacheTTL bounds how long defaultBadgeExists trusts its last
// directory scan. The defaults directory only changes when the commissioner
// drops in new tone artwork (a rare, manual, out-of-band event), so a short
// TTL is "reasonable invalidation" (design decision 4) without forcing a
// fresh os.ReadDir on every TeamMark render.
const defaultBadgeCacheTTL = 30 * time.Second

// badgeToneCache is the cached result of scanning defaultBadgeRoot for
// {tone}.png files. The zero value is ready to use: an empty tones map and
// a zero scanAt just force one scan on first use.
type badgeToneCache struct {
	mu     sync.Mutex
	scanAt time.Time
	tones  map[string]bool
}

// avatarEnvString reads key from the environment, trimmed, falling back to
// fallback when unset or blank — the same envString idiom
// internal/fantasy, internal/openstats, and internal/wire each define for
// their own *_ROOT variables.
func avatarEnvString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// avatarDir resolves the directory uploaded avatar PNGs live in: AVATAR_ROOT
// when the service was constructed with one (see Default), else
// "data/avatars" — the same data/<subsystem> convention
// internal/fantasy, internal/openstats, and internal/wire already use. The
// path is only writable when it remains strictly below avatarDurableDir.
func (s *Service) avatarDir() string {
	if s.avatarRoot != "" {
		return s.avatarRoot
	}
	return filepath.Join("data", "avatars")
}

// avatarDurableDir resolves the pre-existing filesystem boundary for avatar
// writes. It is intentionally independent from avatarDir: retries must use
// the same stable anchor even after a failed attempt has created target
// directories. In the container, the relative default "data" resolves to
// the deployed /app/data PVC root.
func (s *Service) avatarDurableDir() string {
	if s.avatarDurableRoot != "" {
		return s.avatarDurableRoot
	}
	return "data"
}

// defaultBadgeDir resolves the directory the commissioner-supplied default
// tone badges live in: AVATAR_DEFAULTS_ROOT when set, else
// "public/avatars/defaults" (see docs/avatar-default-badges.md).
func (s *Service) defaultBadgeDir() string {
	if s.defaultBadgeRoot != "" {
		return s.defaultBadgeRoot
	}
	return filepath.Join("public", "avatars", "defaults")
}

// validAvatarRef accepts only the lower-case SHA-256 spelling used by the
// immutable object store. Keeping this check beside every path resolver
// prevents a caller from turning a content-addressed reference into a path
// traversal or alternate object name.
func validAvatarRef(ref string) bool {
	if len(ref) != sha256.Size*2 || strings.ToLower(ref) != ref {
		return false
	}
	_, err := hex.DecodeString(ref)
	return err == nil
}

func (s *Service) avatarObjectDir() string {
	return filepath.Join(s.avatarDir(), "objects")
}

// AvatarObjectPath resolves only the exact currently referenced object for a
// known team. A stale, guessed, or unreferenced hash never reaches disk.
func (s *Service) AvatarObjectPath(teamID, ref string) (path string, ok bool) {
	teamID = strings.TrimSpace(teamID)
	ref = strings.TrimSpace(ref)
	if !knownTeam(teamID) || !validAvatarRef(ref) || !s.store.IdentityHealthy() {
		return "", false
	}
	paths, err := canonicalAvatarStoragePaths(s.avatarDurableDir(), s.avatarDir())
	if err != nil {
		return "", false
	}
	current, ok := s.store.AvatarRef(teamID)
	if !ok || current != ref {
		return "", false
	}
	return filepath.Join(paths.objectDir, ref+".png"), true
}

// avatarObjectHandle owns both the rooted directory and the opened object
// descriptor. The directory root must stay alive while the descriptor is in
// use; closing it after the file also makes the ownership boundary explicit
// to callers that need to read immutable object bytes.
type avatarObjectHandle struct {
	root *os.Root
	file *os.File
	info os.FileInfo
}

func (h *avatarObjectHandle) Close() error {
	if h == nil {
		return nil
	}
	fileErr := h.file.Close()
	rootErr := h.root.Close()
	if fileErr != nil {
		return fileErr
	}
	return rootErr
}

// openVerifiedAvatarObject opens one immutable object beneath the configured
// durable anchor without ever following a final-leaf symlink. The anchor is
// pinned by comparing its Lstat identity with the descriptor-backed rooted
// Stat result; a path replacement between those checks is rejected. The
// object directory is then opened relative to that root, so a directory
// replacement cannot escape to a sibling or outside path. Finally, Lstat
// rejects a symlink/non-regular leaf and the descriptor's Stat identity must
// still match it, closing the final-leaf replacement window between check and
// open. All bytes consumed by callers come from this descriptor, never from a
// path reopened after validation.
func openVerifiedAvatarObject(anchor, objectDir, name string) (*avatarObjectHandle, error) {
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
		return nil, fmt.Errorf("invalid avatar object name %q", name)
	}
	relative, err := filepath.Rel(anchor, objectDir)
	if err != nil {
		return nil, err
	}
	if relative == "." {
		return nil, fmt.Errorf("avatar object directory must be strictly below durable anchor")
	}
	if err := avatarPathStrictlyUnder(anchor, objectDir); err != nil {
		return nil, err
	}

	anchorInfo, err := os.Lstat(anchor)
	if err != nil {
		return nil, err
	}
	if anchorInfo.Mode()&os.ModeSymlink != 0 || !anchorInfo.IsDir() {
		return nil, fmt.Errorf("avatar durable anchor %s must be a real directory", anchor)
	}
	anchorRoot, err := os.OpenRoot(anchor)
	if err != nil {
		return nil, err
	}
	rootInfo, err := anchorRoot.Stat(".")
	if err != nil {
		_ = anchorRoot.Close()
		return nil, err
	}
	if !os.SameFile(anchorInfo, rootInfo) {
		_ = anchorRoot.Close()
		return nil, fmt.Errorf("avatar durable anchor changed while opening")
	}
	objectRoot, err := anchorRoot.OpenRoot(filepath.ToSlash(relative))
	if err != nil {
		_ = anchorRoot.Close()
		return nil, err
	}
	_ = anchorRoot.Close()

	leafInfo, err := objectRoot.Lstat(name)
	if err != nil {
		_ = objectRoot.Close()
		return nil, err
	}
	if leafInfo.Mode()&os.ModeSymlink != 0 || !leafInfo.Mode().IsRegular() {
		_ = objectRoot.Close()
		return nil, fmt.Errorf("avatar object %s must be a regular non-symlink file", name)
	}
	file, err := objectRoot.Open(name)
	if err != nil {
		_ = objectRoot.Close()
		return nil, err
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		_ = objectRoot.Close()
		return nil, err
	}
	if !os.SameFile(leafInfo, openedInfo) || !openedInfo.Mode().IsRegular() {
		_ = file.Close()
		_ = objectRoot.Close()
		return nil, fmt.Errorf("avatar object %s changed while opening", name)
	}
	return &avatarObjectHandle{root: objectRoot, file: file, info: openedInfo}, nil
}

func readAvatarObjectBytes(file *os.File, limit int64) ([]byte, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(file, limit+1))
}

// ReadAvatarObject returns the exact bytes and descriptor timestamp of the
// currently referenced immutable object. The handler uses this instead of
// reopening AvatarObjectPath, so a final-leaf replacement cannot redirect a
// request to an outside file after the identity lookup.
func (s *Service) ReadAvatarObject(teamID, ref string) ([]byte, time.Time, bool) {
	teamID = strings.TrimSpace(teamID)
	ref = strings.TrimSpace(ref)
	if !knownTeam(teamID) || !validAvatarRef(ref) || !s.store.IdentityHealthy() {
		return nil, time.Time{}, false
	}
	paths, err := canonicalAvatarStoragePaths(s.avatarDurableDir(), s.avatarDir())
	if err != nil {
		return nil, time.Time{}, false
	}
	current, ok := s.store.AvatarRef(teamID)
	if !ok || current != ref {
		return nil, time.Time{}, false
	}
	handle, err := openVerifiedAvatarObject(paths.anchor, paths.objectDir, ref+".png")
	if err != nil {
		return nil, time.Time{}, false
	}
	defer handle.Close()
	if handle.info.Mode().Perm()&0o222 != 0 {
		return nil, time.Time{}, false
	}
	data, err := readAvatarObjectBytes(handle.file, AvatarMaxBytes)
	if err != nil || len(data) > AvatarMaxBytes {
		return nil, time.Time{}, false
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != ref {
		return nil, time.Time{}, false
	}
	return data, handle.info.ModTime(), true
}

// IdentityHealthy reports whether an uncertain identity commit has been
// reconciled. Serving fails closed while it is false.
func (s *Service) IdentityHealthy() bool {
	return s.store.IdentityHealthy()
}

// defaultBadgeExists reports whether tone has a default badge PNG in
// defaultBadgeDir, using a cached directory scan (defaultBadgeCacheTTL) so a
// page rendering many TeamMark instances pays for one os.ReadDir, not one
// per team.
func (s *Service) defaultBadgeExists(tone string) bool {
	tone = strings.TrimSpace(tone)
	if tone == "" {
		return false
	}
	now := s.clock()
	s.badgeCache.mu.Lock()
	defer s.badgeCache.mu.Unlock()
	if s.badgeCache.tones == nil || now.Sub(s.badgeCache.scanAt) > defaultBadgeCacheTTL {
		s.badgeCache.tones = scanDefaultBadgeTones(s.defaultBadgeDir())
		s.badgeCache.scanAt = now
	}
	return s.badgeCache.tones[tone]
}

// scanDefaultBadgeTones lists dir and returns the set of tones with a
// {tone}.png file. A missing or unreadable directory yields an empty set
// (design decision 4: the defaults directory ships empty and the badge
// tier is skipped entirely until the owner drops artwork in).
func scanDefaultBadgeTones(dir string) map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.EqualFold(filepath.Ext(name), ".png") {
			out[strings.TrimSuffix(name, filepath.Ext(name))] = true
		}
	}
	return out
}

// avatarView resolves the four-tier fallback chain (design decision 4,
// extended by the team-badge picker feature) for one team into the
// fields the render layer needs: hasAvatar reports specifically an
// uploaded photo (tier a — the only tier "reset avatar" applies to);
// hasImage reports whether there is any image to show at all (tier a, a
// claimed badge motif tier b, or the tone's default badge tier c); url is
// that image's request path, empty when hasImage is false. A false
// hasImage means the caller falls back to the plain text mark (tier d) —
// this function never returns a URL pointing at a file it has not just
// confirmed exists (a badge claim always has a matching motif PNG; see
// knownMotif's gate at claim time).
func (s *Service) avatarView(teamID, tone string) (hasAvatar, hasImage bool, url string) {
	if !s.store.IdentityHealthy() {
		// A poisoned identity store must not render a fallback that could be
		// mistaken for the authoritative avatar/badge pair. The caller can
		// keep the text mark while the process is restarted or repaired.
		return false, false, ""
	}
	if ref, referenced := s.store.AvatarRef(teamID); referenced {
		// The database ref is the only identity authority and the hash is the
		// cache version. The serving route verifies the exact ref and fails
		// closed if its immutable object is missing or corrupt.
		return true, true, fmt.Sprintf("/avatars/custom/%s/%s.png", teamID, ref)
	}
	if _, ok := s.store.BadgeClaim(teamID); ok {
		// The query value is the hash of the exact rendered PNG, not the
		// mutable motif slug. This keeps a changed tone/render from being
		// served from a URL whose bytes were cached under the old claim.
		_, version, rendered := s.BadgeImage(teamID)
		if !rendered {
			return false, false, ""
		}
		return false, true, fmt.Sprintf("/avatars/badge/%s.png?v=%s", teamID, version)
	}
	if s.defaultBadgeExists(tone) {
		return false, true, fmt.Sprintf("/avatars/defaults/%s.png", tone)
	}
	return false, false, ""
}

// avatarViewLarge is avatarView's counterpart for the one surface that
// needs BadgeOutputSizeLarge instead of BadgeOutputSize: the team
// identity page's own .team-monogram hero (app/team/page.gsx; see
// BadgeOutputSizeLarge's doc comment on badge.go). Tier a — an uploaded
// custom avatar — is unaffected: that object is already normalized to
// AvatarOutputSize and out of scope for this sizing pass, so this only
// differs from avatarView in tiers b and c. Tier c falls back to the
// BadgeOutputSize default file if no large variant is on disk yet, rather
// than dropping to the text mark, matching avatarView's own
// "never link a file that has not just been confirmed to exist" rule.
func (s *Service) avatarViewLarge(teamID, tone string) (hasAvatar, hasImage bool, url string) {
	if !s.store.IdentityHealthy() {
		return false, false, ""
	}
	if ref, referenced := s.store.AvatarRef(teamID); referenced {
		return true, true, fmt.Sprintf("/avatars/custom/%s/%s.png", teamID, ref)
	}
	if _, ok := s.store.BadgeClaim(teamID); ok {
		_, version, rendered := s.BadgeImageLarge(teamID)
		if !rendered {
			return false, false, ""
		}
		return false, true, fmt.Sprintf("/avatars/badge/%s-lg.png?v=%s", teamID, version)
	}
	if s.defaultBadgeLargeExists(tone) {
		return false, true, fmt.Sprintf("/avatars/defaults/large/%s.png", tone)
	}
	if s.defaultBadgeExists(tone) {
		return false, true, fmt.Sprintf("/avatars/defaults/%s.png", tone)
	}
	return false, false, ""
}

// defaultBadgeLargeExists reports whether tone has a BadgeOutputSizeLarge
// default badge file. Checked directly (not cached like
// defaultBadgeExists's directory scan): avatarViewLarge's only caller
// renders exactly one team's own hero mark per request, so there is no
// many-teams-per-page cost here to amortize.
func (s *Service) defaultBadgeLargeExists(tone string) bool {
	tone = strings.TrimSpace(tone)
	if tone == "" {
		return false
	}
	_, err := os.Stat(s.defaultBadgeLargePath(tone))
	return err == nil
}

// defaultBadgeLargePath resolves the BadgeOutputSizeLarge counterpart of
// a {tone}.png default badge, kept in a "large" subdirectory rather than
// a "{tone}-lg.png" sibling so scanDefaultBadgeTones's flat directory
// listing (which treats every top-level *.png's stem as a tone name)
// never mistakes it for one.
func (s *Service) defaultBadgeLargePath(tone string) string {
	return filepath.Join(s.defaultBadgeDir(), "large", tone+".png")
}

// avatarDigest renders "team-1:<sha256>,team-3:<sha256>" across every
// default team ID that currently has a stored avatar, in order
// (defaultTeamIDs is already sorted team-1..team-8), followed by
// "team-1=wolf" pairs across every team that currently holds a badge claim.
// Custom-avatar refs are content hashes; badge claims use the canonical motif
// slug for this lightweight state fingerprint, while avatarView derives the
// rendered badge URL's exact-byte hash through BadgeImage.
func (s *Service) avatarDigest() string {
	order := defaultTeamIDs()
	parts := make([]string, 0, len(order))
	for _, teamID := range order {
		if ref, ok := s.store.AvatarRef(teamID); ok {
			parts = append(parts, teamID+":"+ref)
		}
	}
	claims := s.store.BadgeClaims()
	for _, teamID := range order {
		if motif, ok := claims[teamID]; ok {
			parts = append(parts, teamID+"="+motif)
		}
	}
	return strings.Join(parts, ",")
}

// canSetAvatar reports whether the request may set teamID's avatar: the
// commissioner may set any team's, matching every other Admin* authority
// gate (requireCommissioner); anyone else only their own seat, resolved the
// same way MakePick/ToggleReady resolve "the acting team" (actingTeam) —
// Google sign-in bound to that team's seat, or a named seat in demo mode.
func (s *Service) canSetAvatar(r *http.Request, teamID string) bool {
	if s.IsCommissioner(r) {
		return true
	}
	acting, err := s.actingTeam(r, teamID)
	return err == nil && acting == teamID
}

// UploadAvatar validates, normalizes, and stores teamID's avatar from the
// raw uploaded file body. Order of operations matters for both security and
// the exact-message contract: teamID is checked against the known team list
// before it can influence an object path, authorization is checked before any
// image decoding work happens, and only then does the request body run through
// processAvatarImage's validate/normalize/re-encode pipeline (itself
// directly unit-testable without an HTTP request at all). The final PNG object
// is installed before the Store transaction, so a crash can leave
// only an invisible orphan. The Store transaction then publishes the ref and
// removes any badge claim atomically. It returns the immutable ref and whether
// the former badge was released.
func (s *Service) UploadAvatar(r *http.Request, teamID string, data []byte) (AvatarUploadResult, error) {
	teamID = strings.TrimSpace(teamID)
	if !knownTeam(teamID) {
		return AvatarUploadResult{}, fmt.Errorf("unknown team %q", teamID)
	}
	if !s.store.IdentityHealthy() {
		return AvatarUploadResult{}, ErrPersistenceIndeterminate
	}
	if !s.canSetAvatar(r, teamID) {
		return AvatarUploadResult{}, ErrAvatarForbidden
	}
	normalized, err := processAvatarImage(data)
	if err != nil {
		return AvatarUploadResult{}, err
	}
	ref, err := writeAvatarBlob(s.avatarDurableDir(), s.avatarDir(), normalized)
	if err != nil {
		return AvatarUploadResult{}, err
	}
	actor := s.seatActor(r)
	released, err := s.store.activateAvatar(teamID, ref, actor)
	if err != nil {
		return AvatarUploadResult{}, err
	}
	return AvatarUploadResult{Ref: ref, BadgeReleased: released}, nil
}

// ResetAvatar deletes teamID's stored avatar, restoring the default-badge
// or text-mark fallback on the next render. Commissioner only — matching
// every other seat-management Admin* action's authority gate.
func (s *Service) ResetAvatar(r *http.Request, teamID string) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	teamID = strings.TrimSpace(teamID)
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	actor := s.seatActor(r)
	return s.store.clearAvatar(teamID, actor)
}

// processAvatarImage validates raw upload bytes and normalizes them into a
// fresh 512x512 PNG thumbnail (design decision 1). image.DecodeConfig reads
// only the format header — never the pixel grid — so the dimension check
// below runs (and can reject) before any full decode is attempted; that
// ordering is the decode-bomb guard the brief calls for. The decode gate
// bounds the number of expensive pixel-grid allocations in a 512 MiB pod.
// Re-encoding as PNG from a freshly decoded image.Image (rather than ever
// writing the caller's original bytes to disk) also strips any embedded
// metadata and means whatever malformed trailing bytes a hostile file
// carried never reach disk.
func processAvatarImage(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, ErrAvatarWrongType
	}
	if len(data) > AvatarMaxBytes {
		return nil, ErrAvatarTooLarge
	}
	reader := bytes.NewReader(data)
	cfg, format, err := image.DecodeConfig(reader)
	if err != nil || (format != "png" && format != "jpeg") {
		return nil, ErrAvatarWrongType
	}
	if cfg.Width < AvatarMinDimension || cfg.Height < AvatarMinDimension ||
		cfg.Width > AvatarMaxDimension || cfg.Height > AvatarMaxDimension {
		return nil, ErrAvatarBadDimensions
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	release := avatarDecodeSlots.acquire()
	defer release()
	img, _, err := image.Decode(reader)
	if err != nil {
		return nil, ErrAvatarWrongType
	}
	thumbnail := centerSquareAndScale(img, AvatarOutputSize)
	var buf bytes.Buffer
	if err := png.Encode(&buf, thumbnail); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// centerSquareAndScale crops src to its largest centered square, then
// high-quality scales that square to an outSize x outSize image. Source
// images smaller than outSize are scaled up, not just cropped — the
// smallest accepted upload (64x64) is well below the 512x512 output, and
// this is the one step that reconciles that (design decision 1:
// "downscale/center-crop to 512x512" covers both directions in practice).
// badge.go's tintedBadgePNG also calls this for its own square-to-square
// downscale (its source art is always the same 512x512 square already,
// so only the scale, never the crop, is exercised there).
func centerSquareAndScale(src image.Image, outSize int) *image.RGBA {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	side := width
	if height < side {
		side = height
	}
	if side < 1 {
		side = 1
	}
	offsetX := bounds.Min.X + (width-side)/2
	offsetY := bounds.Min.Y + (height-side)/2
	dst := image.NewRGBA(image.Rect(0, 0, outSize, outSize))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, image.Rect(offsetX, offsetY, offsetX+side, offsetY+side), draw.Src, nil)
	return dst
}

// writeAvatarBlob installs normalized bytes under their SHA-256 object name.
// The temporary file is fsynced before an install-without-replacement hard
// link, and the containing directory is fsynced after a new object appears.
// If another request already installed the same hash, its bytes must match;
// a mismatch is treated as corruption rather than silently reused.
var avatarSyncDirectory = syncAvatarDirectoryOnDisk

type avatarStoragePaths struct {
	anchor    string
	root      string
	objectDir string
}

// canonicalAvatarPath makes the configured paths stable across retries
// without following symlinks. The component checks below deliberately use
// Lstat, so a symlink cannot turn a lexically-contained path into an object
// outside the configured durability boundary.
func canonicalAvatarPath(path, label string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%s must not be empty", label)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("canonicalize %s: %w", label, err)
	}
	return filepath.Clean(absolute), nil
}

func avatarPathStrictlyUnder(anchor, target string) error {
	relative, err := filepath.Rel(anchor, target)
	if err != nil {
		return err
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("avatar target %s must be strictly below durable anchor %s", target, anchor)
	}
	return nil
}

// rejectAvatarSymlinkPath verifies the stable anchor and all of its existing
// ancestors. The anchor is never provisioned by an upload; it must already be
// a real directory before any child is created.
func rejectAvatarSymlinkPath(path, label string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("stat %s: %w", label, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s %s must not contain a symlink", label, path)
		}
		if current == filepath.Dir(current) {
			break
		}
	}
	return nil
}

// rejectExistingAvatarTargetSymlinks checks the target's already-existing
// prefix. Missing components are intentionally left for
// ensureAvatarDirectory, which creates and checks each one immediately
// before syncing its parent.
func rejectExistingAvatarTargetSymlinks(anchor, target string) error {
	relative, err := filepath.Rel(anchor, target)
	if err != nil {
		return err
	}
	current := anchor
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("avatar target %s must not contain a symlink", target)
		}
		if !info.IsDir() {
			return fmt.Errorf("avatar path %s is not a directory", current)
		}
	}
	return nil
}

func canonicalAvatarStoragePaths(anchor, root string) (avatarStoragePaths, error) {
	anchor, err := canonicalAvatarPath(anchor, "avatar durable anchor")
	if err != nil {
		return avatarStoragePaths{}, err
	}
	root, err = canonicalAvatarPath(root, "avatar root")
	if err != nil {
		return avatarStoragePaths{}, err
	}
	objectDir := filepath.Join(root, "objects")
	if err := avatarPathStrictlyUnder(anchor, root); err != nil {
		return avatarStoragePaths{}, err
	}
	if err := avatarPathStrictlyUnder(anchor, objectDir); err != nil {
		return avatarStoragePaths{}, err
	}
	anchorInfo, err := os.Lstat(anchor)
	if err != nil {
		return avatarStoragePaths{}, fmt.Errorf("avatar durable anchor %s must pre-exist: %w", anchor, err)
	}
	if anchorInfo.Mode()&os.ModeSymlink != 0 || !anchorInfo.IsDir() {
		return avatarStoragePaths{}, fmt.Errorf("avatar durable anchor %s must be a real directory", anchor)
	}
	if err := rejectAvatarSymlinkPath(anchor, "avatar durable anchor"); err != nil {
		return avatarStoragePaths{}, err
	}
	if err := rejectExistingAvatarTargetSymlinks(anchor, root); err != nil {
		return avatarStoragePaths{}, err
	}
	if err := rejectExistingAvatarTargetSymlinks(anchor, objectDir); err != nil {
		return avatarStoragePaths{}, err
	}
	return avatarStoragePaths{anchor: anchor, root: root, objectDir: objectDir}, nil
}

// ensureAvatarDirectory creates one component at a time from the pre-existing
// anchor through target. Every edge is re-checked and its parent is fsynced on
// every attempt, including when the child already exists after an earlier
// failed attempt. The anchor itself and the anchor's parent are never synced.
func ensureAvatarDirectory(anchor, target string) error {
	anchor, err := canonicalAvatarPath(anchor, "avatar durable anchor")
	if err != nil {
		return err
	}
	target, err = canonicalAvatarPath(target, "avatar target")
	if err != nil {
		return err
	}
	if err := avatarPathStrictlyUnder(anchor, target); err != nil {
		return err
	}
	anchorInfo, err := os.Lstat(anchor)
	if err != nil {
		return fmt.Errorf("avatar durable anchor %s must pre-exist: %w", anchor, err)
	}
	if anchorInfo.Mode()&os.ModeSymlink != 0 || !anchorInfo.IsDir() {
		return fmt.Errorf("avatar durable anchor %s must be a real directory", anchor)
	}
	if err := rejectAvatarSymlinkPath(anchor, "avatar durable anchor"); err != nil {
		return err
	}
	if err := rejectExistingAvatarTargetSymlinks(anchor, target); err != nil {
		return err
	}
	relative, err := filepath.Rel(anchor, target)
	if err != nil {
		return err
	}
	current := anchor
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		child := filepath.Join(current, component)
		info, err := os.Lstat(child)
		if os.IsNotExist(err) {
			if err := os.Mkdir(child, 0o750); err != nil && !os.IsExist(err) {
				return err
			}
			info, err = os.Lstat(child)
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("avatar path %s is not a real directory", child)
		}
		if err := avatarSyncDirectory(current); err != nil {
			return err
		}
		current = child
	}
	return nil
}

func writeAvatarBlob(anchor, root string, data []byte) (string, error) {
	digest := sha256.Sum256(data)
	ref := hex.EncodeToString(digest[:])
	paths, err := canonicalAvatarStoragePaths(anchor, root)
	if err != nil {
		return "", err
	}
	if err := ensureAvatarDirectory(paths.anchor, paths.objectDir); err != nil {
		return "", err
	}
	objectDir := paths.objectDir
	objectPath := filepath.Join(objectDir, ref+".png")
	tmp, err := os.CreateTemp(objectDir, ".avatar-object-*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	// Make the installed object read-only only after its bytes are durable,
	// then fsync again so the mode change is durable metadata too.
	if err := tmp.Chmod(0o444); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	if err := os.Link(tmpPath, objectPath); err != nil {
		if !os.IsExist(err) {
			return "", err
		}
		handle, openErr := openVerifiedAvatarObject(paths.anchor, objectDir, ref+".png")
		if openErr != nil {
			return "", openErr
		}
		defer handle.Close()
		existing, readErr := readAvatarObjectBytes(handle.file, int64(len(data)))
		if readErr != nil {
			return "", readErr
		}
		if !bytes.Equal(existing, data) {
			return "", fmt.Errorf("avatar object %s failed content-address verification", ref)
		}
		if handle.info.Mode().Perm() != 0o444 {
			return "", fmt.Errorf("avatar object %s is not immutable", ref)
		}
		// Even an idempotent EEXIST path must complete the same directory
		// durability barrier as a newly linked object. This closes the
		// window where metadata from a concurrent/previous install remains
		// only in the page cache.
		if err := syncAvatarObjectDir(objectDir); err != nil {
			return "", err
		}
		if err := removeAvatarTempAndSync(objectDir, tmpPath); err != nil {
			return "", err
		}
		return ref, nil
	}

	if err := syncAvatarObjectDir(objectDir); err != nil {
		return "", err
	}
	if err := removeAvatarTempAndSync(objectDir, tmpPath); err != nil {
		return "", err
	}
	return ref, nil
}

// syncAvatarObjectDir is the directory metadata durability barrier used by
// both the new-link and idempotent-EEXIST install paths.
func syncAvatarObjectDir(objectDir string) error {
	return avatarSyncDirectory(objectDir)
}

func syncAvatarDirectoryOnDisk(dirPath string) error {
	dir, err := os.Open(dirPath)
	if err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return err
	}
	return dir.Close()
}

// removeAvatarTempAndSync durably removes the second hard link left by the
// staging file after the final object is installed. The deferred Remove in
// writeAvatarBlob remains the error-path backstop; successful installs use
// this helper so no temporary name is left after a crash/restart barrier.
func removeAvatarTempAndSync(objectDir, tmpPath string) error {
	if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncAvatarObjectDir(objectDir)
}
