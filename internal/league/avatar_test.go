package league

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// solidPNG encodes a width x height solid-color PNG.
func solidPNG(t *testing.T, width, height int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode fixture png: %v", err)
	}
	return buf.Bytes()
}

// solidJPEG encodes a width x height solid-color JPEG.
func solidJPEG(t *testing.T, width, height int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode fixture jpeg: %v", err)
	}
	return buf.Bytes()
}

// pngChunk builds one PNG chunk: 4-byte length, 4-byte type, data, 4-byte
// CRC32 over type+data — the same layout png.DecodeConfig itself parses.
func pngChunk(typ string, data []byte) []byte {
	var buf bytes.Buffer
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(data)))
	buf.Write(length[:])
	buf.WriteString(typ)
	buf.Write(data)
	sum := crc32.ChecksumIEEE(append([]byte(typ), data...))
	var crc [4]byte
	binary.BigEndian.PutUint32(crc[:], sum)
	buf.Write(crc[:])
	return buf.Bytes()
}

// fakePNGHeader builds a PNG signature + IHDR + IEND declaring width x
// height, with no IDAT at all. image.DecodeConfig only needs IHDR to
// report a Config, so this is enough to drive the dimension-guard code path
// without a real (and, for the decode-bomb case, enormous) pixel payload —
// proving the guard rejects on the cheap header read alone, since a full
// image.Decode of this data would fail outright (there is no image data to
// decode).
func fakePNGHeader(width, height uint32) []byte {
	signature := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 2 // color type: truecolor
	var buf bytes.Buffer
	buf.Write(signature)
	buf.Write(pngChunk("IHDR", ihdr))
	buf.Write(pngChunk("IEND", nil))
	return buf.Bytes()
}

func TestProcessAvatarImageRejectsWrongType(t *testing.T) {
	_, err := processAvatarImage([]byte("this is not an image, just text"))
	if err != ErrAvatarWrongType {
		t.Fatalf("err = %v, want %v", err, ErrAvatarWrongType)
	}
}

func TestProcessAvatarImageRejectsEmptyUpload(t *testing.T) {
	_, err := processAvatarImage(nil)
	if err != ErrAvatarWrongType {
		t.Fatalf("err = %v, want %v", err, ErrAvatarWrongType)
	}
}

func TestProcessAvatarImageRejectsTooLarge(t *testing.T) {
	oversized := make([]byte, AvatarMaxBytes+1)
	_, err := processAvatarImage(oversized)
	if err != ErrAvatarTooLarge {
		t.Fatalf("err = %v, want %v", err, ErrAvatarTooLarge)
	}
}

func TestProcessAvatarImageRejectsTooSmallDimensions(t *testing.T) {
	data := fakePNGHeader(63, 63)
	_, err := processAvatarImage(data)
	if err != ErrAvatarBadDimensions {
		t.Fatalf("err = %v, want %v", err, ErrAvatarBadDimensions)
	}
}

func TestProcessAvatarImageRejectsTooBigDimensions(t *testing.T) {
	data := fakePNGHeader(4097, 4097)
	_, err := processAvatarImage(data)
	if err != ErrAvatarBadDimensions {
		t.Fatalf("err = %v, want %v", err, ErrAvatarBadDimensions)
	}
}

// TestProcessAvatarImageDecodeBombGuard checks that a PNG declaring an
// enormous size (well past AvatarMaxDimension) is rejected by the
// DecodeConfig-only dimension check before any full pixel decode is
// attempted. The fixture carries no IDAT at all, so a full image.Decode of
// it would fail outright — asserting the *specific* dimensions error (not
// some other decode failure) proves the header-only guard is what fired.
func TestProcessAvatarImageDecodeBombGuard(t *testing.T) {
	data := fakePNGHeader(50000, 50000)
	_, err := processAvatarImage(data)
	if err != ErrAvatarBadDimensions {
		t.Fatalf("err = %v, want %v (decode-bomb guard did not fire on the header alone)", err, ErrAvatarBadDimensions)
	}
}

