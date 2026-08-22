# Gridiron product and design system

This document preserves the design rationale and binding visual-system rules behind Gridiron. Early launch-specific examples are historical context, not runtime league truth. Current league identity, format, team count, dates, membership posture, roster shape, and scoring come from `league.json` and are explained by `/guide` and `/scoring`.

Gridiron is a private fantasy-football league room for 4–14 teams. It combines a fast provisional signal surface, explicit league administration, schedule-backed matchups, and a dedicated draft room with Google sign-in as the trust boundary.

## Product Commitments

- League identity and manager count are configuration-derived; fixtures, standings, roster capacity, and public guide copy must never depend on one deployment's nickname or shape.
- The configured draft timestamp is a meeting window. Readiness uses explicit **Mark ready** and **Mark not ready** actions, and only the commissioner intentionally starts pick one.
- Public RSS/Atom sources poll every two minutes, curated Bluesky events arrive continuously, and the browser checks the private wire every 20 seconds. Open schedules sync every five minutes, injury reports every 15 minutes, and corrected player ledgers every six hours.
- The initial experience is useful in demo mode without credentials; production Google OAuth, the no-key signal wire, and the open-data cache are configured through environment variables.
- Publisher, social, community, and market signals are provenance-weighted but always provisional; they can never mutate fantasy scores. Corrected structured records and commissioner rulings remain authoritative. Closing a scheduled week records final matchup totals and pins its effective lineups so later roster churn cannot rewrite history.
- Server-rendered pages remain the baseline. Browser runtime is reserved for live refresh, filters, countdowns, and draft interactions.
- Regular-season schedules and closed matchup results are durable league state. Week-close readiness is advisory, the normal close waits for finished games and settled statistics, and any forced close is an explicit commissioner decision.
- Cross-league Commissioner HQ is a PII-free operational readout. It links to each isolated league for action rather than merging sessions, databases, memberships, or mutations.

## Visual System

### Territory

**Neo-Retro — “Stadium OS, year 2000.”** An ’80s football broadcast control room reissued through glossy Y2K industrial design. Strong scorebug geometry, chrome rules, CRT scan texture, compressed information, and high-energy live indicators are encouraged. Generic glass-card grids, soft pastel gradients, and quiet minimalism are excluded.

### Typography

- Display: **Archivo Black**, weight 400. Used for the wordmark, scores, and high-impact headings.
- Body: **Plus Jakarta Sans**, weights 400, 600, and 700. Used for navigation, labels, and readable UI copy.
- Mono: **IBM Plex Mono**, weight 600. Used for clocks, records, picks, timestamps, and compact stats.
- Scale: **1.25 (Major Third)**, tuned for a dense sports dashboard.

Type tokens:

- `--type-xs: clamp(0.68rem, 0.66rem + 0.10vw, 0.75rem)`
- `--type-sm: clamp(0.78rem, 0.75rem + 0.14vw, 0.875rem)`
- `--type-base: clamp(0.94rem, 0.90rem + 0.20vw, 1rem)`
- `--type-md: clamp(1.08rem, 1rem + 0.38vw, 1.25rem)`
- `--type-lg: clamp(1.30rem, 1.15rem + 0.72vw, 1.563rem)`
- `--type-xl: clamp(1.62rem, 1.35rem + 1.25vw, 1.953rem)`
- `--type-2xl: clamp(2rem, 1.55rem + 2vw, 2.441rem)`
- `--type-3xl: clamp(2.55rem, 1.82rem + 3.25vw, 3.052rem)`
- `--type-display: clamp(3.25rem, 2rem + 5.75vw, 5.96rem)`

### Color Architecture

The 60–30–10 balance uses midnight navy for the dominant field, layered broadcast-blue surfaces for secondary structure, and acid-lime for decisive action. Magenta and cyan are supporting signal colors, never competing CTAs.

- Dominant canvas: `#070A16`
- Secondary surface: `#111832`; raised surface: `#1A2447`
- Accent: `#D9FF43`
- Signal hot: `#FF4FD8`; signal cyan: `#38E8FF`
- Primary text: `#F7F4EA`, contrast **17.93:1** against canvas (WCAG AAA)
- Secondary text: `#C2CAE1`, contrast **12.06:1** against canvas (WCAG AAA)
- Muted text: `#8995B8`, contrast **6.63:1** against canvas (WCAG AA)
- Accent text/background pairing: canvas ink on acid-lime, contrast **17.23:1** (WCAG AAA)

### Motion

