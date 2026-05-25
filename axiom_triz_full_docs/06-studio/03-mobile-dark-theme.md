# Mobile and Dark Theme

## Design goals

```text
usable on phone
usable near hardware
dark by default
touch-friendly
offline/local-first
```

## Breakpoints

```css
desktop: > 1100px
tablet: 700px - 1100px
phone: < 700px
```

## Mobile behavior

- Navigation becomes tabs.
- Cards stack vertically.
- Graph becomes timeline.
- Tables become key-value cards.
- Source editor gets full-screen mode.
- Buttons have minimum height 44px.

## Theme tokens

```css
:root {
  --bg: #0b1020;
  --surface: #111827;
  --surface-2: #1f2937;
  --text: #e5e7eb;
  --muted: #9ca3af;
  --accent: #22c55e;
  --danger: #ef4444;
  --warning: #f59e0b;
  --info: #38bdf8;
  --border: #334155;
}
```

## Status colors

```text
RUNNABLE: green
BLOCKED: red
UNKNOWN: yellow
SCHEDULED: blue
COMPLETED: green
FAILED: red
```

## Accessibility

- high contrast;
- keyboard navigation;
- visible focus;
- aria labels;
- no color-only status;
- readable font sizes.
