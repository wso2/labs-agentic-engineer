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

import type { ReactNode } from "react";
import { Typography } from "@wso2/oxygen-ui";

/**
 * The all-caps rule over a GROUP of rows — "EARLIER RUNS OF V1", "EARLIER
 * VALIDATION RUNS", "EARLIER BUILD SESSIONS IN THIS RUN".
 *
 * Distinct from `SectionTitle`, which names a section a reader navigates to.
 * This names a boundary INSIDE one: everything below it is history, and the
 * thing above it is what the page is about.
 *
 * Shared because four surfaces now draw it — the two Builds-page groupings, the
 * Validation page's earlier attempts, and the build detail page's earlier
 * delivery runs — and hand-matched copies of a rule this thin drift by a font
 * weight without anybody noticing.
 */
export function SectionCaption({ children }: { children: ReactNode }) {
  return (
    <Typography
      variant="caption"
      sx={{
        display: "block",
        mb: 1,
        fontWeight: 700,
        letterSpacing: "0.08em",
        color: "text.secondary",
      }}
    >
      {children}
    </Typography>
  );
}
