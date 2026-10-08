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

/** Data: the table of records and the activity timeline. */

import type { ThemeTableAction, ThemeTableColumn, ThemeTableProps, ThemeTimelineProps } from "@wso2/prototype-kit";

export const DATA_CSS = `
.pt-table{padding:0;overflow:auto}
.pt-table .pt-card-title{padding:14px 16px 0}
.pt-table table{width:100%;border-collapse:collapse}
.pt-table th{text-align:left;font-size:12px;font-weight:600;color:var(--pt-muted);padding:10px 16px;border-bottom:1px solid var(--pt-border)}
.pt-table td{padding:10px 16px;border-bottom:1px solid var(--pt-border)}
.pt-table tbody tr{cursor:pointer}
.pt-table tbody tr:hover{background:color-mix(in srgb,var(--pt-primary) 5%,transparent)}
.pt-table .pt-fit{width:1%;white-space:nowrap}
.pt-table .pt-number{text-align:right;font-variant-numeric:tabular-nums}
.pt-table .pt-row-actions{display:flex;gap:6px;justify-content:flex-end}
.pt-table .pt-row-actions .pt-button{padding:3px 10px;font-size:13px}
.pt-table tbody tr[aria-selected=true]{background:color-mix(in srgb,var(--pt-primary) 10%,transparent)}
.pt-timeline ol{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:12px}
.pt-timeline li{display:grid;grid-template-columns:120px 1fr;gap:12px}
.pt-timeline time{color:var(--pt-muted);font-size:12px}
`;

/** Status, number and actions columns fit their content; text columns share the rest. */
function columnClass(kind: ThemeTableColumn["kind"]): string | undefined {
  if (kind === "number") return "pt-fit pt-number";
  return kind === "status" ? "pt-fit" : undefined;
}

/** Draws every action inline (no overflow menu). */
function RowActions({ actions }: { actions: ThemeTableAction[] }) {
  return (
    <div className="pt-row-actions">
      {actions.map((a) => (
        <button
          key={a.id}
          type="button"
          className={a.emphasis === "danger" ? "pt-button pt-button-danger" : "pt-button"}
          onClick={(e) => {
            e.stopPropagation();
            a.onPress();
          }}
          {...a.root}
        >
          {a.label}
        </button>
      ))}
    </div>
  );
}

export function Table({ title, columns, rows, hasActions }: ThemeTableProps) {
  return (
    <section className="pt-card pt-table">
      {title && <h3 className="pt-card-title">{title}</h3>}
      <table aria-label={title ?? "Records"}>
        <thead>
          <tr>
            {columns.map((c, i) => (
              <th key={i} className={columnClass(c.kind)}>
                {c.label}
              </th>
            ))}
            {hasActions && <th className="pt-fit pt-number">Actions</th>}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.id} aria-selected={row.highlighted} onClick={row.onPress} {...row.root}>
              {row.cells.map((cell, i) => (
                <td key={i} className={columnClass(columns[i]?.kind ?? "text")}>
                  {cell.tone ? <span className={`pt-badge pt-tone-${cell.tone}`}>{cell.text}</span> : cell.text}
                </td>
              ))}
              {hasActions && (
                <td className="pt-fit">
                  <RowActions actions={row.actions} />
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

export function Timeline({ title, entries }: ThemeTimelineProps) {
  return (
    <section className="pt-card pt-timeline">
      <h3 className="pt-card-title">{title}</h3>
      <ol>
        {entries.map((e, i) => (
          <li key={i}>
            <time>{e.when}</time>
            <div>
              <div>{e.text}</div>
              <div className="pt-muted">{e.who}</div>
            </div>
          </li>
        ))}
      </ol>
    </section>
  );
}
