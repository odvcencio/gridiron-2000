package league

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// writeMotifFixture writes a small synthetic motif PNG — near-white line
// art on transparency, the same shape the real public/avatars/motifs
// files have — to dir/slug.png, so tint tests never depend on the real
// 512x512 shipped artwork.
func writeMotifFixture(t *testing.T, dir, slug string) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 250, G: 250, B: 250, A: 200}) // partial alpha, near-white
	img.SetNRGBA(1, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 0})   // fully transparent
	img.SetNRGBA(0, 1, color.NRGBA{R: 128, G: 128, B: 128, A: 100}) // half-alpha, gray
	img.SetNRGBA(1, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 255}) // opaque white
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, slug+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestKnownMotifAcceptsCatalogAndRejectsUnknown(t *testing.T) {
	if !knownMotif("wolf") {
		t.Fatal("wolf is in BadgeMotifs and must be known")
	}
	if len(BadgeMotifs) != 16 {
		t.Fatalf("BadgeMotifs has %d entries, want 16", len(BadgeMotifs))
	}
	for _, hostile := range []string{"", "WOLF", "wolf ", "../wolf", "unicorn"} {
		if knownMotif(hostile) {
			t.Errorf("knownMotif(%q) = true, want false", hostile)
		}
	}
}

func TestBadgeToneHexCoversEveryPaletteTone(t *testing.T) {
	for _, tone := range validTones {
		hex, ok := BadgeToneHex(tone)
		if !ok || hex == "" {
			t.Errorf("BadgeToneHex(%q) = (%q, %v), want a hex value and ok=true", tone, hex, ok)
		}
	}
	if _, ok := BadgeToneHex("not-a-tone"); ok {
		t.Fatal("BadgeToneHex(\"not-a-tone\") reported ok=true")
	}
}

func TestClaimBadgeExclusivityNamesTheClaimant(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/avatar/badge", nil)

	if err := service.ClaimBadge(request, "team-1", "helmet"); err != nil {
		t.Fatalf("team-1 claim: %v", err)
	}
	err := service.ClaimBadge(request, "team-2", "helmet")
	if err == nil {
		t.Fatal("a second team claiming an already-claimed motif must be rejected")
	}
	team1Name := service.teamByID("team-1").Name
	want := "that badge is already claimed by " + team1Name
	if err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, ErrBadgeTaken) {
		t.Fatal("errors.Is(err, ErrBadgeTaken) = false, want true")
	}
}

func TestClaimBadgeSwapFreesThePreviousMotif(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/avatar/badge", nil)

	if err := service.ClaimBadge(request, "team-1", "helmet"); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := service.ClaimBadge(request, "team-1", "wolf"); err != nil {
		t.Fatalf("swap claim: %v", err)
	}
	motif, ok := service.store.BadgeClaim("team-1")
	if !ok || motif != "wolf" {
		t.Fatalf("team-1 claim = (%q, %v), want (\"wolf\", true)", motif, ok)
	}
	// The freed motif ("helmet") must now be claimable by a different team.
	if err := service.ClaimBadge(request, "team-2", "helmet"); err != nil {
		t.Fatalf("helmet should have been freed by the swap: %v", err)
	}
}

