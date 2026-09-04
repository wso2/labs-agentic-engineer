# Authoring wireframes (design agent)

The companion to the `wireframes` skill for the design turn: where the screens
come from, what makes a set good, how roles split them, the proven anatomies,
and a complete worked example. The DSL grammar itself is in `SKILL.md`; this
file is about deciding what to draw with it.

## Where the screens come from — read the design first

You have up to three sources in context; read them in this order of priority:

1. **`specs/design/flows/`** and **`specs/design/domain-model.md`** — the
   flow files name the journeys the screens must serve (one key flow per
   file, actor first) and the domain model names the entities they display.
   This is your **primary** source for screens: derive the screen list from
   the flows first. (They may not exist on every turn — if absent, promote
   the requirements to primary.)
2. **`specs/requirements/`** (requirements / user stories) — the **coverage
   oracle**, and the detailed source. The numbered user stories are what the
   set has to cover (see *Cover every story* below); they also flesh out each
   screen and catch tasks the flow files only summarized: specific fields,
   states, rules, and edge cases (out-of-stock, guest vs. signed-in,
   validation errors).
3. **This component's `specs/design/components/<name>/design.json`** — a thin
   per-component summary; use it mainly to **scope**, not for screen content:
   its `type` (draw wireframes only for `web-application`), its one-line
   `description`, and its `dependencies` (e.g. an auth dependency means there's
   a signed-in vs. guest distinction → likely role-specific screens).

## Cover every story

The user stories are **numbered**, so coverage is countable. Count it
deliberately, before you write the file, while the requirements are in front
of you:

**Walk the numbered stories in order. For each one, name the `flow` block that
walks it** — the wireframe's own flows, not the `specs/design/flows/` files
you read them from. Each usually serves several stories, and a story may
appear in more than one; what matters is that every story is accounted for —
either walked, or knowingly set aside.

**Some stories have no screen, and that is correct.** Set a story aside when
the product genuinely gives it no view:

- **Sign-in and sign-out**, on a component with an auth dependency. "As a user
  I can sign in" is real, but the platform's SSO owns that page — there is no
  `Login` screen to draw (see *The DSL* in `SKILL.md`). The story is served by
  the auth dependency, not by a flow.
- **Backend rules and jobs** — a nightly export, a retention policy, a
  validation rule enforced in the API.
- **Machine-facing stories** — an endpoint another service calls.

Every other uncovered story is a gap: add the screens it needs and put them in
a flow.

Equally, don't invent screens the stories don't imply.

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
  thing first (it renders highest). A dominant action per screen (the
  `primary` button — usually one; two only when the screen truly has two
  equal main actions).
- **The right primitive for each thing.** A status is a `badge`, not text. A
  section switch is `tabs`. A person is an `avatar`. Picking the primitive that
  matches the real UI element is most of what makes screens feel real.

## Roles change the screens — always show them

Most real apps have more than one kind of user, and the *same feature looks
different for each*. This is the single most common thing wireframes get wrong:
they show one generic view and hide the fact that an admin and a regular user
actually see different screens. Don't do that.

