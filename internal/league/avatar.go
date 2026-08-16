package league

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // format sniffer + decoder for image.DecodeConfig / image.Decode
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Avatar dimension and size limits (design decision 1). AvatarOutputSize is
// the square thumbnail every stored avatar is normalized to.
const (
	AvatarMinDimension = 64
	AvatarMaxDimension = 4096
	AvatarOutputSize   = 256
	// AvatarMaxBytes is the uploaded file's own size ceiling. This is
	// checked on the decoded upload body itself, independent of whatever
	// envelope (multipart boundaries, other fields) the HTTP request wraps
	// it in — see avatarUploadHandler in the main package for the request
	// side of this limit.
	AvatarMaxBytes = 2 * 1024 * 1024
)

// Exact validation messages (brief's security/validation matrix). Every
// caller — the HTTP handler and every test — surfaces err.Error() as-is, so
// these strings are the contract, not the Go error variable names.
var (
	ErrAvatarWrongType     = errors.New("upload a PNG or JPEG image")
	ErrAvatarTooLarge      = errors.New("image must be 2MB or smaller")
	ErrAvatarBadDimensions = errors.New("image must be between 64x64 and 4096x4096")
	ErrAvatarForbidden     = errors.New("only the seat's manager or the commissioner can set this avatar")
)

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
// internal/fantasy, internal/openstats, and internal/wire already use.
func (s *Service) avatarDir() string {
	if s.avatarRoot != "" {
		return s.avatarRoot
	}
	return filepath.Join("data", "avatars")
}

// defaultBadgeDir resolves the directory the commissioner-supplied default
// tone badges live in: AVATAR_DEFAULTS_ROOT when set, else
// "public/avatars/defaults" (see public/avatars/defaults/README.md).
func (s *Service) defaultBadgeDir() string {
	if s.defaultBadgeRoot != "" {
		return s.defaultBadgeRoot
	}
	return filepath.Join("public", "avatars", "defaults")
}

// avatarFilePath joins root and teamID.png. teamID must already be
// validated against knownTeam before this is called — see the traversal
// safety note on AvatarFilePath and UploadAvatar. defaultTeams() defines a
// fixed, binary-owned set of IDs ("team-1".."team-8"); none of this
// package's exported entry points path-join a caller-supplied teamID
// without checking it against that set first.
func (s *Service) avatarFilePath(teamID string) string {
	return filepath.Join(s.avatarDir(), teamID+".png")
}

// AvatarFilePath resolves teamID's on-disk avatar path and reports whether
// teamID is known. Callers (the GET /avatars/{file}.png handler) must check
// ok before using path — an unknown teamID never yields a path at all,
// which is what keeps a request like GET /avatars/../../etc/passwd.png from
// ever reaching a filesystem join with attacker-controlled input.
func (s *Service) AvatarFilePath(teamID string) (path string, ok bool) {
	if !knownTeam(teamID) {
		return "", false
	}
	return s.avatarFilePath(teamID), true
}

