---
name: astryx-design-system
description: Astryx (`@astryxdesign/core`) — this organization's web-app design system, covering its Theme + StyleX wiring, the organization's settled brand colors, and the CLI you confirm every component's props against before writing JSX. Apply to all UI work in a `web-application` that pins it — pages, layouts, forms, tables, dialogs, nav, theming — even when the task never names Astryx.
metadata:
  aep:
    kind: org
    audience: [coding]
---

# Astryx Design System

Astryx (`@astryxdesign/core`) is **this organization's** UI toolkit —
components, layout, and styling (via StyleX) all come from it. Never raw HTML
styling, never another component library, never an invented component prop.

You are here because the component you are building pinned this skill, so the
whole of it is yours — theming included. The organization's colors are settled
in Brand colors below; nobody is interviewed about them, at design time or any
other time.

`react-webapp` owns the app: layout, config, verify sequence, Dockerfile, nginx.
This skill owns what goes **inside** `src/` — the UI. Where the two appear to
disagree, `react-webapp` wins; the conflicts worth naming are listed under
Platform constraints below.

## Correctness through the CLI, not memory

Astryx ships `@astryxdesign/cli` because component APIs move faster than any
model's training data. The CLI reads the *installed* version, so it is always
right; a guessed prop is never right by comparison. The discipline: **before
writing JSX for a component you have not confirmed this session, run the CLI,
then write the JSX** — never the reverse.

**Always invoke it as `npx --no astryx …`.** `--no` restricts resolution to the
`@astryxdesign/cli` in this app's `node_modules` — the version `package-lock.json`
pins. Without it, an `npx astryx` in an app whose install has not run fetches and
executes the unrelated `astryx` package that exists on the public registry. Do not
drop the flag to shorten a command: if the CLI is missing, the right outcome is the
loud `could not determine executable to run`, not a stranger's postinstall script
running in the build pod.

**Violating the letter of this rule is violating the spirit of it.** "It's just a
placeholder page," "the app doesn't have Astryx wired up yet," and "this screen
is throwaway" are reasons to wire Astryx up *faster*, not reasons to skip it — a
page built in raw `<div>`s is what deploys, because there is no human code-review
gate between your PR and the dev environment.

## Setup

`react-webapp` scaffolds the app. Add Astryx to it:

```bash
npm install @astryxdesign/core @stylexjs/stylex @astryxdesign/theme-neutral @astryxdesign/build
npm install -D @astryxdesign/cli
```

`@astryxdesign/core` declares React **19+** as a hard peer dependency — set
`react` / `react-dom` to `^19` in `package.json`, not an older major.

Wire the build (order matters — `astryxStylex()` before `react()`, and **no
`base`**, per `react-webapp`):

```ts
// vite.config.ts
import {defineConfig} from 'vite';
import react from '@vitejs/plugin-react';
import {astryxStylex} from '@astryxdesign/build/vite';

export default defineConfig({plugins: [...astryxStylex(), react()]});
```

```tsx
// main.tsx — reset + theme CSS load before anything renders
import '@astryxdesign/core/reset.css';
import '@astryxdesign/theme-neutral/theme.css';
import {Theme} from '@astryxdesign/core/theme';
import {neutralTheme} from '@astryxdesign/theme-neutral/built';
// wrap <App/> in <Theme theme={neutralTheme}> — swap the theme package to change
// the look, never hand-roll colors instead
```

That stock package is the default and the fallback. It is what a project gets
when this organization has not set brand colors — see the next section.

## Brand colors

A web app is themed at its FIRST build, and retrofitting a theme means revisiting
every screen — so the colors cannot be a per-build decision. They are an
**organization** decision, settled once in the section below and compiled at
build time, exactly like the language a service is written in.

**Never ask anyone for them.** Not at design time, not at coding time, not once
per project. The answer is either set below or it is the stock theme, and both
are complete answers — a question about theming is a defect, not diligence.

### The organization's colors

- Accent (buttons, links, focus): _not set — use the stock theme_
- Neutral (backgrounds, surfaces): _not set — use the stock theme_

