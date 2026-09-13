# Implementing wireframes (coding agent)

The companion to the `wireframes` skill for the coding run: how a
`wireframes.dsl` becomes routes, pages, elements and navigation, and how that
is evidenced. The grammar you are reading is in `SKILL.md`; this file is about
honouring it.

The wireframe is the **screen contract** for a `web-application` component.
The reviewer who validates the deployed app compares the rendered wireframe
with the running page, screen by screen; a missing screen, a dropped element
or a reshuffled layout is a defect at that stage. Read the DSL **before**
writing the first page, and build what it says.

**Read `specs/design/components/<name>/wireframes.dsl`** (never the
`.excalidraw` — that is the compiled picture). The Task's `## Scope` names
which screens are yours; the DSL is where their content lives, so it wins
over the issue text when the two differ.

## Screen for screen

- **One `screen` = one route/page.** Name the route and the page component
  after the screen (`screen RiskQueue` → `/risk-queue`, `RiskQueuePage`),
  so the mapping is legible in the PR. The screen's quoted description tells
  you what the view is for and who uses it — read it as your brief, not as
  page copy: the visible title is the screen's own `heading` element.
- **No invented screens, no dropped screens.** A view the DSL does not
  declare does not exist; a view it declares must exist even when it seems
  minor. If a screen is genuinely unbuildable as drawn (its data has no API,
  its action has no endpoint), build the rest of it, leave the gap explicit
  in the UI, and say so in the PR — never silently fold it into another
  page.
- **Every role gets its own screen.** Where the DSL declares a screen per
  role (`RiskQueue` for the manager, `MyRisks` for the owner), each is its
  own route with its own chrome, showing only the actions and columns its
  screen carries. Do not collapse two role screens into one page that toggles
  on the viewer. How you factor the code behind those routes is your call:
  share a component where the views genuinely overlap, as long as each route
  renders exactly what its screen shows.
  **`/forbidden` and `NoAccess` are platform-prescribed views and appear in no
  `.dsl`.** They are the one carve-out from "build only the screens the DSL
  draws": every app with an auth dependency has both, they come from
  `thunder-authentication`'s `authz` asset, and their absence is a defect even
  though no wireframe names them.

  Gate each screen on the handle `specs/design/security.json` gives it in
  `screens[].requires`, through `thunder-authentication`'s `authz` assets —
  copied verbatim, never re-implemented: `<RequireScope scope="…" />` around the
  route and `<Can scope="…">` around the nav item that reaches it, with the
  scope read from the generated `SCREENS` table rather than typed into JSX.
  `requires: null` is any signed-in user; `requires: "public"` is reachable
  before sign-in. Treat all of it as **presentation only** — the backend
  enforces the same handle and answers 403. A user who holds scopes but opens a
  gated screen by URL sees `Forbidden` **inside** the shell, rail intact; a user
  who can reach nothing at all sees `NoAccess` **instead of** the shell. Never
  read a groups claim, and never fall back to a default role. A component with
  no auth dependency has no roles: build the screen as drawn.

  The screen name `security.json` uses is the one a person reads (`"My Claims"`);
  the DSL cannot carry a space and spells it `MyClaims`. The two are matched
  **normalized** — strip non-alphanumerics, lowercase — and neither file changes
  spelling to suit the other.

## Element for element

Walk each `screen` block top to bottom and make every line a real element,
in that order, with that literal content:

| DSL | Build |
|---|---|
| `navbar "App"` / `sidebar "A -> S \| B"` | the pinned design system's canonical app shell — the brand in the top bar, the `sidebar` items as the navigation rail; each item that carries `-> S` links there. The DSL draws a different rail per role because it draws one role at a time; the app has **ONE** rail whose items are each wrapped in `<Can scope="…">`, which reproduces every one of those pictures and also covers a user holding two roles. Navigation stays in the rail even if a wireframe put a link in the `navbar` |
| `heading`, `text`, `link`, `breadcrumb` | the same words on the page |
| `card "Label \| Value \| Caption"` | a stat tile with that label, that value bound to live data, that caption |
| `table "A \| B \| C"` + `row` lines | a data table with exactly those columns, bound to the real list. A column the list endpoint does not return is filled from **one** more request to another list operation of the contract (a name joined on id from the entity list; a count from the child list filtered once and grouped) — never one request per row, and never dropped while some list operation can supply it. Only a column no list can supply is left out, with the gap named in your report |
| `list`, `tabs`, `badge`, `progress`, `avatar`, `chart`, `image` | the matching UI primitive — a status is a badge, not a paragraph |
| `input`, `textarea`, `select`, `search`, `checkbox`, `radio`, `toggle` | a real form control whose placeholder/label is the DSL label, wired to submit |
| `button "X" primary` | a primary-styled button; every button the DSL marks `primary` is primary on the page, and only those. Unmarked buttons take a non-primary style (destructive where the DSL says `danger`) |
| `row`, `split N/M`, nested `card` children | the same layout: side by side, two columns in that ratio, grouped inside the card |
| a `variant` (`danger`, `success`, …) | the design system's matching status colour |

