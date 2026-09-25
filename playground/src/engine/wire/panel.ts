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
 * THE PANEL a wired session leaves up while you test: one row per running
 * thing, the URL, and the keys.
 *
 * Pinned under the output by the same mechanism the coding run's crew block
 * uses (../pinned-pane.ts), so a line printed while you work lands above it and
 * the block stays where the eye left it.
 *
 * Rows and keys are worked out by pure functions here; reading the keyboard is
 * `session.ts`'s. That split is what lets the key table be tested without a
 * TTY — and the key table is the part that must not drift from what the panel
 * says it does.
 */

import { fit, type PaneRow } from "../pinned-pane.js";
import type { RoleEntry } from "./roles.js";

/** What the panel is looking at right now. */
export interface PanelModel {
  /** The URL the browser was last sent to, role and all. */
  url: string | null;
  services: { name: string; state: string; health: string }[];
  /** The dev server's line; absent for a project with no web application. */
  webapp?: { url: string; port: number } | null;
  /** One line about the last thing that happened — a role opened, a rebuild, a seed. */
  note?: string;
}

/** What a key press means. The panel's contract with the session. */
export type PanelAction =
  | { kind: "role"; entry: RoleEntry }
  | { kind: "seed" }
  | { kind: "rebuild" }
  | { kind: "logs" }
  | { kind: "quit" }
  | { kind: "none" };

/** The number keys, in the picker's order, capped at what a single digit can name. */
export function numberedEntries(entries: RoleEntry[]): RoleEntry[] {
  return entries.slice(0, 9);
}

/**
 * A key press, resolved. Pure, so the panel's legend and its behaviour are
 * pinned by one test rather than kept in step by hand.
 *
 * Ctrl-C arrives here as  because the panel reads raw keys: with raw mode
 * on, the terminal no longer turns it into SIGINT, so quitting on it is this
 * table's job and not the signal handler's.
 */
export function resolveKey(key: string, entries: RoleEntry[]): PanelAction {
  if (key === "q" || key === "" || key === "") return { kind: "quit" };
  if (key === "s") return { kind: "seed" };
  if (key === "r") return { kind: "rebuild" };
  if (key === "l") return { kind: "logs" };
  if (/^[1-9]$/.test(key)) {
    const entry = numberedEntries(entries)[Number(key) - 1];
    return entry ? { kind: "role", entry } : { kind: "none" };
  }
  return { kind: "none" };
}

const GLYPHS: Record<string, string> = {
  running: "●",
  healthy: "✓",
  starting: "◑",
  unhealthy: "✗",
  exited: "✗",
};

/** The block, as the terminal will draw it. `width` is the pane's, already floored. */
export function panelRows(model: PanelModel, entries: RoleEntry[], width: number): PaneRow[] {
  const rows: PaneRow[] = [];
  const rule = "─".repeat(Math.max(8, Math.min(width, 72)));
  rows.push({ text: rule, tone: "muted" });

  for (const service of model.services) {
    const status = service.health || service.state;
    const glyph = GLYPHS[status] ?? "·";
    const tone = status === "exited" || status === "unhealthy" ? "error" : status === "healthy" ? "success" : "info";
    rows.push({ text: `  ${glyph} ${service.name.padEnd(24)} ${status}`, tone });
  }
  if (model.webapp) {
    rows.push({ text: `  ● ${"webapp (vite)".padEnd(24)} ${model.webapp.url}`, tone: "info" });
  }
  if (model.url) {
    rows.push({ text: `  → ${model.url}`, tone: "success" });
  }
  if (model.note) {
    rows.push({ text: `  ${model.note}`, tone: "muted" });
  }

  const numbered = numberedEntries(entries)
    .map((entry, index) => `${String(index + 1)} ${entry.label}`)
    .join("  ");
  rows.push({ text: `  ${numbered}`, tone: "muted" });
  rows.push({ text: "  s seed   r rebuild a service   l logs   q quit (tears everything down)", tone: "muted" });
  return rows.map((row) => ({ ...row, text: fit(row.text, width) }));
}

/** The line a script waits for instead of watching a panel it cannot see. */
export function readyLine(url: string): string {
  return `READY ${url}`;
}