To brand every web app this organization builds, an org edits those two lines to
hex values (Settings → Skills), e.g.:

```markdown
- Accent (buttons, links, focus): #f5c518
- Neutral (backgrounds, surfaces): #0a0a0a
```

HEX only, never color words: "black and yellow" does not say WHICH yellow, and
this file is the whole of what a build sees. The edit reaches every subsequent
build in the org with no conversation involved.

A per-project override still wins, **per value, not per section**: a hex under
the `## Brand colors` heading in the project's `specs/requirements/prd.md`
overrides the same line here, and a line that heading omits still comes from
this section. That heading is there for a project whose colors someone stated
outright — it is not something to solicit, and a half-filled one is not a
reason to drop the organization's other color.

### At build time — compile the theme

Resolve each of the two colors before you wire the theme, independently: the
project's `## Brand colors` line in `specs/requirements/prd.md` if it has one,
otherwise the line in The organization's colors above. **A color neither one
sets is not chosen** — take it from the stock theme's own token and never
invent one, whether that leaves you with two brand colors, one, or none. One
brand color plus one stock color is a valid outcome; a guessed hex is not.

With colors, a brand theme is a **compiled theme of your own** — not hand-written
colors sprinkled over a stock one. Editing component styles to paint them brand
colors violates "colors are tokens" and leaves every unstyled surface off-brand.
Scaffold a stock theme as editable source, retune its color tokens, compile it:

```bash
# 1. scaffold the closest stock theme as YOUR source (once)
npx --no astryx theme add neutral src/theme
# 2. copy it to src/theme/brandTheme.ts, rename the exported symbol +
#    `name:` + its defineSyntaxTheme `name:`, then edit the color tokens
# 3. compile — emits brand.css + brand.js + brand.d.ts beside the source
npx --no astryx theme build src/theme/brandTheme.ts -o src/theme/brand.css
```

Every color token is a `[light, dark]` pair. Retune this set and leave the rest
of the scaffold alone — they are what carry a brand:

| Token | Set it to |
|---|---|
| `--color-accent`, `--color-text-accent`, `--color-icon-accent` | the accent brand color; the **light** slot usually needs a darkened variant to stay AA on a light background |
| `--color-on-accent` | the text color that sits ON the accent — check contrast both ways |
| `--color-accent-muted` | the accent at low alpha (e.g. `#RRGGBB33`) |
| `--color-background-body`, `--color-background-surface`, `--color-background-card`, `--color-background-popover`, `--color-background-muted` | the neutral brand color, as a ramp — body darkest, surface/card a step lighter, never all the same value |

Then wire the compiled output exactly like a package theme — the import is the
build's `-o` CSS plus the sibling JS, both from `src/theme/`, never the `.ts`:

```tsx
// main.tsx
import '@astryxdesign/core/reset.css';
import './theme/brand.css';
import {Theme} from '@astryxdesign/core/theme';
import {brandTheme} from './theme/brand';
// wrap <App/> in <Theme theme={brandTheme}>
```

Commit the generated `brand.css` / `brand.js` / `brand.d.ts` alongside the
source: the Docker build runs `npm run build`, not the theme compiler, so an
uncommitted build output ships an unthemed app. Re-run `theme build` after
every edit to `brandTheme.ts` — editing the source alone changes nothing.

Two colors is the common case, and it does not mean two tokens. Map the neutral
across the background ramp and the accent across the accent trio; a pure
`#FFFF00`-class hue almost always needs darkening for its light-mode slot and
dark text for `--color-on-accent`. Contrast is not negotiable to match a brand:
keep the hue, move the lightness.

## Verify

This skill's step in `react-webapp`'s verify sequence — after `npm install`,
before `npx tsc --noEmit`:

```bash
npx --no astryx doctor
```

A non-zero exit fails verification like any other step in that sequence. It is
there because a wiring fault (a missing `astryxStylex()` plugin, an unimported
theme, a React peer-dependency mismatch) type-checks and builds perfectly clean,
then renders an unstyled page in the cluster — cheap to fix here, expensive to
debug after the Docker build.

