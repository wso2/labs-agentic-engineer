---
name: oxygen-ui-design-system
description: Oxygen UI (`@wso2/oxygen-ui`) — this organization's web-app design system, covering its provider + router wiring, the organization's settled brand colors, the composite components a page is built from, and the verify step a web-app build owes it. Apply to all UI work in a `web-application` that pins it — pages, layouts, forms, tables, dialogs, nav, theming — even when the task never names Oxygen.
metadata:
  aep:
    kind: org
    audience: [coding]
---

# Oxygen UI Design System

Oxygen UI (`@wso2/oxygen-ui`, https://github.com/wso2/oxygen-ui) is **this
organization's** UI toolkit — WSO2's React component library on Material UI
v7, with the Oxygen theme already applied to every export. Components, layout,
icons, and styling all come from it. Never raw HTML styling, never another
component library, never an invented component prop.

You are here because the component you are building pinned this skill, so the
whole of it is yours — theming included. The organization's colors are settled
in Brand colors below; nobody is interviewed about them, at design time or any
other time.

`react-webapp` owns the app: layout, config, verify sequence, Dockerfile, nginx.
This skill owns what goes **inside** `src/` — the UI. Where the two appear to
disagree, `react-webapp` wins; the conflicts worth naming are listed under
Platform constraints below.

## Correctness through the installed types, not memory

Everything `@wso2/oxygen-ui` exports is typed, and the types describe the
*installed* version — a guessed prop is never right by comparison. The
discipline: **before writing a screen, print the API of the composites it
uses, then write the JSX** — never the reverse. One command does it, from the
App Path, after `npm install`:

```bash
node "${AEP_SKILLS_DIR:-.claude/skills}/oxygen-ui-design-system/scripts/props.mjs" PageTitle StatCard ListingTable
```

It reads the installed `.d.ts` and prints, per component, every prop with its
type and whether it is required, then every sub-component (`PageTitle.Actions`,
`ListingTable.Row`) with its own props. `Name.Sub` prints one sub-component. A
name that is not an Oxygen composite (`Button`, `TextField`, `Tabs`) is plain
MUI v7, re-exported unchanged except for the theme; the script says so and
names the MUI API page. Run it **per screen**, for the composites that screen
uses — a lookup made for an earlier screen sits thousands of tokens back and
does not count as confirmed. It reads nothing this skill carries, so it cannot
drift from the version this app installed.

Oxygen's own docs sit beside the types: `components.md` is the index of
**every composite the library ships**, `patterns.md` holds whole-screen
compositions, `theming.md` and `migration.md` the rest. Read them from the
installed package (`node_modules/@wso2/oxygen-ui/.claude/`), which is matched
to the installed version; before `npm install` has run, the same four files are
in `references/oxygen/` beside this skill, taken from 0.13.1. Use the index to
**reach for Oxygen's composite first** — `ListingTable`, `PageTitle`, `Form.*`,
`UserMenu`, `SearchBar`, `ComplexSelect`, `StatCard`, `NotificationPanel` —
and drop to a plain MUI primitive (`Box`, `TextField`, `Chip`, still imported
from `@wso2/oxygen-ui`) only where no composite covers the element. The prose
is hand-written and lags the types: never settle a prop from it — print it
(Known errors below).

### Known errors in the package's docs

Seven snippets in the package's `.claude/*.md` do not compile against the code
they ship with — verified against the 0.13.1 types. The `.d.ts` is right and
the prose is wrong. `references/app-structure.md` beside this skill carries a
corrected scaffold.

| The docs show | It actually is |
|---|---|
| `<StatCard title="Total Users" value="12,345" change={12.5} trend="up" />` | `StatCard` has `value`, `label`, `icon`, `iconColor` and nothing else — no `title`, no `change`/`trend`, no caption slot, and children are discarded |
| `<Footer companyName="WSO2 LLC" />` | `Footer` takes children: `<Footer><Footer.Copyright>© WSO2 LLC</Footer.Copyright></Footer>` |
| `DashboardIcon` | not an export — the lucide name is `LayoutDashboard` |
| `GoogleIcon` | not an export — the brand icons carry no `Icon` suffix, so it is `Google` |
| `<Grid item xs={12} md={4}>` (`patterns.md`) | MUI 7's `Grid` has no `item`/`xs`/`md` props — it is `<Grid size={{ xs: 12, md: 4 }}>`, as the sample app writes it |
| `<ColorSchemeImage light="…" dark="…" />` (`theming.md`) | the props are `src={{ light: "…", dark: "…" }}` and `alt`; `width`/`height` optional |
| `import { extendTheme } from '@mui/material/styles'` (`theming.md`, `migration.md`) | not an Oxygen export, and any `@mui/*` import fails Verify — a brand theme is `createOxygenTheme` from `@wso2/oxygen-ui` (`references/brand-theme.md`) |

A snippet that fails `tsc` is a doc bug, not a version you are missing — print
the props and follow them.

Plain MUI components (`Button`, `TextField`, `Dialog`, `Chip`, `Grid`, …) keep
MUI v7's API; they are re-exported from `@wso2/oxygen-ui` unchanged except for
the theme.

**Violating the letter of this rule is violating the spirit of it.** "It's just a
placeholder page," "the app doesn't have Oxygen wired up yet," and "this screen
is throwaway" are reasons to wire Oxygen up *faster*, not reasons to skip it — a
page built in raw `<div>`s is what deploys, because there is no human code-review
gate between your PR and the dev environment.

## Setup

`react-webapp` scaffolds the app. Add Oxygen to it **before the first
`npm install`** — once the scaffold's `package.json` is written, from the App
Path:

```bash
node "${AEP_SKILLS_DIR:-.claude/skills}/oxygen-ui-design-system/scripts/setup.mjs"   # --charts if a screen draws a chart
```

One command, one install. It pins `react` and `react-dom` to exactly the
version Oxygen's peer dependency names (a newer 19.x fails `npm install` with
`ERESOLVE`, and forcing past that ships two Reacts), adds `@wso2/oxygen-ui`,
`@wso2/oxygen-ui-icons-react` and `react-router` to `package.json`, removes any
`@mui/*`, `@emotion/*` or `lucide-react` a scaffold slipped in, and runs
`npm install` once. `--charts` adds `@wso2/oxygen-ui-charts-react`;
`--no-install` writes the manifest only. Running it again changes nothing.
`AEP_SKILLS_DIR` is where a run finds the skill library; the fallback is the
mirror the platform writes into every project, `.claude/skills/`, which is
also where this skill's own `scripts/` live.