// AvatarVersion reports teamID's stored avatar file's modification time, as
// Unix seconds, and whether an avatar exists at all. The version feeds both
// the cache-busting ?v= query on the serving URL and the StateFingerprint
// avatars digest (see avatarDigest), so a fresh upload is visible
// immediately on every open page despite the serving route's 24-hour
// Cache-Control lifetime.
func (s *Service) AvatarVersion(teamID string) (version int64, ok bool) {
	path, known := s.AvatarFilePath(teamID)
	if !known {
		return 0, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return info.ModTime().Unix(), true
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

// avatarView resolves the three-tier fallback chain (design decision 4) for
// one team into the fields the render layer needs: hasAvatar reports
// specifically an uploaded photo (tier a — the only tier "reset avatar"
// applies to); hasImage reports whether there is any image to show at all
// (tier a or the tone's default badge, tier b); url is that image's
// request path, empty when hasImage is false. A false hasImage means the
// caller falls back to the plain text mark (tier c) — this function never
// returns a URL pointing at a file it has not just confirmed exists.
func (s *Service) avatarView(teamID, tone string) (hasAvatar, hasImage bool, url string) {
	if version, ok := s.AvatarVersion(teamID); ok {
		return true, true, fmt.Sprintf("/avatars/%s.png?v=%d", teamID, version)
	}
	if s.defaultBadgeExists(tone) {
		return false, true, fmt.Sprintf("/avatars/defaults/%s.png", tone)
	}
	return false, false, ""
}

// avatarDigest renders "team-1:169..,team-3:169..." across every default
// team ID that currently has a stored avatar, in order (defaultTeamIDs is
// already sorted team-1..team-8). It is appended to StateFingerprint's
// suffix the same way the Blitz source's version already is (see
// StateFingerprint's |blitz: suffix), so a page open when a manager
// uploads or a commissioner resets an avatar picks it up on its next poll.
func (s *Service) avatarDigest() string {
	order := defaultTeamIDs()
	parts := make([]string, 0, len(order))
	for _, teamID := range order {
		if version, ok := s.AvatarVersion(teamID); ok {
			parts = append(parts, teamID+":"+strconv.FormatInt(version, 10))
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
// before it ever reaches a filesystem path (traversal safety — see
// AvatarFilePath), authorization is checked before any image decoding work
// happens, and only then does the request body run through
// processAvatarImage's validate/normalize/re-encode pipeline (itself
// directly unit-testable without an HTTP request at all). The final PNG is
// written atomically (temp file + rename — see writeAvatarAtomic), so a
// concurrent reader of GET /avatars/{teamID}.png never observes a partial
// file. It returns the new avatar version (the written file's mtime, Unix
// seconds) on success.
func (s *Service) UploadAvatar(r *http.Request, teamID string, data []byte) (int64, error) {
	teamID = strings.TrimSpace(teamID)
	if !knownTeam(teamID) {
		return 0, fmt.Errorf("unknown team %q", teamID)
	}
	if !s.canSetAvatar(r, teamID) {
		return 0, ErrAvatarForbidden
	}
	normalized, err := processAvatarImage(data)
	if err != nil {
		return 0, err
	}
	if err := writeAvatarAtomic(s.avatarDir(), teamID, normalized); err != nil {
		return 0, err
	}
	version, _ := s.AvatarVersion(teamID)
	return version, nil
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
	path, _ := s.AvatarFilePath(teamID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// processAvatarImage validates raw upload bytes and normalizes them into a
// fresh 256x256 PNG thumbnail (design decision 1). image.DecodeConfig reads
// only the format header — never the pixel grid — so the dimension check
// below runs (and can reject) before any full decode is attempted; that
// ordering is the decode-bomb guard the brief calls for. Re-encoding as PNG
// from a freshly decoded image.Image (rather than ever writing the
// caller's original bytes to disk) also strips any embedded metadata and
// means whatever malformed trailing bytes a hostile file carried never
// reach disk.
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
// nearest-neighbor scales that square to an outSize x outSize image. Source
// images smaller than outSize are scaled up, not just cropped — the
// smallest accepted upload (64x64) is well below the 256x256 output, and
// this is the one step that reconciles that (design decision 1:
// "downscale/center-crop to 256x256" covers both directions in practice).
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
	for y := 0; y < outSize; y++ {
		srcY := offsetY + (y*side)/outSize
		for x := 0; x < outSize; x++ {
			srcX := offsetX + (x*side)/outSize
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}

// writeAvatarAtomic writes data to root/teamID.png via a temp file plus
// rename, matching Store.persistLocked's exact atomicity pattern: a reader
// of the final path never observes a partial write, and a failed write
// never corrupts whatever avatar was already stored there.
func writeAvatarAtomic(root, teamID string, data []byte) error {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(root, ".avatar-*.png")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, filepath.Join(root, teamID+".png"))
}
