# Implementing a prototype (coding agent)

The companion to the `prototype` skill for the coding run: how a
`prototype.json` becomes routes, pages, elements and navigation, and how that
is evidenced. The document's shape is **The document** in `SKILL.md`; this
file is about honouring it.

The prototype is the **screen contract** for a `web-application` component.
The user reviewed it as an application before Build, and the reviewer who
validates the deployed app compares it with the running page, screen by
screen; a missing screen, a dropped element or a reshuffled layout is a defect
at that stage. Read the prototype **before** writing the first page, and build
what it says.

**Read `specs/design/components/<name>/prototype.json`.** The Task's
`## Scope` names which screens are yours; the prototype is where their content
lives, so it wins over the issue text when the two differ.

## Screen for screen

- **One `screens[]` entry = one route/page.** Name the route and the page
  component after the screen's id (`screen.risk-queue` → `/risk-queue`,
  `RiskQueuePage`), so the mapping is legible in the PR. The screen's `name`
  is what the rail and the `Forbidden` copy call it.
- **No invented screens, no dropped screens.** A view the prototype does not
  declare does not exist; a view it declares must exist even when it seems
  minor. If a screen is genuinely unbuildable as drawn (its data has no API,
  its action has no endpoint), build the rest of it, leave the gap explicit
  in the UI, and say so in the PR — never silently fold it into another
  page. A dialog or drawer in a screen's `overlays` is part of that screen,
  not a route of its own.
- **Every role gets its own screen.** A screen's `roleIds` name the roles that
  reach it, and they are `security.json`'s role names. Where the prototype
  declares a screen per role (`screen.risk-queue` for the manager,
  `screen.my-risks` for the owner), each is its own route with its own
  content, showing only the actions and columns its screen carries. Do not
  collapse two role screens into one page that toggles on the viewer. How you
  factor the code behind those routes is your call: share a component where
  the views genuinely overlap, as long as each route renders exactly what its
  screen shows.
  **`/forbidden` and `NoAccess` are platform-prescribed views and appear in no
  prototype.** They are the one carve-out from "build only the screens the
  prototype declares": every app with an auth dependency has both, they come
  from `thunder-authentication`'s `authz` asset, and their absence is a defect
  even though no prototype names them.

  Gate each screen on the **operation it loads** — the call whose answer the
  screen renders on open — through `thunder-authentication`'s `authz` assets,
  copied verbatim and never re-implemented: name the operation once in
  `src/authz/screens.ts` (`loads: "GET /me/claims"`), then
  `<RequireOperation op={…} />` around the route and `<Can op={…}>` around the
  nav item that reaches it, both reading the generated operations table rather
  than a handle typed into JSX. A form with no load call names the operation its
  submit makes, so the rail, the route and the button agree; `loads: null` is
  only for a screen that needs no operation at all. Treat all of it as
  **presentation only** — the gateway enforces the operation's scope and
  answers 401 whatever the reason. A user who holds scopes but opens a gated
  screen by URL sees `Forbidden` **inside** the shell, rail intact; a user who
  can reach nothing at all sees `NoAccess` **instead of** the shell. Never read
  a groups claim, and never fall back to a default role. A component with no
  auth dependency has no roles: build the screen as drawn.

## Node for node

Walk each screen's `content` in order and make every node a real element, in
that order, with that literal content:

| Prototype | Build |
|---|---|
| `navigation` (`side-nav` or `top-nav`) named by a screen's `navigationId` | the pinned design system's canonical app shell — the prototype's `name` as the brand, the items as the navigation rail; each item's `navigate` action links to that screen's route. The prototype filters items by `roleIds` one role at a time; the app has **ONE** rail whose items are each wrapped in `<Can …>`, which reproduces every role's view and also covers a user holding two roles. Navigation stays in the rail whatever the `kind` |
| a screen with no `navigationId` | a page without the shell — an app-owned sign-in, the one screen drawn without chrome |
| `heading` with `actions` | the page title with those buttons as its actions |
| `heading`, `text`, `link`, `breadcrumbs` | the same words on the page; a link or breadcrumb with an action navigates |
| `stat` | a stat tile with that label and its value bound to live data |
| `detail` | the record's fields with those labels, bound to the detail operation |
| `table`, `task-queue` | a data table with exactly those `columns`, bound to the real list; `onRow` makes each row open its target. A column the list endpoint does not return is filled from **one** more request to another list operation of the contract (a name joined on id from the entity list; a count from the child list filtered once and grouped) — never one request per row, and never dropped while some list operation can supply it. Only a column no list can supply is left out, with the gap named in your report |
| `filters` | filter controls with those fields, applied to the list they sit above |
| `form` + `field`s | a real form whose controls follow each field's `type` (`text`, `number`, `select` with its `options`, `date`, `textarea`, `switch`) and `label`, wired to submit through the operation the form's primary action performs |
| `validation-summary`, a field's `error` | the form's real validation presentation for invalid input |
| `button`, a button in `actions` | `emphasis: "primary"` is a primary-styled button, and only those are; `"danger"` is destructive; the rest take the neutral style |
| `approval-panel` | the decision: its summary and one control per action, each calling the operation that decision makes |
| `timeline` | the record's history, from the operation that returns it |
| `badge`, `alert`, a row's `tone` | the matching UI primitive in the design system's status colour — a status is a badge, not a paragraph |
| `empty-state` | the list's own empty state, with that copy |
| `tabs`, `stepper` | tabs, or a multi-step form, with those labels and each tab's or step's content |
| `stack`, `grid`, `split` | the same layout: stacked or side by side, that many columns, two panes in that `ratio` of 12 |
| `dialog`, `drawer` in `overlays` | a real dialog or side panel on that screen, opened by the control whose action shows it |

