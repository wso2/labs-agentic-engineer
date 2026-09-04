---
name: wireframes
description: Use when creating or updating UI wireframes for a webapp component (design), or when implementing a web-application component that has a wireframes.dsl (coding) — the DSL is the screen contract the pages must honour, screen for screen and element for element.
metadata:
  aep:
    kind: platform
    audience: [design, coding]
---

# Wireframes (Excalidraw DSL)

This skill has two readers, and this file holds only what both need — the
DSL itself. Which reader you are decides the reference you load next:

- **Designing** — authoring or updating `wireframes.dsl` — read
  `references/authoring.md`: where the screens come from, roles, the proven
  anatomies, a complete worked example.
- **Implementing** — building a `web-application` component that has a
  `wireframes.dsl` — read `references/implementing.md`: how each screen,
  element and arrow becomes a route, a component and working navigation, and
  how the walk evidences it.

Every `web-application` component gets its wireframes as ONE DSL source file at
`specs/design/components/<name>/wireframes.dsl` — all screens in one file.
The design agent writes ONLY the `.dsl`; the platform compiles it to the
`.excalidraw` deterministically. NEVER write a `.excalidraw` file by hand, and
never read one — the `.dsl` is the source of truth for both readers.

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
on is correctly inert. A rail may also name a section this wireframe set does
not draw (`Settings`, `Audits`): leave those without a target — they are
context, and inventing a screen just to receive the arrow is noise. Keep them
few; a rail where most items go nowhere reads as broken.

**A `primary` button must lead somewhere.** It is the screen's main action and
the first thing a reviewer clicks, so one with no `-> Screen` is a dead end
that reads as broken however good the layout is. Give every `primary` button a
target unless its action genuinely completes in place — a status toggle, a save
that stays on the page — and mark that case with a `//` comment so the omission
reads as a decision. "New…", "Invite…", "Add…", "Assign…", "Create…" are never
in place: they need a form or picker the user has not seen, which is a view, so
declare that screen and point at it. Secondary controls may go without an
arrow; the primary one may not.

The one main action that is **not a screen at all** is sign-in on a component
with an auth dependency: the platform's SSO hosts that page, the app only
redirects to it, and the user comes back on their role's home screen. Do not
draw a `Login` for it (see Flows). A `Sign out` item leaves the app the same
way, so it carries no arrow — the platform, not the app, shows what comes next.

**Decide the screens and the paths together.** A wireframe set is a flow, not a
gallery: when you pick the screens, work out how each one is reached. Every
screen should be reachable by clicking — or be a landing screen whose
description says which role it serves. No view is stranded, and no main action
is dead.

### Flows — one per role or journey

The screens say what exists; a **flow** says who walks which ones, in what
order. The prototype's top-level control is the flow picker, so a wireframe set
without flows offers the reviewer no way to ask for the admin's journey.

Declare one `flow` block per role or journey named in the design's `flows/`
files (or in `specs/requirements/` when they are absent), listing that
role's screens in walkthrough order, **entry screen first**. Name the flow for
its **task** ("Approval queue", "Log a risk"), and carry the persona on a
`role` line — not in the name:

```text
flow "Approval queue"
  role "Admin"
  description "An admin reviews queued items and audits the outcome"
  AdminQueue
  AuditDetail

flow "My orders"
  role "Customer"
  description "A signed-in customer checks a placed order"
  Orders
  OrderDetail
```

**A role's flow starts on that role's own screen.** The prototype opens a flow
on its **first listed screen**, so the admin's flow starts on the admin's
queue and the customer's on their orders — the first thing each persona
actually sees.

**Sign-in is not one of the app's screens when the component has an auth
dependency.** The platform's SSO hosts the sign-in page; the app only redirects
to it on load and receives the user back on their role's home screen. There is
nothing to draw and nothing the coding agent could build — so **do not declare
a `Login` screen**, and do not open any flow with one. A shared `Login` at the
head of every role flow is the classic mistake: every persona begins in the
same place, and the one static `-> Screen` on its button sends all of them into
whichever dashboard it names. Show sign-out as a `navbar` item with no arrow —
it leaves the app the same way.

Draw a sign-in screen only when the app **owns** its sign-in form — a component
with no auth dependency that keeps its own credentials. Then it is a real
screen: put it first in the one flow that walks it, point its button at that
flow's home screen, and keep it out of the other flows.

- A flow **references** screens by name; screens stay declared once. A screen
  genuinely shared mid-journey (a settings page both roles reach) may be
  listed in each flow that reaches it — the rule above is about the **first**
  screen, which is where the prototype opens.
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

- `primary` on a screen's main action — fills brand orange. Usually that is
  one button per screen; give it to two only when the screen genuinely has
  two equal main actions (Approve and Reject on a review). Every other button
  stays a plain outline.
- `danger` / `success` / `warning` / `info` on a `badge` or destructive
  `button` (`badge "Overdue" danger`).
- `ai` (purple) for an automated / AI-driven step, if the product has one.
- `active` marks a `checkbox` / `radio` / `toggle` as selected / on.
- `muted` for de-emphasized text (an eyebrow label).

Keep status variants purposeful — a screen dense with red/green/amber badges
stops communicating.