**Never install `@mui/*`, `@emotion/*`, or `lucide-react` yourself.**
`@wso2/oxygen-ui` bundles MUI, MUI X and Emotion as its own dependencies, and
`@wso2/oxygen-ui-icons-react` brings lucide; a second copy
in `package.json` gives the app two Emotion caches and two MUI runtimes, and
the theme silently stops reaching half the tree. The package README's install
line that names `@mui/material` predates this and is wrong for the version you
install. Verify below fails on any of them.

Wire the provider once, outermost, in `main.tsx` — then the router, then the
app. Oxygen bundles its font (Inter Variable) and applies its theme
globally from the provider; there is no stylesheet to import or link:

```tsx
// src/main.tsx
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router';
import { OxygenUIThemeProvider, OxygenTheme } from '@wso2/oxygen-ui';
import App from './App';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </OxygenUIThemeProvider>
  </StrictMode>,
);
```

`OxygenTheme` is the stock theme, and the fallback. It is what a project gets
when this organization has not set brand colors — see Brand colors below. Then
lay the app out as the sample app is laid out — the next section.

## The sample app is the structure

Oxygen ships a reference application, and **every web app this platform
builds has its shape**: the same shell, the same layouts, the same route
table, the same page anatomy. Its source is in `sample/src/` beside this skill
(vendored from `samples/oxygen-ui-test-app` in the Oxygen UI repo; the npm
package does not carry it). Read the real files before scaffolding, and match
them rather than inferring structure from prose:

