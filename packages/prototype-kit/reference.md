# @wso2/prototype-kit reference

Generated from the kit's types by `pnpm --filter @wso2/prototype-kit gen`; do not edit.

A prototype is `prototype.json` (the manifest) and `prototype.tsx`, which imports only `react` and `@wso2/prototype-kit`. Every component with an `id` is an element a reviewer can point at: give each a stable id, unique on its screen.

## Module

### `defineApp(definition: PrototypeAppDefinition): PrototypeApp`

Declare the prototype: `export default defineApp({ screens, data })`.

### `PrototypeAppDefinition`

- `screens: Record<string, ComponentType>` — One component per manifest screen, keyed by the screen's id.
- `data?: Record<string, unknown>` — The mock data the screens share, as JSON literals. An array of records that each have a string `id` is a collection (`useCollection(name)`); anything else is a single value (`useValue(key)`). Changes live until Reset data (or a reload without `--persist`).

## View hooks

### `useNav(): KitNav`

### `KitNav`

- `screen: string` — The screen shown.
- `go: (screenId: string, params?: Record<string, string>) => void` — Go to a screen, optionally with params the target reads with `useParams`.

### `useParams(): Readonly<Record<string, string>>`

The params the navigation to this screen carried. Empty when the reviewer opened the screen from the preview's pickers, so every screen must render without them (fall back to the first record).

### `useRole(): string`

The id of the role the reviewer is viewing as.

### `useDisplayState(): string`

The id of the display state the reviewer chose (e.g. empty, loading, error).

## Data hooks

### `useCollection<T extends { id: string; }>(name: string): Collection<T>`

The collection `name` from `defineApp({ data })`: an array of records that each have a string `id`.

### `Collection`

A collection of mock records, and the ways to change it for every screen.

- `items: readonly T[]` — The records, in insertion order.
- `get: (id: string) => T | undefined`
- `create: (record: Omit<T, "id">) => string` — Adds a record and returns its new id, `${name}-${n}` with `n` past the highest number in use.
- `update: (id: string, patch: Partial<Omit<T, "id">>) => void`
- `remove: (id: string) => void`

### `useValue<T>(key: string): [T, (next: T | ((previous: T) => T)) => void]`

The single value `key` from `defineApp({ data })`, and a setter shared by every screen.

### `useToday(): string`

Today's date, fixed (`PROTOTYPE_TODAY`): a prototype never reads the clock.

### `PROTOTYPE_TODAY` = `"2026-01-15"`

The fixed date every prototype treats as today (ISO `YYYY-MM-DD`).

## Layout

### `<AppShell>`

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

### `AppShellUser`

- `name: string`
- `email?: string` — Shown under the name in the user menu.
- `role?: string` — The role beside the name; the viewing role's name in prototype.json by default, so a role switch shows in the header.

### `<Screen>`

The root of a screen outside the app shell (signed out, a landing page): its content, and navigation if any.

- `nav?: ReactNode` — Navigation for an app drawn without `<AppShell>`: one `<Navigation>`, usually shared by every screen.
- `children?: ReactNode`

### `<Section>`

A titled part of a screen — a table, a form, a group of stats — with the actions that belong to it.

- `id: string`
- `title: string`
- `subtitle?: string` — One line under the title: what the section holds or why.
- `count?: number` — How many records the section lists, shown beside the title.
- `actions?: ReactNode` — The section's own Buttons ("New request" above the requests it adds to).
- `children?: ReactNode`

### `<Stack>`

Children in a column (default) or a wrapping row.

- `direction?: "row" | "column"`
- `children?: ReactNode`

### `<Grid>`

Children in 1–6 equal columns.

- `columns: number` — 1–6 equal columns (one column on a narrow window).
- `children?: ReactNode`

### `<Split>`

Two panes side by side (stacked on a narrow window).

- `left: ReactNode`
- `right: ReactNode`
- `ratio?: number` — The left pane's share of 12 columns (default 6).

### `<Detail>`

A read-only record: label/value pairs.

- `id: string`
- `title?: string`
- `fields: DetailField[]`

### `DetailField`

- `label: string`
- `value: string`

## Navigation

### `<Navigation>`

Navigation for an app drawn on `<Screen nav>` (`<AppShell nav>` draws its own). Its items are what a reviewer points at.