## Platform constraints that override this system's defaults

Four places where Astryx's own defaults do not fit this platform. Each is a
runtime, build, or guidance failure, not a style preference:

1. **Never install Astryx's agent docs.** `astryx init` itself is fine — it
   initializes the design system in an *existing* project, and it is not
   required, because the Setup section above already states the wiring. What
   must not land is the `agents` feature: `astryx init --features agents`
   writes `AGENTS.md` into the repo root, and `--all` includes it. Guidance
   reaches you as skills, so a committed agent file is a second authority that
   nothing updates — it is stale the moment this skill changes. `--features` is
   an allow-list, so name only what you want (`--features theme`) and never
   `--all`; if agent docs already landed, `astryx init --remove-agents` deletes
   them.
2. **Never set `base` in `vite.config.ts`**, whatever an Astryx snippet shows.
   Each web app is served at its own gateway host root; a `base` 404s every asset
   and the page renders blank (`react-webapp`, Served at host root).
3. **The CSS imports go in `main.tsx`, never in `index.html`.** `index.html`'s
   only `<script>` rules are `react-webapp`'s: the synchronous `env-config.js`
   tag first, the module bundle second. Adding a stylesheet or script tag around
   them risks `window._env_` being unset when the first module evaluates.
4. **Theme tokens are not runtime config.** Colors and spacing come from the
   theme package at build time. `window._env_` carries only what the **browser**
   needs — OIDC config and flags — so do not plumb a theme value through it.

Astryx replaces hand-written UI, not the platform's data layer: `openapi-fetch`
and the committed `src/generated/` client stay exactly as `react-webapp`
specifies. "Install no other library" below is about UI and styling.

## Critical rules

1. **Import everything from `@astryxdesign/core/<Category>`** (per-category
   subpath entry points, e.g. `@astryxdesign/core/Button`,
   `@astryxdesign/core/Layout`) — never from Tailwind, MUI, Chakra, Ant Design,
   Bootstrap, or a hand-rolled component.
2. **Run `npx --no astryx component <Name> --dense` before using ANY component**,
   even one already used earlier in this session — confirm the prop exists before
   writing it, don't guess.
3. **Search before building.** Run `npx --no astryx search "<thing>"` when unsure
   what exists; Astryx ships more components than you would assume (tag inputs,
   command palettes, tree lists, chat UI) — check before reaching for a wrapper
   `<div>` or a new dependency.
4. **Layout is `VStack`/`HStack`/`Grid`/`Stack` from `@astryxdesign/core/Layout`**
   — never a raw `<div>`/`<span>` for spacing or arrangement.
5. **Style overrides are `stylex.create()` + the component's `xstyle` prop** —
   never `style={{...}}`, and never `className`/`style` alongside
   `{...stylex.props()}` (use `mergeProps()` if you must combine).
6. **Colors and spacing are tokens, never literals.** Run
   `npx --no astryx docs tokens --dense`; use the CSS-var color tokens and
   `spaceN` gap values it documents, not hex/rgb or raw px.
7. **Page-level structure follows a template, not intuition.** Run
   `npx --no astryx template --list` and `npx --no astryx template <name>
   --skeleton` to find and study a layout skeleton before hand-building a page (dashboard,
   settings, list, wizard, auth) from scratch.
8. **Navigation uses `useLinkComponent()`**, never a hardcoded `<a>`.
9. **Dense data is rows, not cards.** Use `Table` or `List`+`Item` for lists of
   records; `Card` is for widgets, galleries, or grouped settings — not one card
   per row.

## Reach for these components (not raw HTML)

