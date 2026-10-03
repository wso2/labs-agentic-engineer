# ADR-0042 — A prototype is React on a fixed kit, run in a sandbox

**Status:** Accepted · 2026-10-02
**Supersedes:** the json-render prototype decision, "ADR-0035 — The prototype
is a json-render spec over a closed catalog", which exists only on the unmerged
v2 branch `feat/813-prototype-json-render` (on main, ADR-0035 is the agent
evaluation ADR; this ADR does not touch it).
**Related:** [console ADR-0001](../../apps/console/design/decisions/ADR-0001-prototype-review-is-a-full-screen-overlay.md)
(the console's review overlay; Preview acts, Annotate only selects). Feature:
#813, sub-project 1 (#856); AEP stage: #860.

## Context

A clickable prototype was either a full React app an agent writes from scratch
(expensive, different every time, unchecked) or a closed json-render catalog
(validated, but only view state changes: forms cannot submit and data never
carries across screens). The template-react rewrite was expressive but bound to
AEP: Oxygen, AEP paths, no CLI, unpublished.

## Decision

1. **Format.** A prototype is a folder: `prototype.json` (manifest v3: roles,
   display states, screens, flows, entry screen; no host-specific fields) and
   `prototype.tsx`, React that imports only `react` and `@wso2/prototype-kit`.
2. **A fixed kit owns the runtime.** `defineApp`, navigation/role/state hooks,
   a mock data store (collections with deterministic ids, single values, a
   fixed today) and neutral components (25 at first; the app shell made 26). A component is a stub: the kit owns
   element ids, selection and press semantics; a theme registry draws it. The
   registry is a mapped type, so a theme missing a component does not compile.
3. **Checked before anyone sees it.** `prototype check` runs, in order: the
   manifest's shape and references, static source rules (a name bound anywhere
   in the module is not a global), literal navigation targets, then every
   screen x role x state rendered in an isolated child (empty environment, Node
   permission model with no grants, heap and time caps, a `vm` context created
   from `Object.create(null)` without string code generation or wasm, frozen
   shared prototypes). Findings are a stable, documented code enum.
4. **Played in a sandbox.** The frame is `<iframe sandbox="allow-scripts">`
   from `srcdoc` with a no-network CSP (`'unsafe-eval'` only for the module
   loader); host and frame speak a validated message bridge. The host ignores
   frame navigation outside Preview, only moves to screens reachable for the
   current role, and shape-validates `proto:data` snapshots (bounded walk:
   cycles rejected, node cap) before persisting them. Mock-data persistence is
   the host's (`--persist`), because the frame has no origin.
5. **Three public packages.** `@wso2/prototype-kit`,
   `@wso2/prototype-theme-default` (plain React + CSS; no MUI) and
   `@wso2/prototype-cli` (`init`, `check`, `preview`, `export`).

## Consequences

- AEP's Prototype stage reuses the kit, manifest, checks, frame and reducer. It
  is wired in the console (`/prototype`, review and Annotate: [console
  design note](../../apps/console/design/prototype-review.md)). Nothing in a
  prototype names AEP.
- `@wso2/prototype-theme-oxygen` draws the kit on Oxygen UI's own layout kit
  (`AppShell`, `Header`, `Sidebar`, `PageTitle`, `PageContent`,
  `ListingTable`) under the console's theme (`@aep/ui-theme`, through
  `OxygenUIThemeProvider`), so a prototype looks like the console. Oxygen's
  runtimes cannot be tree-shaken: the frame is about 1.7 MB (630 KB gzip) and
  the check runtime 1.5 MB, against 0.44 MB and 0.24 MB for the default theme.
  The frame no longer parses the manifest (the host hands `PrototypeFrame` a
  parsed one), which keeps the schema validator out of it. The console loads
  the frame runtime lazily, on first review, and `PrototypeFrame` covers the
  frame with a loading state until the app first draws, so an early click is
  not lost; a frame that dies or stalls says so instead. The host's resolved
  colour scheme travels in the view, so a prototype is light or dark with the
  console.
- The kit has an app shell (`<AppShell>`: product, signed-in user, user menu
  with Account, Settings, Sign out and the prototype's own entries, side
  navigation), the 26th component,
  and the prototype skill makes it every screen's root. A prototype on bare
  `<Screen>`s stays valid. Its user menu's targets are required props, and a
  theme keeps the closed menu's entries in the markup so the render check
  verifies them.
- The stage is gated at three places, one rule set: the agent's write
  (`agent-stream`, with the isolated render check run by the agents service),
  the Go save gate (manifest schema, references and the static source floor,
  vendored and kept in step by shared test tables), and the build gate, which is
  unchanged. Typed `prototypeFeedback` on a `/prototype` turn is validated by
  Go before a turn opens.
- Network isolation of the render check rests on the permission model plus
  `vm`; a host-realm escape could still reach the network. Running it inside
  AEP needs an egress policy.
- The render check is asynchronous (a spawned child, 15 s cap): the agents
  service keeps serving other conversations while a prototype renders, and
  the turn's later writes and frames wait for its verdict, so the wire order
  is unchanged.
- The Go save gate has no render stage: a prototype that parses and references
  correctly but throws when drawn is stopped only by the agent's gate, so a
  write that bypasses the agent (an edit in the room) is not rendered.
- The agent's gate judges the pair whichever half is written: a
  `prototype.json` written beside an existing `prototype.tsx` is checked
  against it (references, then the render), so a manifest change that breaks
  the screens is refused.
- One TS definition of review feedback (`@wso2/prototype-kit/feedback`: shape,
  limits in UTF-16 code units, validator, Annotate helpers, revision hash) is
  imported by the CLI, `agent-stream` and the console; the Go BFF and the
  OpenAPI contract mirror it behind a shared test table. The hash is plain
  JavaScript, so it works where Web Crypto does not (plain HTTP).
- `'unsafe-eval'` in the frame is accepted: the frame has an opaque origin and
  no network. The CSP does not block the frame navigating itself (`location`).
- `check` does not render closed Dialog/Drawer contents unless a display state
  opens them, so those contents are unchecked until a state shows them.
- Each theme ships its own runtimes with React bundled; their size is watched.
- The prototype stays optional beside wireframes; making it what Build reads is
  sub-project 5.