- `id: string`
- `layout: "side" | "top"`
- `items: NavigationItem[]`

### `NavigationItem`

- `id: string`
- `label: string`
- `to: string` — The screen the entry opens; it shows as active there.

### `<Breadcrumbs>`

A trail back to where the reviewer came from.

- `id: string`
- `items: BreadcrumbItem[]` — The trail, root first.

### `BreadcrumbItem`

- `id: string`
- `label: string`
- `to?: string` — Where the crumb leads; the last crumb is the current page and takes none.
- `params?: Record<string, string>`

### `<Tabs>`

Tabbed panels. Each tab header is an element a reviewer can point at (its `id`); the Tabs itself is layout.

- `id: string`
- `tabs: Panel[]`
- `active?: string` — The open tab, when the screen controls it; the first tab otherwise.
- `onChange?: ((tabId: string) => void)`

### `<Stepper>`

A multi-step wizard. Each step header is an element a reviewer can point at (its `id`).

- `id: string`
- `steps: Panel[]`
- `active?: string` — The current step, when the screen controls it (a Next button); the first step otherwise.
- `onChange?: ((stepId: string) => void)`

### `Panel`

- `id: string`
- `label: string`
- `content: ReactNode`

## Content

### `<Heading>`

A page or section title, with the actions beside it.

- `id: string`
- `text: string`
- `level?: "page" | "section"` — `page` (default) for a screen's title; `section` titles a part of it. Prefer `<Section>` for a part with its own actions or records.
- `actions?: ReactNode` — Buttons and Links beside the heading.

### `<Text>`

A paragraph of body text.

- `id: string`
- `text: string`
- `tone?: "primary" | "secondary"` — `secondary` (default) for body copy, `primary` for emphasis.

### `<Badge>`

A status chip.

- `id: string`
- `label: string`
- `tone?: Tone`

### `<Stat>`

A headline number with its label. Put a screen's stats side by side in a `<StatGroup>`.

- `id: string`
- `label: string`
- `value: string`
- `hint?: string` — A short line under the value that gives it scale or context: "of 20 days", "3 due this week".
- `icon?: StatIcon` — An icon beside the label.
- `tone?: Tone` — Colours the icon (`default` is the theme's primary).

### `StatIcon` = `"Activity" | "Bell" | "Briefcase" | "Bug" | "Building2" | "Calendar" | "CalendarCheck" | "CalendarClock" | "CalendarDays" | "ChartColumn" | "CircleCheck" | "CircleX" | "ClipboardList" | "Clock" | "Cloud" | "Cpu" | "CreditCard" | "Database" | "DollarSign" | "FileText" | "Gauge" | "Globe" | "HeartPulse" | "Hourglass" | "Inbox" | "Layers" | "ListChecks" | "Lock" | "Mail" | "MessageSquare" | "Package" | "Plane" | "Receipt" | "Rocket" | "Server" | "Shield" | "ShoppingCart" | "Star" | "Tag" | "Ticket" | "Timer" | "TrendingDown" | "TrendingUp" | "TriangleAlert" | "Truck" | "User" | "UserCheck" | "Users" | "Wallet" | "Zap"`

The icons a `<Stat>` may show, named as in WSO2's Oxygen icon set (Lucide's names). A theme draws each or none; an unknown name fails the check.

### `<StatGroup>`

A row of `<Stat>`s at equal widths, wrapping on a narrow window.

- `children?: ReactNode` — The `<Stat>`s, in reading order.

### `<Alert>`

A callout: info, success, warning or error.

- `id: string`
- `tone: Tone`
- `title?: string`
- `text: string`

### `<EmptyState>`

What a list shows when there is nothing in it.

- `id: string`
- `title: string`
- `text: string`
- `actions?: ReactNode` — The Buttons that start something.

### `<Button>`

A button. Give it `to` to navigate, `onPress` to do anything else, `submit` to submit its form.

