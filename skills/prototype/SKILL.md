---
name: prototype
description: "Load for the /prototype flow: generate the prototype (prototype.json + prototype.tsx) of every web-application a finished design declares, as a clickable, checked app the reviewer can use before anything is built."
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Prototype

A prototype is the reviewable picture of a `web-application` before it is
built: its screens, navigation, roles, flows and display states, filled with
mock data, running in a sandbox inside the review page. It is **derived from
the design**, so this flow runs after `/design` and asks nothing the design
already answers. It writes two files per web-application and nothing else.

The screens are a React module written against a fixed kit of components
(`@wso2/prototype-kit`). You compose screens from the kit; you do not build
components, style anything, or reach outside the frame.

## Inputs

Read these before writing anything:

- `specs/design/design.cell`: every `component … web-application` line is one
  prototype to write. No web-application means there is nothing to do: say so
  and stop. When the turn names components, write only those.
- `specs/design/security.json`: the roles. When the file exists, each
  prototype role's `id` is a role's `name` from it, **copied verbatim** (same
  case, same spelling), and its `name` is a short human label for that role.
  Include exactly the roles that use this application. Without the file, take
  the roles from the PRD's actors and give them short kebab-case ids.
- The API each web-application reads: the `openapi.yaml` of every component
  listed under `dependencies` (`kind: component`) in its `design.json`, and
  its own when it has one. Screens show what these operations return and
  collect what they accept.
- `specs/requirements/prd.md`: the numbered **User Stories**, the coverage
  oracle. Every story gets at least one screen. Set a story aside only when
  the product gives it no view (sign-in through the platform's SSO, a backend
  job, a machine-facing endpoint) and name it in your closing.
- `specs/design/flows/*.md`: the key flows. Every flow a web-application's
  users walk becomes a `flows` entry of its prototype, one role's walk through
  its screens in order.
- `specs/design/components/<component>/wireframes.dsl`, when it exists: the
  earlier sketch of the screens. It is a hint for layout and screen names, not
  a constraint; the PRD, flows and API decide what the screens hold.
- `specs/design/components/<component>/prototype.json` and `prototype.tsx` if
  they already exist: you are revising them, not starting over (see **Stable
  ids**).

## Output

Two files per web-application, in `specs/design/components/<component>/`,
where `<component>` is the web-application's name in the cell:

1. **`prototype.json`, the manifest**: the roles, display states, screens and
   flows the reviewer picks from. Write it **first**.
2. **`prototype.tsx`, the screens**: one React component per manifest screen,
   exported with `defineApp`. It is checked against the manifest already
   written.

Write a new file with ONE `addFile` of the whole file. Revise an existing one
with `editFile` edits: the file is already in front of you, and an edit costs
what it changes. Never write a partial file to finish later.

Change **no other file**: not the cell, not `security.json`, not any
`design.json`, `openapi.yaml` or requirement. If the design is wrong for the
prototype you would write, say what is wrong in your reply and leave the design
alone.

Every write is checked as it lands: the manifest's shape and references, the
source's static rules, then a render of every screen for every role that reaches
it in every display state. A refusal lists every finding with its code and
where (a JSON path in the manifest, a line in the source, or the screen, role
and state that failed to draw). Fix all of them and retry once. The codes are
listed at the end of **The kit**.

## The manifest

Plain JSON, schema version 3. Every object is closed: a key not shown here is
refused.

```json
{
  "schemaVersion": 3,
  "name": "Expense approvals",
  "entryScreen": "screen.queue",
  "roles": [{ "id": "Approver", "name": "Approver" }],
  "states": [
    { "id": "state.default", "name": "Default" },
    { "id": "state.empty", "name": "Nothing to show" },
    { "id": "state.validation-error", "name": "Validation errors" },
    { "id": "state.failed", "name": "Expense feed failed" }
  ],
  "screens": [
    { "id": "screen.queue", "name": "Approval queue", "roleIds": ["Approver"] },
    { "id": "screen.detail", "name": "Expense detail", "roleIds": ["Approver"] },
    { "id": "screen.account", "name": "Account", "roleIds": ["Approver"] },
    { "id": "screen.settings", "name": "Settings", "roleIds": ["Approver"] },
    { "id": "screen.signed-out", "name": "Signed out", "roleIds": ["Approver"] }
  ],
  "flows": [
    { "id": "flow.approve", "name": "Approve an expense", "roleId": "Approver",
      "screenIds": ["screen.queue", "screen.detail"] }
  ]
}
```

- `roles`, `states` and `screens` have at least one entry; their ids and the
  flows' ids share one namespace and are unique together. `entryScreen` is the
  screen the prototype opens on. `name` is the application's name.