func TestProcessAvatarImageAcceptsMinimumDimension(t *testing.T) {
	data := solidPNG(t, AvatarMinDimension, AvatarMinDimension, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	out, err := processAvatarImage(data)
	if err != nil {
		t.Fatalf("minimum-dimension upload rejected: %v", err)
	}
	assertPNGSize(t, out, AvatarOutputSize, AvatarOutputSize)
}

func TestProcessAvatarImageAcceptsMaximumDimension(t *testing.T) {
	data := solidPNG(t, AvatarMaxDimension, AvatarMaxDimension, color.RGBA{R: 200, G: 50, B: 90, A: 255})
	out, err := processAvatarImage(data)
	if err != nil {
		t.Fatalf("maximum-dimension upload rejected: %v", err)
	}
	assertPNGSize(t, out, AvatarOutputSize, AvatarOutputSize)
}

func TestProcessAvatarImageAcceptsJPEG(t *testing.T) {
	data := solidJPEG(t, 200, 300, color.RGBA{R: 5, G: 250, B: 5, A: 255})
	out, err := processAvatarImage(data)
	if err != nil {
		t.Fatalf("jpeg upload rejected: %v", err)
	}
	assertPNGSize(t, out, AvatarOutputSize, AvatarOutputSize)
}

func TestProcessAvatarImageReencodesAsPNGRegardlessOfInputFormat(t *testing.T) {
	data := solidJPEG(t, 128, 128, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	out, err := processAvatarImage(data)
	if err != nil {
		t.Fatalf("jpeg upload rejected: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("output is not a PNG-signed file")
	}
}

func assertPNGSize(t *testing.T, data []byte, wantWidth, wantHeight int) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("output is not a valid PNG: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != wantWidth || bounds.Dy() != wantHeight {
		t.Fatalf("output size = %dx%d, want %dx%d", bounds.Dx(), bounds.Dy(), wantWidth, wantHeight)
	}
}

// TestCenterSquareAndScaleCropsToCenter builds a 300x100 source split into
// three vertical color bands and checks that the center-crop keeps only the
// middle band: with a 300-wide, 100-tall source the crop square is
// 100x100, centered horizontally at x=[100,200) — exactly the middle
// third when the bands are drawn at x=[0,100), [100,200), [200,300). Every
// pixel of the scaled output should therefore be the middle band's color.
func TestCenterSquareAndScaleCropsToCenter(t *testing.T) {
	width, height := 300, 100
	left := color.RGBA{R: 255, A: 255}
	middle := color.RGBA{G: 255, A: 255}
	right := color.RGBA{B: 255, A: 255}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			switch {
			case x < 100:
				img.Set(x, y, left)
			case x < 200:
				img.Set(x, y, middle)
			default:
				img.Set(x, y, right)
			}
		}
	}
	out := centerSquareAndScale(img, AvatarOutputSize)
	bounds := out.Bounds()
	if bounds.Dx() != AvatarOutputSize || bounds.Dy() != AvatarOutputSize {
		t.Fatalf("output size = %dx%d, want %dx%d", bounds.Dx(), bounds.Dy(), AvatarOutputSize, AvatarOutputSize)
	}
	for _, point := range []struct{ x, y int }{
		{0, 0}, {AvatarOutputSize - 1, 0}, {0, AvatarOutputSize - 1},
		{AvatarOutputSize - 1, AvatarOutputSize - 1}, {AvatarOutputSize / 2, AvatarOutputSize / 2},
	} {
		got := out.At(point.x, point.y)
		r, g, b, _ := got.RGBA()
		wantR, wantG, wantB, _ := middle.RGBA()
		if r != wantR || g != wantG || b != wantB {
			t.Fatalf("pixel (%d,%d) = %v, want the middle band's color %v", point.x, point.y, got, middle)
		}
	}
}

func TestWriteAvatarAtomicIsAtomicAndLeavesNoTempFiles(t *testing.T) {
	root := t.TempDir()
	if err := writeAvatarAtomic(root, "team-1", []byte("first")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := writeAvatarAtomic(root, "team-1", []byte("second, replacing the first")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "team-1.png"))
	if err != nil {
		t.Fatalf("read stored avatar: %v", err)
	}
	if string(got) != "second, replacing the first" {
		t.Fatalf("stored avatar = %q, want the second write's content", got)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "team-1.png" {
			t.Fatalf("leftover file in avatar root: %s", entry.Name())
		}
	}
}

func TestAvatarFilePathRejectsUnknownTeamTraversal(t *testing.T) {
	service := newTestService(t, true)
	for _, hostile := range []string{
		"../../etc/passwd", "..", "team-1/../../../etc/passwd", "team-9", "", "TEAM-1",
	} {
		if _, ok := service.AvatarFilePath(hostile); ok {
			t.Errorf("AvatarFilePath(%q) reported ok=true for a non-team-list value", hostile)
		}
	}
	// A known ID still resolves, and only ever joins under the configured
	// avatar root — never a caller-supplied path fragment.
	path, ok := service.AvatarFilePath("team-3")
	if !ok {
		t.Fatal("AvatarFilePath(\"team-3\") reported ok=false")
	}
	if filepath.Dir(path) != service.avatarDir() {
		t.Fatalf("resolved path %q escaped the avatar root %q", path, service.avatarDir())
	}
}