func TestReleaseBadgeClearsTheClaimAndIsIdempotent(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/avatar/badge", nil)

	if err := service.ClaimBadge(request, "team-1", "helmet"); err != nil {
		t.Fatal(err)
	}
	if err := service.ReleaseBadge(request, "team-1"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, ok := service.store.BadgeClaim("team-1"); ok {
		t.Fatal("team-1 should have no claim after release")
	}
	// Releasing an already-clean seat is a no-op, not an error.
	if err := service.ReleaseBadge(request, "team-1"); err != nil {
		t.Fatalf("idempotent release: %v", err)
	}
}

func TestClaimBadgeRejectsUnknownMotif(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/avatar/badge", nil)
	err := service.ClaimBadge(request, "team-1", "unicorn")
	if err != ErrBadgeUnknownMotif {
		t.Fatalf("err = %v, want %v", err, ErrBadgeUnknownMotif)
	}
}

func TestClaimBadgeRejectsUnknownTeamBeforeAnyMutation(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/avatar/badge", nil)
	if err := service.ClaimBadge(request, "../../etc/passwd", "helmet"); err == nil {
		t.Fatal("a non-team-list teamID must be rejected")
	}
	if err := service.ClaimBadge(request, "team-9", "helmet"); err == nil {
		t.Fatal("an out-of-range teamID must be rejected")
	}
}

func TestClaimBadgeRejectsNonManagerNonCommissioner(t *testing.T) {
	// Non-demo mode with no COMMISSIONER_EMAILS match and no signed-in
	// identity: actingTeam has nothing to resolve, so canSetAvatar must
	// deny — mirrors TestUploadAvatarRejectsNonManager's reasoning.
	service := newTestService(t, false)
	request, _ := http.NewRequest(http.MethodPost, "/avatar/badge", nil)
	if err := service.ClaimBadge(request, "team-1", "helmet"); err != ErrBadgeForbidden {
		t.Fatalf("err = %v, want %v", err, ErrBadgeForbidden)
	}
}

func TestReleaseBadgeRejectsNonManagerNonCommissioner(t *testing.T) {
	service := newTestService(t, false)
	request, _ := http.NewRequest(http.MethodPost, "/avatar/badge", nil)
	if err := service.ReleaseBadge(request, "team-1"); err != ErrBadgeForbidden {
		t.Fatalf("err = %v, want %v", err, ErrBadgeForbidden)
	}
}

func TestClaimBadgeAllowsCommissionerForAnyTeam(t *testing.T) {
	service := newTestService(t, true) // demo mode grants commissioner
	request, _ := http.NewRequest(http.MethodPost, "/avatar/badge", nil)
	if err := service.ClaimBadge(request, "team-4", "star"); err != nil {
		t.Fatalf("commissioner claim rejected: %v", err)
	}
	motif, ok := service.store.BadgeClaim("team-4")
	if !ok || motif != "star" {
		t.Fatalf("team-4 claim = (%q, %v), want (\"star\", true)", motif, ok)
	}
}

// TestAvatarViewBadgePrecedence checks the extended fallback chain: a
// claimed badge outranks the tone default, and an uploaded photo still
// outranks a claimed badge — see avatarView's doc comment.
func TestAvatarViewBadgePrecedence(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/avatar/badge", nil)

	// Tier c: a tone default badge exists, no claim, no upload.
	if err := os.MkdirAll(service.defaultBadgeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service.defaultBadgeRoot, "blue.png"), []byte("badge"), 0o644); err != nil {
		t.Fatal(err)
	}
	hasAvatar, hasImage, url := service.avatarView("team-2", "blue")
	if hasAvatar || !hasImage || url != "/avatars/defaults/blue.png" {
		t.Fatalf("tier c resolution wrong: hasAvatar=%v hasImage=%v url=%q", hasAvatar, hasImage, url)
	}

	// Tier b: a badge claim outranks the tone default.
	if err := service.ClaimBadge(request, "team-2", "wolf"); err != nil {
		t.Fatal(err)
	}
	hasAvatar, hasImage, url = service.avatarView("team-2", "blue")
	if hasAvatar {
		t.Fatal("hasAvatar must stay false for the badge-claim tier")
	}
	if !hasImage || url != "/avatars/badge/team-2.png?v=wolf" {
		t.Fatalf("tier b resolution wrong: hasImage=%v url=%q", hasImage, url)
	}

	// Tier a: an uploaded avatar still outranks the badge claim.
	data := solidPNG(t, 100, 100, color.RGBA{R: 9, G: 9, B: 9, A: 255})
	if _, err := service.UploadAvatar(request, "team-2", data); err != nil {
		t.Fatal(err)
	}
	hasAvatar, hasImage, url = service.avatarView("team-2", "blue")
	if !hasAvatar || !hasImage {
		t.Fatalf("tier a resolution wrong: hasAvatar=%v hasImage=%v", hasAvatar, hasImage)
	}
	if url != "/avatars/team-2.png?v="+formatVersion(t, service, "team-2") {
		t.Fatalf("tier a url = %q, want a versioned /avatars/team-2.png URL", url)
	}
}

