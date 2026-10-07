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

import { useState } from "react";
import { Box, IconButton, Menu, MenuItem, Tooltip, Typography } from "@wso2/oxygen-ui";
import { ListTree } from "@wso2/oxygen-ui-icons-react";

// The project's threads, from the chat header: its main chat, and the Issues
// chat once it holds something, the branch stacked on the main one. Each
// shows its message count; the branch says whether it is open or was summed
// up in the main chat.

/** The Issues branch as the menu lists it. */
export interface IssuesThread {
  /** What the user and the agent said there. */
  count: number;
  /** Open: started on the Issues page. Summarised: the main chat has its From Issues note. */
  state: "open" | "summarised" | null;
}

export function ThreadsMenu({
  projectLabel,
  mainCount,
  issues,
  current,
  onMain,
  onIssues,
}: {
  projectLabel: string;
  mainCount: number;
  /** Null while the Issues chat is empty and not started: it is not listed. */
  issues: IssuesThread | null;
  /** The thread in front. */
  current: "main" | "issues";
  /** Bring the main chat to the front. */
  onMain: () => void;
  /** Go to the Issues chat. */
  onIssues: () => void;
}) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const close = () => setAnchor(null);
  const pick = (go: () => void) => () => {
    close();
    go();
  };
  return (
    <>
      <Tooltip title="Threads">
        <IconButton
          size="small"
          aria-label="Threads"
          aria-haspopup="menu"
          aria-expanded={anchor ? true : undefined}
          onClick={(e) => setAnchor(e.currentTarget)}
        >
          <ListTree size={16} />
        </IconButton>
      </Tooltip>
      <Menu
        anchorEl={anchor}
        open={anchor !== null}
        onClose={close}
        transitionDuration={0}
        slotProps={{ paper: { sx: { width: 300, maxWidth: "calc(100vw - 32px)" } } }}
      >
        <ThreadRow label={`${projectLabel} · main chat`} count={mainCount} selected={current === "main"} onClick={pick(onMain)} />
        {issues && (
          <ThreadRow
            label="Issues"
            state={issues.state}
            count={issues.count}
            branch
            selected={current === "issues"}
            onClick={pick(onIssues)}
          />
        )}
      </Menu>
    </>
  );
}

function ThreadRow({
  label,
  state,
  count,
  branch,
  selected,
  onClick,
}: {
  label: string;
  state?: string | null;
  count: number;
  branch?: boolean;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <MenuItem selected={selected} onClick={onClick} sx={{ gap: 1, alignItems: "baseline", fontSize: "0.8125rem", pl: branch ? 4 : 2 }}>
      {branch && (
        <Box component="span" aria-hidden sx={{ color: "text.secondary", ml: -2 }}>
          └
        </Box>
      )}
      <Typography noWrap sx={{ flex: 1, minWidth: 0, fontSize: "inherit" }}>
        {label}
      </Typography>
      {state && (
        <Typography component="span" variant="caption" color="text.secondary">
          {state}
        </Typography>
      )}
      <Typography component="span" variant="caption" color="text.secondary" sx={{ fontVariantNumeric: "tabular-nums" }}>
        {count}
      </Typography>
    </MenuItem>
  );
}
