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

import { useId, useState } from "react";
import { Box, Button, Chip, IconButton, ListingTable, MenuItem, Typography } from "@wso2/oxygen-ui";
import { Ellipsis } from "@wso2/oxygen-ui-icons-react";
import type { ThemeTableAction, ThemeTableColumn, ThemeTableProps, ThemeTimelineProps } from "@wso2/prototype-kit";
import { TitledCard } from "./card.js";
import { InPlaceMenu } from "./menu.js";
import { buttonRoot } from "./root.js";

/** Status, number and actions columns fit their content, so the text columns share the width that is left. */
const FIT = { width: "1%", whiteSpace: "nowrap" } as const;

function columnProps(kind: ThemeTableColumn["kind"]) {
  if (kind === "number") return { align: "right" as const, sx: { ...FIT, fontVariantNumeric: "tabular-nums" } };
  return kind === "status" ? { sx: FIT } : {};
}

/** More than two actions go behind one overflow button, an `InPlaceMenu`. */
function OverflowActions({ actions }: { actions: ThemeTableAction[] }) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const menuId = useId();
  return (
    <>
      <IconButton
        size="small"
        aria-label="More actions"
        aria-haspopup="menu"
        aria-expanded={anchor !== null || undefined}
        aria-controls={anchor !== null ? menuId : undefined}
        onClick={(e) => setAnchor(e.currentTarget)}
      >
        <Ellipsis size={18} />
      </IconButton>
      <InPlaceMenu id={menuId} anchor={anchor} onClose={() => setAnchor(null)} minWidth={160}>
        {actions.map((a) => (
          <MenuItem
            key={a.id}
            onClick={() => {
              setAnchor(null);
              a.onPress();
            }}
            sx={a.emphasis === "danger" ? { color: "error.main" } : {}}
            {...buttonRoot(a.root)}
          >
            {a.label}
          </MenuItem>
        ))}
      </InPlaceMenu>
    </>
  );
}

function RowActions({ actions }: { actions: ThemeTableAction[] }) {
  if (actions.length > 2) return <OverflowActions actions={actions} />;
  return (
    <>
      {actions.map((a) => (
        <Button key={a.id} size="small" variant="text" color={a.emphasis === "danger" ? "error" : "primary"} disableRipple onClick={a.onPress} {...buttonRoot(a.root)}>
          {a.label}
        </Button>
      ))}
    </>
  );
}

export function Table({ title, columns, rows, hasActions }: ThemeTableProps) {
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
              <ListingTable.Cell key={i} {...columnProps(c.kind)}>
                {c.label}
              </ListingTable.Cell>
            ))}
            {hasActions && (
              <ListingTable.Cell align="right" sx={FIT}>
                Actions
              </ListingTable.Cell>
            )}
          </ListingTable.Row>
        </ListingTable.Head>
        <ListingTable.Body>
          {rows.map((row) => (
            <ListingTable.Row key={row.id} clickable hover selected={row.highlighted} aria-selected={row.highlighted} onClick={row.onPress} {...row.root}>
              {row.cells.map((cell, i) => (
                <ListingTable.Cell key={i} {...columnProps(columns[i]?.kind ?? "text")}>
                  {cell.tone ? <Chip size="small" label={cell.text} color={cell.tone} variant={cell.tone === "default" ? "outlined" : "filled"} /> : cell.text}
                </ListingTable.Cell>
              ))}
              {hasActions && (
                // A press on an action is the action's, not the row's.
                <ListingTable.Cell align="right" sx={FIT} onClick={(e) => e.stopPropagation()}>
                  <ListingTable.RowActions>
                    <RowActions actions={row.actions} />
                  </ListingTable.RowActions>
                </ListingTable.Cell>
              )}
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