Labels are **copy**, not hints: "Open risks", "Review next", "Notify owner
on create" appear on the page as written. Rename only when the wording is
wrong for the data the API actually returns, and say so in the PR.

**Empty, loading, and error states** are implied, not drawn: every `table`,
`list`, and data `card` needs what it shows with no rows, while fetching,
and when the request fails. A wireframe shows the happy path; the page
must not break off it. A region whose operation the viewing role cannot call
renders its own forbidden state — it does not vanish, and it is not the whole
screen's `Forbidden`.

**The example data is the mock's seed.** The reviewer compares the running
page against the rendered wireframe, so mock mode should show the `row`s the
DSL draws, and a stat `card`'s value should agree with the rows it counts.
Print them instead of retyping them:

```bash
node "${AEP_SKILLS_DIR:-.claude/skills}/wireframes/scripts/seed.mjs" specs/design/components/<name>/wireframes.dsl
```

That prints, per screen, every `table` (columns and its `row`s as records),
stat `card` (label, value, caption), `list`, `select` (label and preselected
value) and `badge` as JSON. Map each record onto the provider's schema in the
mock handlers and derive the stat values from those records. `AEP_SKILLS_DIR`
is where a run finds the skill library; the fallback is `.claude/skills/`, the
mirror the platform writes into every project, where this skill's `scripts/`
also live.

## Arrow for arrow

Every `-> Screen` — on a button, a link, a table, a navbar or sidebar item —
is **working navigation** to that screen's route — including the rail item
pointing at the screen it sits on, which is the active nav link and still a
link. A chrome item with no target names a section outside this wireframe set:
render it and leave it inert rather than inventing a destination for it.

Every `flow` block is a journey a role must be able to walk end to end by
clicking: entry screen first, each screen reachable from the one before. A
screen you cannot reach from its flow is a broken page, even if it renders.

## The evidence

Two lists, one pair. The **Task** carries a `Screens:` line and a `Flows:`
checklist with every box unchecked — that is what must be built, and it is
what you work from. The **PR body** carries the same two lists with the boxes
earned ticked. You never tick the Task's copy; re-planning rewrites that body.

The ticks are **evidence, not a claim**, and they come from the walk. Once the
build is clean, `mock-verification` opens the app in a browser, reaches every
screen, uses every drawn control and follows every arrow, and reports one line
per flow and per screen; the lead ticks the PR's lists from that report when it
opens the PR. You write no checklist of your own — a screen you could not build
as drawn is a gap you name in your report to the lead, and the walk records it.

```text
Wireframe fidelity (specs/design/components/<name>/wireframes.dsl)

Screens
- [x] RiskQueue → /risk-queue
- [x] MyRisks → /my-risks
- [ ] NewRisk → /risks/new — "Register" select not built: no registers endpoint

Flows
- [x] F1 · Approval queue — stories 2, 5
- [ ] F2 · Log a risk — stories 1, 3 — breaks at NewRisk → RiskDetail: the
      "Register" select is not built
```

**Screens** come from the Task's `Screens:` line, one line each, naming the
route built. A screen is ticked when the walk reached it, every drawn control
acted and every arrow navigated.

**Flows** carry over from the Task's `Flows:` checklist, keeping each one's
number, name and story set — one line each here, not the Task's labelled
block, since its persona and `Walk:` chain are recorded there. A flow is ticked
when the walk went through its chain end to end by clicking, entry screen
first; otherwise the line names the step it broke at. A flow the Task lists
appears here even when it could not be built: dropping the line is how a
missing journey goes unnoticed, and an unwalkable flow strands the stories it
carries.

Layout and copy are outside the walk. They stay the reviewer's comparison of
the rendered wireframe against the running page.
