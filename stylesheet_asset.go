package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"m31labs.dev/gosx/server"
)

// stylesheetAsset is the shared stylesheet as the server ships it, prepared
// once at boot from public/styles.css:
//
//   - every /* comment */ is removed. The source file is ~48% comments by
//     byte (they are the repo's own audit trail and stay in the source),
//     which shipped as ~96 KB of extra gzipped bytes on every cold load of
//     the one render-blocking asset;
//   - every self-hosted font URL (url(/fonts/<file>)) is content-addressed
//     with the same "?v=" hash convention hashedPublicAssetHref uses, so a
//     font is fetched once per deploy instead of revalidated per page.
//
// The href carries a hash of the SERVED bytes, so a comment-only edit no
// longer busts the cache and a real rule change always does. The middleware
// answers GET/HEAD /styles.css itself: App.servePublic would otherwise win
// the dispatch for a file that exists under public/ (see faviconICO's doc
// comment in app_build.go), so the processed body has to be served from a
// middleware that runs before dispatch, not from an app.Mount route.
type stylesheetAsset struct {
	body []byte
	hash string
	href string
	etag string
}

// stylesheetPublicName is the stylesheet's public path and its name under
// public/; the layout href, the middleware, and the fallback all share it.
const stylesheetPublicName = "styles.css"

// fontURLPattern matches an unversioned self-hosted font reference in the
// stylesheet source. The rewrite keeps the source file plain (a dev server
// or a contract test reading public/styles.css sees the same bytes git
// does) and versions only what the server serves.
var fontURLPattern = regexp.MustCompile(`url\(/fonts/([A-Za-z0-9._-]+)\)`)

// loadStylesheetAsset reads and processes public/styles.css under root. A
// missing or unreadable file returns an error; callers fall back to the
// unprocessed, unversioned href so a packaging error degrades to a
// revalidated stylesheet rather than a broken page (the same degrade
// hashedPublicAssetHref documents).
func loadStylesheetAsset(root string) (*stylesheetAsset, error) {
	src, err := os.ReadFile(filepath.Join(root, "public", stylesheetPublicName))
	if err != nil {
		return nil, err
	}
	body := stripStylesheetComments(src)
	body = fontURLPattern.ReplaceAllFunc(body, func(match []byte) []byte {
		name := string(fontURLPattern.FindSubmatch(match)[1])
		return []byte("url(" + hashedPublicAssetHref(root, "fonts/"+name) + ")")
	})
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:8])
	return &stylesheetAsset{
		body: body,
		hash: hash,
		href: server.AssetURL(stylesheetPublicName) + "?v=" + hash,
		etag: strconv.Quote(hash),
	}, nil
}

// stripStylesheetComments removes every /* ... */ comment outside a string
// literal and collapses the blank lines that leaves behind. It is a small
// state machine rather than a regexp so a "/*" inside a quoted value (a
// content: or url("...") string) is never mistaken for a comment opener.
func stripStylesheetComments(src []byte) []byte {
	out := make([]byte, 0, len(src))
	var quote byte
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case quote != 0:
			out = append(out, c)
			if c == '\\' && i+1 < len(src) {
				i++
				out = append(out, src[i])
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
			out = append(out, c)
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := bytes.Index(src[i+2:], []byte("*/"))
			if end < 0 {
				// An unterminated comment swallows the rest of the file in
				// a browser too; mirror that rather than emit half of it.
				return collapseBlankLines(out)
			}
			i += 2 + end + 1
		default:
			out = append(out, c)
		}
	}
	return collapseBlankLines(out)
}

// collapseBlankLines drops whitespace-only lines so a stripped comment
// block leaves no run of empty lines behind. Rule text is untouched.
func collapseBlankLines(src []byte) []byte {
	lines := bytes.Split(src, []byte("\n"))
	out := make([][]byte, 0, len(lines))
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		out = append(out, bytes.TrimRight(line, " \t"))
	}
	joined := bytes.Join(out, []byte("\n"))
	return append(joined, '\n')
}

// middleware serves the processed stylesheet for GET/HEAD /styles.css and
// passes every other request through. Cache policy mirrors App.servePublic:
// a request carrying the "?v=" hash is immutable for a year; an unversioned
// request revalidates (ETag) on every view.
func (a *stylesheetAsset) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a == nil || r.URL.Path != "/"+stylesheetPublicName || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/css; charset=utf-8")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("ETag", a.etag)
		if r.URL.Query().Get("v") != "" {
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			h.Set("Cache-Control", "public, max-age=0, must-revalidate")
		}
		for _, candidate := range strings.Split(r.Header.Get("If-None-Match"), ",") {
			if strings.TrimSpace(candidate) == a.etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write(a.body)
	})
}

// fontPreloadLinks returns <link rel="preload"> tags for the two fonts every
// first paint needs (the display face for the page title and the body face
// for everything else), using the same content-hashed hrefs the processed
// stylesheet references so the preload and the @font-face fetch are one
// request. A font file missing from public/fonts is simply not preloaded.
func fontPreloadLinks(root string) []server.LinkTag {
	var links []server.LinkTag
	for _, name := range []string{"archivo-black-400.woff2", "plus-jakarta-sans.woff2"} {
		if _, err := os.Stat(filepath.Join(root, "public", "fonts", name)); err != nil {
			continue
		}
		links = append(links, server.LinkTag{
			Rel:         "preload",
			Href:        hashedPublicAssetHref(root, "fonts/"+name),
			As:          "font",
			Type:        "font/woff2",
			CrossOrigin: "anonymous",
		})
	}
	return links
}
