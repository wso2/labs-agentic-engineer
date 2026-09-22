---
name: prototype
description: "Load for the /prototype flow: generate or revise the read-only prototype.json of every web-application a finished design declares, including a turn that carries a prototype feedback batch."
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Prototype

A prototype is the reviewable picture of a `web-application` before it is
built: its screens, navigation, roles, flows and display states, filled with
mock records, rendered read-only by the platform. It is **derived from the
design**, so this flow runs after `/design` and asks nothing the design already
answers. It writes one file per web-application and nothing else.

## Inputs

Read these before writing anything:

- `specs/design/design.cell` — every `component … web-application` line is one
  prototype to write. No web-application means there is nothing to do: say so
  and stop.
- `specs/design/security.json` — the roles. When the file exists, each
  prototype role's `id` is a role's `name` from it, **copied verbatim** (same
  case, same spelling), and its `name` is a short human label for that role.
  Include exactly the roles that use this application. Without the file, take
  the roles from the PRD's actors and give them short kebab-case ids.
- The API each web-application reads: the `openapi.yaml` of every component
  listed under `dependencies` (`kind: component`) in its `design.json`, and
  its own when it has one. Screens show what these operations return and
  collect what they accept.
- `specs/requirements/prd.md` — the stories, which decide the screens and the
  flows.
- `specs/design/components/<component>/prototype.json` if it already exists —
  you are revising it, not starting over (see **Stable IDs**).

## Output

One complete document per web-application at
`specs/design/components/<component>/prototype.json`, where `<component>` is
the web-application's name in the cell and the document's `component` field
is the same name. Write it with ONE `addFile` of the whole document
(`removeFile` first when it already exists) — never piece a prototype together
with edits, and never write a partial file to finish later.

Change **no other file**: not the cell, not `security.json`, not any
`design.json`, `openapi.yaml`, `wireframes.dsl` or requirement. If the design
is wrong for the prototype you would write, say what is wrong in your reply and
leave the design alone.

Every write is validated as it lands. A refusal (`INVALID_PROTOTYPE`,
`PROTOTYPE_COMPONENT_MISMATCH`, `INVALID_JSON`) lists every finding with its
JSON path: fix all of them and re-emit the whole document once.

## The document

Plain JSON, schema version 1. Every object is closed: a key not shown here is
refused.

```json
{
  "schemaVersion": 1,
  "component": "approvals-portal",
  "name": "Expense approvals",
  "defaultScreenId": "screen.queue",
  "roles": [{ "id": "Approver", "name": "Approver" }],
  "states": [
    { "id": "state.default", "name": "Default" },
    { "id": "state.empty", "name": "Nothing to show" }
  ],
  "flows": [
    { "id": "flow.approve", "name": "Approve an expense", "roleId": "Approver",
      "screenIds": ["screen.queue", "screen.detail"] }
  ],
  "navigation": [
    { "kind": "side-nav", "id": "nav.main", "items": [
      { "id": "nav.queue", "label": "Approval queue",
        "action": { "kind": "navigate", "screenId": "screen.queue" }, "roleIds": ["Approver"] }
    ] }
  ],
  "screens": [
    { "id": "screen.queue", "name": "Approval queue", "roleIds": ["Approver"], "navigationId": "nav.main",
      "content": [
        { "kind": "heading", "id": "heading.queue", "text": "Approval queue" },
        { "kind": "task-queue", "id": "queue.expenses", "title": "Expenses",
          "columns": ["Employee", "Amount", "Status"],
          "rows": [{ "id": "expense.1042",
            "values": { "Employee": "Maya Fernando", "Amount": "$148.20", "Status": "Awaiting approval" } }],
          "onRow": { "kind": "navigate", "screenId": "screen.detail" },
          "showIn": ["state.default"] },
        { "kind": "empty-state", "id": "empty.queue", "title": "Nothing to approve",
          "text": "New claims appear here.", "showIn": ["state.empty"] }
      ] },
    { "id": "screen.detail", "name": "Expense detail", "roleIds": ["Approver"], "navigationId": "nav.main",
      "content": [
        { "kind": "detail", "id": "detail.expense", "title": "Expense 1042",
          "fields": [{ "label": "Employee", "value": "Maya Fernando" }, { "label": "Amount", "value": "$148.20" }] },
        { "kind": "approval-panel", "id": "panel.decision", "title": "Your decision",
          "summary": "Within policy for Travel.",
          "actions": [{ "id": "btn.approve", "label": "Approve", "emphasis": "primary",
            "action": { "kind": "show-dialog", "dialogId": "dialog.approved" } }] }
      ],
      "overlays": [
        { "kind": "dialog", "id": "dialog.approved", "title": "Expense approved",
          "content": [{ "kind": "text", "id": "text.approved", "text": "Maya is notified by email." }],
          "actions": [{ "id": "btn.done", "label": "Back to queue",
            "action": { "kind": "navigate", "screenId": "screen.queue" } }] }
      ] }
  ]
}
```

- `roles`, `states` and `screens` have at least one entry. `states` always
  starts with a default state; add one per presentation a reviewer must see —
  empty, validation errors, a failed integration, a delayed one.
- A screen's `roleIds` are the roles that reach it; a navigation item's
  `roleIds` (every role when absent) are the roles that see it. Keep them
  consistent: an item never leads a role to a screen it cannot reach.
- A flow is one role's walk through its screens, in order, for one story.
- A screen with `navigationId` renders inside the application shell with that
  navigation; omit it for a screen that stands alone (a sign-in page).

