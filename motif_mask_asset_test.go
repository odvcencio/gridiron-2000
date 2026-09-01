package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gridiron-2000/internal/league"
)

// motifMaskMaxBytes bounds each CSS-mask export's file size. The CSS-only
// consumer (.badge-option__art, styles.css) renders these at 38.39px and
// needs nothing past a 2x-device-pixel-ratio 96px source; a single-alpha
// PNG at that size compresses well under this ceiling (gap-audit item 1,
// wave 3 — see this directory's own doc comment on why the export is a
// sibling "mask" directory rather than an in-place resize of the shared
// motif source art).
const motifMaskMaxBytes = 8 * 1024

// motifMaskMaxPixels bounds each export's decoded width and height.
const motifMaskMaxPixels = 128

// TestMotifMaskArtIsSmallAndLowRes covers gap-audit item 1 (wave 3, "feel
// and speed"): /team downloaded 2.63MB of 512px motif PNGs used only as a
// CSS mask-image for 38px badge-picker swatches. Every catalog motif must
// have a small, low-resolution export under public/avatars/motifs/mask/
// for that CSS use.
func TestMotifMaskArtIsSmallAndLowRes(t *testing.T) {
	for _, motif := range league.BadgeMotifs {
		motif := motif
		t.Run(motif.Slug, func(t *testing.T) {
			path := filepath.Join("public", "avatars", "motifs", "mask", motif.Slug+".png")
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat %s: %v", path, err)
			}
			if info.Size() > motifMaskMaxBytes {
				t.Errorf("%s is %d bytes, want <= %d (%.1fKB)", path, info.Size(), motifMaskMaxBytes, float64(motifMaskMaxBytes)/1024)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("open %s: %v", path, err)
			}
			defer f.Close()
			cfg, err := png.DecodeConfig(f)
			if err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
			if cfg.Width > motifMaskMaxPixels || cfg.Height > motifMaskMaxPixels {
				t.Errorf("%s decodes to %dx%d, want both dimensions <= %d", path, cfg.Width, cfg.Height, motifMaskMaxPixels)
			}
		})
	}
}

// TestMotifSourceArtStaysHighResolutionForServerTinting guards the split
// this fix depends on: internal/league/badge.go's tintedBadgePNG decodes
// public/avatars/motifs/{slug}.png at native resolution and scales down to
// league.BadgeOutputSizeLarge (384px, the team identity page's
// .team-monogram hero) for every rendered team badge across the app
// (matchups, draft board, standings, home). Shrinking the shared source
// file in place — rather than adding the small mask/ sibling this package
// serves for the CSS-only picker swatch — would silently blur every one of
// those server-rendered badges. This test fails first if that file is ever
// downsized again.
func TestMotifSourceArtStaysHighResolutionForServerTinting(t *testing.T) {
	for _, motif := range league.BadgeMotifs {
		motif := motif
		t.Run(motif.Slug, func(t *testing.T) {
			path := filepath.Join("public", "avatars", "motifs", motif.Slug+".png")
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("open %s: %v", path, err)
			}
			defer f.Close()
			cfg, format, err := image.DecodeConfig(f)
			if err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
			if format != "png" {
				t.Errorf("%s decoded as %q, want png", path, format)
			}
			if cfg.Width < league.BadgeOutputSizeLarge || cfg.Height < league.BadgeOutputSizeLarge {
				t.Errorf("%s is %dx%d, want both dimensions >= %d (league.BadgeOutputSizeLarge)", path, cfg.Width, cfg.Height, league.BadgeOutputSizeLarge)
			}
		})
	}
}
