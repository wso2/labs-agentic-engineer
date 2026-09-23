# The sample app

`src/` is Oxygen UI's own reference application, `samples/oxygen-ui-test-app`
from https://github.com/wso2/oxygen-ui, vendored here so it reaches every
coding run through the project mirror (the installed `@wso2/oxygen-ui` package
does not ship it — 0.13.1 ships only `.claude/*.md` and five small skills).

It is the **structure** a generated web app matches: the app shell in
`layouts/AppLayout.tsx`, the routes grouped under layouts in
`config/appRoutes.tsx`, and every page as `PageContent` > `PageTitle` > content.
`SKILL.md` says which files to read for which screen, and which parts the
platform overrides (`main.tsx`'s entry wiring, `mock-data/`).

Refresh: copy `samples/oxygen-ui-test-app/src` from the oxygen-ui release that
matches the `@wso2/oxygen-ui` version Setup installs, replace `src/` wholesale,
and re-check `references/app-structure.md`'s excerpts against it. Last taken
from the copy vendored under `apps/console/.claude/skills/oxygen-ui/sample`
(repo commit 2edc47eb, 2026-07-03).

## Known defects — take the structure, not these lines

Review found demo shortcuts in this copy. They are left as upstream wrote
them (a refresh must stay a wholesale copy; fixes belong in wso2/oxygen-ui),
and a generated app must not reproduce them:

- `components/LoginBox.tsx` pre-fills the username and password state with
  credentials. A form starts empty; a generated app has no sign-in form at all
  when it has an auth dependency.
- `layouts/AppLayout.tsx` reads the project route param as `projectId` while
  `config/appRoutes.tsx` declares it `:id`, so the project switcher never sees
  a value. Read the name the route declares.
- `pages/ComponentList.tsx` and `pages/EmptyComponentList.tsx` navigate to
  absolute paths that drop the `/o/:orgId` prefix. Keep every path inside the
  route the page lives under.
- `pages/ProjectOverview.tsx` renders the first project when the id matches
  nothing. Render the not-found state instead.
- `pages/Projects.tsx` writes `useParams() || 'default-org'`, which can never
  fall back. Default in the destructuring.
- `pages/Organizations.tsx` collects an advanced filter it never applies, with
  empty option lists. Wire a filter to the list or leave the control out.
- `pages/ComponentCreate.tsx` sends both cards into the "import" screen. Bind
  each card to its own flow.
- `pages/SettingsPage.tsx` gives `Autocomplete` separately created `value` and
  `options` objects without `isOptionEqualToValue`, so the selection may not
  render as selected.
- `pages/ComponentList.tsx` places a `FormLabel` next to a `Select` without
  associating them. Use `TextField select label="…"` (the wireframe table).