### Registry (v1) — the only nodes there are

Every node carries `kind` and `id`, and may carry `showIn: [stateId…]` to
appear only in those states (all states when absent).

| Kind | Fields |
|---|---|
| `stack` | `direction?` (`row` \| `column`), `content` |
| `grid` | `columns` (1–6), `content` |
| `split` | `ratio?` (left share of 12, 1–11), `left`, `right` |
| `detail` | `title?`, `fields: [{label, value}]` |
| `breadcrumbs` | `items: [{id, label, action?}]` |
| `tabs` | `tabs: [{id, label, content}]` |
| `stepper` | `steps: [{id, label, content}]` |
| `text` | `text` |
| `heading` | `text`, `actions?: [button]` |
| `badge` | `label`, `tone?` |
| `stat` | `label`, `value` |
| `alert` | `tone`, `title?`, `text` |
| `empty-state` | `title`, `text`, `action?: button` |
| `button` | `label`, `emphasis?` (`primary` \| `danger`), `action` |
| `link` | `label`, `action` |
| `form` | `title?`, `fields: [field]`, `actions: [button]` |
| `validation-summary` | `issues: [string]` |
| `table` | `title?`, `columns: [label]`, `rows: [row]`, `onRow?: action` |
| `filters` | `fields: [field]` |
| `timeline` | `entries: [{id, when, who, text}]` |
| `approval-panel` | `title`, `summary`, `actions: [button]` |
| `task-queue` | `title`, `columns: [label]`, `rows: [row]`, `onRow?: action` |

- **button** (in `actions`): `{id, label, emphasis?, action}`.
- **field**: `{id, label, type?, value?, options?, error?, errorIn?}` — `type`
  is `text`, `number`, `select`, `date`, `textarea` or `switch`; `value` is
  what it displays; `error` shows in the states `errorIn` lists (all when
  absent).
- **row**: `{id, values: {<column label>: <text>}, tone?}` — keyed by the
  table's column labels.
- **tone**: `default`, `info`, `success`, `warning`, `error`.
- A screen's `overlays` are `{"kind": "dialog", id, title, content, actions}`
  or `{"kind": "drawer", id, title, content}`.
- Navigation `kind` is `side-nav` or `top-nav`.

Nothing else exists: no markup, styles, scripts, charts, file upload or custom
component. Express a screen in these nodes or leave the part out.

The renderer draws these nodes with the design system whose skill is loaded
beside this one. Read it for how an enterprise screen is composed — the page
anatomy, a listing page's header, filters and table, a form's layout and
actions — and build each screen that way from the registry. Its setup,
packages and code are the build's concern, not this flow's.

### Actions — view state only

| Action | Fields |
|---|---|
| `navigate` | `screenId` |
| `set-tab` | `tabsId`, `tabId` |
| `set-step` | `stepperId`, `stepId` |
| `select-row` | `tableId` (a table or task queue), `rowId` |
| `show-dialog` | `dialogId` |
| `show-drawer` | `drawerId` |
| `close-overlay` | — |

A tab, step, row, dialog or drawer an action names must be on the screen the
action runs on; a navigation item runs on every screen that shows its
navigation, so it only navigates.

**The prototype is read-only.** Nothing submits, saves, approves, sends or
calls an integration. A primary button moves the reviewer to what they would
see next — `navigate` to the resulting screen, `show-dialog` for a
confirmation, `close-overlay` to dismiss — and the result's presentation is a
display state, not a simulated backend.

## Mock records

Records are part of the document and never generated at render time, so write
them out in full:

- **Shaped by the API.** Columns, detail fields and form fields are the
  properties of the schemas the screen's operations return or accept, in
  words a user reads (`submittedAt` becomes "Submitted"). A value has the type
  and format its schema declares — an enum value is one of its enum's values,
  an amount reads as an amount.
- **Realistic.** Plausible names, amounts, dates and statuses for the domain —
  never `foo`, `test`, `Lorem ipsum` or `Item 1`.
- **Deterministic.** The same design gives the same records: fixed values,
  no randomness, no "today". A row's `id` is derived from the record
  (`expense.1042`).
- Enough rows to read as real (three to six), and a state in which a list is
  empty when the story has one.

## Stable IDs

Every id lives in ONE namespace for the whole document and must be unique;
every reference must resolve. IDs are identities, labels are not — a reviewer's
links and requests point at ids, so:

- Name them `<kind>.<slug>` from what the thing is (`screen.queue`,
  `btn.approve`, `field.amount`), except role ids, which are `security.json`'s
  names.
- **Revising an existing prototype keeps every id whose thing still exists.**
  Rename labels freely; change an id only when its thing is removed or
  genuinely becomes something else.

## Feedback revision

A turn may carry a **prototype feedback batch**: the path of one prototype and
a list of annotations, each naming a screen, a flow (or none), a display
state, the component ids it points at (none means the whole screen) and the
reviewer's request.

- Rewrite **only the named file**, **once**, applying every annotation in that
  single write.
- Resolve each annotation against the file as it stands: an id that no longer
  exists is reported in your reply, never guessed at.
- Preserve every id an annotation does not require you to change. A request
  about one component changes that component and whatever the change makes
  inconsistent — nothing else.
- Touch no other file, including other prototypes. If a request needs a design
  change, say so in your reply instead of making it.

## Closing

End with one short paragraph per prototype: its screens, the roles and flows it
covers, and anything from the design you could not represent.
