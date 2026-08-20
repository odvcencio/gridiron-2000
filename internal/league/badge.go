package league

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// BadgeMotif is one entry in the fixed team-badge catalog: Slug names the
// source art file (public/avatars/motifs/{Slug}.png, or AVATAR_MOTIFS_ROOT
// when set — see (*Service).motifDir) and the claim value stored on a
// team; Name is the display label the picker UI renders.
type BadgeMotif struct {
	Slug string
	Name string
}

// BadgeTransition describes the identity side effect of selecting a badge.
// AvatarCleared is true only when the same durable Store transaction removed
// a previous custom-avatar reference, allowing the handler to tell the user
// exactly what changed without inferring from a later read.
type BadgeTransition struct {
	AvatarCleared bool
}

// BadgeMotifs is the ordered catalog of every badge motif a team may
// claim. Order is the picker grid's render order (design decision: a
// fixed, deterministic order beats an alphabetical or map-derived one, so
// the grid layout does not reshuffle between page loads). Every slug here
// must have a matching public/avatars/motifs/{slug}.png (16 motifs
// shipped; see that directory).
var BadgeMotifs = []BadgeMotif{
	{Slug: "helmet", Name: "Helmet"},
	{Slug: "bull", Name: "Bull"},
	{Slug: "wolf", Name: "Wolf"},
	{Slug: "bolt", Name: "Bolt"},
	{Slug: "rocket", Name: "Rocket"},
	{Slug: "star", Name: "Star"},
	{Slug: "crown", Name: "Crown"},
	{Slug: "eagle", Name: "Eagle"},
	{Slug: "robot", Name: "Robot"},
	{Slug: "ram", Name: "Ram"},
	{Slug: "shark", Name: "Shark"},
	{Slug: "astronaut", Name: "Astronaut"},
	{Slug: "dice", Name: "Dice"},
	{Slug: "fireball", Name: "Fireball"},
	{Slug: "dolphin", Name: "Dolphin"},
	{Slug: "phoenix", Name: "Phoenix"},
}

// knownMotif reports whether slug names a catalog entry. Every store and
// service entry point that accepts a caller-supplied motif checks this
// before the value is persisted or reaches a filesystem path — the same
// knownTeam discipline avatar.go's traversal-safety note describes for
// teamID.
func knownMotif(slug string) bool {
	for _, motif := range BadgeMotifs {
		if motif.Slug == slug {
			return true
		}
	}
	return false
}

// badgeToneHex is the single source of truth for every tone's tint color.
// It is keyed by the same eight tone names teamsFromSeeds' palette and
// config.go's validTones list use — see BadgeToneHex, the page-layer
// accessor.
var badgeToneHex = map[string]string{
	"cyan":    "#38E8FF",
	"blue":    "#4F9DFF",
	"violet":  "#9B87FF",
	"lime":    "#A8FF5C",
	"orange":  "#FF9E45",
	"gold":    "#FFD36A",
	"magenta": "#FF4FD8",
	"pink":    "#FF8AB8",
}

// BadgeToneHex reports tone's tint color as a "#RRGGBB" string, and
// whether tone is recognized. The page layer uses this to set the
// picker grid's "--badge-tone" custom property once, from the viewing
// team's own tone — see app/team/page.server.go's badge_tone_hex field.
func BadgeToneHex(tone string) (hex string, ok bool) {
	hex, ok = badgeToneHex[strings.TrimSpace(tone)]
	return hex, ok
}

// Exact validation messages (mirroring avatar.go's exact-message
// contract — see its var block doc comment): every caller surfaces
// err.Error() as-is, so these strings are the contract, not the Go error
// variable names.
var (
	ErrBadgeUnknownMotif = errors.New("unknown badge motif")
	ErrBadgeForbidden    = errors.New("only the seat's manager or the commissioner can set this badge")
	// ErrBadgeTaken is the sentinel a badgeTakenError satisfies via its Is
	// method, for callers that only need the failure class (errors.Is).
	// The literal text a caller actually surfaces — flash copy, tests —
	// always comes from badgeTakenError.Error() itself: "that badge is
	// already claimed by <team name>", built with the claimant's current
	// display name (TeamNames override included) at Service.ClaimBadge,
	// the one layer that can resolve a team ID to a display name. The
	// store layer only ever knows the claimant's team ID (see
	// badgeClaimedError), never its name.
	ErrBadgeTaken = errors.New("that badge is already claimed")
)

