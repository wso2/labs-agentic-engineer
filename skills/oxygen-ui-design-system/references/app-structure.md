# Oxygen UI — App Structure & Setup

The canonical structure, mirroring Oxygen's own sample app — whose real
source is in `sample/src/` beside this skill; these are its excerpts, adjusted
to this platform where `SKILL.md` says the platform overrides the sample. It sits inside the layout
`react-webapp` prescribes (`src/env.ts`, `src/generated/`, `src/api.ts`, `mock/`, `nginx/` are
that skill's and stay as it says); this file adds `config/`, `layouts/` and `pages/`. Use this when scaffolding a
new app or adding pages/layouts to an existing one. Stack: **React 19 + Vite + react-router
v7 + @wso2/oxygen-ui**.

## Table of contents
- [Dependencies](#dependencies)
- [File layout](#file-layout)
- [main.tsx — provider + router](#maintsx)
- [App.tsx + appRoutes](#approutes)
- [Layouts](#layouts)
- [Pages](#pages)
- [Icons](#icons)

---

## Dependencies

`react-webapp` scaffolds the app; the install commands are in `SKILL.md`, Setup
(React pinned to the exact version Oxygen's peer dependency names, then
`@wso2/oxygen-ui`, `@wso2/oxygen-ui-icons-react`, `react-router`). Oxygen bundles MUI,
MUI X, Emotion and lucide as its own dependencies — **never install `@mui/*`,
`@emotion/*` or `lucide-react`** (a second copy breaks theming at runtime).

---

## File layout

```
src/
├── main.tsx                # OxygenUIThemeProvider + Router + <App/>
├── App.tsx                 # maps appRoutes -> <Routes>/<Route>
├── config/
│   └── appRoutes.tsx       # routes grouped under layouts
├── layouts/
│   ├── AppLayout.tsx       # the sample app's shell — see sample/src/layouts/AppLayout.tsx
│   ├── GateLayout.tsx      # login / unauthenticated
│   └── DefaultLayout.tsx   # standalone pages (home, error)
└── pages/
    ├── Projects.tsx        # PageContent > PageTitle > content
    └── ...
```

---

## main.tsx

Wrap the root once in `OxygenUIThemeProvider`, then the router, then `<App/>`. Use a single
`theme` for one theme, or `themes={[…]}` + `initialTheme` to allow theme switching.

```tsx
import {
  OxygenUIThemeProvider,
  OxygenTheme,
  AcrylicOrangeTheme,
  WSO2Theme,
} from '@wso2/oxygen-ui';
import { BrowserRouter } from 'react-router';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './App';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <OxygenUIThemeProvider
      themes={[
        { key: 'default', label: 'Default', theme: OxygenTheme },
        { key: 'orange', label: 'Acrylic Orange', theme: AcrylicOrangeTheme },
        { key: 'wso2', label: 'WSO2', theme: WSO2Theme },
      ]}
      initialTheme="default"
    >
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </OxygenUIThemeProvider>
  </StrictMode>,
);
```

Single-theme variant: `<OxygenUIThemeProvider theme={OxygenTheme}>`.

---

## appRoutes

`App.tsx` maps a route config to react-router. Routes are grouped under **layouts** (a
parent route whose `element` is a layout that renders `<Outlet/>`).

```tsx
// src/App.tsx
import { Route, Routes } from 'react-router';
import appRoutes, { type AppRoute } from './config/appRoutes';

// Recursive, because `AppRoute.children` is: a renderer that walks only one
// level type-checks against a grandchild route and then never renders it.
// The branch is required — `RouteProps` is a union, and an index route may
// not carry children, so passing both fails to compile.
function renderRoute(route: AppRoute, key: string) {
  if (route.index) return <Route key={key} index element={route.element} />;
  return (
    <Route key={key} path={route.path} element={route.element}>
      {route.children?.map((child, i) => renderRoute(child, child.path ?? `index-${i}`))}
    </Route>
  );
}

export default function App() {
  return <Routes>{appRoutes.map((route, i) => renderRoute(route, route.path ?? `layout-${i}`))}</Routes>;
}
```

```tsx
// src/config/appRoutes.tsx
import { type RouteProps, Navigate } from 'react-router';
import AppLayout from '../layouts/AppLayout';
import GateLayout from '../layouts/GateLayout';
import LoginPage from '../pages/LoginPage';
import HomePage from '../pages/HomePage';
import Projects from '../pages/Projects';
import SettingsPage from '../pages/SettingsPage';

export interface AppRoute extends Omit<RouteProps, 'children'> {
  children?: AppRoute[];
  label?: string;
}

const appRoutes: AppRoute[] = [
  { path: '/', element: <Navigate to="/login" replace /> },
  {
    element: <GateLayout />,
    children: [{ path: '/login', element: <LoginPage />, label: 'Login' }],
  },
  {
    element: <AppLayout />,
    children: [
      { path: '/home', element: <HomePage />, label: 'Home' },
      { path: '/projects', element: <Projects />, label: 'Projects' },
      { path: '/settings', element: <SettingsPage />, label: 'Settings' },
    ],
  },
];

export default appRoutes;
```

---

## Layouts

### AppLayout — the signed-in app shell

`AppShell` is a compound component: `AppShell.Navbar` (a `Header`), `AppShell.Sidebar` (a
`Sidebar`), `AppShell.Main` (renders the routed page via `<Outlet/>`), and
`AppShell.Footer`. Sidebar items navigate with `link={<Link to="…" />}`.

```tsx
import {
  AppShell, Header, Sidebar, Footer, UserMenu, ColorSchemeToggle, Divider,
} from '@wso2/oxygen-ui';
import { Home, FolderOpen, Settings, LogOut, User } from '@wso2/oxygen-ui-icons-react';
import { Outlet, Link, useLocation } from 'react-router';
import type { JSX } from 'react';

export default function AppLayout(): JSX.Element {
  const { pathname } = useLocation();
  const active = pathname.includes('/settings') ? 'settings'
    : pathname.includes('/projects') ? 'projects' : 'home';

  return (
    <AppShell>
      <AppShell.Navbar>
        <Header>
          <Header.Toggle />
          <Header.Brand>
            <Header.BrandTitle>Console</Header.BrandTitle>
          </Header.Brand>
          <Header.Spacer />
          <Header.Actions>
            <ColorSchemeToggle />
            <Divider orientation="vertical" flexItem sx={{ mx: 2 }} />
            <UserMenu>
              <UserMenu.Trigger name="Jane Doe" />
              <UserMenu.Header name="Jane Doe" email="jane@example.com" />
              <UserMenu.Item icon={<User />} label="Profile" onClick={() => {}} />
              <UserMenu.Item icon={<LogOut />} label="Sign out" onClick={() => {}} />
            </UserMenu>
          </Header.Actions>
        </Header>
      </AppShell.Navbar>

      <AppShell.Sidebar>
        <Sidebar activeItem={active}>
          <Sidebar.Nav>
            <Sidebar.Category>
              <Sidebar.Item id="home" link={<Link to="/home" />}>
                <Sidebar.ItemIcon><Home /></Sidebar.ItemIcon>
                <Sidebar.ItemLabel>Home</Sidebar.ItemLabel>
              </Sidebar.Item>
              <Sidebar.Item id="projects" link={<Link to="/projects" />}>
                <Sidebar.ItemIcon><FolderOpen /></Sidebar.ItemIcon>
                <Sidebar.ItemLabel>Projects</Sidebar.ItemLabel>
              </Sidebar.Item>
              <Sidebar.Item id="settings" link={<Link to="/settings" />}>
                <Sidebar.ItemIcon><Settings /></Sidebar.ItemIcon>
                <Sidebar.ItemLabel>Settings</Sidebar.ItemLabel>
              </Sidebar.Item>
            </Sidebar.Category>
          </Sidebar.Nav>
        </Sidebar>
      </AppShell.Sidebar>

      <AppShell.Main>
        <Outlet />
      </AppShell.Main>

      <AppShell.Footer>
        <Footer><Footer.Copyright>© WSO2 LLC</Footer.Copyright></Footer>
      </AppShell.Footer>
    </AppShell>
  );
}
```

### GateLayout — login / unauthenticated

```tsx
import { Outlet } from 'react-router';
import { Box, ColorSchemeToggle, Layout, ParticleBackground, Stack, ThemeSwitcher } from '@wso2/oxygen-ui';

export default function GateLayout() {
  return (
    <Layout.Content>
      <ParticleBackground opacity={0.5} />
      <Box sx={{ height: '100%' }}>
        <Stack direction="row" spacing={2} sx={{ position: 'fixed', top: '3.7rem', left: '8rem', zIndex: 2 }}>
          <ThemeSwitcher />
          <ColorSchemeToggle />
        </Stack>
        <Outlet />
      </Box>
    </Layout.Content>
  );
}
```

`DefaultLayout` is the same shape without `ParticleBackground` — for standalone pages
(home, error).

---

## Pages

**Every page returns `PageContent` > `PageTitle` > content.** Use a `ListingTable` for list
views, a `Grid` of `Card`s for galleries, `Form.*` for forms.

```tsx
import {
  Box, Button, Card, CardContent, Chip, Grid, IconButton, InputAdornment,
  PageContent, PageTitle, TextField, Typography,
} from '@wso2/oxygen-ui';
import { Search, Plus, MoreVertical, Folder } from '@wso2/oxygen-ui-icons-react';
import { useState, type JSX } from 'react';

export default function Projects(): JSX.Element {
  const [query, setQuery] = useState('');

  return (
    <PageContent>
      <PageTitle>
        <PageTitle.Header>Projects</PageTitle.Header>
        <PageTitle.SubHeader>Manage your projects and workflows</PageTitle.SubHeader>
      </PageTitle>

      <Box sx={{ display: 'flex', gap: 2, mb: 3 }}>
        <TextField
          placeholder="Search projects"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          slotProps={{ input: { startAdornment: (
            <InputAdornment position="start"><Search size={18} /></InputAdornment>
          ) } }}
        />
        <Button variant="contained" startIcon={<Plus size={18} />}>New project</Button>
      </Box>

      <Grid container spacing={3}>
        {projects.map((p) => (
          <Grid key={p.id} size={{ xs: 12, sm: 6, md: 4 }}>
            <Card onClick={() => {/* navigate */}}>
              <CardContent>
                <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <Folder size={20} />
                  <IconButton size="small"><MoreVertical size={18} /></IconButton>
                </Box>
                <Typography variant="h6" sx={{ mt: 1 }}>{p.name}</Typography>
                <Chip label={p.status} color="success" size="small" sx={{ mt: 1 }} />
              </CardContent>
            </Card>
          </Grid>
        ))}
      </Grid>
    </PageContent>
  );
}
```

For a **list/table page**, swap the `Grid` for a `ListingTable` (see
`references/components.md` for the full `ListingTable.*` API).

---

## Icons

Import from `@wso2/oxygen-ui-icons-react` using **bare lucide names** (the convention the
sample app uses), plus WSO2 brand icons:

```tsx
import { Home, Settings, Search, Plus, Trash2, Pencil, Users, Bell, FolderOpen } from '@wso2/oxygen-ui-icons-react';
import { WSO2, GitHub, Google } from '@wso2/oxygen-ui-icons-react';
```

Sizing: `<Search size={18} />`. A lucide name resolves bare or `Icon`-suffixed (`Pencil`
and `PencilIcon` both work); a name lucide does not have (`DashboardIcon`) does not. Brand
icons carry no `Icon` suffix and are cased as listed (`GitHub`, `Google`). Browse real names
at https://lucide.dev/icons/.
