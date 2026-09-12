# Pi Desk Design System

Pi Desk is a compact desktop workbench for agent-driven development. The interface should feel quiet, technical, and exact: fast to scan, comfortable for long conversations, and dense enough for repository and terminal work without looking cramped.

## Locked direction

- Genre: modern-minimal
- Tone: technical / austere
- Macrostructure: Workbench
- Accent: restrained cobalt, used for focus, links, and selection only
- Surfaces: warm near-white in light mode; graphite in dark mode
- Depth: borders and surface lightness first, one whisper shadow for menus and dialogs
- Icons: Lucide only
- Dependencies: no additional UI, font, or animation dependency

## Application structure

The desktop shell has four stable regions:

1. A 44 px application top bar for workspace identity and global actions.
2. A 264 px task sidebar, collapsible to a 48 px rail.
3. A flexible conversation canvas with a readable content measure.
4. A docked 360 px inspector for files, context, and terminal tools.

The inspector is part of the workbench grid, not a floating card. Dialogs are the only large elevated surfaces. Nested cards should be flattened into grouped rows wherever the parent already provides containment.

## Typography

- UI and headings: `Segoe UI Variable`, with `Microsoft YaHei` and `PingFang SC` fallbacks.
- Reading text: the same family at a calmer line height.
- Code and numeric metadata: `Cascadia Code` with standard monospace fallbacks.
- Metadata: 11 px
- Tree and compact labels: 12 px
- Controls: 13 px
- Conversation body: 14 px
- Panel title: 15 px
- Dialog title: 16 px

Use weight and colour before introducing another size. Numbers in diffs, token counts, and timestamps use tabular figures. Clickable labels remain on one line.

## Spacing and geometry

- Spacing scale: 2, 4, 6, 8, 12, 16, 24 px.
- Icon button: 28 px visual size; expands to 44 px on coarse pointers.
- Compact tab/button: 30 px.
- Standard input/button: 34 px.
- Radii: 4 px for tiny controls, 6 px for controls, 8 px for panels, 12 px for dialogs.
- Pills are reserved for status badges and counters.

## Colour roles

Light mode uses a slightly warm application frame, a quiet grey sidebar, and a near-white work canvas. Dark mode uses three graphite elevations. Blue is never a large fill; green and red are reserved for semantic additions, deletions, and status.

Every colour used by production CSS must come through a named token in `frontend/src/styles/tokens.css`. The existing Codex-style code-preview syntax palette remains intentionally fixed so file previews stay familiar and readable.

## Motion

- Press feedback: 90 ms.
- Colour and hover feedback: 140 ms.
- Menus and tooltips: 180 ms.
- Panels and dialogs: 220 ms.
- Easing: exponential ease-out for entry, ease-in for exit, symmetric easing for state toggles.

Motion communicates state only. Use opacity and transform; do not animate layout. Focus rings appear instantly. Reduced-motion mode removes spatial movement and caps non-functional transitions at 150 ms.

## Interaction rules

- Every interactive control has visible focus, pressed, disabled, and loading treatment where applicable.
- Hover styling is paired with keyboard focus and never carries essential information alone.
- Inputs keep a constant 1 px border in every state and use an outline for focus.
- Reversible actions prefer immediate feedback and undo over confirmation.
- Repository trees and conversation content favour density; settings and destructive actions preserve breathing room.
- At 760 px and below, the task sidebar collapses to its rail. At 520 px and below, the inspector becomes an edge-to-edge sheet beside that rail.

## Exports

### Source CSS tokens

The canonical implementation lives in `frontend/src/styles/tokens.css` and exposes the semantic roles used throughout the application:

```css
:root {
  --color-paper: var(--bg-workspace);
  --color-paper-2: var(--bg-raised);
  --color-paper-3: var(--bg-hover);
  --color-rule: var(--border);
  --color-rule-2: var(--border-strong);
  --color-muted: var(--text-muted);
  --color-neutral: var(--text-secondary);
  --color-ink: var(--text);
  --color-accent: var(--blue);
  --color-accent-ink: var(--text-inverse);
  --color-focus: var(--focus);
  --font-display: var(--font-interface-display);
  --font-body: var(--font-interface-body);
  --font-outlier: var(--font-mono);
}
```

### Tailwind v4

```css
@theme {
  --font-display: var(--font-interface-display);
  --font-body: var(--font-interface-body);
  --font-mono: var(--font-code-stack);
  --text-xs: var(--font-size-meta);
  --text-sm: var(--font-size-control);
  --text-base: var(--font-size-body);
  --text-lg: var(--font-size-title);
  --text-xl: var(--font-size-dialog-title);
  --ease-out: cubic-bezier(0.16, 1, 0.3, 1);
  --ease-in: cubic-bezier(0.7, 0, 0.84, 0);
  --ease-in-out: cubic-bezier(0.65, 0, 0.35, 1);
}
```

### DTCG tokens

```json
{
  "$schema": "https://design-tokens.github.io/community-group/format/",
  "color": {
    "paper": { "$value": "oklch(99% 0.003 85)", "$type": "color" },
    "ink": { "$value": "oklch(20% 0.006 255)", "$type": "color" },
    "accent": { "$value": "oklch(52% 0.17 255)", "$type": "color" },
    "rule": { "$value": "oklch(90% 0.006 255)", "$type": "color" }
  },
  "font": {
    "body": { "$value": "Segoe UI Variable, Microsoft YaHei, sans-serif", "$type": "fontFamily" },
    "outlier": { "$value": "Cascadia Code, monospace", "$type": "fontFamily" }
  },
  "size": {
    "meta": { "$value": "11px", "$type": "dimension" },
    "control": { "$value": "13px", "$type": "dimension" },
    "body": { "$value": "14px", "$type": "dimension" },
    "title": { "$value": "15px", "$type": "dimension" }
  },
  "duration": {
    "press": { "$value": "90ms", "$type": "duration" },
    "short": { "$value": "140ms", "$type": "duration" },
    "panel": { "$value": "220ms", "$type": "duration" }
  }
}
```

### shadcn/ui mapping

```css
:root {
  --background: 99% 0.003 85;
  --foreground: 20% 0.006 255;
  --card: 99% 0.003 85;
  --card-foreground: 20% 0.006 255;
  --popover: 99% 0.003 85;
  --popover-foreground: 20% 0.006 255;
  --primary: 20% 0.006 255;
  --primary-foreground: 98% 0.003 85;
  --secondary: 95% 0.005 255;
  --secondary-foreground: 30% 0.006 255;
  --muted: 95% 0.005 255;
  --muted-foreground: 50% 0.007 255;
  --accent: 52% 0.17 255;
  --accent-foreground: 98% 0.003 85;
  --destructive: 52% 0.17 25;
  --destructive-foreground: 98% 0.003 85;
  --border: 90% 0.006 255;
  --input: 84% 0.007 255;
  --ring: 52% 0.17 255;
  --radius: 0.375rem;
}
```
