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

import { Box, Chip, ListingTable, Typography } from "@wso2/oxygen-ui";
import type { ThemeTableProps, ThemeTimelineProps } from "@wso2/prototype-kit";
import { TitledCard } from "./card.js";

export function Table({ title, columns, rows }: ThemeTableProps) {
  return (
    <ListingTable.Container>
      {title && (
        <Typography variant="subtitle1" component="h3" sx={{ fontWeight: 600, px: 2, pt: 2, pb: 1 }}>
          {title}
        </Typography>
      )}
      <ListingTable aria-label={title ?? "Records"}>
        <ListingTable.Head>
          <ListingTable.Row>
            {columns.map((c, i) => (
              <ListingTable.Cell key={i}>{c}</ListingTable.Cell>
            ))}
          </ListingTable.Row>
        </ListingTable.Head>
        <ListingTable.Body>
          {rows.map((row) => (
            <ListingTable.Row key={row.id} clickable hover selected={row.highlighted} aria-selected={row.highlighted} onClick={row.onPress} {...row.root}>
              {row.cells.map((value, i) => (
                <ListingTable.Cell key={i}>{i === row.cells.length - 1 && row.tone ? <Chip size="small" label={value} color={row.tone} /> : value}</ListingTable.Cell>
              ))}
            </ListingTable.Row>
          ))}
        </ListingTable.Body>
      </ListingTable>
    </ListingTable.Container>
  );
}

export function Timeline({ title, entries }: ThemeTimelineProps) {
  return (
    <TitledCard title={title}>
      <Box component="ol" sx={{ listStyle: "none", m: 0, p: 0, display: "flex", flexDirection: "column", gap: 1.5 }}>
        {entries.map((e, i) => (
          <Box component="li" key={i} sx={{ display: "grid", gridTemplateColumns: "120px 1fr", gap: 1.5, pl: 1.5, borderLeft: 2, borderColor: "divider" }}>
            <Typography component="time" variant="caption" color="text.secondary">
              {e.when}
            </Typography>
            <div>
              <Typography variant="body2">{e.text}</Typography>
              <Typography variant="caption" color="text.secondary">
                {e.who}
              </Typography>
            </div>
          </Box>
        ))}
      </Box>
    </TitledCard>
  );
}
