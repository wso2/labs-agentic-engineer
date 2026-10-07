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

import { Box, ButtonBase, Tooltip } from "@wso2/oxygen-ui";
import { idTarget, resolveId, splitIds, type IdIndex } from "../model/ids";
import type { SpecTarget } from "../useSpecWorkspace";
import { IdPreview } from "./IdPreview";

/** The quiet link's look, shared with the editor's (`.aep-ref`). */
export const quietLinkSx = {
  fontFamily: "monospace",
  fontSize: "0.92em",
  color: "text.secondary",
  textDecoration: "underline dotted",
  textUnderlineOffset: "3px",
  cursor: "pointer",
  "&:hover": { color: "primary.main" },
} as const;

/** An ID drawn outside a document: hover shows its line, click opens it (a retired ID, its replacement). */
function QuietId({ id, index, onOpen }: { id: string; index: IdIndex; onOpen: (target: SpecTarget) => void }) {
  const resolved = resolveId(index, id);
  return (
    <Tooltip title={<IdPreview id={id} resolved={resolved} />} placement="top-start">
      <ButtonBase
        disableRipple
        onClick={() => resolved && onOpen(idTarget(resolved.entry))}
        sx={{ ...quietLinkSx, verticalAlign: "baseline", font: "inherit", fontFamily: "monospace" }}
      >
        {id}
      </ButtonBase>
    </Tooltip>
  );
}

/** Text with every ID in it a quiet link. */
export function QuietIdText({
  text,
  index,
  onOpen,
}: {
  text: string;
  index: IdIndex;
  onOpen: (target: SpecTarget) => void;
}) {
  return (
    <Box component="span">
      {splitIds(text).map((part, i) =>
        "id" in part ? (
          <QuietId key={i} id={part.id} index={index} onOpen={onOpen} />
        ) : (
          <span key={i}>{part.text}</span>
        ),
      )}
    </Box>
  );
}
