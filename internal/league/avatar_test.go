package league

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
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

// hashQueryValue reproduces hashedAssetQueryValue's exact recipe (first 8
// hex bytes of the content's SHA-256 sum) so a test can assert the
// production "?v=" query against a fixture's known bytes without hardcoding
// a hash literal that would silently stop testing anything if the recipe
// ever changed on only one side.
func hashQueryValue(t *testing.T, data []byte) string {
	t.Helper()
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
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

func TestProcessAvatarImageAcceptsExactMaxBytes(t *testing.T) {
	data := solidPNG(t, AvatarMinDimension, AvatarMinDimension, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	data = append(data, bytes.Repeat([]byte{0x5a}, AvatarMaxBytes-len(data))...)
	if len(data) != AvatarMaxBytes {
		t.Fatalf("fixture length = %d, want %d", len(data), AvatarMaxBytes)
	}
	if _, err := processAvatarImage(data); err != nil {
		t.Fatalf("exact-limit upload rejected: %v", err)
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

func TestProcessAvatarImageStripsMetadataAndPreservesAlpha(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 96, 64))
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 20, G: 40, B: 60, A: 128})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatalf("encode alpha fixture: %v", err)
	}
	metadata := pngChunk("tEXt", []byte("Comment\x00private metadata"))
	insertAt := len(encoded.Bytes()) - 12 // immediately before the IEND chunk
	data := append([]byte{}, encoded.Bytes()[:insertAt]...)
	data = append(data, metadata...)
	data = append(data, encoded.Bytes()[insertAt:]...)

	out, err := processAvatarImage(data)
	if err != nil {
		t.Fatalf("metadata-bearing alpha upload rejected: %v", err)
	}
	assertPNGSize(t, out, AvatarOutputSize, AvatarOutputSize)
	if bytes.Contains(out, []byte("tEXt")) || bytes.Contains(out, []byte("private metadata")) {
		t.Fatal("normalized PNG retained input metadata")
	}
	decoded, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode normalized alpha PNG: %v", err)
	}
	_, _, _, alpha := decoded.At(AvatarOutputSize/2, AvatarOutputSize/2).RGBA()
	if alpha != uint32(128)*257 {
		t.Fatalf("normalized alpha = %d, want %d", alpha, uint32(128)*257)
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

func TestCenterSquareAndScaleUsesHighQualityInterpolation(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			value := uint8(0)
			if x >= 32 {
				value = 255
			}
			img.SetRGBA(x, y, color.RGBA{R: value, G: value, B: value, A: 255})
		}
	}
	out := centerSquareAndScale(img, AvatarOutputSize)
	r, _, _, _ := out.At(AvatarOutputSize/2-1, AvatarOutputSize/2).RGBA()
	if r == 0 || r == 0xffff {
		t.Fatalf("edge pixel = %d, want an interpolated value between black and white", r)
	}
}

func TestAvatarDecodeGateBlocksAboveConfiguredConcurrency(t *testing.T) {
	gate := newAvatarDecodeGate(avatarDecodeConcurrency)
	releases := make([]func(), 0, avatarDecodeConcurrency)
	for range avatarDecodeConcurrency {
		releases = append(releases, gate.acquire())
	}
	acquired := make(chan struct{})
	go func() {
		release := gate.acquire()
		close(acquired)
		release()
	}()
	select {
	case <-acquired:
		t.Fatal("decode gate admitted more than its configured concurrency")
	case <-time.After(20 * time.Millisecond):
	}
	for _, release := range releases {
		release()
	}
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("decode gate did not release a waiting decoder")
	}
}

