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

/** Layout: the screen shell, section, stack, grid, split and detail record. */

import type { CSSProperties } from "react";
import type { ThemeDetailProps, ThemeGridProps, ThemeScreenProps, ThemeSectionProps, ThemeSplitProps, ThemeStackProps } from "@wso2/prototype-kit";

export const LAYOUT_CSS = `
.pt-screen{display:grid;grid-template-columns:auto minmax(0,1fr);grid-template-rows:auto minmax(0,1fr);flex:1;min-height:0;height:100%}
.pt-main{grid-column:2;grid-row:2;min-width:0;min-height:0;overflow:auto;padding:24px}
.pt-main-inner{display:flex;flex-direction:column;gap:20px;max-width:1100px;margin:0 auto}
.pt-section{display:flex;flex-direction:column;gap:12px;margin-top:8px}
.pt-section-head{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;flex-wrap:wrap}
.pt-section-title{display:flex;align-items:center;gap:8px}
.pt-section-head h3{margin:0;font-size:18px;font-weight:600}
.pt-section-count{font-size:12px;font-weight:600;color:var(--pt-muted);background:var(--pt-border);border-radius:999px;padding:0 8px;line-height:20px}
.pt-section-head p{margin:2px 0 0;color:var(--pt-muted)}
.pt-stack{display:flex;flex-direction:column;gap:16px}
.pt-stack-row{flex-direction:row;flex-wrap:wrap;align-items:center}
.pt-grid{display:grid;gap:16px;grid-template-columns:repeat(var(--pt-columns),minmax(0,1fr))}
@media (max-width:720px){.pt-grid{grid-template-columns:minmax(0,1fr)}}
.pt-split{display:grid;gap:20px;grid-template-columns:minmax(0,var(--pt-left)) minmax(0,var(--pt-right))}
@media (max-width:720px){.pt-split{grid-template-columns:minmax(0,1fr)}}
.pt-split>div{display:flex;flex-direction:column;gap:20px;min-width:0}
.pt-detail dl{display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:12px 24px;margin:0}
.pt-detail dt{font-size:11px;text-transform:uppercase;letter-spacing:.06em;color:var(--pt-muted)}
.pt-detail dd{margin:2px 0 0}
`;

export function Screen({ nav, children }: ThemeScreenProps) {
  return (
    <div className="pt-screen">
      {nav}
      <main className="pt-main">
        <div className="pt-main-inner">{children}</div>
      </main>
    </div>
  );
}

export function Section({ title, subtitle, count, actions, children }: ThemeSectionProps) {
  return (
    <section className="pt-section">
      <div className="pt-section-head">
        <div>
          <div className="pt-section-title">
            <h3>{title}</h3>
            {count !== undefined && <span className="pt-section-count">{count}</span>}
          </div>
          {subtitle && <p>{subtitle}</p>}
        </div>
        {actions !== undefined && <div className="pt-actions">{actions}</div>}
      </div>
      {children}
    </section>
  );
}

export function Stack({ direction, children }: ThemeStackProps) {
  return <div className={direction === "row" ? "pt-stack pt-stack-row" : "pt-stack"}>{children}</div>;
}

export function Grid({ columns, children }: ThemeGridProps) {
  return (
    <div className="pt-grid" style={{ "--pt-columns": columns } as CSSProperties}>
      {children}
    </div>
  );
}

export function Split({ left, right, ratio }: ThemeSplitProps) {
  return (
    <div className="pt-split" style={{ "--pt-left": `${ratio}fr`, "--pt-right": `${12 - ratio}fr` } as CSSProperties}>
      <div>{left}</div>
      <div>{right}</div>
    </div>
  );
}

export function Detail({ title, fields }: ThemeDetailProps) {
  return (
    <section className="pt-card pt-detail">
      {title && <h3 className="pt-card-title">{title}</h3>}
      <dl>
        {fields.map((f, i) => (
          <div key={`${f.label}-${i}`}>
            <dt>{f.label}</dt>
            <dd>{f.value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}