func TestBadgeImageUnknownTeamAndNoClaimReportNotOK(t *testing.T) {
	service := newTestService(t, true)
	if _, _, ok := service.BadgeImage("team-9"); ok {
		t.Fatal("BadgeImage for an unknown team must report ok=false")
	}
	if _, _, ok := service.BadgeImage("team-3"); ok {
		t.Fatal("BadgeImage for a team with no claim must report ok=false")
	}
}

// TestBadgeImageRendersTintedPNGWithAlphaPreserved checks the tint
// pipeline end to end: BadgeImage decodes the source motif fixture,
// tints it in the team's tone, and PNG-encodes the result, with alpha
// copied unchanged and RGB scaled by source luminance.
func TestBadgeImageRendersTintedPNGWithAlphaPreserved(t *testing.T) {
	service := newTestService(t, true)
	service.motifRoot = t.TempDir()
	writeMotifFixture(t, service.motifRoot, "helmet")
	request, _ := http.NewRequest(http.MethodPost, "/avatar/badge", nil)

	if err := service.ClaimBadge(request, "team-1", "helmet"); err != nil {
		t.Fatal(err)
	}
	// team-1's tone in the neutral default config is "cyan" (the palette
	// cycle's first entry — see teamsFromSeeds).
	toneHex, ok := BadgeToneHex(service.teamByID("team-1").Tone)
	if !ok {
		t.Fatalf("team-1's tone %q has no BadgeToneHex entry", service.teamByID("team-1").Tone)
	}
	toneR, toneG, toneB, err := parseHexColor(toneHex)
	if err != nil {
		t.Fatal(err)
	}

	data, version, ok := service.BadgeImage("team-1")
	if !ok {
		t.Fatal("BadgeImage reported ok=false for a claimed team")
	}
	if version != "helmet" {
		t.Fatalf("version = %q, want \"helmet\"", version)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("BadgeImage output is not a valid PNG: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 2 || bounds.Dy() != 2 {
		t.Fatalf("output size = %dx%d, want 2x2 (matching the fixture)", bounds.Dx(), bounds.Dy())
	}

	// The fully-opaque near-white source pixel (1,1) must render at
	// exactly full alpha and the tone's own color (luminance 1.0).
	r, g, b, a := nrgbaAt(img, 1, 1)
	if a != 255 {
		t.Fatalf("opaque source pixel lost its alpha: got %d, want 255", a)
	}
	if r != toneR || g != toneG || b != toneB {
		t.Fatalf("opaque source pixel = (%d,%d,%d), want the tone color (%d,%d,%d)", r, g, b, toneR, toneG, toneB)
	}
	if r == 0 && g == 0 && b == 0 {
		t.Fatal("tint output color is all zero — the tint never applied")
	}

	// Every source pixel's alpha must survive unchanged.
	wantAlpha := map[[2]int]uint8{{0, 0}: 200, {1, 0}: 0, {0, 1}: 100, {1, 1}: 255}
	for point, want := range wantAlpha {
		_, _, _, gotAlpha := nrgbaAt(img, point[0], point[1])
		if gotAlpha != want {
			t.Errorf("pixel %v alpha = %d, want %d (alpha must be copied unchanged)", point, gotAlpha, want)
		}
	}
}

// TestTintedBadgePNGIsCachedPerMotifAndTone checks that repeated renders
// of the same motif+tone pair reuse the cached bytes (same underlying
// array), while a different tone produces a distinct render.
func TestTintedBadgePNGIsCachedPerMotifAndTone(t *testing.T) {
	service := newTestService(t, true)
	service.motifRoot = t.TempDir()
	writeMotifFixture(t, service.motifRoot, "helmet")

	first, err := service.tintedBadgePNG("helmet", "cyan")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.tintedBadgePNG("helmet", "cyan")
	if err != nil {
		t.Fatal(err)
	}
	if &first[0] != &second[0] {
		t.Fatal("tintedBadgePNG did not return the cached slice for a repeated motif+tone")
	}
	third, err := service.tintedBadgePNG("helmet", "blue")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, third) {
		t.Fatal("a different tone should render different bytes")
	}
}

