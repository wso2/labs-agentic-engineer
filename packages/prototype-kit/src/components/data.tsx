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
import { pressHandler } from "../runtime/press.js";
import { SelectableBox, excerpt, requireId, selectableRootProps, type SelectableRootProps } from "../runtime/selectable.js";
import { useThemed } from "../theme/context.js";
import type { Tone } from "./content.js";

export interface TableRow {
  /** The row's element id: unique on the screen (prefix it with the table's). */
  id: string;
  /** Cells in column order. */
  cells: string[];
  /** Colours the last cell as a status. */
  tone?: Tone | undefined;
  /** Open this screen when the row is pressed. */
  to?: string | undefined;
  params?: Record<string, string> | undefined;
}

export interface TableProps {
  id: string;
  title?: string | undefined;
  columns: string[];
  rows: TableRow[];
  /** Run when a row is pressed, with its id; a row with neither this nor `to` is only highlighted. */
  onRowPress?: ((rowId: string) => void) | undefined;
  /** What an empty table shows instead (an `<EmptyState>`). */
  empty?: ReactNode;
}

export interface ThemeTableRow {
  id: string;
  /** Exactly one per column. */
  cells: string[];
  tone?: Tone | undefined;
  /** The row the reviewer last pressed (when it leads nowhere). */
  highlighted: boolean;
  onPress: () => void;
  root: SelectableRootProps;
}

export interface ThemeTableProps {
  title?: string | undefined;
  columns: string[];
  rows: ThemeTableRow[];
}

/** A table of records. */
export function Table({ id, title, columns, rows, onRowPress, empty }: TableProps) {
  const ctx = useKit();
  const Themed = useThemed("Table");
  requireId("Table", id);
  const [highlighted, setHighlighted] = useState<string | null>(null);
  if (rows.length === 0 && empty !== undefined) return <>{empty}</>;
  return (
    <SelectableBox id={id} label={title ?? columns[0] ?? "Table"} container>
      <Themed
        title={title}
        columns={columns}
        rows={rows.map((row) => {
          const bound = onRowPress !== undefined || row.to !== undefined;
          const press = pressHandler(ctx, { to: row.to, params: row.params, onPress: onRowPress && (() => onRowPress(row.id)) });
          return {
            id: requireId("Table row", row.id),
            cells: columns.map((_, i) => row.cells[i] ?? ""),
            tone: row.tone,
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