- A screen's `roleIds` are the roles that reach it. A flow is one role's walk
  through screens that role reaches, in order, for one story or key flow.
- The app shell's screens, an account screen, a settings screen and a
  signed-out screen, go to **every** role: every user has them.
- `states` always starts with a default state (`state.default`); add one per
  presentation a reviewer must see:
  - `state.empty` wherever a list can be empty.
  - Wherever a screen's operations declare error responses in the API, the
    states those responses produce. A form whose operation answers `400` or
    `422` for invalid input shows its validation errors in
    `state.validation-error`: its Fields' `error` and a ValidationSummary.
    An operation that can fail (`5xx`, or a dependency it calls) gets
    `state.failed`: an error Alert saying what failed and what the user can
    do. A call that can be slow (an external dependency, a long-running
    operation) gets `state.delayed`: an info or warning Alert saying the
    result is on its way.
  A prototype with only default and empty states hides the presentations a
  reviewer most needs to judge.

## The screens

```tsx
import { useState, type ReactNode } from "react";
import {
  Alert, AppShell, Button, Detail, Dialog, EmptyState, Field, Form, Heading, Screen, Section, Stat,
  StatGroup, Table, defineApp, useCollection, useDisplayState, useNav, useParams,
} from "@wso2/prototype-kit";

interface Expense { id: string; employee: string; amount: string; status: string }

const expenses: Expense[] = [
  { id: "1042", employee: "Maya Fernando", amount: "$148.20", status: "Awaiting approval" },
  { id: "1039", employee: "Ravi Perera", amount: "$62.00", status: "Awaiting approval" },
  { id: "1031", employee: "Ana Silva", amount: "$310.00", status: "Approved" },
];

const user = { name: "Ravi Perera", email: "ravi@acme.example" };

function Shell({ children }: { children: ReactNode }) {
  return (
    <AppShell
      id="shell"
      user={user}
      nav={[{ id: "nav.queue", label: "Approval queue", to: "screen.queue" }]}
      account="screen.account"
      settings="screen.settings"
      signOut="screen.signed-out"
    >
      {children}
    </AppShell>
  );
}

function Queue() {
  const state = useDisplayState();
  const all = useCollection<Expense>("expenses");
  const waiting = state === "state.empty" ? [] : all.items.filter((e) => e.status === "Awaiting approval");
  const approved = all.items.filter((e) => e.status === "Approved").length;
  return (
    <Shell>
      <Heading id="heading.queue" text="Approval queue" />
      {state === "state.failed" && (
        <Alert id="alert.feed-failed" tone="error" title="Expense feed unavailable" text="Amounts may be stale. Refresh in a few minutes." />
      )}
      <StatGroup>
        <Stat id="stat.waiting" label="Awaiting you" value={String(waiting.length)} hint="claims to decide" icon="Inbox" tone="warning" />
        <Stat id="stat.approved" label="Approved" value={String(approved)} hint="this month" icon="CircleCheck" tone="success" />
      </StatGroup>
      <Section id="section.expenses" title="Expenses" count={waiting.length} subtitle="Oldest first. Open a claim to reject it with a reason.">
        <Table
          id="table.expenses"
          columns={["Employee", { label: "Amount", kind: "number" }, { label: "Status", kind: "status" }]}
          rows={waiting.map((e) => ({
            id: `expense.${e.id}`,
            cells: [e.employee, e.amount],
            status: { text: e.status, tone: "warning" },
            to: "screen.detail",
            params: { expense: e.id },
            actions: [{ id: `expense.${e.id}.approve`, label: "Approve", onPress: () => all.update(e.id, { status: "Approved" }) }],
          }))}
          empty={<EmptyState id="empty.queue" title="Nothing to approve" text="New claims appear here." />}
        />
      </Section>
    </Shell>
  );
}

function ExpenseDetail() {
  const { expense: id } = useParams();
  const state = useDisplayState();
  const navigate = useNav();
  const all = useCollection<Expense>("expenses");
  const [rejecting, setRejecting] = useState(false);
  const expense = (id ? all.get(id) : undefined) ?? all.items[0]!;
  const decide = (status: string) => {
    all.update(expense.id, { status });
    navigate.go("screen.queue");
  };
  return (
    <Shell>
      <Heading id="heading.detail" text={`Expense ${expense.id}`} />
      <Detail
        id="detail.expense"
        fields={[
          { label: "Employee", value: expense.employee },
          { label: "Amount", value: expense.amount },
        ]}
      />
      <Button id="btn.approve" label="Approve" emphasis="primary" onPress={() => decide("Approved")} />
      <Button id="btn.reject" label="Reject" emphasis="danger" onPress={() => setRejecting(true)} />
      <Dialog id="dialog.reject" title={`Reject expense ${expense.id}`} open={rejecting} onClose={() => setRejecting(false)}>
        <Form
          id="form.reject"
          onSubmit={() => decide("Rejected")}
          actions={<Button id="btn.reject.confirm" label="Reject" emphasis="danger" submit />}
        >
          <Field
            id="field.reason"
            label="Reason"
            type="textarea"
            required
            error={state === "state.validation-error" ? "Give the employee a reason" : undefined}
          />
        </Form>
      </Dialog>
    </Shell>
  );
}

function Account() {
  return (
    <Shell>
      <Heading id="heading.account" text="Account" />
      <Detail id="detail.account" fields={[{ label: "Name", value: user.name }, { label: "Email", value: user.email }]} />
    </Shell>
  );
}

function Settings() {
  const navigate = useNav();
  return (
    <Shell>
      <Heading id="heading.settings" text="Settings" />
      <Form
        id="form.settings"
        onSubmit={() => navigate.go("screen.queue")}
        actions={<Button id="btn.save-settings" label="Save settings" emphasis="primary" submit />}
      >
        <Field id="field.digest" label="Email me a daily digest" type="switch" defaultValue="on" />
      </Form>
    </Shell>
  );
}

function SignedOut() {
  return (
    <Screen>
      <Heading id="heading.signed-out" text="You are signed out" />
      <Button id="btn.sign-in" label="Sign in" emphasis="primary" to="screen.queue" />
    </Screen>
  );
}

export default defineApp({
  screens: {
    "screen.queue": Queue,
    "screen.detail": ExpenseDetail,
    "screen.account": Account,
    "screen.settings": Settings,
    "screen.signed-out": SignedOut,
  },
  data: { expenses },
});
```

