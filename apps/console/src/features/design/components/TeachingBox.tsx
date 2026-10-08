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
import { Box, Link, Typography } from "@wso2/oxygen-ui";
import { soft } from "../../spec/components/Tag";

/**
 * How feedback works, taught once per user: the design turn says it in the
 * chat, and this box says it on the surface until the user has done the loop
 * once (their first Address comments). After that it folds into a link.
 */
export function TeachingBox({ taught }: { taught: boolean }) {
  const [open, setOpen] = useState(false);
  if (taught && !open) {
    return (
      <Link component="button" type="button" variant="body2" onClick={() => setOpen(true)} sx={{ alignSelf: "flex-start" }}>
        How commenting works
      </Link>
    );
  }
  return (
    <Box
      component="aside"
      aria-label="How feedback works"
      sx={{
        border: 1,
        borderColor: "info.main",
        bgcolor: soft("info"),
        borderRadius: 2.5,
        px: 1.75,
        py: 1.5,
        fontSize: "0.8125rem",
        display: "flex",
        flexDirection: "column",
        gap: 0.5,
        maxWidth: "72ch",
      }}
    >
      <Typography component="b" sx={{ fontWeight: 600, fontSize: "0.8125rem" }}>
        How feedback works here
      </Typography>
      <Box component="ol" sx={{ m: 0, pl: 2.5, display: "flex", flexDirection: "column", gap: 0.25 }}>
        <li>
          Click anything in an artifact to pin a comment. In the prototype and flows, switch to <b>Comment</b> first.
        </li>
        <li>
          Pin as many as you like, then press <b>Address comments</b>. The agent works through them together.
        </li>
        <li>
          Check each change, then <b>Resolve</b> the comment, or reply if it isn&apos;t right yet.
        </li>
      </Box>
      <Typography variant="body2" color="text.secondary" sx={{ fontSize: "0.8125rem" }}>
        You can also just say it in the chat.
      </Typography>
      {taught && (
        <Link component="button" type="button" variant="body2" onClick={() => setOpen(false)} sx={{ alignSelf: "flex-start" }}>
          Hide
        </Link>
      )}
    </Box>
  );
}
