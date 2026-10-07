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

import { useState, type ReactNode } from "react";
import { useKit } from "../runtime/context.js";
import { pressHandler, type Pressable } from "../runtime/press.js";
import { SelectableBox, excerpt, requireId, selectableRootProps, type SelectableRootProps } from "../runtime/selectable.js";
import { useThemed } from "../theme/context.js";
import type { Tone } from "./content.js";

export interface TableColumn {
  label: string;
  /**
   * `text` (default); `number` aligns figures to the right; `status` shows each
   * row's `status` as a badge (one per table). A plain string is a `text` column.
   */
  kind?: "text" | "number" | "status" | undefined;
}

export interface TableStatus {
  text: string;
  tone: Tone;
}

export interface TableAction extends Pressable {
  /** The action's element id: unique on the screen (prefix it with the row's). */
  id: string;
  label: string;
  /** `danger` for a destructive action. */
  emphasis?: "danger" | undefined;
}

export interface TableRow {
  /** The row's element id: unique on the screen (prefix it with the table's). */
  id: string;
  /** One per `text` or `number` column, in column order; the status column takes `status`. */
  cells: string[];
  /** The badge in the table's status column. */
  status?: TableStatus | undefined;
  /** What can be done to this record, drawn as buttons in a trailing column; pressing one does not press the row. */
  actions?: TableAction[] | undefined;
  /** Colours the last cell as a status, in a table without a status column. Prefer `status`. */
  tone?: Tone | undefined;
  /** Open this screen when the row is pressed. */
  to?: string | undefined;
  params?: Record<string, string> | undefined;
}

export interface TableProps {
  id: string;
  title?: string | undefined;
  columns: (string | TableColumn)[];
  rows: TableRow[];
  /** Run when a row is pressed, with its id; a row with neither this nor `to` is only highlighted. */
  onRowPress?: ((rowId: string) => void) | undefined;
  /** What an empty table shows instead (an `<EmptyState>`). */
  empty?: ReactNode;
}

export interface ThemeTableColumn {
  label: string;
  kind: "text" | "number" | "status";
}

export interface ThemeTableCell {
  text: string;
  /** Present: draw the cell as a status badge in this tone. */
  tone?: Tone | undefined;
}

export interface ThemeTableAction {
  id: string;
  label: string;
  emphasis?: "danger" | undefined;
  /** Stop the click there: a press on an action must not also press its row. */
  onPress: () => void;
  root: SelectableRootProps;
}

export interface ThemeTableRow {
  id: string;
  /** Exactly one per column. */
  cells: ThemeTableCell[];
  /** Drawn in the trailing actions column; may be empty. */
  actions: ThemeTableAction[];
  /** The row the reviewer last pressed (when it leads nowhere). */
  highlighted: boolean;
  onPress: () => void;
  root: SelectableRootProps;
}

export interface ThemeTableProps {
  title?: string | undefined;
  columns: ThemeTableColumn[];
  rows: ThemeTableRow[];
  /** Whether any row has actions: draw a trailing actions column after `columns`. */
  hasActions: boolean;
}

function tableColumns(columns: (string | TableColumn)[]): ThemeTableColumn[] {
  const resolved = columns.map((c): ThemeTableColumn => (typeof c === "string" ? { label: c, kind: "text" } : { label: c.label, kind: c.kind ?? "text" }));
  if (resolved.filter((c) => c.kind === "status").length > 1) throw new Error("<Table> takes one status column");
  return resolved;
}

/** One cell per column: `cells` fill the text and number columns in order, the status column takes `status`. */
function tableCells(columns: ThemeTableColumn[], row: TableRow): ThemeTableCell[] {
  const hasStatusColumn = columns.some((c) => c.kind === "status");
  if (row.status !== undefined && !hasStatusColumn) {
    throw new Error(`<Table> row ${JSON.stringify(row.id)} has a status, but no column is { kind: "status" }`);
  }
  let next = 0;
  const cells = columns.map((c): ThemeTableCell => (c.kind === "status" ? { text: row.status?.text ?? "", tone: row.status?.tone } : { text: row.cells[next++] ?? "" }));
  const last = cells.length - 1;
  if (!hasStatusColumn && row.tone !== undefined && last >= 0) cells[last] = { text: cells[last]!.text, tone: row.tone };
  return cells;
}

/** A table of records. */
export function Table({ id, title, columns, rows, onRowPress, empty }: TableProps) {
  const ctx = useKit();
  const Themed = useThemed("Table");
  requireId("Table", id);
  const [highlighted, setHighlighted] = useState<string | null>(null);
  if (rows.length === 0 && empty !== undefined) return <>{empty}</>;
  const resolved = tableColumns(columns);
  return (
    <SelectableBox id={id} label={title ?? resolved[0]?.label ?? "Table"} container>
      <Themed
        title={title}
        columns={resolved}
        hasActions={rows.some((row) => (row.actions?.length ?? 0) > 0)}
        rows={rows.map((row) => {
          const bound = onRowPress !== undefined || row.to !== undefined;
          const press = pressHandler(ctx, { to: row.to, params: row.params, onPress: onRowPress && (() => onRowPress(row.id)) });
          return {
            id: requireId("Table row", row.id),
            cells: tableCells(resolved, row),
            actions: (row.actions ?? []).map(({ id: actionId, label, emphasis, ...target }) => ({
              id: requireId("Table row action", actionId),
              label,
              emphasis,
              onPress: pressHandler(ctx, target),
              root: selectableRootProps(ctx, actionId, label, target.to),
            })),
            highlighted: highlighted === row.id,
            onPress: () => {
              if (ctx.view.mode === "annotate") return;
              if (bound) press();
              else setHighlighted(row.id);
            },
            root: selectableRootProps(ctx, row.id, excerpt(row.cells[0] ?? row.id), row.to),
          };
        })}
      />
    </SelectableBox>
  );
}

export interface TimelineEntry {
  when: string;
  who: string;
  text: string;
}

export interface TimelineProps {
  id: string;
  title?: string | undefined;
  /** Newest first. */
  entries: TimelineEntry[];
}

export interface ThemeTimelineProps {
  title: string;
  entries: TimelineEntry[];
}

/** An activity history. */
export function Timeline({ id, title = "Activity", entries }: TimelineProps) {
  const Themed = useThemed("Timeline");
  return (
    <SelectableBox id={requireId("Timeline", id)} label={title}>
      <Themed title={title} entries={entries} />
    </SelectableBox>
  );
}
