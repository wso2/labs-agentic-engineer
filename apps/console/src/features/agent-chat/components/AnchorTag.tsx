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

import { Chip, Stack, Tooltip } from "@wso2/oxygen-ui";
import { Crosshair } from "@wso2/oxygen-ui-icons-react";
import type { components } from "../../../generated/aep-api";

type TurnAnchor = components["schemas"]["TurnAnchor"];

/**
 * What a message was aimed at (#666), above the words it was aimed with.
 *
 * ABOVE, not below like the attachment chips: an attachment is an addendum,
 * something that came along with what you said, while an anchor is the SUBJECT
 * of the sentence. Read after the words, "make these shorter" is a puzzle
 * solved retroactively; read before them, the message parses on the first pass.
 *
 * FROZEN (console ADR-0024). It records what was pointed at when the message
 * was sent and is never re-checked against the current document. A tag that
 * validated itself would make scrolling back through months of conversation
 * show tags flipping to "missing" as specs evolved underneath — the transcript
 * would stop being a record and become a live query with a confusing answer.
 * When the agent cannot find what was named, IT says so, in its own reply,
 * where the failure actually happened.
 */
export function AnchorTag({ anchor }: { anchor: TurnAnchor }) {
  const first = anchor.nodes[0];
  if (!first) return null;
  const extra = anchor.nodes.length - 1;
  // The full selection, for the tooltip: the chip has one line and a markdown
  // name can be a whole sentence, so the row shows the first node and the
  // hover shows everything that was actually selected.
  const detail = anchor.nodes
    .map((n) => (n.context ? `${n.context} · ${n.name}` : n.name))
    .join("\n");

  return (
    <Stack
      direction="row"
      spacing={0.5}
      useFlexGap
      sx={{ flexWrap: "wrap", mb: 0.75 }}
      data-testid="user-message-anchor"
    >
      <Tooltip title={<span style={{ whiteSpace: "pre-line" }}>{detail}</span>}>
        <Chip
          size="small"
          variant="outlined"
          icon={<Crosshair size={12} />}
          // The kind rides the label rather than a second chip: it is what tells
          // a paragraph from an operation once the transcript is all a reader
          // has, and it is one word.
          label={first.context ? `${first.context} · ${first.name}` : first.name}
          sx={{
            maxWidth: "100%",
            "& .MuiChip-icon": { ml: 0.75, mr: -0.25, flexShrink: 0 },
            "& .MuiChip-label": { overflow: "hidden", textOverflow: "ellipsis" },
          }}
        />
      </Tooltip>
      {extra > 0 && (
        <Chip size="small" variant="outlined" label={`+${extra} more`} />
      )}
    </Stack>
  );
}