func TestUploadAvatarRejectsUnknownTeamBeforeTouchingDisk(t *testing.T) {
	service := newTestService(t, true) // demo mode grants commissioner
	request, _ := http.NewRequest(http.MethodPost, "/avatar/upload", nil)
	data := solidPNG(t, 100, 100, color.RGBA{R: 9, G: 9, B: 9, A: 255})

	if _, err := service.UploadAvatar(request, "../../etc/passwd", data); err == nil {
		t.Fatal("a non-team-list teamID must be rejected")
	}
	if _, err := os.Stat(service.avatarDir()); !os.IsNotExist(err) {
		t.Fatalf("avatar root should not have been created for a rejected upload: stat err = %v", err)
	}
}

func TestUploadAvatarRejectsNonManager(t *testing.T) {
	// Non-demo mode with no COMMISSIONER_EMAILS match and no signed-in
	// identity: actingTeam has nothing to resolve, so canSetAvatar must
	// deny. This mirrors TestCommissionerForceAutopick's "rejected for
	// non-commissioners" case — see admin_test.go's doc comment on why a
	// positive "this really is my own seat" check is only reachable in
	// demo mode in this test package.
	service := newTestService(t, false)
	request, _ := http.NewRequest(http.MethodPost, "/avatar/upload", nil)
	data := solidPNG(t, 100, 100, color.RGBA{R: 9, G: 9, B: 9, A: 255})

	_, err := service.UploadAvatar(request, "team-1", data)
	if err != ErrAvatarForbidden {
		t.Fatalf("err = %v, want %v", err, ErrAvatarForbidden)
	}
}

func TestUploadAvatarAcceptsCommissionerForAnyTeam(t *testing.T) {
	service := newTestService(t, true) // demo mode grants commissioner
	request, _ := http.NewRequest(http.MethodPost, "/avatar/upload", nil)
	data := solidPNG(t, 100, 100, color.RGBA{R: 9, G: 9, B: 200, A: 255})

	version, err := service.UploadAvatar(request, "team-4", data)
	if err != nil {
		t.Fatalf("commissioner upload rejected: %v", err)
	}
	if version == 0 {
		t.Fatal("version should be nonzero once an avatar is stored")
	}
	if _, err := os.Stat(filepath.Join(service.avatarDir(), "team-4.png")); err != nil {
		t.Fatalf("avatar file not written: %v", err)
	}
}

func TestUploadAvatarPropagatesValidationErrors(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/avatar/upload", nil)

	if _, err := service.UploadAvatar(request, "team-1", []byte("not an image")); err != ErrAvatarWrongType {
		t.Fatalf("err = %v, want %v", err, ErrAvatarWrongType)
	}
}

func TestAvatarViewFallbackChain(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/avatar/upload", nil)
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	// Tier c: nothing uploaded, no default badge for the tone.
	hasAvatar, hasImage, url := service.avatarView("team-2", "blue")
	if hasAvatar || hasImage || url != "" {
		t.Fatalf("fresh team should have no avatar image: hasAvatar=%v hasImage=%v url=%q", hasAvatar, hasImage, url)
	}

	// Tier b: a default badge exists for the tone, still no upload. Advance
	// the injected clock past the badge-scan cache TTL first (see
	// TestDefaultBadgeExistsCacheInvalidatesAfterTTL) so this new file is
	// actually picked up rather than reusing the tier-c scan's cached
	// (empty) result.
	if err := os.MkdirAll(service.defaultBadgeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service.defaultBadgeRoot, "blue.png"), []byte("badge"), 0o644); err != nil {
		t.Fatal(err)
	}
	now = now.Add(defaultBadgeCacheTTL + time.Second)
	hasAvatar, hasImage, url = service.avatarView("team-2", "blue")
	if hasAvatar {
		t.Fatal("hasAvatar must stay false for the default-badge tier")
	}
	if !hasImage || url != "/avatars/defaults/blue.png" {
		t.Fatalf("tier b resolution wrong: hasImage=%v url=%q", hasImage, url)
	}

	// Tier a: an uploaded avatar outranks the default badge.
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

func formatVersion(t *testing.T, service *Service, teamID string) string {
	t.Helper()
	version, ok := service.AvatarVersion(teamID)
	if !ok {
		t.Fatalf("expected an avatar version for %s", teamID)
	}
	return strconv.FormatInt(version, 10)
}