Labels are **copy**, not hints: "Open risks", "Review next", "Notify owner
on create" appear on the page as written. Rename only when the wording is
wrong for the data the API actually returns, and say so in the PR.

**Display states are the app's real states.** A node with `showIn` belongs to
the display states it names: an `empty-state` shown in an empty state is what
the page shows with no rows, an `alert` shown in a failed state is what it
shows when the request fails, a field's `error` is what invalid input shows.
Build each as the page's genuine response to that condition — never as a
switch the user flips. States the prototype does not draw are still owed:
every table, list and data region needs what it shows while fetching and when
its request fails. A region whose operation the viewing role cannot call
renders its own forbidden state — it does not vanish, and it is not the whole
screen's `Forbidden`.

**The mock records are the mock's seed.** The reviewer compares the running
page against the prototype, so mock mode shows the same records: a table's
`rows`, a `detail`'s values, a `timeline`'s entries. They are already JSON in
the file — map each record onto the provider's schema in the mock handlers
rather than retyping it, and derive every `stat` value from those records so
the numbers agree with the table.

## Action for action

The prototype is read-only, so its actions only move the view; the app's
controls do their real work **and** land where the action says:

| Action | Build |
|---|---|
| `navigate` | working navigation to that screen's route — including a rail item pointing at the screen it sits on, which is the active nav link and still a link |
| `show-dialog`, `show-drawer`, `close-overlay` | the control opens or closes that overlay |
| `set-tab`, `set-step` | the control switches to that tab or step |
| `select-row` | the control selects that row of its table |

A control with a submit behind it (a form's primary button, an approval
decision) calls its operation first, then goes where its action points.

Every `flows[]` entry is a journey its role must be able to walk end to end by
clicking: its first screen first, each screen reachable from the one before.
A screen you cannot reach from its flow is a broken page, even if it renders.

## The evidence

Two lists, one pair. The **Task** carries a `Screens:` line and a `Flows:`
checklist with every box unchecked — that is what must be built, and it is
what you work from. The **PR body** carries the same two lists with the boxes
earned ticked. You never tick the Task's copy; re-planning rewrites that body.

The ticks are **evidence, not a claim**, and they come from the walk. Once the
build is clean, `mock-verification` opens the app in a browser, reaches every
screen, uses every drawn control and follows every action, and reports one
line per flow and per screen; the lead ticks the PR's lists from that report
when it opens the PR. You write no checklist of your own — a screen you could
not build as drawn is a gap you name in your report to the lead, and the walk
records it.

```text
Prototype fidelity (specs/design/components/<name>/prototype.json)

Screens
- [x] screen.risk-queue → /risk-queue
- [x] screen.my-risks → /my-risks
- [ ] screen.new-risk → /new-risk — "Register" field not built: no registers endpoint

Flows
- [x] F1 · Approval queue — stories 2, 5
- [ ] F2 · Log a risk — stories 1, 3 — breaks at screen.new-risk → screen.risk-detail:
      the "Register" field is not built
```

**Screens** come from the Task's `Screens:` line, one line each, naming the
route built. A screen is ticked when the walk reached it, every drawn control
acted and every action landed.

**Flows** carry over from the Task's `Flows:` checklist, keeping each one's
number, name and story set — one line each here, not the Task's labelled
block, since its persona and `Walk:` chain are recorded there. A flow is ticked
when the walk went through its chain end to end by clicking, first screen
first; otherwise the line names the step it broke at. A flow the Task lists
appears here even when it could not be built: dropping the line is how a
missing journey goes unnoticed, and an unwalkable flow strands the stories it
carries.

Layout and copy are outside the walk. They stay the reviewer's comparison of
the prototype against the running page.
