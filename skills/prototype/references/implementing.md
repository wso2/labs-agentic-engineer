# Implementing a prototype (coding run)

The prototype is the **screen contract** of a `web-application`. The reviewer
approved it before the build, and the built app must match it. Two files in
`specs/design/components/<name>/` make it:

- `prototype.json`, the manifest: the roles, the display states, the screens
  (each with the `roleIds` that reach it) and the flows (one role's walk
  through screens, in order). The schema is in this skill's `SKILL.md`,
  **The manifest**.
- `prototype.tsx`: one React component per manifest screen, on the kit
  `@wso2/prototype-kit`, with the mock records written in the file. The kit's
  components and props are in `SKILL.md`, **The kit**.

Read both files before you write the first page. You have no prototype CLI:
read the source. The manifest carries no story ids. Map the stories of your
issue to screens yourself, from the screen names, the flows and what each
screen holds. The prototype wins over the issue text when the two differ.

## Screen for screen

- **One manifest screen is one route and one page.** Name both from the screen
  id (`screen.risk-queue` → `/risk-queue`, `RiskQueuePage`). The screen's
  `name` is its label in the rail and in reports. The visible title is the
  screen's own `Heading`.
- **No invented screens, no dropped screens.** Build every screen in the
  manifest, including the shell screens Account, Settings (only when the
  manifest has one) and Signed out. If
  a screen cannot be built as drawn (its data has no API, its action has no
  endpoint), build the rest of it, leave the gap explicit in the UI, and name
  it in your report. Never fold it into another page.
- **Every role gets its screens.** A screen reaches the roles in its
  `roleIds`. Where the prototype draws one screen per role, each is its own
  route; do not collapse them into one page that toggles on the viewer. Where
  one screen branches on `useRole()`, build one route, and gate each
  role-specific control with `<Can op={…}>` on the operation that control
  calls. Share code between routes where the views overlap, as long as each
  route shows what its screen draws.
- **`/forbidden` and `NoAccess` are in no prototype.** They are the one
  carve-out from "build only the screens the manifest lists". Every app with
  an auth dependency has both. They come from `idp-authentication`'s
  `authz` asset, and their absence is a defect.

Gate each screen on the **operation it loads** (the call whose answer the
screen renders on open) through `idp-authentication`'s `authz` assets.
Copy them verbatim; never re-implement them. Name the operation once in
`src/authz/screens.ts` (`loads: "GET /me/claims"`). Then put
`<RequireOperation op={…} />` around the route and `<Can op={…}>` around the
nav item that reaches it, both read from the generated operations table. A
form with no load call names the operation its submit makes. `loads: null` is
only for a screen that needs no operation. A screen drawn on a bare `<Screen>`
root (Signed out, a landing page) is public: route it above the sign-in guard.
All of this is **presentation only**: the gateway enforces the operation's
scope. A user who holds scopes but opens a gated screen by URL sees
`Forbidden` inside the shell. A user who can reach nothing sees `NoAccess`
instead of the shell. Never read a groups claim, and never fall back to a
default role. A component with no auth dependency has no roles: build each
screen as drawn.

## The shell

`<AppShell>` is the design system's app shell on every signed-in screen:

- `product` (the manifest `name` when absent) is the brand in the header.
- `nav` entries are the sidebar rail, in that order; each `to` links to that
  screen's route. The prototype draws the rail of one role at a time. The app
  has **ONE** rail whose items are each wrapped in `<Can op={…}>`. That rail
  shows each role its own entries and also covers a user with two roles.
  Navigation stays in the rail; the header holds none.
- `user` is the signed-in user from the session, in the header's user menu.
- `account` and `settings` are user menu items that open the Account and
  Settings routes. A prototype without a `settings` screen has no Settings
  item and no Settings route: never add one. `menu` entries are more user
  menu items, after Settings.
- `signOut` is the last user menu item, outside any `Can`. It calls
  `signOut()`.

## Element for element

Walk each screen component top to bottom. Make each kit element a real
element, in that order, with that literal content. The pinned design system
names the exact component: for Oxygen, see `oxygen-ui-design-system`'s
**Implementing a prototype with Oxygen**.