| To see… | Read in `sample/src/` |
|---|---|
| The app shell: `Header` (toggle, brand, actions) in `AppShell.Navbar`, `Sidebar` categories and items in `AppShell.Sidebar`, `AppShell.Main`, `Footer` | `layouts/AppLayout.tsx` |
| The route table grouped under layouts | `config/appRoutes.tsx`, `App.tsx` |
| Login / unauthenticated layout | `layouts/GateLayout.tsx` |
| A listing page with a table and a page-title action | `pages/Organizations.tsx`, `pages/Projects.tsx` |
| A detail page (title, avatar, back button, sections) | `pages/ProjectOverview.tsx` |
| Rows-as-cards listing (`ListingTable variant="card"`) | `pages/ProjectOverview.tsx` (MCP Servers section) |
| A multi-step form / wizard | `components/ComponentCreate/IntegrationWizard.tsx` |
| A settings page | `pages/SettingsPage.tsx` |
| A dashboard with stat tiles and charts | `pages/Analytics.tsx` |
| Empty and error states | `pages/EmptyComponentList.tsx`, `pages/ErrorPage.tsx` |

**The shell is not negotiable.** `AppLayout` is `AppShell` with the brand and
the account cluster in the header, the app's navigation in the **sidebar**,
the routed page in `AppShell.Main`, and a `Footer`. A generated app never
moves navigation into the header, never drops the sidebar, and never invents a
second shell: the wireframes are drawn against this shell too (`wireframes`
draws a brand-only `navbar` and a `sidebar` on every screen), so the
wireframe, the running app and the sample line up one to one. What the
sample's sidebar shows for its console — organisations, projects, analytics —
becomes the wireframe's `sidebar` items for this app; the categories, icons
and footer items (Settings, Help) keep the sample's arrangement.

**Known defects in the sample.** It is vendored verbatim so a refresh stays a
wholesale copy, and it carries a few demo shortcuts a generated app must not
inherit — `sample/README.md` lists them (a login box pre-filled with
credentials, a route param read under the wrong name, absolute paths that drop
the organisation prefix, a detail page that shows the first record for an
unknown id, an advanced filter that is never applied). Take the structure;
where a line is on that list, write it right.

**What the platform overrides in the sample.** Three files are Oxygen's demo
wiring, not this platform's — take the structure and not the code:
`main.tsx` (its `HashRouter`, `window.__APP_RUNTIME_CONFIG__` and theme
picker; ours is the Setup snippet above, with `BrowserRouter` and one theme),
`mock-data/` (this platform's mock is `react-webapp`'s `mock/` with msw),
and the org/project `Header.Switchers` selects, which a wireframe has to draw
before they exist. `references/app-structure.md` carries the excerpts already
adjusted to this platform.

## Brand colors

A web app is themed at its FIRST build, and retrofitting a theme means revisiting
every screen — so the colors cannot be a per-build decision. They are an
**organization** decision, settled once in the section below, exactly like the
language a service is written in.

**Never ask anyone for them.** Not at design time, not at coding time, not once
per project. The answer is either set below or it is the stock theme, and both
are complete answers — a question about theming is a defect, not diligence.

### The organization's colors

- Accent (buttons, links, focus): _not set — use the stock theme_
- Neutral (backgrounds, surfaces): _not set — use the stock theme_

