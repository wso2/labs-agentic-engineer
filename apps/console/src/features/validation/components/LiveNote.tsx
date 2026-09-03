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

import { Typography } from "@wso2/oxygen-ui";

/**
 * What the run is doing right now, said once above the criterion rows.
 *
 * Shared by both tiles because both can be on screen while a cycle is in flight:
 * a first attempt renders PendingTile, a repeat renders VerdictTile over the
 * previous attempt's verdict. A note that appeared on only one of them would go
 * missing exactly when a reader is most anxious about a run — the repeat.
 *
 * It renders ONLY in the windows where the rows below have nothing to say (see
 * liveLine.ts), so it never competes with them. That is why it is set quietly:
 * when it is on screen it is the only thing moving, and when the rows are moving
 * it is not on screen at all.
 */
export function LiveNote({ note }: { note: string }) {
  if (!note) return null;
  return (
    <Typography
      variant="body2"
      color="text.secondary"
      sx={{ mt: 0.5, fontStyle: "italic" }}
    >
      {note}
    </Typography>
  );
}
