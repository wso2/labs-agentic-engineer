---
name: wireframes
description: Use when creating or updating UI wireframes for a webapp component — sketching screens, forms, tables, dashboards, or navigation flow.
metadata:
  aep:
    kind: platform
    audience: [design, coding]
---

# Wireframes (Excalidraw DSL)

Every `web-application` component gets its wireframes as ONE DSL source file at
`specs/design/components/<name>/wireframes.dsl` — all screens in one file.
Write ONLY the `.dsl`; the platform compiles it to the `.excalidraw`
deterministically. NEVER write a `.excalidraw` file by hand.

Wireframes are **low-fidelity but product-flavored**: they validate layout,
hierarchy, and flow, not pixel-perfect visuals. The compiler applies the look
(Oxygen UI palette, hand-drawn style) AND computes every position — **you
write structure and content, never coordinates**. Elements stack top-to-bottom
by default; `row` puts things side by side; `split` makes two columns. The
compiler guarantees nothing overlaps and nothing leaves the screen.

Produce **one canonical set of screens** — the agreed design, not a gallery of
alternatives. One screen per distinct user task, per role (for a store: a
product list, a product detail page, a checkout form). Don't ship two takes on
the same screen.

### Where the screens come from — read the design first

You have up to three sources in context; read them in this order of priority:

1. **`specs/design/design.md`** — the architecture doc for the whole system.
   This is your **primary** source for screens: it names the user roles, each
   component's responsibilities, and the main flows. Derive the screen list from
   here first. (It may not exist on every turn — if it's absent, promote the
   requirements to primary.)
2. **`specs/requirements/`** (requirements / user stories) — the **detailed**
   source. Use it to flesh out each screen and to catch tasks the design doc
   only summarized: specific fields, states, rules, and edge cases (out-of-
   stock, guest vs. signed-in, validation errors).
3. **This component's `specs/design/components/<name>/design.json`** — a thin
   per-component summary; use it mainly to **scope**, not for screen content:
   its `type` (draw wireframes only for `web-application`), its one-line
   `description`, and its `dependencies` (e.g. an auth dependency means there's
   a signed-in vs. guest distinction → likely role-specific screens).

**Cover every task.** Walk the design and requirements and make sure each
distinct user-facing task — for each role they name — has a wireframe screen
that serves it; nothing user-facing should be left without a view. Equally,
don't invent screens the design doesn't imply. A quick check: list the
tasks/roles, and for each name the screen that fulfills it — a task with no
screen is a gap, a screen with no task is noise.

## What makes a wireframe good

A good wireframe reads like a real screen someone could use, and it explains
itself. What carries the quality:

- **Real content, not placeholders.** Write "Open risks: 24", "Platform team",
  "Overdue" — never "text here" or "label". The wireframe is a communication
  artifact; concrete content is what makes a layout legible and reviewable.
- **Say what each view is.** Give every screen a description (the quoted phrase
  after its name) so anyone can tell at a glance what the view does and who
  uses it — not just infer it from the widgets.
- **A clear visual hierarchy.** A page has a title, then primary content, then
  secondary detail. Use `heading` for section titles; put the most important
  thing first (it renders highest). One dominant action per screen (the one
  `primary` button).
- **The right primitive for each thing.** A status is a `badge`, not text. A
  section switch is `tabs`. A person is an `avatar`. Picking the primitive that
  matches the real UI element is most of what makes screens feel real.

## Roles change the screens — always show them

Most real apps have more than one kind of user, and the *same feature looks
different for each*. This is the single most common thing wireframes get wrong:
they show one generic view and hide the fact that an admin and a regular user
actually see different screens. Don't do that.

**First, identify the roles.** Read `design.md` for distinct user types —
admin/manager/owner vs. member/employee/developer vs. viewer/customer (the
design doc usually spells the roles out; the requirements add the detail, and a
`design.json` auth dependency is a strong hint a signed-in role exists). If the
app has more than one, roles are in scope even when the prompt doesn't say "per
role."