To brand every web app this organization builds, an org edits those two lines
to hex values (Settings → Skills) — HEX only, never color words ("black and
yellow" does not say WHICH yellow), e.g. `#f5c518` / `#0a0a0a`. The edit
reaches every subsequent build in the org with no conversation involved.

A per-project override still wins, **per value, not per section**: a hex under
the `## Brand colors` heading in the project's `specs/requirements/prd.md`
overrides the same line here, and a line that heading omits still comes from
this section. That heading is there for a project whose colors someone stated
outright — it is not something to solicit.

### At build time

Resolve each of the two colors independently, before you wire the theme: the
project's `## Brand colors` line in `specs/requirements/prd.md` if it has one,
otherwise the line above. **A color neither one sets is not chosen** — that
part of the theme stays stock and nothing is invented. With no hex resolved,
the provider keeps `OxygenTheme` exactly as Setup wires it and this section is
done. With one or two, derive `src/theme.ts` per `references/brand-theme.md` — a
theme of your own from the stock one, never brand colors painted over
components through `sx`.

## Verify

This skill's step in `react-webapp`'s verify sequence — after `npm install`,
before `npx tsc --noEmit`, from the App Path:

```bash
node "${AEP_SKILLS_DIR:-.claude/skills}/oxygen-ui-design-system/scripts/verify.mjs"
```

`AEP_SKILLS_DIR` falls back to `.claude/skills/`, the mirror the BFF writes
into every project (this skill lands at
`.claude/skills/oxygen-ui-design-system/`). A non-zero exit
fails verification like any other step in that sequence, and every failure
line names its fix. It is there because Oxygen's wiring faults — a second MUI
or Emotion installed beside the bundled one, an import that bypasses
`@wso2/oxygen-ui`, a root not wrapped in `OxygenUIThemeProvider`, a React that
is not the exact version Oxygen's peer dependency names — type-check and build
perfectly clean, then render an unthemed or broken page in the cluster: cheap
to fix here, expensive to debug after the Docker build.

## Platform constraints that override this system's defaults

Five places where Oxygen's own defaults and docs do not fit this platform. Each
is a runtime, build, or guidance failure, not a style preference:

1. **Never run `npx @wso2/oxygen-ui init` (or `update`).** That CLI writes
   `CLAUDE.md`, `AGENTS.md`, `.claude/oxygen-ui/` and `.claude/skills/` into the
   repo root. Guidance reaches you as skills, so a committed agent file is a
   second authority that nothing updates — it is stale the moment this skill
   changes, and it lands in a directory the platform owns. The same docs are
   already in the installed package (see Correctness above); read them there.
2. **Never install the peer set Oxygen's README lists** (`@mui/material`,
   `@emotion/*`, `@mui/x-*`). They are bundled — Setup above.
3. **Never set `base` in `vite.config.ts`**, and never give `BrowserRouter` a
   `basename`, whatever a sample shows. Each web app is served at its own
   gateway host root; a `base` 404s every asset and the page renders blank
   (`react-webapp`, Served at host root).
4. **`index.html` gets no extra tags.** Its only `<script>` rules are
   `react-webapp`'s: the synchronous `env-config.js` tag first, the module
   bundle second. Oxygen bundles its font, so no `<link>` for Inter or a
   stylesheet, and no script tag around those two — it risks
   `window._env_` being unset when the first module evaluates.
5. **Theme values are not runtime config.** Colors come from `src/theme.ts` at
   build time. `window._env_` carries only what the **browser** needs — OIDC
   config and flags — so do not plumb a theme value through it.

Oxygen replaces hand-written UI, not the platform's data layer: `openapi-fetch`
and the committed `src/generated/` client stay exactly as `react-webapp`
specifies. "Install no other library" above is about UI and styling.

## Critical rules

1. **Import everything from `@wso2/oxygen-ui`** — never from `@mui/material`,
   `@mui/x-*`, Tailwind, Chakra, Ant Design, Bootstrap, or a hand-rolled
   component. The package re-exports the entire MUI API with the theme applied.
2. **Icons come from `@wso2/oxygen-ui-icons-react`**, never `lucide-react`. It
   re-exports all of lucide plus the brand icons `WSO2`, `GitHub`, `GitLab`,
   `Bitbucket`, `Google`, `Facebook`, `MCP`, `Lambda`. Two rules the compiler
   enforces and memory does not:
   - **A lucide icon works bare or `Icon`-suffixed** (`Pencil` and `PencilIcon`
     both resolve). Prefer the bare name for consistency, but neither is an
     error — what fails is a name lucide does not have, like `DashboardIcon`
     for `LayoutDashboard`. Check it at https://lucide.dev/icons/.
   - **A brand icon has no `Icon` alias and is cased exactly as listed** —
     `Google` not `GoogleIcon`, `GitHub` not `Github`.

   Size with the `size` prop: `<Search size={18} />`.
3. **MUI X is namespaced**: `DataGrid.DataGrid`, `DatePickers.DatePicker`,
   `TreeView.SimpleTreeView` — import the namespace from `@wso2/oxygen-ui`.
4. **Confirm a composite component's API before using it** — print it with
   `scripts/props.mjs` (Correctness above); don't guess a sub-component or
   prop, and don't settle one from the package's prose docs.
5. **Colors and spacing are theme tokens through `sx`, never literals.**
   `p: 2`, `gap: 2`, `bgcolor: 'background.paper'`, `color: 'text.secondary'`,
   `borderColor: 'divider'` — no hex, no rgb, no raw px. Brand colors live in
   `src/theme.ts` (above), never in a component.