// TestStateFingerprintChangesOnBadgeClaimAndRelease checks that a badge
// claim (and its release) feed StateFingerprint the same way an avatar
// upload does — see avatarDigest's doc comment.
func TestStateFingerprintChangesOnBadgeClaimAndRelease(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/avatar/badge", nil)

	before := service.StateFingerprint(1)
	if err := service.ClaimBadge(request, "team-5", "fireball"); err != nil {
		t.Fatal(err)
	}
	afterClaim := service.StateFingerprint(1)
	if before == afterClaim {
		t.Fatal("StateFingerprint did not change after a badge claim")
	}

	if err := service.ReleaseBadge(request, "team-5"); err != nil {
		t.Fatal(err)
	}
	afterRelease := service.StateFingerprint(1)
	if afterClaim == afterRelease {
		t.Fatal("StateFingerprint did not change after a badge release")
	}
}

// TestBadgeGridReflectsClaims checks the picker-grid view model: a free
// motif is free, another team's claim is visible only as an abbreviation,
// and this team's own claim is flagged "mine" with no abbreviation.
func TestBadgeGridReflectsClaims(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/avatar/badge", nil)
	if err := service.ClaimBadge(request, "team-2", "wolf"); err != nil {
		t.Fatal(err)
	}

	grid := service.badgeGrid(service.store.Snapshot(), "team-1")
	byslug := map[string]map[string]any{}
	for _, cell := range grid {
		byslug[cell["slug"].(string)] = cell
	}
	if len(grid) != len(BadgeMotifs) {
		t.Fatalf("badgeGrid has %d entries, want %d", len(grid), len(BadgeMotifs))
	}
	free := byslug["helmet"]
	if free["claimed"] != false || free["mine"] != false || free["claimed_by_abbr"] != "" {
		t.Fatalf("free motif entry wrong: %+v", free)
	}
	taken := byslug["wolf"]
	if taken["claimed"] != true || taken["mine"] != false {
		t.Fatalf("taken-by-other motif entry wrong: %+v", taken)
	}
	wantAbbr := service.teamByID("team-2").Abbreviation
	if taken["claimed_by_abbr"] != wantAbbr {
		t.Fatalf("taken motif claimed_by_abbr = %v, want %q", taken["claimed_by_abbr"], wantAbbr)
	}

	mineGrid := service.badgeGrid(service.store.Snapshot(), "team-2")
	mine := map[string]map[string]any{}
	for _, cell := range mineGrid {
		mine[cell["slug"].(string)] = cell
	}
	if mine["wolf"]["claimed"] != true || mine["wolf"]["mine"] != true || mine["wolf"]["claimed_by_abbr"] != "" {
		t.Fatalf("own-claim motif entry wrong: %+v", mine["wolf"])
	}
}

// TestLegacyMotifSlugResolvesForward pins the rename migration: a claim
// persisted by a binary that predates the catalog rename still renders,
// while the retired slug is refused as new input. Read paths map forward;
// write paths do not, so a retired slug can never be re-persisted.
func TestLegacyMotifSlugResolvesForward(t *testing.T) {
	for old, want := range map[string]string{
		"marlin": "rocket",
		"knight": "robot",
		"planet": "astronaut",
		"kraken": "fireball",
	} {
		if got := resolveMotifSlug(old); got != want {
			t.Errorf("resolveMotifSlug(%q) = %q, want %q", old, got, want)
		}
		if knownMotif(old) {
			t.Errorf("knownMotif(%q) = true; a retired slug must never be accepted as new input", old)
		}
		if !knownMotif(want) {
			t.Errorf("knownMotif(%q) = false; the replacement must be in the catalog", want)
		}
	}
	if got := resolveMotifSlug("helmet"); got != "helmet" {
		t.Errorf("resolveMotifSlug(helmet) = %q; a current slug must pass through", got)
	}
}

// TestEveryCatalogMotifHasArtwork pins the catalog against the shipped
// files. A slug with no PNG renders an empty badge in the picker and a
// broken image on a claimed team, and neither failure is visible from Go
// tests that only exercise the catalog slice.
func TestEveryCatalogMotifHasArtwork(t *testing.T) {
	for _, motif := range BadgeMotifs {
		path := filepath.Join("..", "..", "public", "avatars", "motifs", motif.Slug+".png")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("motif %q (%s) has no artwork at %s", motif.Slug, motif.Name, path)
		}
	}
}
