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

/** Content: text, heading, badge, stat and stat group, alert, empty state, button and link. */

import type { ThemeAlertProps, ThemeBadgeProps, ThemeButtonProps, ThemeEmptyStateProps, ThemeHeadingProps, ThemeLinkProps, ThemeStatGroupProps, ThemeStatProps, ThemeTextProps } from "@wso2/prototype-kit";

export const CONTENT_CSS = `
.pt-button{font:inherit;font-weight:500;padding:6px 14px;border-radius:6px;border:1px solid var(--pt-primary);background:var(--pt-surface);color:var(--pt-primary);cursor:pointer}
.pt-button:disabled{opacity:.5;cursor:default}
.pt-button-primary{background:var(--pt-primary);color:var(--pt-primary-text)}
.pt-button-danger{background:var(--pt-danger);border-color:var(--pt-danger);color:#fff}
.pt-link{font:inherit;background:none;border:0;padding:0;color:var(--pt-primary);cursor:pointer;text-decoration:underline;text-underline-offset:2px}
.pt-text{margin:0;color:var(--pt-muted)}
.pt-text-primary{color:var(--pt-text)}
.pt-heading{display:flex;align-items:center;justify-content:space-between;gap:16px}
.pt-heading h2{margin:0;font-size:22px;font-weight:600}
.pt-heading h3{margin:0;font-size:16px;font-weight:600}
.pt-actions{display:flex;gap:8px;flex-wrap:wrap}
.pt-badge{display:inline-block;padding:1px 10px;border-radius:999px;font-size:12px;font-weight:600;color:var(--pt-tone);background:color-mix(in srgb,var(--pt-tone) 12%,transparent)}
.pt-stat-group{display:grid;gap:16px;grid-template-columns:repeat(auto-fit,minmax(200px,1fr))}
.pt-stat-group .pt-stat{height:100%}
.pt-stat{display:flex;flex-direction:column;gap:4px}
.pt-stat-label{color:var(--pt-muted);font-size:13px}
.pt-stat-value{font-size:28px;font-weight:600;line-height:1.2;font-variant-numeric:tabular-nums}
.pt-stat-hint{color:var(--pt-muted);font-size:12px}
.pt-alert{border-left:4px solid var(--pt-tone);background:color-mix(in srgb,var(--pt-tone) 8%,var(--pt-surface));padding:10px 14px;border-radius:6px}
.pt-alert strong{display:block;margin-bottom:2px}
.pt-empty{text-align:center;padding:32px 16px;display:flex;flex-direction:column;align-items:center;gap:8px}
.pt-empty h3{margin:0;font-size:16px}
`;

export function Button({ label, emphasis, disabled, onPress }: ThemeButtonProps) {
  const variant = emphasis === "primary" ? " pt-button-primary" : emphasis === "danger" ? " pt-button-danger" : "";
  return (
    <button type="button" className={`pt-button${variant}`} disabled={disabled} onClick={onPress}>
      {label}
    </button>
  );
}

export function Link({ label, onPress }: ThemeLinkProps) {
  return (
    <button type="button" className="pt-link" onClick={onPress}>
      {label}
    </button>
  );
}

export function Text({ text, tone }: ThemeTextProps) {
  return <p className={tone === "primary" ? "pt-text pt-text-primary" : "pt-text"}>{text}</p>;
}

export function Heading({ text, level, actions }: ThemeHeadingProps) {
  return (
    <div className="pt-heading">
      {level === "page" ? <h2>{text}</h2> : <h3>{text}</h3>}
      {actions !== undefined && <div className="pt-actions">{actions}</div>}
    </div>
  );
}

export function Badge({ label, tone }: ThemeBadgeProps) {
  return <span className={`pt-badge pt-tone-${tone}`}>{label}</span>;
}

/** The plain theme draws no icon. */
export function Stat({ label, value, hint }: ThemeStatProps) {
  return (
    <div className="pt-card pt-stat">
      <div className="pt-stat-label">{label}</div>
      <div className="pt-stat-value">{value}</div>
      {hint && <div className="pt-stat-hint">{hint}</div>}
    </div>
  );
}

export function StatGroup({ children }: ThemeStatGroupProps) {
  return <div className="pt-stat-group">{children}</div>;
}

export function Alert({ tone, title, text }: ThemeAlertProps) {
  return (
    <div role="status" className={`pt-alert pt-tone-${tone === "default" ? "info" : tone}`}>
      {title && <strong>{title}</strong>}
      {text}
    </div>
  );
}

export function EmptyState({ title, text, actions }: ThemeEmptyStateProps) {
  return (
    <div className="pt-card pt-empty">
      <h3>{title}</h3>
      <p className="pt-text">{text}</p>
      {actions}
    </div>
  );
}
