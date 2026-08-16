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

import { Box, Skeleton, Typography } from "@wso2/oxygen-ui";

// The PRD's own outline, matching what the start skill writes — the skeleton
// is a promise about THIS document, so the section names must stay in step
// with the prd-contract skill's structure.
const SECTIONS: ReadonlyArray<{ title: string; widths: string[] }> = [
  { title: "Problem", widths: ["88%", "72%"] },
  { title: "Actors", widths: ["55%"] },
  { title: "Journey & stories", widths: ["80%", "64%"] },
  { title: "Product decisions", widths: ["60%"] },
  { title: "Out of scope", widths: ["40%"] },
];

/**
 * The fresh project's main pane (#485): the PRD's section outline with
 * shimmer bars where prose will land. Sets the expectation the document
 * itself will meet — no lorem, no empty void — while the BE-started `/start`
 * turn interviews and writes. Replaced by the real prd.md the moment the
 * agent streams it into the room (SpecView's file union picks it up live).
 */
export function PrdSkeleton({ projectName }: { projectName: string }) {
  return (
    <Box data-testid="prd-skeleton" sx={{ maxWidth: "60ch" }}>
      <Typography variant="h6">{projectName} — PRD</Typography>
      <Typography variant="body2" color="text.disabled" sx={{ mb: 1 }}>
        Taking shape from your idea and your answers…
      </Typography>
      {SECTIONS.map((s) => (
        <Box key={s.title} sx={{ mt: 2.5 }}>
          <Typography variant="subtitle2" color="text.disabled">
            {s.title}
          </Typography>
          {s.widths.map((w) => (
            <Skeleton key={w} animation="wave" height={14} sx={{ width: w }} />
          ))}
        </Box>
      ))}
    </Box>
  );
}