// badgeClaimedError is Store.ClaimBadge's internal representation of "this
// motif is already claimed" — it carries the claimant's team ID, nothing
// more, because Store has no business rendering a display name (that is
// a service/view-layer concern, exactly like every other team-name
// resolution in this package going through teamView/teamByID). Not
// exported: callers outside this package see only Service.ClaimBadge's
// wrapped badgeTakenError.
type badgeClaimedError struct {
	teamID string
}

func (e *badgeClaimedError) Error() string {
	return fmt.Sprintf("badge already claimed by team %s", e.teamID)
}

// badgeTakenError is Service.ClaimBadge's exact-message wrapping of a
// badgeClaimedError: teamName is the claimant's current display name
// (teamByID, so a commissioner TeamNames override is honored). Its Is
// method makes errors.Is(err, ErrBadgeTaken) true without baking a fixed
// team name into a package-level sentinel — see ErrBadgeTaken's doc
// comment.
type badgeTakenError struct {
	teamName string
}

func (e *badgeTakenError) Error() string {
	return fmt.Sprintf("that badge is already claimed by %s", e.teamName)
}

func (e *badgeTakenError) Is(target error) bool {
	return target == ErrBadgeTaken
}

// motifDir resolves the directory the 16 shipped motif PNGs live in:
// AVATAR_MOTIFS_ROOT when the service was constructed with one, else
// "public/avatars/motifs" — the same AVATAR_ROOT/AVATAR_DEFAULTS_ROOT
// override pattern avatarDir/defaultBadgeDir already use.
func (s *Service) motifDir() string {
	if s.motifRoot != "" {
		return s.motifRoot
	}
	return filepath.Join("public", "avatars", "motifs")
}

// badgeArtCache is the in-memory cache of tinted, PNG-encoded badge
// renders, keyed by "{motif}|{tone}" (see badgeArtKey). With 16 motifs
// and 8 tones the whole keyspace is 128 entries at most — unbounded is
// fine at this scale (the brief's own sizing note: "≤14 teams").
type badgeArtCache struct {
	mu    sync.Mutex
	cache map[string][]byte
}

func badgeArtKey(motif, tone string) string {
	return motif + "|" + tone
}

// tintedBadgePNG renders motif's source art tinted in tone's color and
// returns the PNG-encoded bytes, using badgeArt as a memoized cache keyed
// by badgeArtKey so a page rendering many BadgeImage calls (the picker
// grid preview links, the served badge itself) never re-decodes and
// re-tints the same motif+tone pair twice.
func (s *Service) tintedBadgePNG(motif, tone string) ([]byte, error) {
	// Keep the catalog gate immediately beside the filesystem path join. A
	// persisted value is not trusted merely because it came from the store:
	// pre-v1 databases may still contain a retired or otherwise hostile slug.
	if !knownMotif(motif) {
		return nil, ErrBadgeUnknownMotif
	}
	key := badgeArtKey(motif, tone)
	s.badgeArt.mu.Lock()
	if cached, ok := s.badgeArt.cache[key]; ok {
		s.badgeArt.mu.Unlock()
		return cached, nil
	}
	s.badgeArt.mu.Unlock()

	hex, ok := BadgeToneHex(tone)
	if !ok {
		return nil, fmt.Errorf("unknown tone %q", tone)
	}
	toneR, toneG, toneB, err := parseHexColor(hex)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(s.motifDir(), motif+".png")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	tinted := tintMotif(src, toneR, toneG, toneB)
	var buf bytes.Buffer
	if err := png.Encode(&buf, tinted); err != nil {
		return nil, err
	}
	encoded := buf.Bytes()

	s.badgeArt.mu.Lock()
	if s.badgeArt.cache == nil {
		s.badgeArt.cache = map[string][]byte{}
	}
	s.badgeArt.cache[key] = encoded
	s.badgeArt.mu.Unlock()
	return encoded, nil
}

