/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/** Navigation: the app's side or top navigation, breadcrumbs, tabs and the stepper. */

import { Box, Breadcrumbs as OxygenBreadcrumbs, Link, ListItemButton, Sidebar, Step, StepButton, Stepper as OxygenStepper, Tab, Tabs as OxygenTabs, Typography, type SxProps, type Theme } from "@wso2/oxygen-ui";
import { ChevronRight } from "@wso2/oxygen-ui-icons-react";
import type { ThemeBreadcrumbsProps, ThemeNavigationItem, ThemeNavigationProps, ThemeStepperProps, ThemeTabsProps } from "@wso2/prototype-kit";
import type { ReactNode } from "react";
import { buttonRoot } from "./root.js";

/**
 * Side navigation on Oxygen's `Sidebar`, for `<AppShell nav>` and a side
 * `<Navigation>`. Each entry's selectable root wraps its button through
 * `Sidebar.Item`'s `link` slot, the one place the item takes an element.
 */
export function SideNav({ items, label, sx }: { items: ThemeNavigationItem[]; label?: string | undefined; sx?: SxProps<Theme> }) {
  return (
    <Sidebar
      collapsed={false}
      activeItem={items.find((i) => i.active)?.id ?? ""}
      onSelect={(id) => items.find((i) => i.id === id)?.onPress()}
      {...(sx !== undefined ? { sx } : {})}
    >
      <Sidebar.Nav>
        <Sidebar.Category>
          {label !== undefined && <Sidebar.CategoryLabel>{label}</Sidebar.CategoryLabel>}
          {items.map((item) => (
            <Sidebar.Item key={item.id} id={item.id} link={<Box component="span" aria-current={item.active ? "page" : undefined} sx={{ display: "block" }} {...buttonRoot(item.root)} />}>
              <Sidebar.ItemLabel>{item.label}</Sidebar.ItemLabel>
            </Sidebar.Item>
          ))}
        </Sidebar.Category>
      </Sidebar.Nav>
    </Sidebar>
  );
}

export function Navigation({ layout, appName, items }: ThemeNavigationProps) {
  if (layout === "top") {
    return (
      <Box
        component="nav"
        aria-label={`${appName} navigation`}
        sx={{ gridColumn: "1 / span 2", gridRow: 1, display: "flex", alignItems: "center", gap: 3, px: 3, bgcolor: "background.paper", borderBottom: 1, borderColor: "divider" }}
      >
        <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
          {appName}
        </Typography>
        <Box component="ul" sx={{ display: "flex", gap: 0.5, listStyle: "none", m: 0, p: 0 }}>
          {items.map((item) => (
            <li key={item.id}>
              <ListItemButton
                component="button"
                aria-current={item.active ? "page" : undefined}
                onClick={item.onPress}
                disableRipple
                sx={{
                  py: 1.5,
                  borderBottom: 2,
                  borderColor: item.active ? "primary.main" : "transparent",
                  color: item.active ? "primary.main" : "text.primary",
                  fontWeight: item.active ? 600 : 400,
                  font: "inherit",
                }}
                {...buttonRoot(item.root)}
              >
                {item.label}
              </ListItemButton>
            </li>
          ))}
        </Box>
      </Box>
    );
  }
  return <SideNav items={items} label={appName} sx={{ gridColumn: 1, gridRow: "1 / span 2" }} />;
}

export function Breadcrumbs({ items }: ThemeBreadcrumbsProps) {
  return (
    <OxygenBreadcrumbs aria-label="Breadcrumbs" separator={<ChevronRight size={16} aria-hidden="true" />}>
      {items.map((item) =>
        item.link ? (
          <Link key={item.id} component="button" type="button" variant="body2" underline="hover" color="text.secondary" onClick={item.link.onPress} {...item.link.root}>
            {item.label}
          </Link>
        ) : (
          <Typography key={item.id} variant="body2" color="text.primary" aria-current="page">
            {item.label}
          </Typography>
        ),
      )}
    </OxygenBreadcrumbs>
  );
}

function Panel({ children, role }: { children: ReactNode; role?: "tabpanel" | undefined }) {
  return (
    <Box role={role} sx={{ display: "flex", flexDirection: "column", gap: 2, pt: 2 }}>
      {children}
    </Box>
  );
}

export function Tabs({ tabs, activeId, onOpen, content }: ThemeTabsProps) {
  return (
    <div>
      <OxygenTabs value={activeId ?? false} onChange={(_, id: string) => onOpen(id)} sx={{ borderBottom: 1, borderColor: "divider" }}>
        {tabs.map((t) => (
          <Tab key={t.id} value={t.id} label={t.label} disableRipple {...buttonRoot(t.root)} />
        ))}
      </OxygenTabs>
      <Panel role="tabpanel">{content}</Panel>
    </div>
  );
}

export function Stepper({ steps, activeIndex, onOpen, content }: ThemeStepperProps) {
  return (
    <div>
      <OxygenStepper nonLinear activeStep={activeIndex}>
        {steps.map((s, i) => (
          <Step key={s.id} completed={i < activeIndex}>
            <StepButton aria-current={i === activeIndex ? "step" : undefined} onClick={() => onOpen(s.id)} disableRipple {...buttonRoot(s.root)}>
              {s.label}
            </StepButton>
          </Step>
        ))}
      </OxygenStepper>
      <Panel>{content}</Panel>
    </div>
  );
}
