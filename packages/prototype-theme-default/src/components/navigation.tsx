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

import type { ThemeBreadcrumbsProps, ThemeNavigationProps, ThemeStepperProps, ThemeTabsProps } from "@wso2/prototype-kit";

export const NAVIGATION_CSS = `
.pt-nav{background:var(--pt-surface);border-color:var(--pt-border);border-style:solid;border-width:0}
.pt-nav-side{grid-column:1;grid-row:1/span 2;width:220px;border-right-width:1px;padding:16px 8px;overflow:auto}
.pt-nav-top{grid-column:1/span 2;grid-row:1;border-bottom-width:1px;padding:0 16px;display:flex;align-items:center;gap:16px}
.pt-nav-title{font-weight:600;padding:0 8px 8px}
.pt-nav-top .pt-nav-title{padding:0}
.pt-nav-items{display:flex;gap:2px;list-style:none;margin:0;padding:0}
.pt-nav-side .pt-nav-items{flex-direction:column}
.pt-nav-item{font:inherit;width:100%;text-align:left;background:none;border:0;border-radius:6px;padding:8px 10px;cursor:pointer;color:var(--pt-text)}
.pt-nav-top .pt-nav-item{border-radius:0;padding:12px 10px;border-bottom:2px solid transparent}
.pt-nav-item[aria-current=page]{color:var(--pt-primary);font-weight:600;background:color-mix(in srgb,var(--pt-primary) 8%,transparent)}
.pt-nav-top .pt-nav-item[aria-current=page]{background:none;border-bottom-color:var(--pt-primary)}
.pt-crumbs ol{display:flex;gap:6px;list-style:none;margin:0;padding:0;font-size:13px;color:var(--pt-muted)}
.pt-crumbs li+li::before{content:"/";margin-right:6px}
.pt-tablist{display:flex;gap:4px;border-bottom:1px solid var(--pt-border)}
.pt-tab{font:inherit;background:none;border:0;border-bottom:2px solid transparent;padding:8px 12px;cursor:pointer;color:var(--pt-muted)}
.pt-tab[aria-selected=true]{color:var(--pt-primary);border-bottom-color:var(--pt-primary);font-weight:600}
.pt-panel{display:flex;flex-direction:column;gap:16px;padding-top:16px}
.pt-steps{display:flex;gap:8px;list-style:none;margin:0;padding:0;counter-reset:step}
.pt-step{font:inherit;display:flex;gap:8px;align-items:center;background:none;border:0;cursor:pointer;color:var(--pt-muted);padding:4px 8px}
.pt-step::before{counter-increment:step;content:counter(step);display:inline-grid;place-items:center;width:22px;height:22px;border-radius:50%;border:1px solid currentColor;font-size:12px}
.pt-step[aria-current=step]{color:var(--pt-primary);font-weight:600}
`;

export function Navigation({ layout, appName, items }: ThemeNavigationProps) {
  return (
    <nav aria-label={`${appName} navigation`} className={`pt-nav pt-nav-${layout}`}>
      <div className="pt-nav-title">{appName}</div>
      <ul className="pt-nav-items">
        {items.map((item) => (
          <li key={item.id}>
            <button type="button" className="pt-nav-item" aria-current={item.active ? "page" : undefined} onClick={item.onPress} {...item.root}>
              {item.label}
            </button>
          </li>
        ))}
      </ul>
    </nav>
  );
}

export function Breadcrumbs({ items }: ThemeBreadcrumbsProps) {
  return (
    <nav aria-label="Breadcrumbs" className="pt-crumbs">
      <ol>
        {items.map((item) => (
          <li key={item.id}>
            {item.link ? (
              <button type="button" className="pt-link" onClick={item.link.onPress} {...item.link.root}>
                {item.label}
              </button>
            ) : (
              <span aria-current="page">{item.label}</span>
            )}
          </li>
        ))}
      </ol>
    </nav>
  );
}

export function Tabs({ tabs, activeId, onOpen, content }: ThemeTabsProps) {
  return (
    <div>
      <div role="tablist" className="pt-tablist">
        {tabs.map((t) => (
          <button key={t.id} type="button" role="tab" className="pt-tab" aria-selected={t.id === activeId} onClick={() => onOpen(t.id)} {...t.root}>
            {t.label}
          </button>
        ))}
      </div>
      <div role="tabpanel" className="pt-panel">
        {content}
      </div>
    </div>
  );
}

export function Stepper({ steps, activeIndex, onOpen, content }: ThemeStepperProps) {
  return (
    <div>
      <ol className="pt-steps">
        {steps.map((s, i) => (
          <li key={s.id}>
            <button type="button" className="pt-step" aria-current={i === activeIndex ? "step" : undefined} onClick={() => onOpen(s.id)} {...s.root}>
              {s.label}
            </button>
          </li>
        ))}
      </ol>
      <div className="pt-panel">{content}</div>
    </div>
  );
}