**Cinematic**, concentrated in page entry, score changes, draft-clock urgency, and tactile controls. Continuous motion is limited to small live-state indicators and stops under reduced-motion preferences.

- Fast: 150ms
- Standard: 240ms
- Reveal: 420ms
- Cinematic: 720ms
- Ease-out: `cubic-bezier(0.16, 1, 0.3, 1)`
- Ease-spring: `cubic-bezier(0.34, 1.56, 0.64, 1)`
- Ease-in-out: `cubic-bezier(0.76, 0, 0.24, 1)`

### Spacing

The base unit is 8px, with responsive expansion only at larger composition steps.

- `--space-xs: clamp(0.5rem, 0.46rem + 0.18vw, 0.75rem)`
- `--space-sm: clamp(0.75rem, 0.70rem + 0.22vw, 1rem)`
- `--space-md: clamp(1rem, 0.90rem + 0.46vw, 1.5rem)`
- `--space-lg: clamp(1.5rem, 1.28rem + 0.95vw, 2rem)`
- `--space-xl: clamp(2rem, 1.55rem + 1.8vw, 3rem)`
- `--space-2xl: clamp(3rem, 2.18rem + 3.2vw, 4rem)`
- `--space-3xl: clamp(4rem, 2.55rem + 5.7vw, 6rem)`

### Binding CSS Properties

```css
:root {
  --font-display: "Archivo Black", "Arial Black", sans-serif;
  --font-body: "Plus Jakarta Sans", sans-serif;
  --font-mono: "IBM Plex Mono", monospace;

  --type-xs: clamp(0.68rem, 0.66rem + 0.10vw, 0.75rem);
  --type-sm: clamp(0.78rem, 0.75rem + 0.14vw, 0.875rem);
  --type-base: clamp(0.94rem, 0.90rem + 0.20vw, 1rem);
  --type-md: clamp(1.08rem, 1rem + 0.38vw, 1.25rem);
  --type-lg: clamp(1.30rem, 1.15rem + 0.72vw, 1.563rem);
  --type-xl: clamp(1.62rem, 1.35rem + 1.25vw, 1.953rem);
  --type-2xl: clamp(2rem, 1.55rem + 2vw, 2.441rem);
  --type-3xl: clamp(2.55rem, 1.82rem + 3.25vw, 3.052rem);
  --type-display: clamp(3.25rem, 2rem + 5.75vw, 5.96rem);

  --color-canvas: #070A16;
  --color-surface: #111832;
  --color-surface-raised: #1A2447;
  --color-surface-soft: #222D51;
  --color-text-primary: #F7F4EA;
  --color-text-secondary: #C2CAE1;
  --color-text-muted: #8995B8;
  --color-accent: #D9FF43;
  --color-accent-hot: #FF4FD8;
  --color-accent-cyan: #38E8FF;
  --color-danger: #FF665C;
  --color-ink-on-accent: #070A16;
  --color-border: rgba(194, 202, 225, 0.22);
  --color-border-strong: rgba(194, 202, 225, 0.45);
  --color-scanline: rgba(7, 10, 22, 0.20);
  --color-shadow: rgba(0, 0, 0, 0.42);

  --duration-fast: 150ms;
  --duration-standard: 240ms;
  --duration-reveal: 420ms;
  --duration-cinematic: 720ms;
  --ease-out: cubic-bezier(0.16, 1, 0.3, 1);
  --ease-spring: cubic-bezier(0.34, 1.56, 0.64, 1);
  --ease-in-out: cubic-bezier(0.76, 0, 0.24, 1);

  --space-xs: clamp(0.5rem, 0.46rem + 0.18vw, 0.75rem);
  --space-sm: clamp(0.75rem, 0.70rem + 0.22vw, 1rem);
  --space-md: clamp(1rem, 0.90rem + 0.46vw, 1.5rem);
  --space-lg: clamp(1.5rem, 1.28rem + 0.95vw, 2rem);
  --space-xl: clamp(2rem, 1.55rem + 1.8vw, 3rem);
  --space-2xl: clamp(3rem, 2.18rem + 3.2vw, 4rem);
  --space-3xl: clamp(4rem, 2.55rem + 5.7vw, 6rem);
}
```

## Accessibility Baseline

- All actions are native links, buttons, or forms with visible keyboard focus.
- Score status is never communicated by color alone.
- Live updates use a polite status region and preserve focus.
- Motion and texture effects are disabled or simplified for `prefers-reduced-motion`.
- Narrow layouts preserve the matchup reading order rather than shrinking the desktop grid.