**Then, for each role, show its main view and how it differs.** At minimum,
give every role its own `screen` for the primary task they do, named and
described for that role — because the difference is usually *capability*, not
cosmetics. The same feature splits into genuinely different screens:

- The one who **approves/administers** gets a queue or roster with the action
  and the columns to act on (Approve/Reject, an "assignee" column, bulk
  controls); the one who **submits/contributes** sees only their own items with
  a single Submit/Upload action — no approve, no assign.
- The one who **owns** a record edits it (an Edit form, private stats, a
  Reassign control); the one who **consumes** it sees the same record read-only,
  able at most to comment or take the one action meant for them.

Two roles, two screens — the admin's screen simply *has* buttons and columns the
member's does not. Show both.

Rules of thumb:

- Name the screen for its role and put the role in the description:
  `screen ReviewQueue "Manager reviews and approves pending requests"`.
- **Scope the chrome to the role.** An admin screen's `sidebar` lists the
  admin's destinations; a customer screen's lists the customer's. Real
  permissioned apps do not show people links they cannot use, and a prototype
  that does sends the reviewer down another persona's path.
- **Keep the overlap identical.** Items both roles have use the same label,
  the same order, and the same `navbar` brand, so the screens still read as one
  product rather than two. Only the role-specific entries differ.
- Reflect the real difference in **actions and data** — a role that can't
  approve/assign/delete simply doesn't have that button or column. Don't reskin
  one layout and call it two. The role difference should be legible from the
  screen itself, not spelled out in a caption.
- A screen that is genuinely identical for everyone (a shared login, a generic
  detail page) stays single — don't fork it just to have a matching set.

## Proven screen anatomies

Most webapp screens are one of three shapes. Follow these anatomies — they're
what makes a wireframe read like a designed product instead of a pile of boxes.
(Every `navbar` includes a notification bell + account avatar at its top-right:
the compiler draws this account cluster as part of the `navbar` itself — don't
add your own.)

**Dashboard / landing** (top → bottom):

1. A small muted eyebrow (`text`, e.g. the team or context: "OPERATIONS"), then
   a `row` with a human `heading` ("Good morning — here's where things stand")
   and, after `right`, a `search` and a filter `select`.
2. A `row` of 3–4 stat-tile `card`s — `card "Open items | 47 | across 5 active
   projects"`. Every number gets its label and a caption that explains it. The
   row makes them equal-width automatically.
3. A `row` of rich entity cards (one per project/order/record): a `card` whose
   title is the entity, with nested children — a `progress "47/60"`, a meta
   `text` line ("47 of 60 tasks · Due 14 Sep"), and a status `badge`
   (`success`/`warning`: "On track"/"At risk") that docks to the card's corner
   automatically.
4. A `row` with a section `heading` ("Needs your attention") and, after
   `right`, the page's primary CTA. Then a `table` whose LAST column is the
   action — put "Review →" / "Follow up →" in each `row`'s final cell.

**List / queue**: `heading`, then a `row` of filter `badge`s with counts
("All (146)", "Open (23)", "Resolved (98)") or `tabs`, then a full-width
`table` with real `row`s — status as a word in a status column, owner, due
date, and a trailing action cell.

**Detail / record** (the screen for ONE item): `breadcrumb`, then a `row` with
the `heading` and its status `badge`s, a short description `text` — then a
`split 60/40`:

- **`left` (main)**: the item's content — a bordered panel `card` with detail
  `text` lines, an items/records `table` with rows, an "Upload new" / primary
  action.