// tintMotif renders src — near-white line art on transparency, with every
// shape detail carried in the alpha channel (see public/avatars/motifs's
// own doc note) — into a solid-tone badge. Each output pixel's RGB is the
// tone color scaled by that source pixel's own luminance, so a softer
// (more anti-aliased, lower-luminance) stroke renders as a softer tint
// rather than snapping to a flat color; alpha is copied unchanged. This
// is the exact formula the shipped public/avatars/defaults/*.png tone
// badges were generated with.
func tintMotif(src image.Image, toneR, toneG, toneB uint8) *image.NRGBA {
	bounds := src.Bounds()
	out := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := nrgbaAt(src, x, y)
			// Standard luma weights (ITU-R BT.601): src is near-white
			// (r≈g≈b) by construction, so any reasonable weighting agrees
			// here, but this is the same formula a non-gray source would
			// need.
			luminance := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 255
			out.SetNRGBA(x, y, color.NRGBA{
				R: scaleToneChannel(toneR, luminance),
				G: scaleToneChannel(toneG, luminance),
				B: scaleToneChannel(toneB, luminance),
				A: a,
			})
		}
	}
	return out
}

// nrgbaAt reads img's pixel at (x, y) as straight (non-premultiplied)
// 8-bit channels, regardless of the decoded image's concrete type —
// png.Decode of a truecolor+alpha PNG already yields *image.NRGBA, but
// converting through color.NRGBAModel keeps this correct for any
// image.Image source.
func nrgbaAt(img image.Image, x, y int) (r, g, b, a uint8) {
	c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	return c.R, c.G, c.B, c.A
}

// scaleToneChannel rounds tone*luminance to the nearest uint8, clamped to
// [0, 255] (luminance is already bounded to [0, 1], so clamping only
// guards float rounding at the edges).
func scaleToneChannel(tone uint8, luminance float64) uint8 {
	value := math.Round(float64(tone) * luminance)
	if value < 0 {
		value = 0
	}
	if value > 255 {
		value = 255
	}
	return uint8(value)
}

// parseHexColor parses a "#RRGGBB" string into its three channels.
func parseHexColor(hex string) (r, g, b uint8, err error) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0, fmt.Errorf("invalid tone hex %q", hex)
	}
	value, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid tone hex %q: %w", hex, err)
	}
	return uint8(value >> 16), uint8(value >> 8), uint8(value), nil
}

// BadgeImage resolves teamID's claimed motif and tone and returns the
// tinted PNG. version is the SHA-256 of the exact rendered bytes, so the
// caller can include it in the URL and use an immutable cache policy without
// serving changed bytes under an old mutable URL. ok is false when teamID is
// unknown, identity persistence is unhealthy, or the team has no badge claim.
func (s *Service) BadgeImage(teamID string) (data []byte, version string, ok bool) {
	if !knownTeam(teamID) || !s.store.IdentityHealthy() {
		return nil, "", false
	}
	motif, claimed := s.store.BadgeClaim(teamID)
	if !claimed {
		return nil, "", false
	}
	// This is deliberately immediately before tintedBadgePNG's artwork path
	// gate. Invalid persisted claims are stripped by state normalization, but
	// this second check keeps a future store/read path from turning arbitrary
	// state into a filesystem path.
	if !knownMotif(motif) {
		return nil, "", false
	}
	team := s.teamByID(teamID)
	data, err := s.tintedBadgePNG(motif, team.Tone)
	if err != nil {
		return nil, "", false
	}
	digest := sha256.Sum256(data)
	return data, hex.EncodeToString(digest[:]), true
}

// ClaimBadge sets teamID's badge to motif. Order of checks matters, the
// same way it does in UploadAvatar: teamID is validated before it can
// reach any store or filesystem path, authorization is checked before
// the store mutation is attempted, and only a store-level failure (an
// unknown motif slipping past knownMotif, or another team already
// holding motif) can still fail after that. Selecting a badge atomically
// claims the motif and clears this team's custom-avatar reference, so the
// two identity choices can never be published as an uncertain pair. A
// previous badge claim is replaced in the same transaction; its freed motif
// needs no separate release call.
func (s *Service) ClaimBadge(r *http.Request, teamID, motif string) error {
	_, err := s.ClaimBadgeWithTransition(r, teamID, motif)
	return err
}