| If you're about to build… | Use instead |
|---|---|
| Page shell with top bar + side nav | `AppShell`, `TopNav`, `SideNav`, `MobileNav` |
| A data table / list view | `Table`, `List` + `Item`, `MetadataList` |
| A form with grouped fields | `FormLayout`, `Field`, `FieldStatus` |
| A select / combobox / tag input | `Selector`, `MultiSelector`, `ComplexSelector`, `Typeahead`, `Tokenizer` |
| A modal / confirmation dialog | `Dialog`, `AlertDialog` |
| A dropdown / context / command menu | `DropdownMenu`, `ContextMenu`, `MoreMenu`, `CommandPalette` |
| Status / count / label chip | `Badge`, `StatusDot`, `Token`, `Indicator` |
| Tooltip / hover detail / anchored popup | `Tooltip`, `HoverCard`, `Popover` |
| Date/time entry | `DateInput`, `DateRangeInput`, `DateTimeInput`, `TimeInput`, `Calendar` |
| Loading / empty state | `Skeleton`, `Spinner`, `ProgressBar`, `EmptyState` |
| Breadcrumbs / global search | `Breadcrumbs`, `PowerSearch` |
| Toggle / choice input | `Switch`, `CheckboxInput`, `CheckboxList`, `RadioList`, `SegmentedControl`, `ToggleButton` |

This table is a quick guide, not the catalog — run
`npx --no astryx component --list` for every component grouped by category, or
`npx --no astryx search` when nothing here fits.

## Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| Astryx components render unstyled/unthemed | `reset.css`/theme CSS not imported, or imported after other global CSS without layer ordering | Import reset + theme first in `main.tsx`; if the app has other global CSS, assign every stylesheet an explicit `@layer` (`npx --no astryx docs migration`) |
| `npm install` fails / peer-dependency warnings on React | `package.json` is on React <19 | Set `react`/`react-dom` to `^19` before installing `@astryxdesign/core` |
| Build succeeds but StyleX classes/styles don't apply | `astryxStylex()` missing from `vite.config.ts`, or ordered after `react()` | Add `...astryxStylex()` to `plugins`, listed before `react()` |
| Page renders blank in dev, every asset 404s | `base` was set in `vite.config.ts` from an Astryx snippet | Remove it — served at host root (`react-webapp`) |
| A prop doesn't exist, or is the old spelling | Answered from memory instead of the CLI | Run `npx --no astryx component <Name> --dense` — the CLI reflects the installed version, training data doesn't |
| Every row in a list is wrapped in its own `Card` | Defaulted to a generic "card grid" instead of checking data density | `npx --no astryx docs principles --dense` — dense data is `Table`/`List`+`Item`; `Card` is for widgets/galleries/settings groups |
| Brand colors are set, deployed app is stock-themed | Colors were read but never compiled into a theme, or `brandTheme.ts` was edited without re-running `theme build` | Re-run `npx --no astryx theme build src/theme/brandTheme.ts -o src/theme/brand.css` and confirm `brand.css`/`brand.js` are COMMITTED — the image build never runs the compiler |
| The user gave brand colors in chat, the build ignored them | A coding run never sees a conversation — colors reach it only from this skill or the project's `specs/requirements/prd.md` | Set them in The organization's colors (Settings → Skills) for the whole org, or under `## Brand colors` in the project's `specs/requirements/prd.md` for one project; an answer that is not in a file did not happen |
| Brand accent is unreadable — pale text on a pale button | The brand hex was pasted into both `[light, dark]` slots of `--color-accent` | Darken the light-mode slot and set `--color-on-accent` to a color that contrasts with the accent in each mode |

## Red flags — stop and use Astryx

- About to write `<div style={{...}}>` or a raw `className` for layout, color, or
  spacing
- About to `npm install` any other component or styling library
- About to write JSX for a form, list, card, dialog, nav, or button from scratch
- Thinking "it's just a placeholder" or "Astryx isn't set up in this app yet"
- Using a prop without having confirmed it exists via
  `astryx component <Name> --dense`
- About to satisfy a brand-color requirement by styling components instead of
  compiling a theme — or about to ignore one because no stock theme matches
- About to ask which theme or colors to use — that is settled in Brand colors,
  and "not set" means the stock theme, not an open question

All of these mean: stop, run `astryx search` / `astryx component <Name> --dense`,
and use what it returns.
