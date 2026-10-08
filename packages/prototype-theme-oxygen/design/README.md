# `@wso2/prototype-theme-oxygen` — design notes

Every kit component on `@wso2/oxygen-ui` (MUI underneath), so a prototype looks
like a WSO2 product and like the console. Same shape as
`@wso2/prototype-theme-default`: a default-exported `PrototypeTheme`, plus
`dist/frame-runtime.js` and `dist/check-runtime.js` from the kit's
`buildThemeRuntimes` (`scripts/build-runtimes.ts`, after `tsc`). Select it with
`prototype --theme @wso2/prototype-theme-oxygen`; a host imports the runtimes
by those two subpaths.

## Choices

- **Provider.** The console's own theme (`aepTheme` from `@aep/ui-theme`)
  through Oxygen's `OxygenUIThemeProvider`, as the console's `main.tsx`
  applies it, so a prototype cannot drift from the console. One fixed theme:
  no theme switching, so nothing is fetched or stored. The scheme is the
  host's resolved one (the kit's `colorScheme`), set with MUI's
  `useColorScheme().setMode`; without one it follows the system. MUI's scheme
  storage is guarded and the frame has none, so nothing is stored. The kit's Annotate colour (`--proto-select`)
  is the theme's primary.
- **Nothing loaded.** Emotion injects styles inline; Oxygen ships Inter as
  `data:` fonts (the frame CSP allows `font-src data:`).
- **Oxygen's templates first.** Where Oxygen has a component for a kit
  component and it fits the kit's contract, the theme uses it:

  | kit | Oxygen |
  |---|---|
  | `AppShell` | `AppShell` + `Header` (brand title) + `Sidebar` + `PageContent`; user menu: `Menu` + `UserMenu.Header` |
  | `Screen` | `PageContent` (beside a `<Navigation>`) |
  | `Navigation` side / top | `Sidebar` / hand-built tab row (Oxygen has no top nav) |
  | `Heading` page / section | `PageTitle` (+ `Actions`) / the section header (`Typography` h4 + actions) |
  | `Section` | the section header (title, count `Chip`, subtitle, actions) over its content |
  | `Table` | `ListingTable` (`Container`, `Head`, `Body`, `Row`, `Cell`, `RowActions`); status cells are `Chip`s |
  | `EmptyState` | `ListingTable.EmptyState` in a `Card` |
  | `Stat` / `StatGroup` | `Card` > `CardContent` > overline label, h4 value, caption hint, Lucide icon / an auto-fit grid |
  | `Breadcrumbs` | MUI `Breadcrumbs` with `AppBreadcrumbs`' chevron |
  | the rest | Oxygen's MUI components (`Button`, `Chip`, `Alert`, `TextField`, `Tabs`, `Stepper`, `Card`, `Paper`) |

  Not used, because they do not fit: `UserMenu` whole (its menu portals out of
  the scene, and its entries take no element props, so they cannot carry the
  kit's selectable roots and targets; composed as the console's own user menu
  is instead), `AppBreadcrumbs` (its crumbs take no element props either) and
  `StatCard` (label and value only, children discarded: no room for the
  hint; the Card composition is the one Oxygen's design guidance gives for a
  KPI tile with a caption, used for every stat so a group reads as one).
  `Sidebar.Item` takes the kit's root through its `link` slot.
- **No portals.** Dialog and Drawer are drawn in place on Oxygen `Paper`, not
  MUI's `Modal`: a portal leaves the kit's scene (Annotate would not reach
  inside) and draws nothing in the render check. Selects are native for the
  same reason. Menus (the user menu, a row's overflow actions) are an
  `InPlaceMenu`: kept in place (`disablePortal`) and mounted while closed
  (`keepMounted`), so the render check sees their targets; the modal's
  `container` is the trigger's parent, or it would hide the whole app from
  assistive technology.
- **Page width and columns.** `PageContent` is capped at 1200px (Oxygen's
  1400px default leaves tables stretched). Status, number and actions columns
  fit their content (`width: 1%`, no wrap), so text columns share the rest;
  numbers align right.
- **Row actions.** Up to two are text `Button`s in `ListingTable.RowActions`;
  more go behind one "More actions" `IconButton` and an `InPlaceMenu`
  (`components/menu.tsx`, shared with the user menu). The actions cell stops
  the click, so the row is not pressed too. Menu entries are annotatable only
  while the menu is open, as the user menu's are.
- **Selectable roots.** Rows, row actions, nav items, tabs, steps, crumbs and menu entries
  spread the kit's `SelectableRootProps`; `components/root.ts` drops undefined
  entries so they type-check against `ButtonBase`.
- **Required fields** show Oxygen's asterisk; it is `aria-hidden`, so a
  field's accessible name is its label.

## Render check

Oxygen UI's single-module bundle evaluates Prism's language files, which read
a bare `Prism` global that Prism publishes on Node's `global`. The frame has
`window`, the render check's bare context has neither, so this theme's
`scripts/build-runtimes.ts` passes `define: { global: "globalThis" }` to the
kit's `buildThemeRuntimes`.

## Size

Oxygen's bundle cannot be tree-shaken (it pulls in `@mui/x-data-grid`, Prism
and the inlined fonts, about 290 KB, with any import). Measured 2026-10-04,
minified (the stat icons, Lucide's, add about 20 KB):

| runtime | Oxygen | gzip | default theme | gzip |
|---|---|---|---|---|
| `frame-runtime.js` | 1.70 MB | 636 KB | 0.45 MB | 122 KB |
| `check-runtime.js` | 1.51 MB | 592 KB | 0.24 MB | 74 KB |

The frame was 2.13 MB (722 KB gzip) until it stopped parsing the manifest
(zod, about 450 KB minified, now stays in the host).

`@wso2/oxygen-ui-icons-react` imports a small CSS file (Lucide stroke width),
so esbuild also writes `frame-runtime.css` and `check-runtime.css`. Nothing
loads them; icons draw at Lucide's default stroke.

## Tests

`test/check.test.ts` runs the CLI's `prototype check --theme` over the CLI's
fixtures: every valid one passes, and each render-stage failure reports what
the default theme reports (the app shell's included). `pnpm test:browser`
plays four fixtures under `prototype preview --theme` in Chromium (navigation,
forms, tabs, dialog, drawer, stepper, the app shell's user menu, role and sign
out, Annotate, the host's loading cover, no requests beyond the preview
server).
It also fails on any uncaught error in the page or its sandboxed frame. The
frame has no `allow-same-origin`, so `localStorage` throws there; Oxygen and
MUI X only touch it inside try/catch (a probe, `storageManager={null}` for the
colour scheme), so a prototype raises nothing. A harness that runs its own
script in every frame (a Playwright init script writing `localStorage`) does
raise it, in its own code: guard such a script or run it in the top frame only.