| Kit | Build |
|---|---|
| `Heading` (page) + `actions` | the page title, with its buttons beside it |
| `Section` | a titled part of the page with its `count`, `subtitle` and its own `actions` |
| `Stat` in a `StatGroup` | a row of stat tiles: label, value from live data, `hint` as the caption, `icon` |
| `Table` | a data table with exactly those columns, bound to the real list. A column the list endpoint does not return is filled from **one** more request to another list operation that every role loading the screen may call (a name joined on id from the entity list; a count from the child list filtered once and grouped). Never one request per row, and never dropped while some list operation can supply it. A column that no list can supply is a contract gap: report it. The status column is a status chip. Row `actions` are row buttons. `empty` is the table's empty state |
| `Detail`, `Timeline` | the record's fields as label and value; its activity, newest first |
| `Form` + `Field` | a real form wired to its operation. A Field's `label` is the control's label, `type` sets the control (`select` with its `options`, `switch`, `date`, `textarea`, `number`, `text`), `required` and `pattern` are validation |
| `Filters` | filter controls that narrow the list through the list operation's parameters |
| `Badge`, `Alert`, `EmptyState`, `Text`, `Link`, `Breadcrumbs`, `Tabs`, `Stepper` | the matching primitive. A status is a chip, not a paragraph |
| `Dialog`, `Drawer` | the same overlay, opened by the same control |
| `Button` | `emphasis="primary"` is primary on the page, and only those; `"danger"` is destructive; any other is outlined |
| `Split`, `Grid`, `Stack` | the same arrangement: side by side in that ratio, equal columns, a row or a column |

Labels are **copy**, not hints: use the prototype's text exactly as it is
written, letter case included: the screen titles (the `Heading` text), the
navigation entries (the `nav` labels), the back links (the target screen's
`name`), the buttons, the column headers and the field labels. The prototype
writes them in sentence case, and the app does too: "My claims" stays "My
claims", never "My Claims", and "Submit claim" stays "Submit claim". Rename
one only when the wording is wrong for the data the API returns, and say so in
your report. Element ids (`<kind>.<slug>`) name things in reports. They need
not be in the DOM.

## Display states

`prototype.tsx` branches on `useDisplayState()` to show what each manifest
state draws. A display state is a real condition on its screen's own route.
It is **never** its own route or screen.

| State | Condition | Shows |
|---|---|---|
| `state.empty` | the list has no rows | the `EmptyState` the screen draws |
| `state.validation-error` | the operation answers 400 or 422 | each Field's `error` and the `ValidationSummary` |
| `state.failed` | the request fails (5xx, network) | the error `Alert` |
| `state.delayed` | the call is slow | the info or warning `Alert`, while the call is pending |

Another state names its condition in the operation's responses. Every table,
list and stat also needs a loading state, drawn or not. A region whose
operation the viewing role cannot call shows its own forbidden state. It does
not vanish, and it is not the whole screen's `Forbidden`.

## Navigation

- Each `to` (on a nav entry, Button, Link, table row, row action or
  breadcrumb) is working navigation to that screen's route. The rail item of
  the current screen is the active link and is still a link.
- `params` become route parameters: a row with `to: "screen.detail"` and
  `params: { expense: e.id }` opens `/expense/:expense`. The target reads the
  parameter with `useParams`. The prototype falls back to the first record
  when a parameter is missing; the app does not. An unknown id shows a
  not-found state.
- `useNav().go(…)` in an `onPress` is navigation after an action. Navigate
  when the action's request succeeds.
- A create lands where the new row shows. If that list has a filter, it opens
  with a filter value that includes the new row.
- Each manifest flow is a walk the role can do by clicking, from its first
  screen through each screen in order. A screen you cannot reach from its
  flow is a broken page, even if it renders.

## Mock data

The records in `prototype.tsx` (`defineApp({ data })` and the constants it
names) seed the mock handlers. The reviewer saw these records, so mock mode
shows them. Map each record onto the schema of the operation that returns it:
the prototype names fields in user words, the schema has the real names and
formats. Derive each stat value from the records it counts, so the numbers
agree with the table.

## Fidelity

Fidelity is **structure, not pixels**. The screens, navigation, element kinds,
display states, labels and flows match the prototype. Spacing and exact layout
are free: the design system draws them.

## Evidence

The walk is the evidence. Once the build is clean, `mock-verification` opens
the app in mock mode, walks each manifest flow under its role, and reports one
line per item. You write no checklist of your own. A screen you could not
build as drawn is a gap you name in your report to the lead.
