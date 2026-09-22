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
- **A render throw is contained to its section.** `components/ErrorBoundary`
  wraps the shell's page outlet and the chat panel, and the root route wraps
  the whole app in one more as the last resort. Routes carry no error
  component of their own, so a page's throw lands in the outlet boundary and
  the shell stays up; without these, the router's
  top-level catch replaced the whole app with its "Show Error" page. The
  boundary logs the error and component stack, retries on its own twice
  (2s, then 5s — observed failures are races that a same-content re-render
  clears), then waits for new input via `resetKey` or the "Try again"
  button. The app-level one, once spent, says "Unable to recover. Please
  contact your administrator." and offers a reload, since a stale bundle
  after a deploy is the commonest whole-app failure. It is containment:
  the stack it records is what fixes the cause.
- **Accessibility.** Interactive elements are keyboard-reachable; icons that
  convey meaning get labels; color is never the only signal.
- **Page precedents first.** Before composing any page, check whether the
  oxygen-ui skill's sample app already has that page and match it —
  `sample/src/pages/ProjectOverview.tsx` for project pages,
  `Projects.tsx`/`Organizations.tsx` for listings, `Analytics.tsx` for
  dashboards. Don't assemble layouts from primitives when a page-level
  precedent exists (learned on #77/PR #79: the hand-rolled header and
  component grid had to be redone to match the sample's page).