// TestDefaultBadgeExistsCacheInvalidatesAfterTTL checks that a badge file
// dropped in after the first scan is picked up once defaultBadgeCacheTTL
// has elapsed against the service's injected clock, without needing a real
// sleep.
func TestDefaultBadgeExistsCacheInvalidatesAfterTTL(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	if service.defaultBadgeExists("gold") {
		t.Fatal("no badge file exists yet")
	}
	if err := os.MkdirAll(service.defaultBadgeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service.defaultBadgeRoot, "gold.png"), []byte("badge"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Still within the TTL window: the cached (empty) scan wins.
	if service.defaultBadgeExists("gold") {
		t.Fatal("cache should not have re-scanned within the TTL window")
	}
	now = now.Add(defaultBadgeCacheTTL + time.Second)
	if !service.defaultBadgeExists("gold") {
		t.Fatal("cache should have re-scanned once the TTL elapsed")
	}
}

func TestAvatarDigestChangesOnUploadAndFeedsStateFingerprint(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/avatar/upload", nil)

	before := service.StateFingerprint(1)
	data := solidPNG(t, 100, 100, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	if _, err := service.UploadAvatar(request, "team-5", data); err != nil {
		t.Fatal(err)
	}
	after := service.StateFingerprint(1)
	if before == after {
		t.Fatal("StateFingerprint did not change after an avatar upload")
	}

	digestBefore := after
	if err := service.ResetAvatar(request, "team-5"); err != nil {
		t.Fatal(err)
	}
	digestAfter := service.StateFingerprint(1)
	if digestBefore == digestAfter {
		t.Fatal("StateFingerprint did not change after an avatar reset")
	}
}

func TestResetAvatarRequiresCommissioner(t *testing.T) {
	service := newTestService(t, false)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	if err := service.ResetAvatar(request, "team-1"); err == nil {
		t.Fatal("a non-commissioner reset must be rejected")
	}
}

func TestResetAvatarDeletesFileAndIsIdempotent(t *testing.T) {
	service := newTestService(t, true) // demo mode grants commissioner
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	data := solidPNG(t, 100, 100, color.RGBA{R: 4, G: 5, B: 6, A: 255})
	if _, err := service.UploadAvatar(request, "team-6", data); err != nil {
		t.Fatal(err)
	}
	if _, ok := service.AvatarVersion("team-6"); !ok {
		t.Fatal("avatar should exist before reset")
	}
	if err := service.ResetAvatar(request, "team-6"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, ok := service.AvatarVersion("team-6"); ok {
		t.Fatal("avatar should be gone after reset")
	}
	// Resetting an already-clean seat is a no-op, not an error.
	if err := service.ResetAvatar(request, "team-6"); err != nil {
		t.Fatalf("idempotent reset: %v", err)
	}
}

// TestMatchupMapsCarriesAvatarFields checks the one TeamMark render path
// that does not build its team maps through teamMap (matchupMaps builds its
// own away/home maps to carry the live score) — see matchupMaps's doc
// comment in service.go.
func TestMatchupMapsCarriesAvatarFields(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/avatar/upload", nil)
	data := solidPNG(t, 100, 100, color.RGBA{R: 8, G: 8, B: 8, A: 255})
	if _, err := service.UploadAvatar(request, "team-1", data); err != nil {
		t.Fatal(err)
	}

	matchups := []ScoreMatchup{{
		ID:   "m1",
		Away: ScoreTeam{ID: "team-1", Name: "Aqua 1", Abbreviation: "AQ1", Score: 10},
		Home: ScoreTeam{ID: "team-2", Name: "Aqua 2", Abbreviation: "AQ2", Score: 7},
	}}
	out := service.matchupMaps(service.store.Snapshot(), matchups)
	if len(out) != 1 {
		t.Fatalf("matchupMaps = %d entries, want 1", len(out))
	}
	away, _ := out[0]["away"].(map[string]any)
	home, _ := out[0]["home"].(map[string]any)
	if away["has_avatar"] != true || away["has_avatar_image"] != true || away["avatar_image_url"] == "" {
		t.Fatalf("away team (has an uploaded avatar) wrong: %+v", away)
	}
	if home["has_avatar"] != false || home["has_avatar_image"] != false || home["avatar_image_url"] != "" {
		t.Fatalf("home team (no avatar) wrong: %+v", home)
	}
}

func TestTeamMapCarriesAvatarFields(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/avatar/upload", nil)
	data := solidPNG(t, 100, 100, color.RGBA{R: 7, G: 7, B: 7, A: 255})
	if _, err := service.UploadAvatar(request, "team-1", data); err != nil {
		t.Fatal(err)
	}

	view := service.teamMap(service.teamView(service.store.Snapshot(), "team-1"))
	if view["has_avatar"] != true || view["has_avatar_image"] != true {
		t.Fatalf("teamMap avatar fields wrong: %+v", view)
	}
	url, _ := view["avatar_image_url"].(string)
	if url == "" {
		t.Fatal("teamMap avatar_image_url is empty")
	}

	untouched := service.teamMap(service.teamView(service.store.Snapshot(), "team-2"))
	if untouched["has_avatar"] != false || untouched["has_avatar_image"] != false || untouched["avatar_image_url"] != "" {
		t.Fatalf("untouched team should carry the false/empty defaults: %+v", untouched)
	}
}
