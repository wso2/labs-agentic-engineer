# Implementing wireframes (coding agent)

The companion to the `wireframes` skill for the coding run: how a
`wireframes.dsl` becomes routes, pages, elements and navigation, and the
fidelity checklist the PR carries. The grammar you are reading is in
`SKILL.md`; this file is about honouring it.

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
  Read the role in the SPA from the sign-in identity the auth
  dependency provides (`user.profile.groups`; see `thunder-authentication`),
  and treat it as **presentation only** — the backend enforces permission and
  answers 403. A component with no auth dependency has no roles: build the
  screen as drawn.

## Element for element

Walk each `screen` block top to bottom and make every line a real element,
in that order, with that literal content:

| DSL | Build |
|---|---|
| `navbar "App \| Nav -> S"` / `sidebar "…"` | the app's real navigation chrome, identical on every screen of a role; each item that carries `-> S` links there |
| `heading`, `text`, `link`, `breadcrumb` | the same words on the page |
| `card "Label \| Value \| Caption"` | a stat tile with that label, that value bound to live data, that caption |
| `table "A \| B \| C"` + `row` lines | a data table with exactly those columns; the `row`s are example data, so bind the table to the real list |
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
must not break off it.

## Arrow for arrow

Every `-> Screen` — on a button, a link, a table, a navbar or sidebar item —
is **working navigation** to that screen's route — including the rail item
pointing at the screen it sits on, which is the active nav link and still a
link. A chrome item with no target names a section outside this wireframe set:
render it and leave it inert rather than inventing a destination for it.

Every `flow` block is a journey a role must be able to walk end to end by
clicking: entry screen first, each screen reachable from the one before. A
screen you cannot reach from its flow is a broken page, even if it renders.

## The fidelity checklist

The checklist is **evidence, not a claim**. A web application is opened and
walked in a browser before its work is committed — mock mode, no cluster behind
it, one verdict per story (`mock-verification`) — so a tick here means the screen
was rendered and its arrows were clicked, never that the code looks as though it
would. Write it once the walk has settled, and put it in the PR body — one
line per screen, then one per `flow` — with any gap named beside it:

```text
Wireframe fidelity (specs/design/components/<name>/wireframes.dsl)

Screens
- [x] RiskQueue → /risk-queue — all elements; "Review next" → QueueRiskDetail
- [x] MyRisks → /my-risks — all elements; table → RiskDetail
- [ ] NewRisk → /risks/new — "Register" select not built: no registers endpoint

Flows
- [x] "Approval queue" (Manager) — RiskQueue → QueueRiskDetail walks by clicking
- [ ] "Log a risk" (Risk owner) — MyRisks → NewRisk walks; NewRisk → RiskDetail
      blocked on the missing select
```

A screen is ticked only when every element in its block was on the page and
every arrow it carries navigated when clicked. A flow is ticked only when it was
walked end to end by clicking, entry screen first — an unwalkable flow strands
the screens behind it, so the e2e test written against the acceptance criteria
cannot reach them either. An unticked line with its reason is fine — it tells
the reviewer exactly where to look, and a screen whose story could not be judged
against a mock says that in the same place.