**First, identify the roles.** Read the PRD and the design's flow files for distinct user types —
admin/manager/owner vs. member/employee/developer vs. viewer/customer (the
flows usually spell the actors out; the requirements add the detail, and a
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
- A screen that is genuinely identical for everyone (a public landing page, a
  generic detail page) stays single — don't fork it just to have a matching set.

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

## Consistency rules

- **One primary navigation, not two.** A content-heavy tool (admin console,
  dashboard app) uses the `sidebar` for section links and a brand-only
  `navbar` (`navbar "Acme"`). A simple public flow (storefront, checkout) uses
  a link-carrying `navbar` and **no sidebar**. Never both on one screen.
- Repeat the SAME `navbar` on every screen of one app. Repeat the SAME
  `sidebar` too, EXCEPT where a role's screens scope it to that role's
  destinations — the items both roles share still keep the same label and
  order, so the screens read as one product even when the rail isn't
  word-for-word identical.
- Comments start with `//`. A whole-line comment is safe anywhere; a trailing
  one is safe on an element line (`button "Escalate" primary  // in place`)
  but NOT inside a `flow` block, where a screen-name line is matched whole and
  a trailing comment rejects the write. Every screen should be reachable from
  some control's `-> Screen`, except a flow's entry screen — it is where the
  prototype opens, so nothing needs to point at it.

## Worked example — risk register webapp wireframes

A complete `wireframes.dsl` for a two-role desktop flow. The manager and the
owner each get their own dashboard and detail screen — a shared `RiskDetail`
would need two different sidebars, and a screen can only carry one, so each
role's landing view and remediation view are split in two. `navbar` stays
brand-only and identical everywhere; each `sidebar` lists only that role's
destinations, and the items both roles have (Overview, All Registers, Audits,
Settings) keep the same label and the same order — only the role-specific
entry (Review Queue vs. My Risks) differs. Note the rest of the rhythm: blocks
stack in reading order; `row` groups things side by side; each screen's main
action is its `primary` button; status is carried by `badge`s, not
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
    button "Cancel" -> MyRisks
    button "Create risk" primary -> RiskDetail

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
        button "Escalate" primary  // in place — raises severity, opens no view
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
        button "Update status" primary  // in place — sets status on this page
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
screen first. Each flow walks in the order it lists — `RiskQueue`'s "Review
next" CTA and its table both lead to `QueueRiskDetail`; `MyRisks`'s "New risk"
leads to `NewRisk`, whose "Create risk" lands on the new risk's `RiskDetail`
(`MyRisks`'s table reaches the same screen for a risk that already exists).
They are what the prototype's flow picker offers the reviewer —
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
- Each role or journey named in the design's `flows/` files has its own `flow "<name>"` block,
  entry screen first, referencing existing screens by name.
- **Every numbered user story is accounted for**: walked by at least one flow,
  or on the set-aside list with its reason (sign-in, a backend rule, a job, a
  machine-facing endpoint). Go through the stories and say which for each —
  the one thing that must not happen is a story nobody looked at.
- **Each role flow opens on that role's own screen, and there is no `Login`
  screen when the component has an auth dependency** — the platform's SSO
  hosts sign-in and returns the user to their home screen, so the app never
  renders one. A shared `Login` at the head of every flow funnels every persona
  through one static `-> Screen` into the same dashboard. Sign-out is a navbar
  item with no arrow. Draw a sign-in screen only for an app that owns its own
  credentials, and then in one flow only.
- Labels are content-bearing ("Open risks | 24 | across 6 registers",
  "Platform team", "Overdue"), never placeholders like "text" or "label".
- The right primitive does each job — `badge` for status, `tabs` for section
  switching, `avatar` for people, `progress` for completion, `table` + `row`
  for real data.
- Color is rare and meaningful: `primary` on each screen's main action
  (usually one, at most two equals), plus the odd status `badge`.
- Navigation is on the control that triggers it: the button/link/table that
  leads to another screen ends with `-> ScreenName`, and every target matches
  a real `screen`.
- **Every `primary` button carries a `-> Screen`**, or a `//` comment saying
  why its action completes in place. Walk the screens and check each one: a
  "New…"/"Invite…"/"Add…"/"Assign…" primary with no arrow means the form it
  opens was never declared as a screen — declare it and point at it.
- NO coordinates and no manual sizes unless an element truly needs one — the
  layout comes from stacking, `row`, and `split`.

## Updating existing wireframes

The DSL is line-oriented, so `editFile` works naturally: anchor on the
`screen <Name>` line plus the element line you're changing. Add a screen by
appending a new `screen` block. Add table data by inserting `row` lines under
the `table`. Insert a new element by adding its line where it should appear in
the stack — everything below it moves down automatically.