- **`right` (rail)**: the collaboration side — a "Discussion" `card` with
  nested comment `text` lines (author + time + message), a `textarea` + "Post"
  `button`, then an "Activity" `heading` and timestamped `text` rows ("2 days
  ago — J. Alvarez uploaded report-final.pdf").

A record's conversation and history belong in this right rail, beside the
record — not on a separate screen. The divider between the columns is drawn
automatically. Keep each comment `text` SHORT (a phrase, not a sentence).

## The DSL

Line-oriented, nested by 2-space indentation. **No coordinates anywhere** —
position comes from structure:

```text
screen <Name> ["what this view is for"]   // one per view; description renders as a subtitle
  navbar "App | Nav1 -> Screen | Nav2"    // top bar; first item is the brand; bell+avatar automatic
  sidebar "Item1 -> Screen | Item2"       // left rail; same items on every screen a role sees
  <kind> "<label>" [WxH] [variant] [-> Screen]   // a block: stacks below the previous one
  row                            // children go side by side (equal shares, 16px gaps)
    <kind> "<label>" …
    right                        // everything after this packs to the right edge
    <kind> "<label>" …
  split 60/40                    // two columns + automatic vertical divider
    left
      <blocks…>                  // each column is its own stack (rows allowed)
    right
      <blocks…>
  card "Title"                   // a card with nested children lays them out inside
    <elements…>                  //   …a badge child docks to the card's top-right
    row                          //   …and a `row` lays children side by side IN the card
      <elements…>
  table "Col1 | Col2" [-> Screen]
    row "cell | cell"            // table data — quoted `row` lines belong to the table
```

**The six rules of layout** (everything else is automatic):

1. Blocks **stack** top-to-bottom in the order you write them.
2. `row` puts children **side by side** — flexible things (cards, inputs,
   selects, charts, tables) share the width equally; small things (badges,
   buttons) keep their natural size. A row can never overflow.
3. `right` inside a row **right-aligns** what follows — header CTAs, the
   search+filter pair, footer Cancel/Save (primary rightmost).
4. `split N/M` + `left`/`right` makes **two columns** with the divider drawn
   for you.
5. Children indented under a `card` render **inside** it; the card grows to
   fit; a `badge` child docks to its top-right corner; a nested `row` lays
   children side by side within the card (two stats, a label+value pair).
6. `WxH` is optional and rarely needed (a taller `chart "…" 600x260`); widths
   are clamped to fit. The screen grows downward if content is long.

**Navigation** is attached to the control that triggers it: put `-> ScreenName`
at the end of the button (or link, or table) that leads there, and the compiler
draws a `→ Screen N · ScreenName` marker beside it. The target must be a
`screen` name that exists, and it must be a **different** screen — an arrow
pointing back at the screen it sits on says "go to where you already are", and
the prototype drops it. If the action opens a picker, modal or form, that is a
view — give it its own `screen` and point at that. When two buttons sit in a
row, put the `->` on the primary/forward one.

**Chrome navigates too.** A `navbar` or `sidebar` item may carry its own
`-> ScreenName`, and that is what makes an app walkable: the rail is how a real
user reaches Templates, History or Settings, so annotate every item that names a
view. The arrow goes inside the label, on the item itself, before the closing
quote (`"Home | Templates -> Templates"`) — one after the quote attaches to
nothing and silently does not navigate. The first `navbar` item is the brand
and takes no target; put arrows on the links after it. The rail repeats on
each screen, so annotate it each time; an item pointing at the screen it sits
on is correctly inert.

**Decide the screens and the paths together.** A wireframe set is a flow, not a
gallery: when you pick the screens, work out how each one is reached. Every
screen should be reachable by clicking — or be a landing screen whose
description says which role it serves. Not every control needs an arrow; what
matters is that no view is stranded.

### Flows — one per role or journey

The screens say what exists; a **flow** says who walks which ones, in what
order. The prototype's top-level control is the flow picker, so a wireframe set
without flows offers the reviewer no way to ask for the admin's journey.

Declare one `flow` block per role or journey named in `design.md` (or in
`specs/requirements/` when the design doc is absent), listing that
role's screens in walkthrough order, **entry screen first**. Name the flow for
its **task** ("Approval queue", "Log a risk"), and carry the persona on a
`role` line — not in the name:

```text
flow "Approval queue"
  role "Admin"
  description "An admin reviews queued items and audits the outcome"
  Login
  AdminQueue
  AuditDetail

flow "My orders"
  role "Customer"
  description "A signed-in customer checks a placed order"
  Login          // a reference, not a copy — one screen, two memberships
  Orders
```

- A flow **references** screens by name; screens stay declared once. List a
  shared screen (sign-in, a sign-out landing) in every flow that reaches it.
- The name is quoted and must be unique — declaring one flow twice rejects the
  write.
- A name that matches no `screen` rejects the write with its line number.
- `role "…"` names the persona who walks the flow; `description "…"` says what
  the journey is, like a screen's description says what the view is. Both are
  keyword lines inside the block, at most one of each (a duplicate rejects the
  write). Give every role-serving flow its `role`; a genuinely role-less
  journey (a public checkout) may omit it.
- A screen in no flow is allowed, but ask yourself who reaches it.

Syntax is validated at write time: an unknown keyword, a misplaced
`left`/`right`/table-`row`, or old-style x,y coordinates rejects the write with
line numbers (`INVALID_DSL`) — fix every listed line and re-emit the file.

### Element kinds

Chrome & structure: `navbar`, `sidebar`, `tabs` (`"A | B | C"`, first active),
`breadcrumb` (`"Projects / Acme / Settings"`), `divider` (a horizontal rule;
column dividers come from `split` automatically).

Content & data:

| Kind | Use for |
|---|---|
| `heading` | page / section titles (renders large with an underline rule) |
| `text` | body copy, values, helper text |
| `link` | inline navigation text (renders blue) |
| `card` | stat tile — `"Open items \| 47 \| across 5 audits"` (label, BIG value, caption); one-part label = a panel; nested children = a container |
| `table` | data grids; label = `\|`-separated headers; nested `row "…"` lines for data |
| `list` | stacked rows (feed, comments, nav); `\|`-separated items |
| `image` | logos, photos, media slots (renders a crossed box) |
| `chart` | data viz placeholder (renders axes + bars) |
| `progress` | progress bar; label `"60%"`/`"3/4"` sets the fill |
| `badge` | status pills — `"Open"`, `"Overdue"` (color via variant) |
| `avatar` | a person — label `"Jane Doe"` renders initials `JD` |
| `icon` | a small glyph slot; label is 1–2 chars |

Inputs & controls: `input` (label = placeholder), `textarea`, `select`
(renders ▾), `search` (renders ⌕), `button`, `checkbox` / `radio` / `toggle`
(add `active` to show selected/on). Generic `rect` / `ellipse` only when
nothing above fits.

### Color

The compiler applies the Oxygen theme automatically: white/neutral surfaces,
brand orange on the active navbar/sidebar item and section-heading rules. You
add color only through a trailing **variant**, to carry *status meaning*:

- `primary` on the ONE main action per screen — fills brand orange. Every
  other button stays a plain outline.
- `danger` / `success` / `warning` / `info` on a `badge` or destructive
  `button` (`badge "Overdue" danger`).
- `ai` (purple) for an automated / AI-driven step, if the product has one.
- `active` marks a `checkbox` / `radio` / `toggle` as selected / on.
- `muted` for de-emphasized text (an eyebrow label).

Keep status variants purposeful — a screen dense with red/green/amber badges
stops communicating.

### Consistency rules

- **One primary navigation, not two.** A content-heavy tool (admin console,
  dashboard app) uses the `sidebar` for section links and a brand-only
  `navbar` (`navbar "Acme"`). A simple public flow (storefront, checkout) uses
  a link-carrying `navbar` and **no sidebar**. Never both on one screen.
- Repeat the SAME `navbar` on every screen of one app. Repeat the SAME
  `sidebar` too, EXCEPT where a role's screens scope it to that role's
  destinations — the items both roles share still keep the same label and
  order, so the screens read as one product even when the rail isn't
  word-for-word identical.
- Comments start with `//`. Every screen should be reachable from some
  control's `-> Screen`.

## Worked example — risk register webapp wireframes

A complete `wireframes.dsl` for a two-role desktop flow. The manager and the
owner each get their own dashboard and detail screen — a shared `RiskDetail`
would need two different sidebars, and a screen can only carry one, so each
role's landing view and remediation view are split in two. `navbar` stays
brand-only and identical everywhere; each `sidebar` lists only that role's
destinations, and the items both roles have (Overview, All Registers, Audits,
Settings) keep the same label and the same order — only the role-specific
entry (Review Queue vs. My Risks) differs. Note the rest of the rhythm: blocks
stack in reading order; `row` groups things side by side; the primary action
is the one `primary` button per screen; status is carried by `badge`s, not
prose. No coordinates anywhere — the compiler computes every position.

```text
// Risk register — two roles, five screens, desktop

screen RiskQueue "Manager monitors open risk across registers and acts on what's overdue"
  navbar "RiskHub"
  sidebar "Overview | Review Queue -> RiskQueue | All Registers | Audits | Settings"
  row
    heading "Risk Queue"
    right
    select "Register: All"
  row
    card "Open risks | 24 | across 6 registers"
    card "Overdue actions | 6 | need follow-up"
    card "High severity | 3 | review this week"
  row
    heading "Needs review"
    right
    button "Review next" primary -> QueueRiskDetail
  tabs "All | Overdue | High severity"
  table "Risk | Owner | Severity | Status | Updated" -> QueueRiskDetail
    row "Unpatched edge servers | Platform team | High | Open | 2h ago"
    row "Stale access keys | Security | Medium | In review | 1d ago"
    row "Vendor SOC2 lapse | Compliance | High | Overdue | 3d ago"

screen MyRisks "Owner tracks the risks they own and logs new ones"
  navbar "RiskHub"
  sidebar "Overview | My Risks -> MyRisks | All Registers | Audits | Settings"
  row
    heading "My Risks"
    right
    button "New risk" primary -> NewRisk
  table "Risk | Severity | Status | Updated" -> RiskDetail
    row "Unpatched edge servers | High | Open | 2h ago"
    row "Rotate edge certs | Medium | In progress | 1d ago"

screen NewRisk "An owner logs a new risk into a register"
  navbar "RiskHub"
  sidebar "Overview | My Risks -> MyRisks | All Registers | Audits | Settings"
  breadcrumb "My Risks / New risk"
  heading "New Risk"
  input "Title — e.g. Unpatched edge servers"
  textarea "What is the risk and why does it matter?"
  row
    select "Register: Infrastructure"
    select "Owner: Platform team"
  row
    select "Impact: High"
    select "Likelihood: Likely"
  checkbox "Notify owner on create" active
  row
    right
    button "Cancel"
    button "Create risk" primary -> MyRisks

screen QueueRiskDetail "Manager reviews progress and escalates risks that stall"
  navbar "RiskHub"
  sidebar "Overview | Review Queue -> RiskQueue | All Registers | Audits | Settings"
  breadcrumb "Risk Queue / Unpatched edge servers"
  row
    heading "Unpatched edge servers"
    badge "High" danger
    badge "Open" info
  text "Owner: Platform team — Updated 2h ago"
  split 60/40
    left
      heading "Remediation"
      progress "60%" info
      text "6 of 10 actions complete"
      table "Action | Assignee | Due | Status"
        row "Patch kernel CVE-2026-1 | A. Chen | Fri | Done"
        row "Rotate edge certs | M. Diaz | Mon | In progress"
        row "Close inbound 8443 | Platform | Tue | To do"
      row
        right
        button "Reassign owner"
        button "Escalate" primary
    right
      card "Review notes"
        text "You · 3d: second week overdue — needs a date."
        text "R. Osei · 1d: agreed, escalate if Monday slips."
        row
          textarea "Add a review note…"
          button "Post note"
      heading "Review history"
      text "1d ago — You flagged this risk for review"
      text "6d ago — R. Osei reassigned it to Platform team"

screen RiskDetail "The owner tracks remediation for the risks they own"
  navbar "RiskHub"
  sidebar "Overview | My Risks -> MyRisks | All Registers | Audits | Settings"
  breadcrumb "My Risks / Unpatched edge servers"
  row
    heading "Unpatched edge servers"
    badge "High" danger
    badge "Open" info
  text "Owner: Platform team — Updated 2h ago"
  split 60/40
    left
      heading "Remediation"
      progress "60%" info
      text "6 of 10 actions complete"
      table "Action | Assignee | Due | Status"
        row "Patch kernel CVE-2026-1 | A. Chen | Fri | Done"
        row "Rotate edge certs | M. Diaz | Mon | In progress"
        row "Close inbound 8443 | Platform | Tue | To do"
      row
        right
        button "Update status" primary
    right
      card "Discussion"
        text "K. Smith · 2d: when does the cert rotation land?"
        text "M. Diaz · 1d: Monday, after the freeze."
        row
          textarea "Add a comment…"
          button "Post"
      heading "Activity"
      text "2h ago — A. Chen closed CVE-2026-1"
      text "1d ago — M. Diaz started cert rotation"

flow "Approval queue"
  role "Manager"
  description "A manager triages queued risks and reviews each in detail"
  RiskQueue
  QueueRiskDetail

flow "Log a risk"
  role "Risk owner"
  description "An owner records a new risk and tracks its remediation"
  MyRisks
  NewRisk
  RiskDetail
```

The two `flow` blocks close the file: each names its task, carries its `role`
and a one-line `description`, and lists only its own role's screens, entry
screen first. Each screen is reachable by clicking from somewhere in its own
flow — `RiskQueue`'s "Review next" CTA and its table both lead to
`QueueRiskDetail`; `MyRisks`'s button leads to `NewRisk` and its table leads to
`RiskDetail`. They are what the prototype's flow picker offers the reviewer —
the Manager's "Approval queue" walks queue-then-escalate, the Risk owner's
"Log a risk" walks log-then-remediate — and neither flow references a screen
the other role can't reach.

Checklist before finishing a wireframe file:

- Every screen from the requirements has a `screen` block; no extras, no
  duplicate takes on the same screen. Where a role changes the view, there's a
  screen per role, named and described for it.
- Every screen has a one-line description saying what it's for.
- `navbar` is identical across every screen of the app; `sidebar` is scoped
  to each role's destinations, with shared items kept at the same label and
  order across roles.
- Each role or journey named in `design.md` has its own `flow "<name>"` block,
  entry screen first, referencing existing screens by name.
- Labels are content-bearing ("Open risks | 24 | across 6 registers",
  "Platform team", "Overdue"), never placeholders like "text" or "label".
- The right primitive does each job — `badge` for status, `tabs` for section
  switching, `avatar` for people, `progress` for completion, `table` + `row`
  for real data.
- Color is rare and meaningful: one `primary` action per screen, plus the odd
  status `badge`.
- Navigation is on the control that triggers it: the button/link/table that
  leads to another screen ends with `-> ScreenName`, and every target matches
  a real `screen`.
- NO coordinates and no manual sizes unless an element truly needs one — the
  layout comes from stacking, `row`, and `split`.

## Updating existing wireframes

The DSL is line-oriented, so `editFile` works naturally: anchor on the
`screen <Name>` line plus the element line you're changing. Add a screen by
appending a new `screen` block. Add table data by inserting `row` lines under
the `table`. Insert a new element by adding its line where it should appear in
the stack — everything below it moves down automatically.
