# Default team badges

This directory (`public/avatars/defaults`, or `AVATAR_DEFAULTS_ROOT` when
set) holds the default avatar badges. A default badge shows for a team that
has no uploaded avatar. The league ships this directory empty. Add badge
files here to enable the default-badge fallback tier.

## Naming convention

Name each file after the team's tone, in lower case, with a `.png`
extension:

- `cyan.png`
- `blue.png`
- `violet.png`
- `lime.png`
- `orange.png`
- `gold.png`
- `magenta.png`
- `pink.png`

The league checks this directory for a file that matches the team's tone.
A team with no uploaded avatar and no matching badge file falls back to
the plain text mark (the team's abbreviation in a colored box).

## Image requirements

- Format: PNG, with a transparent background.
- Size: 128x128 pixels. This is the site's own render size for every
  "many marks per page" surface (matchups, draft board, standings, home,
  admin) — twice the widest such CSS slot at a 2x device pixel ratio.

## The large variant

One surface — the team identity page's own hero mark
(`.team-monogram`) — displays up to 192 CSS px wide and needs a bigger
file to stay crisp at a 2x device pixel ratio. Add a matching file to
`large/{tone}.png` (same naming convention, 384x384 pixels) to enable it.
A tone with only the 128x128 file still renders correctly everywhere;
the team identity page falls back to that file until a 384x384 one
exists.

## How the fallback chain works

1. An uploaded avatar, if the team has one.
2. A default badge, if a file matches the team's tone.
3. The text mark, always available, as the final fallback.

The league server scans this directory and caches the result for up to
30 seconds, so a new badge file appears on the site within that window.
