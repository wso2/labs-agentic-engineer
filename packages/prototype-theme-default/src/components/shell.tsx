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

/** The app shell: header with the product and the user menu, side navigation and the screen's content. */

import { useState } from "react";
import type { ThemeAppShellProps, ThemeNavigationItem } from "@wso2/prototype-kit";

export const SHELL_CSS = `
.pt-shell{display:grid;grid-template-columns:220px minmax(0,1fr);grid-template-rows:auto minmax(0,1fr);flex:1;min-height:0;height:100%}
.pt-shell-header{grid-column:1/span 2;display:flex;align-items:center;gap:16px;padding:8px 16px;background:var(--pt-surface);border-bottom:1px solid var(--pt-border)}
.pt-shell-product{font-weight:600;font-size:16px;margin-right:auto}
.pt-shell-user{position:relative}
.pt-shell-user-button{font:inherit;display:flex;align-items:center;gap:8px;background:none;border:0;border-radius:999px;padding:4px 10px 4px 4px;cursor:pointer;color:var(--pt-text)}
.pt-shell-avatar{display:inline-grid;place-items:center;width:28px;height:28px;border-radius:50%;background:var(--pt-primary);color:var(--pt-primary-text);font-weight:600;font-size:13px}
.pt-shell-menu{position:absolute;right:0;top:calc(100% + 4px);z-index:10;min-width:220px;background:var(--pt-surface);border:1px solid var(--pt-border);border-radius:var(--pt-radius);box-shadow:0 8px 24px rgba(15,23,42,.14);padding:4px 0}
.pt-shell-menu[hidden]{display:none}
.pt-shell-who{padding:8px 14px 10px;border-bottom:1px solid var(--pt-border)}
.pt-shell-who small{display:block;color:var(--pt-muted)}
.pt-shell-menu ul{list-style:none;margin:0;padding:4px 0}
.pt-shell-menu ul+ul{border-top:1px solid var(--pt-border)}
.pt-shell-entry{font:inherit;width:100%;text-align:left;background:none;border:0;padding:8px 14px;cursor:pointer;color:var(--pt-text)}
.pt-shell-entry:hover{background:var(--pt-bg)}
.pt-shell .pt-nav-side{grid-row:2}
.pt-shell .pt-main{grid-row:2}
`;

function Entry({ item, onPress }: { item: ThemeNavigationItem; onPress: () => void }) {
  return (
    <li>
      <button type="button" role="menuitem" className="pt-shell-entry" onClick={onPress} {...item.root}>
        {item.label}
      </button>
    </li>
  );
}

export function AppShell({ product, user, userRoot, nav, menu, signOut, children }: ThemeAppShellProps) {
  const [open, setOpen] = useState(false);
  const press = (item: ThemeNavigationItem) => () => {
    setOpen(false);
    item.onPress();
  };
  return (
    <div className="pt-shell">
      <header className="pt-shell-header">
        <span className="pt-shell-product">{product}</span>
        <div
          className="pt-shell-user"
          onKeyDown={(e) => {
            if (e.key !== "Escape" || !open) return;
            // Used here, so the frame does not hand it on to the host.
            e.preventDefault();
            setOpen(false);
          }}
        >
          <button type="button" className="pt-shell-user-button" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((o) => !o)} {...userRoot}>
            <span className="pt-shell-avatar" aria-hidden>
              {user.name.charAt(0).toUpperCase()}
            </span>
            {user.name}
          </button>
          {/* Kept in the markup while closed, so the render check sees where the entries lead. */}
          <div className="pt-shell-menu" role="menu" aria-label={`${user.name} menu`} hidden={!open}>
            <div className="pt-shell-who">
              <strong>{user.name}</strong>
              <small>{user.role}</small>
              {user.email !== undefined && <small>{user.email}</small>}
            </div>
            <ul>
              {menu.map((item) => (
                <Entry key={item.id} item={item} onPress={press(item)} />
              ))}
            </ul>
            {signOut && (
              <ul>
                <Entry item={signOut} onPress={press(signOut)} />
              </ul>
            )}
          </div>
        </div>
      </header>
      <nav aria-label={`${product} navigation`} className="pt-nav pt-nav-side">
        <ul className="pt-nav-items">
          {nav.map((item) => (
            <li key={item.id}>
              <button type="button" className="pt-nav-item" aria-current={item.active ? "page" : undefined} onClick={item.onPress} {...item.root}>
                {item.label}
              </button>
            </li>
          ))}
        </ul>
      </nav>
      <main className="pt-main">
        <div className="pt-main-inner">{children}</div>
      </main>
    </div>
  );
}
