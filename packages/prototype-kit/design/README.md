# `@wso2/prototype-kit` — design notes

## What it is

The one import a generated prototype has besides React, and the machinery every
host needs. Subpaths: `.` (generated-code API + theme contract), `/manifest`,
`/source`, `/check`, `/host`, `/feedback`, `/build`. `/check` and `/manifest`
are React-free so the CLI runs without React installed.

## Stubs and themes

A kit component resolves its `id` (throws without one), wraps the theme's output
in a `SelectableBox` (or hands the theme `SelectableRootProps` for rows, tabs,
steps, navigation entries and crumbs), applies press semantics (`onPress`, then
`to`, Preview only) and renders `registry[Name]` with resolved props. Annotate
therefore behaves the same under every theme. `KitComponentProps` lists every
component's theme props; `ThemeRegistry` maps over it.

`<AppShell>` is a screen's root inside the product's chrome: product name,
signed-in user (role label defaults to the viewing role's name), side
navigation, and a user menu whose Account, Settings and Sign out entries open
screens the prototype names (`account`, `settings`, `signOut`; required, a
missing one throws), plus the prototype's own entries (`menu`). Entries the role cannot reach are not drawn, as for
`<Navigation>`. A theme keeps the closed menu's entries in the markup, so the
render check sees their `to` and ids. A bare `<Screen>` stays valid.

Grouping is the kit's, not the theme's guess. `<StatGroup>` lays its
`<Stat>`s at one width (a theme grid, wrapping); `<Section>` is a selectable
container with a title, optional `count` and `subtitle`, and its own `actions`.
A `<Stat>`'s `icon` is one of the kit's `StatIcon` names (Oxygen's, so Lucide's);
the stub throws on any other, so the check fails the same under every theme,
and a theme maps each name or draws none.

`<Table>` columns are strings or `{ label, kind }` (`text`, `number`,
`status`). The stub hands the theme one cell per column: `cells` fill the text
and number columns in order, the single status column takes the row's
`status`, and a legacy row `tone` (no status column) still badges the last
cell. A row's `actions` reach the theme with their own selectable roots and
press handlers; a selectable root claims clicks inside it except those on a
root nested in it, so Annotate selects the action, not its row. Keeping a
Preview press on an action from also pressing the row is the theme's (stop the
click in the actions cell).

## Store

Seeded from `defineApp({ data })`. An array whose records all have a string `id`
is a collection; anything else is a value. New ids are `${name}-${n}`, `n` past
the highest number any seeded or current id ends in. Every change emits a JSON
snapshot (the frame posts it as `proto:data`); a store can start from a
snapshot, per key, falling back to the seed for a key of the wrong kind.

## Forms

The frame has no `allow-forms`, so browsers never fire a submit event there. A
`<Button submit>` calls its `<Form>`'s submit through `FormSubmitContext`;
themes call `onSubmit` for Enter in a single-line field. Validation (`required`,
`pattern`) runs at submit and again as a failed form is edited.

## Checks

`checkPrototypeFiles` stops at the first failing stage: files, manifest
(JSON, version, shape, references), static source rules, literal `to=`/`go()`
targets, isolated render.

- Static global check: a name bound anywhere in the module (a local `history`)
  is not a global, so it is not flagged.
- Render child: gets only strings; its `vm` context is created from
  `Object.create(null)`, so the context's global has no host-realm prototype
  chain; it hardens `Object.prototype`, `Array.prototype` and
  `Function.prototype` before the module runs, so prototype pollution throws.
  `RENDER_TIMEOUT_MS` is 15 s. The child is spawned asynchronously, so
  `checkPrototypeFiles` and `checkPrototype` return promises and a host keeps
  its event loop while a prototype renders (the agents service serves other
  conversations; the CLI preview keeps answering).
- Limits of the render check: see ADR-0042, Consequences.

## Go mirror

The aep-api save gate (`services/aep-api/internal/platform/prototypespec`)
re-judges the files a hand push can bring: the manifest against an embedded
copy of `schema/prototype-manifest.schema.json` (byte-pinned by a vendoring
test), a Go port of `references.ts`, and a static floor for the source
(syntax, imports, size). It never renders. Drift is closed by shared tables the
kit asserts here and Go reads from `test/fixtures`: `manifest-cases.json`
(code, location and wording of every reference rule; schema rows share only the
code) and `source-floor-cases.json` (the floor's syntax, import and size rows,
plus the size cap). Change a rule in one place and a row fails in the other.

## Feedback (`/feedback`)

The one TS definition of a reviewer's requests: `FeedbackRequest`,
`FeedbackSubmission`, the limits (`MAX_FEEDBACK_REQUESTS` 50, `MAX_FEEDBACK_TEXT`
4000, `MAX_FEEDBACK_ID` 200, counted in UTF-16 code units),
`parseFeedbackSubmission`, the Annotate queue's `requestFor`/`pinsOnScreen`, and
`prototypeHash` (SHA-256 of manifest, NUL, source). The hash is plain
JavaScript and synchronous: Web Crypto's `crypto.subtle` exists only in secure
contexts, and a console served over plain HTTP must still name a revision. The
CLI, `@aep/agent-stream` and the console import it; the Go BFF and the OpenAPI
contract mirror it, held by `test/fixtures/feedback-cases.json`, which the kit,
agent-stream and Go all assert.

## Host reducer and bridge (`/host`)

The reducer owns view state. `NAVIGATE` only moves to a screen reachable for the
current role. `proto:data` snapshots are shape-validated with a bounded walk
(cycles rejected, node cap) before a host persists them (`isDataSnapshot`).
`PrototypeFrame` re-sends `load` on every frame `ready`, so a reloaded frame
recovers. The host ignores frame navigation outside Preview. Until the frame
first draws (`proto:rendered` or `proto:error`) after its latest `ready`,
`PrototypeFrame` covers it with its `loading` node (a plain "Loading the
prototype…" by default), so a click while the large runtime starts is not
silently lost. The frame reports what kills it: an inline prelude posts
`proto:error` for any uncaught error or rejection (the runtime failing as it
loads included), and a boundary around the kit root reports a theme Provider
that throws. A frame that neither draws nor errs within
`PROTOTYPE_START_TIMEOUT_MS` (30 s) gets a visible "didn't start" error
instead of the cover. `FrameView.colorScheme` (optional `light | dark`,
`PrototypeFrame`'s `colorScheme`) reaches the theme's Provider; absent, the
theme follows the system. `PrototypeWindow` (`/host`) is the shared browser-window chrome around the frame: title, dots and a read-only address (`prototype://<screenId>`, plus `?flow=&state=` when not default); hosts style it with `--proto-window-*` variables and `proto-window*` classes. The frame does not parse the manifest: the host passes a parsed
`PrototypeManifest`, which keeps zod (about 450 KB minified) out of every
frame runtime.

## Build helper

`buildThemeRuntimes({ theme, resolveDir, outDir, define? })` bundles a theme with the
kit and React into `frame-runtime.js` (entry `bundle/frame-entry`) and
`check-runtime.js` (entries `bundle/check-prelude`, `bundle/check-entry`; the
prelude gives the bare `vm` context the timers, `MessageChannel` and
`TextEncoder` that React looks for at load). A theme whose library expects a
bundler-provided name passes it as `define` (Oxygen: `global`); the kit prelude
stays library-neutral. The
source dir is `src/bundle`; the public subpaths stay `/build`, `/build/*`.
Generated: `schema/prototype-manifest.schema.json` and `reference.md`
(`pnpm --filter @wso2/prototype-kit gen`; `test/generated.test.ts` fails when
stale).
