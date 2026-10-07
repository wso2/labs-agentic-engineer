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

import { Box, Typography } from "@wso2/oxygen-ui";
import type { ResolvedId } from "../model/ids";

/** What hovering an ID shows: the line's words, a feature's name and purpose, or where a retired ID went. */
export function IdPreview({ id, resolved }: { id: string; resolved: ResolvedId | null }) {
  if (!resolved) {
    return (
      <Typography variant="body2" color="text.secondary">
        No {id} in this spec.
      </Typography>
    );
  }
  const { entry, retiredFrom } = resolved;
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.25, maxWidth: 360 }}>
      {retiredFrom && (
        <Typography variant="caption" color="text.secondary">
          {retiredFrom} is now {entry.id}
        </Typography>
      )}
      <Typography variant="body2" sx={{ fontWeight: 600 }}>
        <Box component="span" sx={{ fontFamily: "monospace", fontWeight: 400, color: "text.secondary", mr: 0.75 }}>
          {entry.id}
        </Box>
        {entry.title ?? ""}
      </Typography>
      <Typography variant="body2">{entry.text}</Typography>
    </Box>
  );
}
