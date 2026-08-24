# Console design system guide

The console is built on **Oxygen UI** (WSO2's MUI-based design system). This
doc is the project-specific layer on top of it: what to reach for, in what
order, and which skills to use.

## Component sourcing order

1. **`@aep/ui-*` packages** (`packages/ui/`) — shared AEP components
   (explorer, md-editor, openapi-view, …). If it exists there, use it.
2. **Oxygen UI components** — the default for everything generic (buttons,
   forms, tables, dialogs, navigation).
3. **New shared component** — if it's reusable beyond one feature, it becomes
   a new `packages/ui/<component>` package (one component per package, see
   `packages/ui/AGENTS.md`), *not* a console-local widget.
4. **Feature-local component** — only when genuinely feature-specific; lives
   in `features/<feature>/components/`.

Never pull raw MUI or another component library alongside Oxygen UI.

## Skills to use

- **`oxygen-ui` skill** — the authority on Oxygen UI usage patterns
  (components, theming, dos/don'ts). Consult it for any Oxygen UI work; the
  conventions below are the console-specific layer on top, not a replacement.
  Installed at `.claude/skills/oxygen-ui/`; its `references/` (app-structure,
  components, patterns, theming, migration) and `sample/` canonical app source
  are the primary material — read them before scaffolding pages or reaching for
  a component.
- **`dataviz` skill** — read it **before** writing any chart, dashboard, stat
  tile, or visualization code. Non-optional for anything chart-shaped.
- **`console-feature` skill** — the entry point for a frontend feature, from
  either an idea or an existing issue number: it grills, records the outcome
  (new issue body, or a comment on the issue), and drives the build (see
  `development-flow.md`). It runs the **`grill-me`** interview for you; reach
  for `grill-me` directly only to re-grill a feature's shape outside the
  cycle.

## Conventions

- **Tokens over values.** Colors, spacing, and typography come from the Oxygen
  UI theme. No hex codes, no px literals for spacing — if a value isn't in the
  theme, that's a design decision to record, not a one-off style.
- **Light and dark.** Every screen must hold up in both themes; never encode
  "white background" assumptions.
- **Layout.** App shell (nav + content) is shared, not per-feature. Features
  render inside the shell's content region and own nothing outside it.
- **Density.** This is an engineer-facing console: default to information-dense
  tables and lists over card grids; progressive disclosure over pagination
  walls.
- **Empty, loading, error states are part of the design**, not afterthoughts.
  Every view ships all three (see `api-guidelines.md` for the doctrine).
- **Accessibility.** Interactive elements are keyboard-reachable; icons that
  convey meaning get labels; color is never the only signal.
- **Page precedents first.** Before composing any page, check whether the
  oxygen-ui skill's sample app already has that page and match it —
  `sample/src/pages/ProjectOverview.tsx` for project pages,
  `Projects.tsx`/`Organizations.tsx` for listings, `Analytics.tsx` for
  dashboards. Don't assemble layouts from primitives when a page-level
  precedent exists (learned on #77/PR #79: the hand-rolled header and
  component grid had to be redone to match the sample's page).
