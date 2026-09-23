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

// Data: table, filters, timeline — and the record table a task queue shares.

import { Card, CardContent, CardHeader, Chip, ListingTable, Stack, Typography } from "@wso2/oxygen-ui";
import type {
  PrototypeAction,
  PrototypeFiltersNode,
  PrototypeRow,
  PrototypeTableNode,
  PrototypeTimelineNode,
} from "@aep/prototype-model";
import { pressProps, usePrototypeRender } from "../renderContext";
import { toneColor } from "./content";
import { FieldView } from "./forms";

/**
 * Mock records in columns. A row click runs the table's `onRow` action, or
 * highlights the row when the table has none. Rows stop the click in Annotate
 * so selecting a row never also selects its table.
 */
export function RecordTable({
  tableId,
  title,
  columns,
  rows,
  onRow,
}: {
  tableId: string;
  title?: string | undefined;
  columns: string[];
  rows: PrototypeRow[];
  onRow?: PrototypeAction | undefined;
}) {
  const ctx = usePrototypeRender();
  const statusColumn = columns[columns.length - 1];
  return (
    <ListingTable.Container>
      {title && (
        <Typography variant="subtitle1" component="h3" sx={{ px: 2, pt: 1.5 }}>
          {title}
        </Typography>
      )}
      <ListingTable aria-label={title ?? "Records"}>
        <ListingTable.Head>
          <ListingTable.Row>
            {columns.map((c) => (
              <ListingTable.Cell key={c}>{c}</ListingTable.Cell>
            ))}
          </ListingTable.Row>
        </ListingTable.Head>
        <ListingTable.Body>
          {rows.map((row) => (
            <ListingTable.Row
              key={row.id}
              hover
              clickable
              selected={ctx.view.selectedRowIds[tableId] === row.id}
              {...pressProps(ctx, row.id, onRow ?? { kind: "select-row", tableId, rowId: row.id })}
            >
              {columns.map((c) => (
                <ListingTable.Cell key={c}>
                  {c === statusColumn && row.tone ? (
                    <Chip size="small" label={row.values[c] ?? ""} color={toneColor(row.tone)} />
                  ) : (
                    (row.values[c] ?? "")
                  )}
                </ListingTable.Cell>
              ))}
            </ListingTable.Row>
          ))}
        </ListingTable.Body>
      </ListingTable>
    </ListingTable.Container>
  );
}

export function TableView({ node }: { node: PrototypeTableNode }) {
  return <RecordTable tableId={node.id} title={node.title} columns={node.columns} rows={node.rows} onRow={node.onRow} />;
}

export function FiltersView({ node }: { node: PrototypeFiltersNode }) {
  return (
    <Stack direction="row" spacing={2} alignItems="center" flexWrap="wrap" useFlexGap>
      {node.fields.map((f) => (
        <FieldView key={f.id} field={f} compact />
      ))}
    </Stack>
  );
}

export function TimelineView({ node }: { node: PrototypeTimelineNode }) {
  return (
    <Card variant="outlined">
      <CardHeader title="Activity" />
      <CardContent>
        <Stack component="ol" spacing={2} sx={{ m: 0, p: 0, listStyle: "none" }}>
          {node.entries.map((e) => (
            <Stack key={e.id} component="li" direction="row" spacing={2}>
              <Typography variant="caption" color="text.secondary" sx={{ minWidth: 110 }}>
                {e.when}
              </Typography>
              <Stack>
                <Typography variant="body2">{e.text}</Typography>
                <Typography variant="caption" color="text.secondary">
                  {e.who}
                </Typography>
              </Stack>
            </Stack>
          ))}
        </Stack>
      </CardContent>
    </Card>
  );
}