- **One component per manifest screen**, keyed by the screen's id in
  `defineApp({ screens })`: exactly the manifest's screens, no more, no less.
- **Every screen sits in the app shell.** Write one `<AppShell>` in a small
  local component (`Shell` above) and make it every screen's root: it draws
  the product's header, the signed-in user and the user menu (Account,
  Settings, Sign out) and the side navigation. Its `account`, `settings` and
  `signOut` are screens you write: an account screen with the user's details,
  a settings screen with the preferences the application offers, and a
  signed-out screen. Only that signed-out screen, outside the product, has a
  bare `<Screen>` as its root, with a way to sign back in.
  The shell's `menu` adds the application's own user-menu entries (a profile,
  billing) between Settings and Sign out. A reviewer points at an entry by
  opening the menu in Preview, then switching to Annotate.
- **One shell serves every role.** A navigation entry is drawn only for the
  roles that reach its screen. Give `user` the person each role signs in as
  (pick by `useRole()` when roles differ); the header shows the viewing role's
  name beside them.
- **Read the review through the hooks.** `useDisplayState()` is the display
  state the reviewer chose and `useRole()` the role they view as: branch on
  them to show an error, an empty list, a role's own controls. Show a control
  only to the roles that reach where it leads: a `to` a role cannot follow is
  refused.
- **Navigate with `to`** on a Button, Link, table row, row action or breadcrumb (with
  `params` the target reads through `useParams()`); `useNav().go(...)` inside
  an `onPress` for a navigation that follows an action. Every screen must
  also draw **without** params (the reviewer can open any screen from the
  pickers), so fall back to the first record.
