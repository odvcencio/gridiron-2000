# Self-hosted fonts

Latin subsets of the three families the design system uses, served from
this origin so first paint does not wait on fonts.googleapis.com and
fonts.gstatic.com (two extra DNS/TLS hops on a phone), and so a league on a
private network renders its own type with no third-party request.

| File | Family | Weight | License |
| --- | --- | --- | --- |
| `archivo-black-400.woff2` | Archivo Black | 400 | SIL Open Font License 1.1 |
| `ibm-plex-mono-600.woff2` | IBM Plex Mono | 600 | SIL Open Font License 1.1 |
| `plus-jakarta-sans.woff2` | Plus Jakarta Sans (variable, wght 200–800) | 400–700 used | SIL Open Font License 1.1 |

Source: the Google Fonts CSS API (`css2?family=...`) latin subset, fetched
2026-09-03. The `@font-face` rules live at the top of `public/styles.css`.
The server content-addresses these URLs (`?v=<hash>`) when it prepares the
stylesheet (see `stylesheet_asset.go`), so a replaced file is picked up on
the next boot.

The SIL Open Font License permits bundling and redistribution with software;
the fonts themselves are not sold. Full license text:
https://openfontlicense.org/open-font-license-official-text/
