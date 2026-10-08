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

/** Overlays: a modal dialog and a side drawer, drawn in place over the screen (no portal). */

import type { ThemeDialogProps, ThemeDrawerProps } from "@wso2/prototype-kit";

export const OVERLAYS_CSS = `
.pt-backdrop{position:fixed;inset:0;background:rgba(15,23,42,.45);display:flex;z-index:10}
.pt-dialog{margin:auto;width:min(560px,calc(100% - 32px));background:var(--pt-surface);border-radius:12px;padding:20px;display:flex;flex-direction:column;gap:16px;box-shadow:0 20px 50px rgba(0,0,0,.25)}
.pt-dialog h2,.pt-drawer h2{margin:0;font-size:18px}
.pt-dialog-actions{display:flex;gap:8px;justify-content:flex-end}
.pt-drawer{margin-left:auto;width:min(380px,100%);height:100%;background:var(--pt-surface);padding:20px;display:flex;flex-direction:column;gap:16px;overflow:auto}
.pt-drawer-head{display:flex;align-items:center;justify-content:space-between}
`;

export function Dialog({ title, titleId, open, onClose, children, actions }: ThemeDialogProps) {
  if (!open) return null;
  return (
    <div className="pt-backdrop" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="pt-dialog"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => {
          if (e.key !== "Escape") return;
          // Used here, so the frame does not hand it on to the host.
          e.preventDefault();
          onClose();
        }}
      >
        <h2 id={titleId}>{title}</h2>
        <div className="pt-stack">{children}</div>
        {actions !== undefined && <div className="pt-dialog-actions">{actions}</div>}
      </div>
    </div>
  );
}

export function Drawer({ title, open, onClose, children }: ThemeDrawerProps) {
  if (!open) return null;
  return (
    <div className="pt-backdrop" onClick={onClose}>
      <aside role="dialog" aria-label={title} className="pt-drawer" onClick={(e) => e.stopPropagation()}>
        <div className="pt-drawer-head">
          <h2>{title}</h2>
          <button type="button" className="pt-link" aria-label="Close" onClick={onClose}>
            Close
          </button>
        </div>
        <div className="pt-stack">{children}</div>
      </aside>
    </div>
  );
}
