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

/**
 * Navigation: the app's side or top navigation, breadcrumbs, tabs and the
 * stepper. A navigation entry leading to a screen the viewing role cannot
 * reach is not drawn, so one navigation serves every role.
 */

import { useState, type ReactNode } from "react";
import { isScreen, reaches, useKit, type KitContextValue } from "../runtime/context.js";
import { pressHandler } from "../runtime/press.js";
import { SelectableBox, requireId, selectableRootProps, type SelectableRootProps } from "../runtime/selectable.js";
import { useThemed } from "../theme/context.js";

export interface NavigationItem {
  id: string;
  label: string;
  /** The screen the entry opens; it shows as active there. */
  to: string;
}

export interface NavigationProps {
  id: string;
  layout: "side" | "top";
  items: NavigationItem[];
}

export interface ThemeNavigationItem {
  id: string;
  label: string;
  active: boolean;
  onPress: () => void;
  root: SelectableRootProps;
}

export interface ThemeNavigationProps {
  layout: "side" | "top";
  /** The app's name (the manifest's), for the navigation's title and accessible name. */
  appName: string;
  items: ThemeNavigationItem[];
}

/** Navigation for an app drawn on `<Screen nav>` (`<AppShell nav>` draws its own). Its items are what a reviewer points at. */
export function Navigation({ id, layout, items }: NavigationProps) {
  const ctx = useKit();
  const Themed = useThemed("Navigation");
  requireId("Navigation", id);
  return <Themed layout={layout} appName={ctx.manifest.name} items={navigationItems(ctx, items, "Navigation item")} />;
}

/**
 * The entries the viewing role sees, ready for a theme. An entry to a screen
 * the role cannot reach is not drawn; one to a screen that does not exist
 * stays drawn, so the render check reports its `to`.
 */
export function navigationItems(ctx: KitContextValue, items: NavigationItem[], what: string): ThemeNavigationItem[] {
  return items
    .filter((i) => !isScreen(ctx, i.to) || reaches(ctx, i.to))
    .map((item) => ({
      id: requireId(what, item.id),
      label: item.label,
      active: item.to === ctx.view.screenId,
      onPress: pressHandler(ctx, { to: item.to }),
      root: selectableRootProps(ctx, item.id, item.label, item.to),
    }));
}

export interface BreadcrumbItem {
  id: string;
  label: string;
  /** Where the crumb leads; the last crumb is the current page and takes none. */
  to?: string | undefined;
  params?: Record<string, string> | undefined;
}

export interface BreadcrumbsProps {
  id: string;
  /** The trail, root first. */
  items: BreadcrumbItem[];
}

export interface ThemeBreadcrumbItem {
  id: string;
  label: string;
  /** Present on a crumb that leads somewhere; absent on the current page. */
  link?: { onPress: () => void; root: SelectableRootProps } | undefined;
}

export interface ThemeBreadcrumbsProps {
  items: ThemeBreadcrumbItem[];
}

/** A trail back to where the reviewer came from. */
export function Breadcrumbs({ id, items }: BreadcrumbsProps) {
  const ctx = useKit();
  const Themed = useThemed("Breadcrumbs");
  const last = items.length - 1;
  return (
    <SelectableBox id={requireId("Breadcrumbs", id)} label={items[last]?.label ?? "Breadcrumbs"} container>
      <Themed
        items={items.map((item, i) => ({
          id: requireId("Breadcrumbs item", item.id),
          label: item.label,
          link:
            item.to !== undefined && i < last
              ? { onPress: pressHandler(ctx, { to: item.to, params: item.params }), root: selectableRootProps(ctx, item.id, item.label, item.to) }
              : undefined,
        }))}
      />
    </SelectableBox>
  );
}

export interface Panel {
  id: string;
  label: string;
  content: ReactNode;
}

export interface ThemePanelHeader {
  id: string;
  label: string;
  root: SelectableRootProps;
}

export interface TabsProps {
  id: string;
  tabs: Panel[];
  /** The open tab, when the screen controls it; the first tab otherwise. */
  active?: string | undefined;
  onChange?: ((tabId: string) => void) | undefined;
}

export interface ThemeTabsProps {
  tabs: ThemePanelHeader[];
  activeId: string | undefined;
  onOpen: (tabId: string) => void;
  /** The open tab's content. */
  content: ReactNode;
}

/** Tabbed panels. Each tab header is an element a reviewer can point at (its `id`); the Tabs itself is layout. */
export function Tabs({ id, tabs, active, onChange }: TabsProps) {
  const ctx = useKit();
  const Themed = useThemed("Tabs");
  requireId("Tabs", id);
  const [own, setOwn] = useState<string | undefined>(undefined);
  const current = tabs.find((t) => t.id === (active ?? own)) ?? tabs[0];
  const open = (tabId: string) => {
    if (ctx.view.mode === "annotate") return;
    setOwn(tabId);
    onChange?.(tabId);
  };
  return (
    <Themed
      tabs={tabs.map((t) => ({ id: requireId("Tab", t.id), label: t.label, root: selectableRootProps(ctx, t.id, t.label) }))}
      activeId={current?.id}
      onOpen={open}
      content={current?.content}
    />
  );
}

export interface StepperProps {
  id: string;
  steps: Panel[];
  /** The current step, when the screen controls it (a Next button); the first step otherwise. */
  active?: string | undefined;
  onChange?: ((stepId: string) => void) | undefined;
}

export interface ThemeStepperProps {
  steps: ThemePanelHeader[];
  activeIndex: number;
  onOpen: (stepId: string) => void;
  /** The current step's content. */
  content: ReactNode;
}

/** A multi-step wizard. Each step header is an element a reviewer can point at (its `id`). */
export function Stepper({ id, steps, active, onChange }: StepperProps) {
  const ctx = useKit();
  const Themed = useThemed("Stepper");
  requireId("Stepper", id);
  const [own, setOwn] = useState<string | undefined>(undefined);
  const index = Math.max(0, steps.findIndex((s) => s.id === (active ?? own)));
  const open = (stepId: string) => {
    if (ctx.view.mode === "annotate") return;
    setOwn(stepId);
    onChange?.(stepId);
  };
  return (
    <Themed
      steps={steps.map((s) => ({ id: requireId("Step", s.id), label: s.label, root: selectableRootProps(ctx, s.id, s.label) }))}
      activeIndex={index}
      onOpen={open}
      content={steps[index]?.content}
    />
  );
}