// ClaimBadgeWithTransition is ClaimBadge plus the exact identity transition
// metadata needed by the browser flash. The claim and custom-avatar clear
// still happen in one Store transaction; the metadata is returned only after
// that transaction succeeds.
func (s *Service) ClaimBadgeWithTransition(r *http.Request, teamID, motif string) (BadgeTransition, error) {
	teamID = strings.TrimSpace(teamID)
	motif = strings.TrimSpace(motif)
	if !knownTeam(teamID) {
		return BadgeTransition{}, fmt.Errorf("unknown team %q", teamID)
	}
	if !s.canSetAvatar(r, teamID) {
		return BadgeTransition{}, ErrBadgeForbidden
	}
	if !knownMotif(motif) {
		return BadgeTransition{}, ErrBadgeUnknownMotif
	}
	actor := s.seatActor(r)
	cleared, err := s.store.claimBadgeForActorTransition(teamID, motif, actor)
	if err != nil {
		var claimed *badgeClaimedError
		if errors.As(err, &claimed) {
			return BadgeTransition{}, &badgeTakenError{teamName: s.teamByID(claimed.teamID).Name}
		}
		return BadgeTransition{}, err
	}
	return BadgeTransition{AvatarCleared: cleared}, nil
}

// ReleaseBadge clears teamID's badge claim, restoring the tone-default
// badge (or text-mark) fallback tier on the next render — see
// avatarView. Same auth gate as ClaimBadge; releasing an already-clean
// seat is a no-op, not an error, matching ResetAvatar's idempotent-reset
// precedent.
func (s *Service) ReleaseBadge(r *http.Request, teamID string) error {
	teamID = strings.TrimSpace(teamID)
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	if !s.canSetAvatar(r, teamID) {
		return ErrBadgeForbidden
	}
	actor := s.seatActor(r)
	_, err := s.store.releaseBadgeForActor(teamID, actor)
	return err
}

// badgeGrid renders the full 16-motif catalog for the team-badge picker
// (design decision: the grid always shows all 16 — what is taken is as
// useful to see as what is free). claimed_by_abbr stays empty for a free
// motif or the viewer's own claim; every non-empty value it can carry is
// already public league data (a team abbreviation), so this never leaks
// anything the viewer could not already see on the standings or roster
// pages.
func (s *Service) badgeGrid(state PersistedState, teamID string) []map[string]any {
	if !s.store.IdentityHealthy() {
		return []map[string]any{}
	}
	// Treat the supplied snapshot as untrusted persisted state too. The
	// normal load path strips these rows, but filtering here keeps a future
	// caller from rendering a retired slug or allowing duplicate canonical
	// art to appear occupied after a hand-edited/imported state.
	claims := make(map[string]string, len(state.BadgeClaims))
	claimedMotifs := make(map[string]struct{}, len(state.BadgeClaims))
	holders := make([]string, 0, len(state.BadgeClaims))
	for holder := range state.BadgeClaims {
		holders = append(holders, holder)
	}
	sort.Strings(holders)
	for _, holder := range holders {
		claimedMotif := state.BadgeClaims[holder]
		if !knownTeam(holder) || !knownMotif(claimedMotif) {
			continue
		}
		// A valid custom avatar is the active identity for this seat; do not
		// let a hand-edited/imported conflicting badge row make the picker
		// advertise canonical art that cannot actually be claimed by it.
		if ref := state.AvatarRefs[holder]; validAvatarRef(ref) {
			continue
		}
		if _, duplicate := claimedMotifs[claimedMotif]; duplicate {
			continue
		}
		claims[holder] = claimedMotif
		claimedMotifs[claimedMotif] = struct{}{}
	}
	out := make([]map[string]any, 0, len(BadgeMotifs))
	for _, motif := range BadgeMotifs {
		holder, taken := "", false
		for candidate, claimedMotif := range claims {
			if claimedMotif == motif.Slug {
				holder, taken = candidate, true
				break
			}
		}
		mine := taken && holder == teamID
		claimedByAbbr := ""
		if taken && !mine {
			claimedByAbbr = s.teamView(state, holder).Abbreviation
		}
		out = append(out, map[string]any{
			"slug": motif.Slug, "name": motif.Name,
			"claimed": taken, "claimed_by_abbr": claimedByAbbr, "mine": mine,
		})
	}
	return out
}