- `id: string`
- `label: string`
- `emphasis?: "primary" | "danger"` — `primary` for the screen's main action, `danger` for a destructive one; outlined otherwise.
- `disabled?: boolean`
- `submit?: boolean` — Submits the enclosing `<Form>` (which validates its fields, then calls its `onSubmit`).
- `to?: string` — Navigate to this screen when pressed. Prefer it to `onPress` for plain navigation: the checks verify it.
- `params?: Record<string, string>` — The params `to` carries, read on the target with `useParams`.
- `onPress?: (() => void)` — Run when pressed: change mock data, open a dialog, navigate conditionally.

### `<Link>`

An inline link; presses like a Button.

- `id: string`
- `label: string`
- `to?: string` — Navigate to this screen when pressed. Prefer it to `onPress` for plain navigation: the checks verify it.
- `params?: Record<string, string>` — The params `to` carries, read on the target with `useParams`.
- `onPress?: (() => void)` — Run when pressed: change mock data, open a dialog, navigate conditionally.

### `Tone` = `"default" | "info" | "success" | "warning" | "error"`

### `Pressable`

- `to?: string` — Navigate to this screen when pressed. Prefer it to `onPress` for plain navigation: the checks verify it.
- `params?: Record<string, string>` — The params `to` carries, read on the target with `useParams`.
- `onPress?: (() => void)` — Run when pressed: change mock data, open a dialog, navigate conditionally.

## Forms

### `<Form>`

A form card: its Fields, then its action Buttons. Validates on submit.

- `id: string`
- `title?: string`
- `children?: ReactNode` — Its Fields.
- `actions?: ReactNode` — Its Buttons, under the fields; a `<Button submit>` submits.
- `onSubmit?: ((values: Record<string, string>) => void)` — Called with every field's value, keyed by name, when a submit passes validation.

### `<Field>`

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

### `FieldType` = `"number" | "text" | "select" | "date" | "textarea" | "switch"`

### `<Filters>`

A filter bar above a list.

- `id: string`
- `children?: ReactNode` — Its Fields, laid out compactly in a row.

### `<ValidationSummary>`

The problems a failed submit shows, for a display state that shows them without a submit.

- `id: string`
- `issues: string[]`

## Data

### `<Table>`

A table of records.

- `id: string`
- `title?: string`
- `columns: (string | TableColumn)[]`
- `rows: TableRow[]`
- `onRowPress?: ((rowId: string) => void)` — Run when a row is pressed, with its id; a row with neither this nor `to` is only highlighted.
- `empty?: ReactNode` — What an empty table shows instead (an `<EmptyState>`).

### `TableColumn`

- `label: string`
- `kind?: "number" | "text" | "status"` — `text` (default); `number` aligns figures to the right; `status` shows each row's `status` as a badge (one per table). A plain string is a `text` column.

### `TableRow`

- `id: string` — The row's element id: unique on the screen (prefix it with the table's).
- `cells: string[]` — One per `text` or `number` column, in column order; the status column takes `status`.
- `status?: TableStatus` — The badge in the table's status column.
- `actions?: TableAction[]` — What can be done to this record, drawn as buttons in a trailing column; pressing one does not press the row.
- `tone?: Tone` — Colours the last cell as a status, in a table without a status column. Prefer `status`.
- `to?: string` — Open this screen when the row is pressed.
- `params?: Record<string, string>`

### `TableStatus`

- `text: string`
- `tone: Tone`

### `TableAction`

- `id: string` — The action's element id: unique on the screen (prefix it with the row's).
- `label: string`
- `emphasis?: "danger"` — `danger` for a destructive action.
- `to?: string` — Navigate to this screen when pressed. Prefer it to `onPress` for plain navigation: the checks verify it.
- `params?: Record<string, string>` — The params `to` carries, read on the target with `useParams`.
- `onPress?: (() => void)` — Run when pressed: change mock data, open a dialog, navigate conditionally.

### `<Timeline>`

An activity history.

- `id: string`
- `title?: string`
- `entries: TimelineEntry[]` — Newest first.

### `TimelineEntry`

- `when: string`
- `who: string`
- `text: string`

## Overlays

### `<Dialog>`

A modal.

- `id: string`
- `title: string`
- `open: boolean`
- `onClose: () => void`
- `children?: ReactNode`
- `actions?: ReactNode` — Its Buttons, at the bottom.

### `<Drawer>`

A side panel.

- `id: string`
- `title: string`
- `open: boolean`
- `onClose: () => void`
- `children?: ReactNode`

## Finding codes

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