func TestWriteAvatarBlobIsContentAddressedAndIdempotent(t *testing.T) {
	anchor := t.TempDir()
	root := filepath.Join(anchor, "avatars")
	first := []byte("first")
	ref, err := writeAvatarBlob(anchor, root, first)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	if !validAvatarRef(ref) {
		t.Fatalf("invalid content reference %q", ref)
	}
	again, err := writeAvatarBlob(anchor, root, first)
	if err != nil {
		t.Fatalf("idempotent write: %v", err)
	}
	if again != ref {
		t.Fatalf("same bytes produced refs %q and %q", ref, again)
	}
	second, err := writeAvatarBlob(anchor, root, []byte("second"))
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if second == ref {
		t.Fatal("different bytes reused the first content reference")
	}
	entries, err := os.ReadDir(filepath.Join(root, "objects"))
	if err != nil {
		t.Fatalf("read object directory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("object directory has %d entries, want two immutable objects", len(entries))
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".png" {
			t.Fatalf("leftover temporary object: %s", entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("stat immutable object %s: %v", entry.Name(), err)
		}
		if got := info.Mode().Perm(); got != 0o444 {
			t.Fatalf("object %s mode = %o, want read-only 0444", entry.Name(), got)
		}
	}
}

func TestWriteAvatarBlobRejectsFinalLeafSymlinkWithoutTouchingOutside(t *testing.T) {
	anchor := t.TempDir()
	root := filepath.Join(anchor, "avatars")
	data := []byte("symlink-safe avatar bytes")
	ref, err := writeAvatarBlob(anchor, root, data)
	if err != nil {
		t.Fatalf("initial object write: %v", err)
	}
	objectPath := filepath.Join(root, "objects", ref+".png")
	outside := filepath.Join(t.TempDir(), "outside.png")
	outsideBytes := []byte("outside secret bytes")
	if err := os.WriteFile(outside, outsideBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	outsideBefore, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(objectPath, objectPath+".regular"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, objectPath); err != nil {
		t.Fatal(err)
	}
	if _, err := writeAvatarBlob(anchor, root, data); err == nil {
		t.Fatal("EEXIST reuse followed a final-leaf symlink")
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, outsideBytes) {
		t.Fatalf("outside bytes changed to %q, want %q", got, outsideBytes)
	}
	outsideAfter, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if outsideAfter.Mode().Perm() != outsideBefore.Mode().Perm() {
		t.Fatalf("outside mode changed from %o to %o", outsideBefore.Mode().Perm(), outsideAfter.Mode().Perm())
	}
	linkInfo, err := os.Lstat(objectPath)
	if err != nil {
		t.Fatal(err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatal("rejected final leaf was not left as a symlink")
	}
}

func TestWriteAvatarBlobRejectsWritableExistingHardlinkWithoutMutatingAlias(t *testing.T) {
	anchor := t.TempDir()
	root := filepath.Join(anchor, "avatars")
	data := []byte("writable hardlink bytes")
	digest := sha256.Sum256(data)
	ref := hex.EncodeToString(digest[:])
	objectDir := filepath.Join(root, "objects")
	if err := ensureAvatarDirectory(anchor, objectDir); err != nil {
		t.Fatalf("provision object directory: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, data, 0o644); err != nil {
		t.Fatal(err)
	}
	objectPath := filepath.Join(objectDir, ref+".png")
	if err := os.Link(outside, objectPath); err != nil {
		t.Fatalf("create multiply-linked object: %v", err)
	}
	outsideBefore, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeAvatarBlob(anchor, root, data); err == nil {
		t.Fatal("EEXIST reuse accepted a writable hard-linked object")
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("outside alias bytes changed to %q, want %q", got, data)
	}
	outsideAfter, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if outsideAfter.Mode().Perm() != outsideBefore.Mode().Perm() {
		t.Fatalf("outside alias mode changed from %o to %o", outsideBefore.Mode().Perm(), outsideAfter.Mode().Perm())
	}
	objectInfo, err := os.Stat(objectPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(outsideAfter, objectInfo) {
		t.Fatal("rejected writable object no longer aliases the outside inode")
	}
}

func TestReadAvatarObjectRejectsFinalLeafSymlink(t *testing.T) {
	service := newTestService(t, true)
	request, err := http.NewRequest(http.MethodPost, "/avatar/upload", nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.UploadAvatar(request, "team-1", solidPNG(t, 96, 96, color.RGBA{R: 23, G: 31, B: 47, A: 255}))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	path, ok := service.AvatarObjectPath("team-1", result.Ref)
	if !ok {
		t.Fatal("uploaded object has no authoritative path")
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	outsideBytes := []byte("serving must not follow this")
	if err := os.WriteFile(outside, outsideBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	outsideBefore, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".regular"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if data, _, ok := service.ReadAvatarObject("team-1", result.Ref); ok || data != nil {
		t.Fatalf("symlinked avatar object returned ok=%v data=%q", ok, data)
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, outsideBytes) {
		t.Fatalf("outside serving target changed to %q, want %q", got, outsideBytes)
	}
	outsideAfter, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if outsideAfter.Mode().Perm() != outsideBefore.Mode().Perm() {
		t.Fatalf("outside serving target mode changed from %o to %o", outsideBefore.Mode().Perm(), outsideAfter.Mode().Perm())
	}
}

func TestWriteAvatarBlobConcurrentSameDigestLeavesOneDurableObject(t *testing.T) {
	anchor := t.TempDir()
	root := filepath.Join(anchor, "avatars")
	data := bytes.Repeat([]byte("same normalized avatar bytes"), 4096)
	const workers = 24
	refs := make([]string, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			refs[index], errs[index] = writeAvatarBlob(anchor, root, data)
		}(worker)
	}
	close(start)
	wg.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("worker %d write: %v", index, err)
		}
		if !validAvatarRef(refs[index]) {
			t.Fatalf("worker %d returned invalid ref %q", index, refs[index])
		}
		if refs[index] != refs[0] {
			t.Fatalf("worker %d returned ref %q, want shared ref %q", index, refs[index], refs[0])
		}
	}

	objectPath := filepath.Join(root, "objects", refs[0]+".png")
	stored, err := os.ReadFile(objectPath)
	if err != nil {
		t.Fatalf("read shared object: %v", err)
	}
	if !bytes.Equal(stored, data) {
		t.Fatal("concurrent same-digest object bytes differ from the normalized upload")
	}
	info, err := os.Stat(objectPath)
	if err != nil {
		t.Fatalf("stat shared object: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o444 {
		t.Fatalf("shared object mode = %o, want read-only 0444", got)
	}
	entries, err := os.ReadDir(filepath.Join(root, "objects"))
	if err != nil {
		t.Fatalf("read object directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != refs[0]+".png" {
		t.Fatalf("concurrent object directory = %#v, want one immutable object", entries)
	}
}

func TestWriteAvatarBlobProvisionsDirectoriesWithFsyncBarriers(t *testing.T) {
	originalSync := avatarSyncDirectory
	t.Cleanup(func() { avatarSyncDirectory = originalSync })
	var synced []string
	avatarSyncDirectory = func(path string) error {
		synced = append(synced, path)
		return nil
	}
	anchor := t.TempDir()
	root := filepath.Join(anchor, "nested", "avatars")
	if _, err := writeAvatarBlob(anchor, root, []byte("directory durability")); err != nil {
		t.Fatalf("first-use write: %v", err)
	}
	objects := filepath.Join(root, "objects")
	wantCalls := []string{anchor, filepath.Join(anchor, "nested"), root, objects, objects}
	if !reflect.DeepEqual(synced, wantCalls) {
		t.Fatalf("first-use sync calls = %#v, want exact anchor-to-object barriers %#v", synced, wantCalls)
	}
	synced = nil
	if _, err := writeAvatarBlob(anchor, root, []byte("directory durability")); err != nil {
		t.Fatalf("matching EEXIST write: %v", err)
	}
	if !reflect.DeepEqual(synced, wantCalls) {
		t.Fatalf("EEXIST sync calls = %#v, want exact anchor-to-object barriers %#v", synced, wantCalls)
	}
}

func TestWriteAvatarBlobRetriesMissedAncestorBarrierAfterDirectoryCreate(t *testing.T) {
	anchor := t.TempDir()
	root := filepath.Join(anchor, "nested", "avatars")
	missedParent := anchor
	originalSync := avatarSyncDirectory
	t.Cleanup(func() { avatarSyncDirectory = originalSync })
	syncErr := errors.New("injected ancestor directory fsync failure")
	failed := false
	var calls []string
	avatarSyncDirectory = func(path string) error {
		calls = append(calls, path)
		if path == missedParent && !failed {
			failed = true
			return syncErr
		}
		return nil
	}
	data := []byte("ancestor retry bytes")
	if _, err := writeAvatarBlob(anchor, root, data); err != syncErr {
		t.Fatalf("first write error = %v, want %v", err, syncErr)
	}
	if _, err := os.Stat(filepath.Join(anchor, "nested")); err != nil {
		t.Fatalf("directory component created before fsync failure disappeared: %v", err)
	}
	failedCalls := len(calls)
	ref, err := writeAvatarBlob(anchor, root, data)
	if err != nil {
		t.Fatalf("retry after ancestor fsync failure: %v", err)
	}
	if !failed {
		t.Fatal("the injected ancestor barrier was never exercised")
	}
	if len(calls) <= failedCalls {
		t.Fatal("retry did not perform any directory durability work")
	}
	// The target already existed on retry, so this successful barrier is the
	// repair that makes the first attempt's directory entry durable before the
	// object can be installed and referenced.
	repaired := false
	for _, path := range calls[failedCalls:] {
		if path == missedParent {
			repaired = true
			break
		}
	}
	if !repaired {
		t.Fatalf("retry never re-synced missed parent %q; calls = %#v", missedParent, calls)
	}
	if _, err := os.Stat(filepath.Join(root, "objects", ref+".png")); err != nil {
		t.Fatalf("object was not installed after repaired ancestor barrier: %v", err)
	}
}

func TestWriteAvatarBlobRetriesMissedObjectDirectoryBarrierBeforeInstall(t *testing.T) {
	anchor := t.TempDir()
	root := filepath.Join(anchor, "avatars")
	objectDir := filepath.Join(root, "objects")
	originalSync := avatarSyncDirectory
	t.Cleanup(func() { avatarSyncDirectory = originalSync })
	syncErr := errors.New("injected object directory fsync failure")
	failed := false
	var calls []string
	avatarSyncDirectory = func(path string) error {
		calls = append(calls, path)
		if path == root && !failed {
			failed = true
			return syncErr
		}
		return nil
	}
	data := []byte("object directory retry bytes")
	if _, err := writeAvatarBlob(anchor, root, data); err != syncErr {
		t.Fatalf("first object-directory write error = %v, want %v", err, syncErr)
	}
	if _, err := os.Stat(objectDir); err != nil {
		t.Fatalf("object directory disappeared after fsync failure: %v", err)
	}
	failedCalls := len(calls)
	ref, err := writeAvatarBlob(anchor, root, data)
	if err != nil {
		t.Fatalf("retry after object-directory fsync failure: %v", err)
	}
	if !failed {
		t.Fatal("the injected object-directory barrier was never exercised")
	}
	repaired := false
	for _, path := range calls[failedCalls:] {
		if path == root {
			repaired = true
			break
		}
	}
	if !repaired {
		t.Fatalf("retry never re-synced existing object directory parent %q; calls = %#v", root, calls)
	}
	if _, err := os.Stat(filepath.Join(objectDir, ref+".png")); err != nil {
		t.Fatalf("object was not installed after repaired object-directory barrier: %v", err)
	}
}

type avatarSyncEvent struct {
	path          string
	objectPresent bool
}

func assertAvatarUploadStillUnpublished(t *testing.T, service *Service, objectPath string) {
	t.Helper()
	if _, ok := service.store.AvatarRef("team-1"); ok {
		t.Fatal("failed upload published an avatar ref")
	}
	if _, ok := service.store.BadgeClaim("team-1"); ok {
		t.Fatal("failed upload published a badge claim")
	}
	if _, err := os.Stat(objectPath); !os.IsNotExist(err) {
		t.Fatalf("failed upload object stat = %v, want object absent", err)
	}
}

func assertAvatarSyncEventsWithin(t *testing.T, events []avatarSyncEvent, allowed map[string]bool) {
	t.Helper()
	for _, event := range events {
		if !allowed[event.path] {
			t.Errorf("avatar durability synced outside explicit anchor chain: %q; allowed = %#v", event.path, allowed)
		}
	}
}

func TestUploadAvatarRepairsAncestorBarrierBeforeActivation(t *testing.T) {
	service := newTestService(t, true)
	anchor := t.TempDir()
	root := filepath.Join(anchor, "nested", "avatars")
	service.avatarDurableRoot = anchor
	service.avatarRoot = root
	request, err := http.NewRequest(http.MethodPost, "/avatar/upload", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := solidPNG(t, 96, 96, color.RGBA{R: 7, G: 9, B: 11, A: 255})
	normalized, err := processAvatarImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	ref := avatarRefForCrashBody(string(normalized))
	objectDir := filepath.Join(root, "objects")
	objectPath := filepath.Join(objectDir, ref+".png")
	syncErr := errors.New("ancestor barrier still unavailable")
	failures := 2
	var events []avatarSyncEvent
	originalSync := avatarSyncDirectory
	t.Cleanup(func() { avatarSyncDirectory = originalSync })
	avatarSyncDirectory = func(path string) error {
		_, statErr := os.Stat(objectPath)
		events = append(events, avatarSyncEvent{path: path, objectPresent: statErr == nil})
		if path == anchor && failures > 0 {
			failures--
			return syncErr
		}
		return nil
	}

	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := service.UploadAvatar(request, "team-1", raw); err != syncErr {
			t.Fatalf("ancestor attempt %d error = %v, want %v", attempt, err, syncErr)
		}
		assertAvatarUploadStillUnpublished(t, service, objectPath)
	}
	if _, err := os.Stat(filepath.Join(anchor, "nested")); err != nil {
		t.Fatalf("first failed attempt did not leave its created ancestor component: %v", err)
	}
	result, err := service.UploadAvatar(request, "team-1", raw)
	if err != nil {
		t.Fatalf("third ancestor attempt: %v", err)
	}
	if result.Ref != ref {
		t.Fatalf("third ancestor ref = %q, want %q", result.Ref, ref)
	}
	if got, ok := service.store.AvatarRef("team-1"); !ok || got != ref {
		t.Fatalf("activated ancestor ref = %q, %v; want %q, true", got, ok, ref)
	}
	if _, ok := service.store.BadgeClaim("team-1"); ok {
		t.Fatal("successful avatar activation left a badge claim")
	}
	stored, err := os.ReadFile(objectPath)
	if err != nil || !bytes.Equal(stored, normalized) {
		t.Fatalf("activated ancestor object read = %v, bytes match = %v", err, bytes.Equal(stored, normalized))
	}
	assertAvatarSyncEventsWithin(t, events, map[string]bool{
		anchor: true, filepath.Join(anchor, "nested"): true, root: true, objectDir: true,
	})
	barrierIndex := -1
	objectSyncIndex := -1
	for index, event := range events {
		if event.path == anchor && barrierIndex < 0 {
			barrierIndex = index
		}
		if event.path == objectDir && event.objectPresent && objectSyncIndex < 0 {
			objectSyncIndex = index
		}
	}
	if barrierIndex < 0 || objectSyncIndex < 0 || barrierIndex >= objectSyncIndex {
		t.Fatalf("effect order = %#v, want repaired anchor barrier before linked-object directory fsync", events)
	}
}

func TestUploadAvatarRepairsObjectDirectoryBarrierBeforeActivation(t *testing.T) {
	service := newTestService(t, true)
	anchor := t.TempDir()
	root := filepath.Join(anchor, "avatars")
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	service.avatarDurableRoot = anchor
	service.avatarRoot = root
	request, err := http.NewRequest(http.MethodPost, "/avatar/upload", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := solidPNG(t, 96, 96, color.RGBA{R: 13, G: 17, B: 19, A: 255})
	normalized, err := processAvatarImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	ref := avatarRefForCrashBody(string(normalized))
	objectDir := filepath.Join(root, "objects")
	objectPath := filepath.Join(objectDir, ref+".png")
	syncErr := errors.New("object directory barrier still unavailable")
	failures := 2
	var events []avatarSyncEvent
	originalSync := avatarSyncDirectory
	t.Cleanup(func() { avatarSyncDirectory = originalSync })
	avatarSyncDirectory = func(path string) error {
		_, statErr := os.Stat(objectPath)
		events = append(events, avatarSyncEvent{path: path, objectPresent: statErr == nil})
		if path == root && failures > 0 {
			failures--
			return syncErr
		}
		return nil
	}

	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := service.UploadAvatar(request, "team-1", raw); err != syncErr {
			t.Fatalf("object-directory attempt %d error = %v, want %v", attempt, err, syncErr)
		}
		assertAvatarUploadStillUnpublished(t, service, objectPath)
	}
	if _, err := os.Stat(objectDir); err != nil {
		t.Fatalf("first failed attempt did not leave its created objects directory: %v", err)
	}
	result, err := service.UploadAvatar(request, "team-1", raw)
	if err != nil {
		t.Fatalf("third object-directory attempt: %v", err)
	}
	if result.Ref != ref {
		t.Fatalf("third object-directory ref = %q, want %q", result.Ref, ref)
	}
	if got, ok := service.store.AvatarRef("team-1"); !ok || got != ref {
		t.Fatalf("activated object-directory ref = %q, %v; want %q, true", got, ok, ref)
	}
	if _, ok := service.store.BadgeClaim("team-1"); ok {
		t.Fatal("successful avatar activation left a badge claim")
	}
	stored, err := os.ReadFile(objectPath)
	if err != nil || !bytes.Equal(stored, normalized) {
		t.Fatalf("activated object-directory object read = %v, bytes match = %v", err, bytes.Equal(stored, normalized))
	}
	assertAvatarSyncEventsWithin(t, events, map[string]bool{anchor: true, root: true, objectDir: true})
	barrierIndex := -1
	objectSyncIndex := -1
	for index, event := range events {
		if event.path == root && barrierIndex < 0 {
			barrierIndex = index
		}
		if event.path == objectDir && event.objectPresent && objectSyncIndex < 0 {
			objectSyncIndex = index
		}
	}
	if barrierIndex < 0 || objectSyncIndex < 0 || barrierIndex >= objectSyncIndex {
		t.Fatalf("effect order = %#v, want repaired objects-parent barrier before linked-object directory fsync", events)
	}
}

func TestAvatarStorageRejectsOutsideAnchorAndSymlinkTargets(t *testing.T) {
	anchor := t.TempDir()
	outside := t.TempDir()
	data := []byte("rejected storage path")
	rootSymlink := filepath.Join(anchor, "root-link")
	if err := os.Symlink(outside, rootSymlink); err != nil {
		t.Fatal(err)
	}
	objectRoot := filepath.Join(anchor, "object-link-root")
	if err := os.Mkdir(objectRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(objectRoot, "objects")); err != nil {
		t.Fatal(err)
	}
	anchorSymlink := filepath.Join(t.TempDir(), "anchor-link")
	if err := os.Symlink(anchor, anchorSymlink); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		anchor string
		root   string
	}{
		{name: "outside", anchor: anchor, root: filepath.Join(outside, "avatars")},
		{name: "sibling", anchor: anchor, root: filepath.Join(filepath.Dir(anchor), "sibling-avatars")},
		{name: "root symlink", anchor: anchor, root: rootSymlink},
		{name: "objects symlink", anchor: anchor, root: objectRoot},
		{name: "anchor symlink", anchor: anchorSymlink, root: filepath.Join(anchorSymlink, "avatars")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := writeAvatarBlob(test.anchor, test.root, data); err == nil {
				t.Fatal("unsafe avatar storage path was accepted")
			}
			if entries, err := os.ReadDir(outside); err != nil {
				t.Fatal(err)
			} else if len(entries) != 0 {
				t.Fatalf("unsafe write touched outside anchor: %#v", entries)
			}
		})
	}
}

func TestWriteAvatarBlobDirectorySyncFailurePreventsActivation(t *testing.T) {
	store, path := newAvatarIdentityStore(t)
	originalSync := avatarSyncDirectory
	t.Cleanup(func() { avatarSyncDirectory = originalSync })
	syncErr := errors.New("injected avatar directory fsync failure")
	avatarSyncDirectory = func(string) error { return syncErr }
	anchor := t.TempDir()
	root := filepath.Join(anchor, "first-use", "avatars")
	if _, err := writeAvatarBlob(anchor, root, []byte("must not activate")); err != syncErr {
		t.Fatalf("directory fsync error = %v, want %v", err, syncErr)
	}
	if _, ok := store.AvatarRef("team-1"); ok {
		t.Fatal("failed object provisioning published an avatar ref")
	}
	stored := reloadStoredState(t, path)
	if _, ok := stored.AvatarRefs["team-1"]; ok {
		t.Fatal("failed object provisioning activated an avatar in the database")
	}
	objects := filepath.Join(root, "objects")
	entries, err := os.ReadDir(objects)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatalf("read provisioned objects directory: %v", err)
		}
		return
	}
	if len(entries) != 0 {
		t.Fatalf("failed directory sync left object files: %d", len(entries))
	}
}

func TestUploadAvatarDirectorySyncFailureDoesNotActivateIdentity(t *testing.T) {
	service := newTestService(t, true)
	service.avatarRoot = filepath.Join(service.avatarDurableDir(), "first-use", "avatars")
	originalSync := avatarSyncDirectory
	t.Cleanup(func() { avatarSyncDirectory = originalSync })
	syncErr := errors.New("injected upload directory fsync failure")
	avatarSyncDirectory = func(string) error { return syncErr }
	request, err := http.NewRequest(http.MethodPost, "/avatar/upload", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UploadAvatar(request, "team-1", solidPNG(t, 64, 64, color.RGBA{R: 9, G: 8, B: 7, A: 255})); err != syncErr {
		t.Fatalf("upload directory fsync error = %v, want %v", err, syncErr)
	}
	if _, ok := service.store.AvatarRef("team-1"); ok {
		t.Fatal("a directory durability failure activated a custom avatar")
	}
	if got := service.store.Snapshot().BadgeClaims["team-1"]; got != "" {
		t.Fatalf("directory durability failure changed badge identity to %q", got)
	}
}

func TestAvatarObjectPathRejectsUnknownTeamTraversal(t *testing.T) {
	service := newTestService(t, true)
	ref := strings.Repeat("a", 64)
	for _, hostile := range []string{
		"../../etc/passwd", "..", "team-1/../../../etc/passwd", "team-9", "", "TEAM-1",
	} {
		if _, ok := service.AvatarObjectPath(hostile, ref); ok {
			t.Errorf("AvatarObjectPath(%q, ref) reported ok=true for a non-team-list value", hostile)
		}
	}
	// A known ID without a DB reference has no serving path.
	if _, ok := service.AvatarObjectPath("team-3", ref); ok {
		t.Fatal("unreferenced team unexpectedly resolved an avatar path")
	}
	data := solidPNG(t, 100, 100, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	request, _ := http.NewRequest(http.MethodPost, "/avatar/upload", nil)
	result, err := service.UploadAvatar(request, "team-3", data)
	if err != nil {
		t.Fatal(err)
	}
	path, ok := service.AvatarObjectPath("team-3", result.Ref)
	if !ok || filepath.Dir(path) != filepath.Join(service.avatarDir(), "objects") || !strings.HasSuffix(path, result.Ref+".png") {
		t.Fatalf("resolved path %q does not use the current immutable object", path)
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

	result, err := service.UploadAvatar(request, "team-4", data)
	if err != nil {
		t.Fatalf("commissioner upload rejected: %v", err)
	}
	if !validAvatarRef(result.Ref) {
		t.Fatal("upload should return a valid immutable content reference")
	}
	path, ok := service.AvatarObjectPath("team-4", result.Ref)
	if !ok {
		t.Fatal("stored avatar has no authoritative path")
	}
	if _, err := os.Stat(path); err != nil {
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
	wantBlueHref := "/avatars/defaults/blue.png?v=" + hashQueryValue(t, []byte("badge"))
	if !hasImage || url != wantBlueHref {
		t.Fatalf("tier b resolution wrong: hasImage=%v url=%q, want %q (a content-hash ?v= query, matching hashedPublicAssetHref's convention)", hasImage, url, wantBlueHref)
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
	ref, ok := service.store.AvatarRef("team-2")
	if !ok {
		t.Fatal("uploaded avatar ref missing")
	}
	if url != "/avatars/custom/team-2/"+ref+".png" {
		t.Fatalf("tier a url = %q, want a versioned content-addressed URL", url)
	}
}

// TestAvatarViewLargeFallbackChain checks avatarViewLarge's own fallback
// chain: tier c prefers a large default file but falls back to the
// BadgeOutputSize default when no large file exists yet; tier b serves the
// distinct "-lg" badge URL at BadgeImageLarge's own version hash; tier a
// (an uploaded custom avatar) is identical to avatarView's own URL, since
// that pipeline is untouched by this sizing pass.
func TestAvatarViewLargeFallbackChain(t *testing.T) {
	service := newTestService(t, true)
	service.motifRoot = t.TempDir()
	writeMotifFixture(t, service.motifRoot, "wolf")
	request, _ := http.NewRequest(http.MethodPost, "/avatar/badge", nil)
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	// Tier c, no defaults at all: no image.
	hasAvatar, hasImage, url := service.avatarViewLarge("team-2", "blue")
	if hasAvatar || hasImage || url != "" {
		t.Fatalf("fresh team should have no large avatar image: hasAvatar=%v hasImage=%v url=%q", hasAvatar, hasImage, url)
	}

	// Tier c, only the BadgeOutputSize default exists: avatarViewLarge
	// falls back to it rather than dropping to the text mark. Advance the
	// injected clock past defaultBadgeExists's scan-cache TTL first (see
	// TestDefaultBadgeExistsCacheInvalidatesAfterTTL) so the new file is
	// actually picked up rather than reusing the tier-c scan's cached
	// (empty) result.
	if err := os.MkdirAll(service.defaultBadgeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service.defaultBadgeRoot, "blue.png"), []byte("badge"), 0o644); err != nil {
		t.Fatal(err)
	}
	now = now.Add(defaultBadgeCacheTTL + time.Second)
	hasAvatar, hasImage, url = service.avatarViewLarge("team-2", "blue")
	if hasAvatar {
		t.Fatal("hasAvatar must stay false for the default-badge tier")
	}
	wantBlueHref := "/avatars/defaults/blue.png?v=" + hashQueryValue(t, []byte("badge"))
	if !hasImage || url != wantBlueHref {
		t.Fatalf("tier c fallback resolution wrong: hasImage=%v url=%q, want %q", hasImage, url, wantBlueHref)
	}

	// Tier c, a large default now exists: it outranks the fallback.
	largeDir := filepath.Join(service.defaultBadgeRoot, "large")
	if err := os.MkdirAll(largeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(largeDir, "blue.png"), []byte("large badge"), 0o644); err != nil {
		t.Fatal(err)
	}
	hasAvatar, hasImage, url = service.avatarViewLarge("team-2", "blue")
	if hasAvatar {
		t.Fatal("hasAvatar must stay false for the default-badge tier")
	}
	wantLargeHref := "/avatars/defaults/large/blue.png?v=" + hashQueryValue(t, []byte("large badge"))
	if !hasImage || url != wantLargeHref {
		t.Fatalf("tier c large resolution wrong: hasImage=%v url=%q, want %q", hasImage, url, wantLargeHref)
	}

	// Tier b: a badge claim outranks the tone default, at the distinct
	// "-lg" URL and BadgeImageLarge's own version hash.
	if err := service.ClaimBadge(request, "team-2", "wolf"); err != nil {
		t.Fatal(err)
	}
	hasAvatar, hasImage, url = service.avatarViewLarge("team-2", "blue")
	if hasAvatar {
		t.Fatal("hasAvatar must stay false for the badge-claim tier")
	}
	_, smallVersion, ok := service.BadgeImage("team-2")
	if !ok {
		t.Fatal("BadgeImage should render the claimed motif")
	}
	_, largeVersion, ok := service.BadgeImageLarge("team-2")
	if !ok {
		t.Fatal("BadgeImageLarge should render the claimed motif")
	}
	if smallVersion == largeVersion {
		t.Fatal("BadgeImage and BadgeImageLarge must render distinct bytes")
	}
	if !hasImage || url != "/avatars/badge/team-2-lg.png?v="+largeVersion {
		t.Fatalf("tier b large resolution wrong: hasImage=%v url=%q", hasImage, url)
	}

	// Tier a: an uploaded avatar still outranks the badge claim, at the
	// same URL avatarView itself would return — that pipeline is out of
	// scope for this sizing pass.
	data := solidPNG(t, 100, 100, color.RGBA{R: 9, G: 9, B: 9, A: 255})
	if _, err := service.UploadAvatar(request, "team-2", data); err != nil {
		t.Fatal(err)
	}
	hasAvatar, hasImage, url = service.avatarViewLarge("team-2", "blue")
	if !hasAvatar || !hasImage {
		t.Fatalf("tier a resolution wrong: hasAvatar=%v hasImage=%v", hasAvatar, hasImage)
	}
	_, _, smallURL := service.avatarView("team-2", "blue")
	if url != smallURL {
		t.Fatalf("tier a large url = %q, want the same custom-avatar url as avatarView: %q", url, smallURL)
	}
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

func TestResetAvatarClearsReferenceAndIsIdempotent(t *testing.T) {
	service := newTestService(t, true) // demo mode grants commissioner
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	data := solidPNG(t, 100, 100, color.RGBA{R: 4, G: 5, B: 6, A: 255})
	result, err := service.UploadAvatar(request, "team-6", data)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := service.AvatarObjectPath("team-6", result.Ref); !ok {
		t.Fatal("avatar should exist before reset")
	}
	ref, ok := service.store.AvatarRef("team-6")
	if !ok {
		t.Fatal("avatar ref should exist before reset")
	}
	if err := service.ResetAvatar(request, "team-6"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, ok := service.AvatarObjectPath("team-6", ref); ok {
		t.Fatal("cleared avatar should not be served")
	}
	if _, ok := service.store.AvatarRef("team-6"); ok {
		t.Fatal("reset should clear the authoritative avatar ref")
	}
	if _, err := os.Stat(filepath.Join(service.avatarDir(), "objects", ref+".png")); err != nil {
		t.Fatalf("immutable object should remain recoverable after reset: %v", err)
	}
	// Resetting an already-clean seat is a no-op, not an error.
	if err := service.ResetAvatar(request, "team-6"); err != nil {
		t.Fatalf("idempotent reset: %v", err)
	}
}

// TestResetAvatarRecordsCommissionerEventOnlyOnRealChange checks the wave-2
// commissioner-console audit trail: the first reset (a real mutation)
// leaves one avatar.reset row, and the second, idempotent reset of an
// already-clean seat leaves no second row — mirroring
// TestResetAvatarClearsReferenceAndIsIdempotent's own no-op-is-not-an-error
// contract, extended to the audit trail.
func TestResetAvatarRecordsCommissionerEventOnlyOnRealChange(t *testing.T) {
	service := newTestService(t, true) // demo mode grants commissioner
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	data := solidPNG(t, 100, 100, color.RGBA{R: 7, G: 8, B: 9, A: 255})
	if _, err := service.UploadAvatar(request, "team-6", data); err != nil {
		t.Fatal(err)
	}
	if err := service.ResetAvatar(request, "team-6"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "avatar.reset" || events[0].Refs.TeamID != "team-6" {
		t.Fatalf("commissioner events = %+v, want one avatar.reset row for team-6", events)
	}
	if err := service.ResetAvatar(request, "team-6"); err != nil {
		t.Fatalf("idempotent reset: %v", err)
	}
	if got := service.store.Snapshot().CommissionerEvents; len(got) != 1 {
		t.Fatalf("commissioner events after a no-op reset = %+v, want still exactly one row", got)
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
