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

import { alpha, Stack, Typography } from "@wso2/oxygen-ui";
import { WorkingPulse } from "../../agent-chat/components/WorkingIndicator";

/**
 * MUI sizes an Alert's message column to fit-content, so a tinted child stops
 * short of the tile's edge. Both tiles spread this to let the note span.
 */
export const FULL_WIDTH_ALERT_MESSAGE = {
  "& .MuiAlert-message": { width: "100%" },
} as const;

/**
 * What the run is doing right now, above the criterion rows. Shared by both
 * tiles — a first attempt renders PendingTile, a repeat renders VerdictTile.
 *
 * Rendered ONLY while validation is running (see ValidationPage): the pulse
 * claims an agent is working, and it would be a lie over a settled verdict.
 */
export function LiveNote({ note }: { note: string }) {
  if (!note) return null;
  return (
    <Stack
      direction="row"
      spacing={1}
      sx={{
        // Centred with no offset, as WorkingIndicator pairs this dot with a label.
        alignItems: "center",
        mt: 1,
        p: 1.25,
        borderRadius: 1.5,
        // From text.primary so it inverts with the theme: action.hover is ~4%
        // black in light mode, which vanished on the Alert's own info tint.
        bgcolor: (t) => alpha(t.palette.text.primary, 0.08),
      }}
    >
      <WorkingPulse />
      <Typography variant="body2" sx={{ minWidth: 0 }}>
        {note}
      </Typography>
    </Stack>
  );
}