- **Overlays are the screen's own state**: `const [open, setOpen] =
  useState(false)` and `<Dialog open={open} onClose={() => setOpen(false)}>`.
  Tabs and Steppers keep their own position, or take `active` and `onChange`
  when a button moves them (a wizard's Next).
- **Forms submit through the kit.** A `<Form onSubmit>` with a `<Button submit>`
  validates its Fields (`required`, `pattern`) and then calls `onSubmit` with
  their values keyed by `name` (default: the Field's id). A Field with no
  `value` holds what is typed into it; give it `defaultValue` to start filled.
- **The mock data is `defineApp({ data })`.** An array of records that each have
  a string `id` is a collection: read it with `useCollection(name)` and change
  it with `create`, `update` and `remove`, so the next screen shows the result.
  Anything else is a single value for `useValue(key)`. The change lives in
  memory until the reviewer presses Reset data; nothing is sent anywhere.

What a prototype may not use is refused as it lands: imports other than `react`
and `@wso2/prototype-kit`; raw HTML elements (`<div>`, `<table>`, `<img>`, …),
because every element is a kit component; the page, storage, network and
code-generation APIs (`window`, `document`, `fetch`, `eval`, …); and anything
nondeterministic (`Math.random()`, `Date.now()`, `new Date()` with no argument;
use `useToday()` for today's date). Styles do not exist: the theme draws every
component, and a component takes no `className` or `style`.

### What the reviewer sees

The review renderer draws only what the screen component renders: kit
components, inside the chrome its `<AppShell>` draws (the header with the
product's name and the signed-in user, the user menu with Account, Settings
and Sign out, the side navigation). A bare `<Screen>` is drawn with no chrome
at all.

Nothing else is drawn automatically: no notification bell, search, theme
toggle, help menu or footer, whatever the built application's shell will
carry. If a reviewer asks for such chrome, model it in the kit: a navigation
entry, or a Button or Link in the screen's content (a Heading's `actions` for
the top right) whose `to` is the screen it leads to or whose `onPress` opens a
Drawer (a notification list) or a Dialog (a confirmation). Only if what they
ask for is outside the kit, say so in your reply and name the limitation.
Never tell a reviewer the platform draws something the screen does not render.

### The Oxygen look

The reviewer sees the prototype in the Oxygen theme, the look of WSO2 products,
so a prototype should read as an Oxygen screen. The host applies the theme: the
source never imports one, never styles anything, and never names Oxygen. Your
part is composing screens the way an Oxygen application composes them:

- **Page anatomy.** A screen is the `<AppShell>` holding a `<Heading>` (the
  page title, with page-wide action Buttons in `actions`), then the content in
  the order a user works through it: headline numbers as one `<StatGroup>` of
  `<Stat>`s, then a `<Section>` per part of the page (a list, a form, a
  record), with `<Filters>` and the `<Table>` inside it, and `<Detail>`,
  `<Form>` and `<Timeline>` for a single record. A sub-page opens with
  `<Breadcrumbs>` back to its list.
- **Stats in a group.** Put a screen's `<Stat>`s in one `<StatGroup>`, never a
  `<Stack direction="row">` or `<Grid>`: the group gives them one width. Give
  each a `hint` that gives the number its scale ("of 20 left this year",
  "claims to decide") and an `icon` from the kit's list that names what it
  counts; `tone` colours the icon when the number needs attention.
- **Sections own their actions.** A `<Section>` titles a part of the page,
  with `count` for the records it lists and `subtitle` for one line on what it
  holds. An action that adds to or acts on that part sits in the Section's
  `actions` (New request above the requests), not under the table and not in
  the page Heading.
- **One primary action per screen.** `emphasis="primary"` marks the screen's
  main action and nothing else; destructive actions are `"danger"`.
- **Status is a chip, not prose.** A record's status is the table's status
  column (`{ label: "Status", kind: "status" }` in `columns`, and each row's
  `status: { text, tone }`) or a `<Badge>`: `success`, `warning`, `error`,
  `info`, `default`. Put the column where it reads best; the row's `cells` fill
  the other columns in order. Callouts are `<Alert>`s with a tone.
- **Row actions are buttons, not cells.** What a user does to one record
  without opening it (Cancel, Approve) is the row's `actions`, each with an id
  built from the row's (`` `${rowId}.cancel` ``), a `label`, and `to` or
  `onPress` like a Button (`emphasis: "danger"` for a destructive one). They
  draw in a trailing column (behind one menu past two) and pressing one never
  presses the row, so the row can still open the record. Never write an
  action's label into a cell. Figures are `{ label, kind: "number" }` columns.
- **Navigation is the shell's side rail**, one entry per top-level screen.
  Account and Settings live in the user menu, not the rail.
- **Records, not cards.** Show a list of records as a `<Table>` whose rows
  open the record; use `<Split>` for a record next to its activity.
- **Forms are short and labelled.** One `<Form>` per task, a `<Field>` per
  input with a plain label, `required` where the API requires it, errors in
  the Field's `error` and a `<ValidationSummary>` for the whole form.

The design-system skill loaded beside this one is the full account of how an
enterprise screen is composed (a listing page's header, filters and table, a
form's layout and actions). Read it for the arrangement and build each screen
that way from the kit. Its setup, packages and code are the build's concern,
not this flow's: the kit's components are the only vocabulary here.

## The kit

Nothing else exists: no markup, styles, scripts, charts, file upload or custom
component. Express a screen in these components or leave the part out.

<!-- kit:start -->
A prototype is `prototype.json` (the manifest) and `prototype.tsx`, which imports only `react` and `@wso2/prototype-kit`. Every component with an `id` is an element a reviewer can point at: give each a stable id, unique on its screen.

### Module

#### `defineApp(definition: PrototypeAppDefinition): PrototypeApp`

Declare the prototype: `export default defineApp({ screens, data })`.

#### `PrototypeAppDefinition`

- `screens: Record<string, ComponentType>` — One component per manifest screen, keyed by the screen's id.
- `data?: Record<string, unknown>` — The mock data the screens share, as JSON literals. An array of records that each have a string `id` is a collection (`useCollection(name)`); anything else is a single value (`useValue(key)`). Changes live until Reset data (or a reload without `--persist`).

### View hooks

#### `useNav(): KitNav`

#### `KitNav`

- `screen: string` — The screen shown.
- `go: (screenId: string, params?: Record<string, string>) => void` — Go to a screen, optionally with params the target reads with `useParams`.

#### `useParams(): Readonly<Record<string, string>>`

The params the navigation to this screen carried. Empty when the reviewer opened the screen from the preview's pickers, so every screen must render without them (fall back to the first record).

#### `useRole(): string`

The id of the role the reviewer is viewing as.

#### `useDisplayState(): string`

The id of the display state the reviewer chose (e.g. empty, loading, error).

### Data hooks

#### `useCollection<T extends { id: string; }>(name: string): Collection<T>`

The collection `name` from `defineApp({ data })`: an array of records that each have a string `id`.

#### `Collection`

A collection of mock records, and the ways to change it for every screen.

- `items: readonly T[]` — The records, in insertion order.
- `get: (id: string) => T | undefined`
- `create: (record: Omit<T, "id">) => string` — Adds a record and returns its new id, `${name}-${n}` with `n` past the highest number in use.
- `update: (id: string, patch: Partial<Omit<T, "id">>) => void`
- `remove: (id: string) => void`

#### `useValue<T>(key: string): [T, (next: T | ((previous: T) => T)) => void]`

The single value `key` from `defineApp({ data })`, and a setter shared by every screen.

#### `useToday(): string`

Today's date, fixed (`PROTOTYPE_TODAY`): a prototype never reads the clock.

#### `PROTOTYPE_TODAY` = `"2026-01-15"`

The fixed date every prototype treats as today (ISO `YYYY-MM-DD`).

### Layout

#### `<AppShell>`

The root of a screen inside the product's chrome: header, user menu and side navigation. Use `<Screen>` for a screen outside it (signed out).

- `id: string` — The shell's id; its user menu's elements are `<id>.user`, `<id>.account`, `<id>.settings` and `<id>.sign-out`.
- `product?: string` — The product's name in the header; prototype.json's `name` by default.
- `user: AppShellUser` — The signed-in user the header shows.
- `nav: NavigationItem[]` — The side navigation's entries.
- `account: string` — The screen the user menu's Account entry opens.
- `settings: string` — The screen the user menu's Settings entry opens.
- `signOut: string` — The screen Sign out leads to: a signed-out screen, drawn on a bare `<Screen>`.
- `menu?: NavigationItem[]` — More user-menu entries (a profile, billing), drawn after Account and Settings and before Sign out.
- `children?: ReactNode` — The screen's content.

#### `AppShellUser`

- `name: string`
- `email?: string` — Shown under the name in the user menu.
- `role?: string` — The role beside the name; the viewing role's name in prototype.json by default, so a role switch shows in the header.

#### `<Screen>`

The root of a screen outside the app shell (signed out, a landing page): its content, and navigation if any.

- `nav?: ReactNode` — Navigation for an app drawn without `<AppShell>`: one `<Navigation>`, usually shared by every screen.
- `children?: ReactNode`

#### `<Section>`

A titled part of a screen — a table, a form, a group of stats — with the actions that belong to it.

- `id: string`
- `title: string`
- `subtitle?: string` — One line under the title: what the section holds or why.
- `count?: number` — How many records the section lists, shown beside the title.
- `actions?: ReactNode` — The section's own Buttons ("New request" above the requests it adds to).
- `children?: ReactNode`

#### `<Stack>`

Children in a column (default) or a wrapping row.

- `direction?: "row" | "column"`
- `children?: ReactNode`

#### `<Grid>`

Children in 1–6 equal columns.

- `columns: number` — 1–6 equal columns (one column on a narrow window).
- `children?: ReactNode`

#### `<Split>`

Two panes side by side (stacked on a narrow window).

- `left: ReactNode`
- `right: ReactNode`
- `ratio?: number` — The left pane's share of 12 columns (default 6).

#### `<Detail>`

A read-only record: label/value pairs.

- `id: string`
- `title?: string`
- `fields: DetailField[]`

#### `DetailField`

- `label: string`
- `value: string`

### Navigation

#### `<Navigation>`

Navigation for an app drawn on `<Screen nav>` (`<AppShell nav>` draws its own). Its items are what a reviewer points at.

- `id: string`
- `layout: "side" | "top"`
- `items: NavigationItem[]`

#### `NavigationItem`

- `id: string`
- `label: string`
- `to: string` — The screen the entry opens; it shows as active there.

#### `<Breadcrumbs>`

A trail back to where the reviewer came from.

- `id: string`
- `items: BreadcrumbItem[]` — The trail, root first.

#### `BreadcrumbItem`

- `id: string`
- `label: string`
- `to?: string` — Where the crumb leads; the last crumb is the current page and takes none.
- `params?: Record<string, string>`

#### `<Tabs>`

Tabbed panels. Each tab header is an element a reviewer can point at (its `id`); the Tabs itself is layout.

- `id: string`
- `tabs: Panel[]`
- `active?: string` — The open tab, when the screen controls it; the first tab otherwise.
- `onChange?: ((tabId: string) => void)`

#### `<Stepper>`

A multi-step wizard. Each step header is an element a reviewer can point at (its `id`).

- `id: string`
- `steps: Panel[]`
- `active?: string` — The current step, when the screen controls it (a Next button); the first step otherwise.
- `onChange?: ((stepId: string) => void)`

#### `Panel`

- `id: string`
- `label: string`
- `content: ReactNode`

### Content

#### `<Heading>`

A page or section title, with the actions beside it.

- `id: string`
- `text: string`
- `level?: "page" | "section"` — `page` (default) for a screen's title; `section` titles a part of it. Prefer `<Section>` for a part with its own actions or records.
- `actions?: ReactNode` — Buttons and Links beside the heading.

#### `<Text>`

A paragraph of body text.

- `id: string`
- `text: string`
- `tone?: "primary" | "secondary"` — `secondary` (default) for body copy, `primary` for emphasis.

#### `<Badge>`

A status chip.

- `id: string`
- `label: string`
- `tone?: Tone`

#### `<Stat>`

A headline number with its label. Put a screen's stats side by side in a `<StatGroup>`.

- `id: string`
- `label: string`
- `value: string`
- `hint?: string` — A short line under the value that gives it scale or context: "of 20 days", "3 due this week".
- `icon?: StatIcon` — An icon beside the label.
- `tone?: Tone` — Colours the icon (`default` is the theme's primary).

#### `StatIcon` = `"Activity" | "Bell" | "Briefcase" | "Bug" | "Building2" | "Calendar" | "CalendarCheck" | "CalendarClock" | "CalendarDays" | "ChartColumn" | "CircleCheck" | "CircleX" | "ClipboardList" | "Clock" | "Cloud" | "Cpu" | "CreditCard" | "Database" | "DollarSign" | "FileText" | "Gauge" | "Globe" | "HeartPulse" | "Hourglass" | "Inbox" | "Layers" | "ListChecks" | "Lock" | "Mail" | "MessageSquare" | "Package" | "Plane" | "Receipt" | "Rocket" | "Server" | "Shield" | "ShoppingCart" | "Star" | "Tag" | "Ticket" | "Timer" | "TrendingDown" | "TrendingUp" | "TriangleAlert" | "Truck" | "User" | "UserCheck" | "Users" | "Wallet" | "Zap"`

The icons a `<Stat>` may show, named as in WSO2's Oxygen icon set (Lucide's names). A theme draws each or none; an unknown name fails the check.

#### `<StatGroup>`

A row of `<Stat>`s at equal widths, wrapping on a narrow window.

- `children?: ReactNode` — The `<Stat>`s, in reading order.

#### `<Alert>`

A callout: info, success, warning or error.

- `id: string`
- `tone: Tone`
- `title?: string`
- `text: string`

#### `<EmptyState>`

What a list shows when there is nothing in it.

- `id: string`
- `title: string`
- `text: string`
- `actions?: ReactNode` — The Buttons that start something.

#### `<Button>`

A button. Give it `to` to navigate, `onPress` to do anything else, `submit` to submit its form.

- `id: string`
- `label: string`
- `emphasis?: "primary" | "danger"` — `primary` for the screen's main action, `danger` for a destructive one; outlined otherwise.
- `disabled?: boolean`
- `submit?: boolean` — Submits the enclosing `<Form>` (which validates its fields, then calls its `onSubmit`).
- `to?: string` — Navigate to this screen when pressed. Prefer it to `onPress` for plain navigation: the checks verify it.
- `params?: Record<string, string>` — The params `to` carries, read on the target with `useParams`.
- `onPress?: (() => void)` — Run when pressed: change mock data, open a dialog, navigate conditionally.

#### `<Link>`

An inline link; presses like a Button.

- `id: string`
- `label: string`
- `to?: string` — Navigate to this screen when pressed. Prefer it to `onPress` for plain navigation: the checks verify it.
- `params?: Record<string, string>` — The params `to` carries, read on the target with `useParams`.
- `onPress?: (() => void)` — Run when pressed: change mock data, open a dialog, navigate conditionally.

#### `Tone` = `"default" | "info" | "success" | "warning" | "error"`

#### `Pressable`

- `to?: string` — Navigate to this screen when pressed. Prefer it to `onPress` for plain navigation: the checks verify it.
- `params?: Record<string, string>` — The params `to` carries, read on the target with `useParams`.
- `onPress?: (() => void)` — Run when pressed: change mock data, open a dialog, navigate conditionally.

### Forms

#### `<Form>`

A form card: its Fields, then its action Buttons. Validates on submit.

- `id: string`
- `title?: string`
- `children?: ReactNode` — Its Fields.
- `actions?: ReactNode` — Its Buttons, under the fields; a `<Button submit>` submits.
- `onSubmit?: ((values: Record<string, string>) => void)` — Called with every field's value, keyed by name, when a submit passes validation.

#### `<Field>`

One input. It holds what is typed into it; inside a Form, the Form validates and submits it.

- `id: string`
- `label: string`
- `name?: string` — The key of its value in the enclosing Form's `onSubmit` values (default: its id).
- `type?: FieldType`
- `value?: string` — Controls the field: it shows this; with `onChange` it is editable, without it read-only.
- `defaultValue?: string` — The starting value of a field that holds its own (no `value`). A switch is `on` or `off`.
- `options?: string[]` — A select's choices.
- `error?: string` — A problem to show regardless of validation (e.g. in a validation-error display state).
- `required?: boolean`
- `pattern?: string` — A regular expression the whole value must match when it is not empty.
- `patternMessage?: string` — What a value that does not match `pattern` shows.
- `placeholder?: string`
- `onChange?: ((value: string) => void)`

#### `FieldType` = `"number" | "text" | "select" | "date" | "textarea" | "switch"`

#### `<Filters>`

A filter bar above a list.

- `id: string`
- `children?: ReactNode` — Its Fields, laid out compactly in a row.

#### `<ValidationSummary>`

The problems a failed submit shows, for a display state that shows them without a submit.

- `id: string`
- `issues: string[]`

### Data

#### `<Table>`

A table of records.

- `id: string`
- `title?: string`
- `columns: (string | TableColumn)[]`
- `rows: TableRow[]`
- `onRowPress?: ((rowId: string) => void)` — Run when a row is pressed, with its id; a row with neither this nor `to` is only highlighted.
- `empty?: ReactNode` — What an empty table shows instead (an `<EmptyState>`).

#### `TableColumn`

- `label: string`
- `kind?: "number" | "text" | "status"` — `text` (default); `number` aligns figures to the right; `status` shows each row's `status` as a badge (one per table). A plain string is a `text` column.

#### `TableRow`

- `id: string` — The row's element id: unique on the screen (prefix it with the table's).
- `cells: string[]` — One per `text` or `number` column, in column order; the status column takes `status`.
- `status?: TableStatus` — The badge in the table's status column.
- `actions?: TableAction[]` — What can be done to this record, drawn as buttons in a trailing column; pressing one does not press the row.
- `tone?: Tone` — Colours the last cell as a status, in a table without a status column. Prefer `status`.
- `to?: string` — Open this screen when the row is pressed.
- `params?: Record<string, string>`

#### `TableStatus`

- `text: string`
- `tone: Tone`

#### `TableAction`

- `id: string` — The action's element id: unique on the screen (prefix it with the row's).
- `label: string`
- `emphasis?: "danger"` — `danger` for a destructive action.
- `to?: string` — Navigate to this screen when pressed. Prefer it to `onPress` for plain navigation: the checks verify it.
- `params?: Record<string, string>` — The params `to` carries, read on the target with `useParams`.
- `onPress?: (() => void)` — Run when pressed: change mock data, open a dialog, navigate conditionally.

#### `<Timeline>`

An activity history.

- `id: string`
- `title?: string`
- `entries: TimelineEntry[]` — Newest first.

#### `TimelineEntry`

- `when: string`
- `who: string`
- `text: string`

### Overlays

#### `<Dialog>`

A modal.

- `id: string`
- `title: string`
- `open: boolean`
- `onClose: () => void`
- `children?: ReactNode`
- `actions?: ReactNode` — Its Buttons, at the bottom.

#### `<Drawer>`

A side panel.

- `id: string`
- `title: string`
- `open: boolean`
- `onClose: () => void`
- `children?: ReactNode`

### Finding codes

`prototype check --json` prints `{ ok, findings: [{ code, file, location, message }] }` and exits 1 when there are findings.

| Code | Meaning |
|---|---|
| `MISSING_FILE` | prototype.json or prototype.tsx is not in the folder. |
| `SCHEMA_VIOLATION` | prototype.json is not JSON, or does not have the manifest's shape. |
| `UNSUPPORTED_VERSION` | prototype.json's schemaVersion is not 3. |
| `DUPLICATE_ID` | A role, state, screen or flow id repeats another; the four share one namespace. |
| `UNKNOWN_REFERENCE` | A manifest reference names nothing, or a flow step its role cannot reach. |
| `SYNTAX_ERROR` | prototype.tsx does not parse. |
| `FORBIDDEN_IMPORT` | An import other than react and @wso2/prototype-kit, a dynamic import(), or require. |
| `FORBIDDEN_API` | A browser, network, storage, code-generation or nondeterministic API. |
| `FORBIDDEN_ELEMENT` | A raw HTML element; screens are drawn with kit components only. |
| `SOURCE_TOO_LARGE` | prototype.tsx is over the size cap. |
| `NO_APP` | prototype.tsx does not `export default defineApp({ screens, data })`. |
| `SCREEN_MISMATCH` | defineApp's screens are not exactly the manifest's screens. |
| `UNKNOWN_NAV_TARGET` | A `to` or `go()` names a screen that does not exist, or one the viewing role cannot reach. |
| `RENDER_FAILED` | The module, or a screen for some role and display state, throws or runs too long. |
| `DUPLICATE_ELEMENT_ID` | Two elements on one screen share an id. |
<!-- kit:end -->

## Mock data

Records are part of the source and never generated at render time, so write
them out in full:

- **Shaped by the API.** Columns, detail fields and form fields are the
  properties of the schemas the screen's operations return or accept, in
  words a user reads (`submittedAt` becomes "Submitted"). A value has the type
  and format its schema declares: an enum value is one of its enum's values,
  an amount reads as an amount. Type the records after the schema
  (`interface Expense { … }`).
- **Realistic.** Plausible names, amounts, dates and statuses for the domain;
  never `foo`, `test`, `Lorem ipsum` or `Item 1`.
- **Deterministic.** The same design gives the same records: fixed values, no
  randomness, no "today" other than `useToday()`. A row's id is derived from the
  record (`expense.1042`).
- Enough rows to read as real (three to six), and a state in which a list is
  empty when the story has one.

## Stable ids

Manifest ids and element ids (the `id` of every kit component) are identities,
labels are not: a reviewer's annotations point at element ids, and shared
links name screens, flows and states, so:

- Name them `<kind>.<slug>` from what the thing is (`screen.queue`,
  `btn.approve`, `field.amount`), except role ids, which are `security.json`'s
  names. An element id is unique on its screen; a row's id is prefixed with
  its table's kind (`expense.1042`).
- **Revising an existing prototype keeps every key and id whose thing still
  exists.** Rename labels freely; change an id only when its thing is removed
  or genuinely becomes something else.

## Feedback revision

A turn that arrives with a reviewer's feedback is a **revision of one
prototype**, not a generation. Its brief names the web-application, the revision
the reviewer looked at and a numbered list of requests. Each request gives the
screen, flow, role and display state the reviewer was in, the ids of the
elements they pointed at (none means the whole screen), and their words quoted
verbatim.

- **Read the two files first** (`prototype.json` and `prototype.tsx` of that
  component) and change only them, with `editFile` edits. Write no other
  component's prototype and no other file.
- **Find the thing by its id.** Element ids are on the screen's components,
  screen, flow and state ids are in the manifest. An id that no longer exists
  means the prototype moved on since the reviewer looked: apply what still makes
  sense and say what differs.
- **Take the words at face value.** Do exactly what a request asks, no more.
  Several requests can touch the same element; reconcile them, and when two
  conflict, apply neither and ask in your reply.
- **Apply every request you can.** Decline one only when it conflicts with the
  design (the cell, `security.json`'s roles, the API or the stories), and say
  which part. A request to add a capability the kit lacks is declined with that
  reason, not worked around.
- **Keep ids stable** (see **Stable ids**): the next round of feedback points at
  them. Add ids for new elements; keep every key and id whose thing remains.
- The same write checks apply. Revise until the pair passes.

End with a numbered answer: for each request, `Request N: applied` and what you
changed, or `Request N: declined` and why. Nothing more is needed per request.

## Closing

End with one short paragraph per prototype: its screens, the roles and flows it
covers, any story you set aside and why, and anything from the design you could
not represent.
