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
 * The app shell: the chrome every real product has, so a prototype need not
 * build it — a header with the product's name and the signed-in user, the
 * user menu (Account, Settings, Sign out) and the side navigation, around the
 * screen's content.
 */

import type { ReactNode } from "react";
import { useKit, type KitContextValue } from "../runtime/context.js";
import { requireId, selectableRootProps, type SelectableRootProps } from "../runtime/selectable.js";
import { useThemed } from "../theme/context.js";
import { navigationItems, type NavigationItem, type ThemeNavigationItem } from "./navigation.js";

export interface AppShellUser {
  name: string;
  /** Shown under the name in the user menu. */
  email?: string | undefined;
  /** The role beside the name; the viewing role's name in prototype.json by default, so a role switch shows in the header. */
  role?: string | undefined;
}

export interface AppShellProps {
  /** The shell's id; its user menu's elements are `<id>.user`, `<id>.account`, `<id>.settings` and `<id>.sign-out`. */
  id: string;
  /** The product's name in the header; prototype.json's `name` by default. */
  product?: string | undefined;
  /** The signed-in user the header shows. */
  user: AppShellUser;
  /** The side navigation's entries. */
  nav: NavigationItem[];
  /** The screen the user menu's Account entry opens. */
  account: string;
  /** The screen the user menu's Settings entry opens. */
  settings: string;
  /** The screen Sign out leads to: a signed-out screen, drawn on a bare `<Screen>`. */
  signOut: string;
  /** More user-menu entries (a profile, billing), drawn after Account and Settings and before Sign out. */
  menu?: NavigationItem[] | undefined;
  /** The screen's content. */
  children?: ReactNode;
}

export interface ThemeAppShellProps {
  product: string;
  user: { name: string; email?: string | undefined; role: string };
  /** The user menu's trigger, an element a reviewer can point at. */
  userRoot: SelectableRootProps;
  nav: ThemeNavigationItem[];
  /**
   * The user menu's entries the viewing role reaches: Account, Settings and
   * the prototype's own; Sign out is `signOut`. Keep them in the markup while the menu is closed (hidden),
   * so the render check sees where they lead.
   */
  menu: ThemeNavigationItem[];
  signOut: ThemeNavigationItem | undefined;
  children?: ReactNode;
}

function requireTarget(prop: string, entry: string, screenId: unknown): string {
  if (typeof screenId !== "string" || screenId === "") throw new Error(`<AppShell> needs ${prop}: the screen its user menu's ${entry} entry opens`);
  return screenId;
}

function roleName(ctx: KitContextValue): string {
  return ctx.manifest.roles.find((r) => r.id === ctx.view.roleId)?.name ?? ctx.view.roleId;
}

/** The root of a screen inside the product's chrome: header, user menu and side navigation. Use `<Screen>` for a screen outside it (signed out). */
export function AppShell({ id, product, user, nav, account, settings, signOut, menu: extra = [], children }: AppShellProps) {
  const ctx = useKit();
  const Themed = useThemed("AppShell");
  requireId("AppShell", id);
  if (typeof user?.name !== "string") throw new Error("<AppShell> needs user: the signed-in user's { name }");
  const entry = (suffix: string, label: string, to: string): NavigationItem => ({ id: `${id}.${suffix}`, label, to });
  const menu = navigationItems(
    ctx,
    [entry("account", "Account", requireTarget("account", "Account", account)), entry("settings", "Settings", requireTarget("settings", "Settings", settings)), ...extra],
    "AppShell menu entry",
  );
  const [out] = navigationItems(ctx, [entry("sign-out", "Sign out", requireTarget("signOut", "Sign out", signOut))], "AppShell menu entry");
  return (
    <Themed
      product={product ?? ctx.manifest.name}
      user={{ name: user.name, email: user.email, role: user.role ?? roleName(ctx) }}
      userRoot={selectableRootProps(ctx, `${id}.user`, user.name)}
      nav={navigationItems(ctx, nav, "AppShell nav entry")}
      menu={menu}
      signOut={out}
    >
      {children}
    </Themed>
  );
}
