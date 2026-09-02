# Design — Pi Desk

A locked design system for the Pi Desk desktop application. All frontend components share this system; business logic and component ownership remain unchanged.

## Genre

Modern-minimal, technical, austere.

## Macrostructure family

- App pages: Workbench — compact application rail, bounded reading axis, contextual inspector.
- Dialogs: focused sheet — one containment layer, clear header/body/action hierarchy.
- Content and preview surfaces: long document — typography and rules carry hierarchy.

## Theme

- `--color-paper`: `oklch(100% 0 0)`
- `--color-paper-2`: `oklch(98.5% 0 0)`
- `--color-paper-3`: `oklch(96% 0 0)`
- `--color-ink`: `oklch(14.5% 0 0)`
- `--color-ink-2`: `oklch(32% 0 0)`
- `--color-rule`: `oklch(91% 0 0)`
- `--color-accent`: `oklch(14.5% 0 0)`
- `--color-accent-ink`: `oklch(100% 0 0)`
- `--color-focus`: `oklch(14.5% 0 0)`

Dark mode keeps the same neutral anchor: near-black paper, lighter raised surfaces, near-white ink, and white-on-dark active controls.

## Typography

- Display: Bahnschrift, weight 700, roman.
- Body: Segoe UI Variable Text, weight 400.
- Mono: Cascadia Code, weight 400; reserved for code, paths, terminal output, and tabular values.
- Display tracking: `-0.025em`.
- Body size: 16 px for reading surfaces; compact desktop controls may use 13–14 px with accessible names and coarse-pointer hit-target expansion.

## Spacing

Four-point scale. Components use Tailwind spacing utilities; runtime-calculated pane positions and virtual-list transforms are the only inline-style exception.

## Motion

- State transitions: background/color/opacity and at most a 1 px press translation.
- Easings: `--ease-out`, `--ease-in`, `--ease-in-out`.
- Focus rings appear instantly.
- Reduced motion disables spatial effects and keeps functional loading indicators.

## Microinteractions stance

- Silent success when the resulting state is already visible.
- Hover and focus have equivalent affordances.
- Destructive irreversible actions retain confirmation.
- Loading, error, disabled, empty, selected, and active states remain explicit.

## CTA voice

- Primary: deep-ink fill, inverse text, 8 px radius, compact verb-first copy.
- Secondary: raised paper, fine border, neutral ink.
- Destructive: red border/text; filled red only on deliberate hover or confirmation.

## Per-page allowances

- App pages use no decorative enrichment; function carries the surface.
- Markdown, code, diff, terminal, and third-party editor internals may retain targeted CSS where runtime-generated markup cannot carry Tailwind classes.

## What every component shares

- One neutral palette and one icon library (Lucide).
- Fine borders, subtle shadows, 8–12 px radii.
- Single-line interactive labels.
- Visible focus, active, disabled, loading, error, success, and selected states where applicable.
- Responsive behavior at 320, 375, 414, 768, and desktop widths.

## Exports

### tokens.css

```css
:root {
  --color-paper: oklch(100% 0 0);
  --color-paper-2: oklch(98.5% 0 0);
  --color-paper-3: oklch(96% 0 0);
  --color-rule: oklch(91% 0 0);
  --color-rule-2: oklch(82% 0 0);
  --color-muted: oklch(55% 0 0);
  --color-neutral: oklch(40% 0 0);
  --color-ink-2: oklch(32% 0 0);
  --color-ink: oklch(14.5% 0 0);
  --color-accent: oklch(14.5% 0 0);
  --color-accent-ink: oklch(100% 0 0);
  --color-focus: oklch(14.5% 0 0);
  --font-display: "Bahnschrift", "Arial Narrow", sans-serif;
  --font-body: "Segoe UI Variable Text", "Segoe UI", sans-serif;
  --font-outlier: "Cascadia Code", monospace;
  --space-3xs: .25rem; --space-2xs: .5rem; --space-xs: .75rem;
  --space-sm: 1rem; --space-md: 1.5rem; --space-lg: 2rem;
  --space-xl: 3rem; --space-2xl: 4.5rem; --space-3xl: 7rem;
  --ease-out: cubic-bezier(.16, 1, .3, 1);
  --ease-in: cubic-bezier(.7, 0, .84, 0);
  --ease-in-out: cubic-bezier(.65, 0, .35, 1);
  --dur-micro: 120ms; --dur-short: 220ms; --dur-long: 420ms;
  --radius-card: 12px; --radius-pill: 999px; --radius-input: 8px;
}
```

### Tailwind v4 `@theme`

```css
@theme {
  --color-paper: oklch(100% 0 0); --color-paper-2: oklch(98.5% 0 0);
  --color-ink: oklch(14.5% 0 0); --color-accent: oklch(14.5% 0 0);
  --font-display: "Bahnschrift", "Arial Narrow", sans-serif;
  --font-body: "Segoe UI Variable Text", "Segoe UI", sans-serif;
  --font-outlier: "Cascadia Code", monospace;
  --spacing-xs: .75rem; --spacing-sm: 1rem; --spacing-md: 1.5rem; --spacing-lg: 2rem;
  --ease-out: cubic-bezier(.16, 1, .3, 1);
}
```

### DTCG `tokens.json`

```json
{
  "$schema": "https://design-tokens.github.io/community-group/format/",
  "color": {
    "paper": { "$value": "oklch(100% 0 0)", "$type": "color" },
    "ink": { "$value": "oklch(14.5% 0 0)", "$type": "color" },
    "accent": { "$value": "oklch(14.5% 0 0)", "$type": "color" }
  },
  "font": {
    "display": { "$value": "Bahnschrift, Arial Narrow, sans-serif", "$type": "fontFamily" },
    "body": { "$value": "Segoe UI Variable Text, Segoe UI, sans-serif", "$type": "fontFamily" },
    "outlier": { "$value": "Cascadia Code, monospace", "$type": "fontFamily" }
  },
  "space": { "md": { "$value": "1.5rem", "$type": "dimension" } },
  "duration": { "short": { "$value": "220ms", "$type": "duration" } }
}
```

### shadcn/ui CSS variables

```css
:root {
  --background: 100% 0 0; --foreground: 14.5% 0 0;
  --card: 98.5% 0 0; --card-foreground: 14.5% 0 0;
  --popover: 100% 0 0; --popover-foreground: 14.5% 0 0;
  --primary: 14.5% 0 0; --primary-foreground: 100% 0 0;
  --secondary: 96% 0 0; --secondary-foreground: 32% 0 0;
  --muted: 91% 0 0; --muted-foreground: 55% 0 0;
  --border: 91% 0 0; --input: 82% 0 0; --ring: 14.5% 0 0;
  --radius: 8px;
}
```
