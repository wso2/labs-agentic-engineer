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
 * The app shell on Oxygen's layout kit, as a WSO2 product draws its chrome:
 * `AppShell` with a `Header` (brand title, the signed-in user's menu) and a
 * `Sidebar`, around the screen's `PageContent`.
 *
 * The user menu is composed as the console's own is: Oxygen's `Menu` with
 * `UserMenu.Header`. `UserMenu` whole does not fit a prototype: its menu
 * portals out of the kit's scene (Annotate would not reach inside, the render
 * check would not see it) and its entries take no element props, so they
 * could not carry the kit's selectable roots and targets. It is an
 * `InPlaceMenu` instead (menu.tsx).
 */

import { useId, useState } from "react";
import { AppShell as OxygenAppShell, Avatar, Box, Divider, Header, IconButton, ListItemIcon, ListItemText, MenuItem, UserMenu } from "@wso2/oxygen-ui";
import { ChevronRight, LogOut } from "@wso2/oxygen-ui-icons-react";
import type { ThemeAppShellProps, ThemeNavigationItem } from "@wso2/prototype-kit";
import { Page } from "./layout.js";
import { InPlaceMenu } from "./menu.js";
import { SideNav } from "./navigation.js";
import { buttonRoot } from "./root.js";

function AccountMenu({ user, userRoot, menu, signOut }: Pick<ThemeAppShellProps, "user" | "userRoot" | "menu" | "signOut">) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const menuId = useId();
  const open = anchor !== null;
  const press = (item: ThemeNavigationItem) => () => {
    setAnchor(null);
    item.onPress();
  };
  return (
    <>
      <IconButton
        size="small"
        aria-label={`${user.name}, ${user.role}`}
        aria-haspopup="menu"
        aria-expanded={open || undefined}
        aria-controls={open ? menuId : undefined}
        onClick={(e) => setAnchor(e.currentTarget)}
        sx={{ p: 0.5, pr: 1.5, gap: 1, borderRadius: 4 }}
        {...buttonRoot(userRoot)}
      >
        <Avatar sx={{ width: 32, height: 32, fontSize: 14, fontWeight: 600, bgcolor: "primary.main", color: "primary.contrastText" }}>{user.name.charAt(0).toUpperCase()}</Avatar>
        <Box component="span" sx={{ fontSize: 14, fontWeight: 500, color: "text.primary", display: { xs: "none", sm: "block" } }}>
          {user.name}
        </Box>
      </IconButton>
      <InPlaceMenu id={menuId} anchor={anchor} onClose={() => setAnchor(null)} minWidth={240}>
        <UserMenu.Header name={user.name} email={user.email ?? ""} role={user.role} />
        {menu.map((item) => (
          <MenuItem key={item.id} onClick={press(item)} sx={{ py: 1.5 }} {...buttonRoot(item.root)}>
            <ListItemText primary={item.label} />
            <ChevronRight size={16} aria-hidden="true" />
          </MenuItem>
        ))}
        {signOut && <Divider />}
        {signOut && (
          <MenuItem onClick={press(signOut)} sx={{ py: 1.5 }} {...buttonRoot(signOut.root)}>
            <ListItemIcon>
              <LogOut size={18} />
            </ListItemIcon>
            <ListItemText primary={signOut.label} />
          </MenuItem>
        )}
      </InPlaceMenu>
    </>
  );
}

export function AppShell({ product, user, userRoot, nav, menu, signOut, children }: ThemeAppShellProps) {
  return (
    // The frame is the viewport: no collapsing on a narrow one (the entries have no icons to collapse to).
    <OxygenAppShell collapseOnMobile={false} collapseOnSelectOnMobile={false} sx={{ height: "100%", flex: 1, minHeight: 0 }}>
      <OxygenAppShell.Navbar>
        <Header>
          <Header.Brand>
            <Header.BrandTitle>{product}</Header.BrandTitle>
          </Header.Brand>
          <Header.Spacer />
          <Header.Actions>
            <AccountMenu user={user} userRoot={userRoot} menu={menu} signOut={signOut} />
          </Header.Actions>
        </Header>
      </OxygenAppShell.Navbar>
      <OxygenAppShell.Sidebar>
        <SideNav items={nav} />
      </OxygenAppShell.Sidebar>
      <OxygenAppShell.Main>
        <Page>{children}</Page>
      </OxygenAppShell.Main>
    </OxygenAppShell>
  );
}