6. **Layout is `Stack`/`Box`/`Grid` from `@wso2/oxygen-ui`** — never a raw
   `<div>`/`<span>` for spacing or arrangement, and never `style={{…}}`.
7. **Page-level structure follows a precedent, not intuition.** Every page is
   `PageContent` > `PageTitle` (`PageTitle.Header`, `.SubHeader`, `.Actions`,
   `.BackButton`) > content, inside the `AppShell` of `AppLayout`. A screen's
   header buttons go in `PageTitle.Actions` — never a `Stack` around
   `PageTitle` with the button beside it, which squeezes the button until its
   label wraps. Before composing a listing, detail, dashboard, settings, or
   login screen, find it in the package's `.claude/patterns.md` and match its
   composition.
8. **Navigation goes through `react-router`**: `Sidebar.Item link={<Link to="…" />}`,
   `useNavigate()` for actions, never a hardcoded `<a href>`.
9. **Dense data is rows, not cards.** Use `ListingTable` for lists of records
   (`variant="card"` when each row wants breathing room); `Card` is for widgets,
   galleries, or grouped settings — not one card per record.

## Reach for these components (not raw MUI, never raw HTML)

| If you're about to build… | Use instead |
|---|---|
| The page shell | the sample's `AppLayout`: `AppShell` > `AppShell.Navbar` (`Header`: toggle, brand, spacer, actions), `AppShell.Sidebar` (`Sidebar` categories + items), `AppShell.Main`, `AppShell.Footer` (`Footer`) — `sample/src/layouts/AppLayout.tsx`, excerpt in `references/app-structure.md`. Navigation lives in the sidebar, never in the header |
| A page heading, with or without its action buttons | `PageTitle` (`.Header`, `.SubHeader`, `.Actions` for the buttons, `.BackButton`, `.Avatar`) inside `PageContent` |
| A data table / list of records | `ListingTable` (`.Container`, `.Toolbar`, `.Head`, `.Body`, `.Row`, `.Cell`, `.RowActions`, `.EmptyState`, `.Footer`) |
| A form with grouped fields | `Form.Section` + `Form.Stack` (fields are plain `TextField`, `Select`, `Checkbox`, `Switch`) |
| A multi-step flow / wizard | `Form.Wizard` |
| A user avatar + account menu | `UserMenu` (`.Trigger`, `.Header`, `.Item`, `.Divider`, `.Logout`) |
| A KPI / metric tile: label + value | `StatCard` (`value`, `label`, `icon`, `iconColor` — nothing else) |
| A KPI tile with a caption or any third line | `Card` > `CardContent` > three `Typography`s — `StatCard` has no slot for it |
| Breadcrumbs / search / rich select | `AppBreadcrumbs` / `SearchBar` / `ComplexSelect` |
| Notifications | `NotificationPanel` (in `AppShell.NotificationPanel`) / `NotificationBanner` |
| A modal / confirmation dialog | `Dialog` + `DialogTitle` + `DialogContent` + `DialogActions` |
| Status / count / label chip | `Chip` (`color="success"` etc.), `Badge` |
| Light/dark toggle, theme picker | `ColorSchemeToggle`, `ThemeSwitcher` |
| Login / unauthenticated page | `Layout.Content` + `ParticleBackground` (`references/app-structure.md`, GateLayout) |
| Date/time entry | `DatePickers.DatePicker`, `DatePickers.DateTimePicker` |
| A chart | `@wso2/oxygen-ui-charts-react` |

## Implementing a wireframe with Oxygen

`wireframes/references/implementing.md` says what each DSL line must become;
this is what it becomes here. Every row names the exact component and its
props, and what to do when the DSL carries more than the component holds —
each was checked against a rendered screen, so follow the row rather than
improvising around the component:

| DSL | Oxygen |
|---|---|
| a `screen` with no `navbar`/`sidebar` — an app-owned sign-in, the one screen the wireframes skill draws without chrome | the sample's `GateLayout` (`Layout.Content` + `ParticleBackground`), the form inside it; every signed-in screen is under `AppLayout` |
| `navbar "Brand"` + `sidebar "A -> S \| B \| C -> T"` | the sample's `AppLayout`, on every screen of a role: `Header` with `Header.Toggle`, `Header.Brand` > `Header.BrandTitle` (the brand), `Header.Spacer`, `Header.Actions` (`ColorSchemeToggle`, `UserMenu`) in `AppShell.Navbar`; one `Sidebar.Item id link={<Link to="…" />}` per sidebar item in `AppShell.Sidebar`, grouped with `Sidebar.Category` as the sample groups its own, `activeItem` from the route; a `Footer` in `AppShell.Footer`. A link the wireframe put in the `navbar` still goes in the sidebar — the header never carries navigation |
| `row` + `heading` + `right` + `button`(s) at the top of a screen | one `PageTitle`: the heading in `PageTitle.Header`, the buttons in `PageTitle.Actions`. Never a `Stack` around `PageTitle` — it squeezes the button until its label wraps onto two lines |
| `heading` elsewhere | `Typography variant="h6"` (section title) |
| `text`, `link`, `breadcrumb` | `Typography`, `Link component={RouterLink} to="…"`, `AppBreadcrumbs` |
| `card "Label \| Value \| Caption"` | `Card` > `CardContent` > `Typography variant="overline"` (label), `Typography variant="h4"` (value), `Typography variant="caption" color="text.secondary"` (caption). `StatCard` holds only `label` + `value` (+ `icon`): use it for a two-part `card "Label \| Value"`, and never park the caption outside it |
| `card "Title"` with nested children | `Card` > `CardHeader title="Title"` + `CardContent` holding the children |
| `table "A \| B \| C" [-> S]` + `row` lines | `ListingTable.Container` > `ListingTable` > `.Head` / `.Body` / `.Row` / `.Cell` with exactly those columns; `-> S` makes each `ListingTable.Row clickable onClick={() => navigate(…)}`; `ListingTable.EmptyState` with no rows. **Every drawn column is built.** A column the list response does not carry is filled from **one** request to whichever other list operation of the contract supplies it — a name column from the entity list joined on id, a count column from the child list filtered once and grouped; two missing columns may need two such requests, one per list, never one per row. Only a column no list operation can supply is omitted, with a line in your report |
| `select "Label: Value"` | `TextField select label="Label"` with a `MenuItem` per option and `Value` preselected — never a bare `Select`: its `label` prop renders nothing without `FormControl` + `InputLabel`, so the control shows no label at all |
| `select "Active only"` (one filter, no `Label:`) | `FormControlLabel control={<Switch />} label="Active only"` — the DSL string is the visible label, and a select whose only choice is on/off is a switch |
| `input "Label"` | `TextField label="Label"`; a label naming a date → `TextField type="date" label="Label" slotProps={{ inputLabel: { shrink: true } }}` |
| `textarea`, `search`, `checkbox`, `radio`, `toggle` | `TextField multiline`, `SearchBar`, `FormControlLabel` + `Checkbox`, `RadioGroup`, `FormControlLabel` + `Switch` — inside `Form.Section` / `Form.Stack` |
| `button "X" primary` / `danger` / other | `Button variant="contained"` / `Button variant="outlined" color="error"` / `Button variant="outlined"`; a `row` of buttons after `right` at the foot of a form is `Stack direction="row" justifyContent="flex-end" spacing={2}` |
| `badge "X" variant` | `Chip label="X" color=… size="small"` |
| `list`, `tabs`, `progress`, `avatar`, `chart`, `image` | `List`, `Tabs`, `LinearProgress`, `Avatar`, a charts-react chart, `ColorSchemeImage` |
| `row`, `split N/M` | `Stack direction="row"` / `Grid` with `size={{ md: N }}` and `size={{ md: M }}` |
| a `variant` (`danger`, `success`, …) | the palette's matching status color: `color="error"`, `"success"`, `"warning"`, `"info"` |

The `row` lines under a `table`, the stat `card`s and the `select`s are the
demo data the reviewer compares the screen against. Do not retype them:
`node "${AEP_SKILLS_DIR:-.claude/skills}/wireframes/scripts/seed.mjs" <wireframes.dsl>` prints
them per screen as JSON; mock handlers serve those rows, and every stat value
derives from them, so the numbers agree with the table by construction.

## Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| `npm install` fails with `ERESOLVE` on `react` | Oxygen's peer dependency is an exact React version and the scaffold installed a newer one | Run `scripts/setup.mjs` (Setup) — it pins `react`/`react-dom` to that exact version; never `--force` or `--legacy-peer-deps` past it |
| A header button wraps onto two lines | The button sits in a `Stack` beside `PageTitle`, which takes the width | Put it in `PageTitle.Actions` |
| A select shows its value but no label | `label` on a bare `Select` needs `FormControl` + `InputLabel` to render | `TextField select label="…"` |
| A stat card's caption sits outside the card | `StatCard` has no caption slot and discards children | `Card` > `CardContent` > three `Typography`s (the wireframe table) |
| A list screen fires one request per row | A column the list endpoint does not return, filled from the detail endpoint | Fill it from one bulk request to another list operation (join on id, or filter + group), as the wireframe table says |
| A drawn column is missing from the page | The list endpoint lacks it and the agent dropped it | Drop a column only when no list operation of the contract can supply it, and say so in your report; a joinable entity list or a filterable child list supplies it in one request |
| Components render in stock Material blue, not the Oxygen theme | `OxygenUIThemeProvider` missing, or not outermost in `main.tsx` | Wrap the root exactly as Setup shows; Verify fails on this |
| Theme applies to some components and not others; console warns about multiple Emotion/MUI instances | `@mui/material` or `@emotion/*` installed beside Oxygen's bundled copy, or imported directly | Remove them from `package.json` and every import; import from `@wso2/oxygen-ui` only |
| `Cannot find module 'lucide-react'` or an icon import fails | Icons imported from the wrong package, or a made-up name | Import the bare lucide name from `@wso2/oxygen-ui-icons-react`; check the name at lucide.dev |
| `DataGrid is not a component` / `DatePicker is not exported` | Namespace used as a component | `DataGrid.DataGrid`, `DatePickers.DatePicker` |
| Page renders blank in the cluster, every asset 404s | `base` in `vite.config.ts` or `basename` on the router, copied from a sample | Remove both — served at host root (`react-webapp`) |
| A sub-component or prop "does not exist" | Answered from memory, or from the package's prose docs | `scripts/props.mjs <Component>` — the installed types reflect the installed version; neither training data nor `components.md` does |
| Every record in a list is its own `Card` | Defaulted to a card grid instead of checking data density | `ListingTable` for records; `Card` for widgets and galleries |
| Brand colors are set, deployed app is stock-themed; or the accent is unreadable in one mode | The theme was never derived, or a pale hue was used in both schemes | `references/brand-theme.md` — derive `brandTheme` with `createOxygenTheme`; darken the light scheme's `main` |
| The user gave brand colors in chat, the build ignored them | A coding run never sees a conversation — colors reach it only from this skill or the project's `specs/requirements/prd.md` | Set them in The organization's colors (Settings → Skills) for the whole org, or under `## Brand colors` in the project's `specs/requirements/prd.md` for one project; an answer that is not in a file did not happen |

## Red flags — stop and use Oxygen

- About to write `<div style={{...}}>`, a `className` with a stylesheet, or a
  raw `<button>`/`<table>`/`<input>` for layout, color, spacing, or a control
- About to `npm install` `@mui/*`, `@emotion/*`, `lucide-react`, or any other
  component or styling library
- About to run `npx @wso2/oxygen-ui init`
- About to write JSX for a form, list, card, dialog, nav, or page header from
  scratch instead of from the Reach-for table and the package's `.claude/patterns.md`
- Thinking "it's just a placeholder" or "Oxygen isn't set up in this app yet"
- Using a sub-component or prop without having printed it with
  `scripts/props.mjs` for this screen
- About to wrap `PageTitle` in a `Stack` to place a button beside it, use a
  bare `Select` with a `label`, or put a caption under a `StatCard` — each is
  a row in the wireframe table above, with the component that fits
- About to put navigation in the header, drop the sidebar, or lay a page out
  in a way `sample/src/` does not — the sample app is the structure, and the
  wireframe was drawn against it
- About to satisfy a brand-color requirement by styling components instead of
  deriving a theme — or about to ignore one because no stock theme matches
- About to ask which theme or colors to use — that is settled in Brand colors,
  and "not set" means the stock theme, not an open question

All of these mean: stop, open the reference, and use what it documents.
