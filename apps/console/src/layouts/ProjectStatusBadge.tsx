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

import { Box, Chip, CircularProgress, alpha } from "@wso2/oxygen-ui";
import type { Theme } from "@wso2/oxygen-ui";
import { useParams } from "@tanstack/react-router";
import { useProjectStatus } from "../features/projects/api/queries";
import { projectChip } from "../features/projects/lib/projectChip";
import type { StatusTone } from "../components/StatusChip";

// The tone's palette family. `neutral` is the one tone with no family of its
// own, so it reads off the text colour like every other neutral in the app.
const FAMILY: Record<
  Exclude<StatusTone, "neutral">,
  "success" | "info" | "warning" | "error" | "primary"
> = {
  success: "success",
  info: "info",
  warning: "warning",
  error: "error",
  primary: "primary",
};

function toneColor(theme: Theme, tone: StatusTone): string {
  return tone === "neutral"
    ? theme.palette.text.disabled
    : theme.palette[FAMILY[tone]].main;
}

/**
 * The project's state, in the toolbar beside the project switcher.
 *
 * It used to be a soft `StatusChip` beside the page title, repeated on
 * Overview, Deployments and Issues — three copies of one fact, each scrolling
 * away with its page and each absent from the pages that never adopted it. It
 * belongs with the project's identity, and the project's identity lives in the
 * switcher.
 *
 * A soft chip, matching every other status pill in the console. A bare dot and
 * a caption was tried first and lost the fight with the toolbar: at that weight
 * it read as a label rather than as a state.
 *
 * It reports the repository and the delivery stages, and nothing before the
 * first build. The spec is not part of it: a spec can be amended while the
 * last version builds, so one line cannot carry both — `projectChip` says why.
 *
 * The leading mark is the one thing this chip does that the shared `StatusChip`
 * cannot. `busy` states — the platform is working and the label will change on
 * its own — get a spinner; states that only change when somebody acts get a
 * plain dot. So "Building" and "Deploying" turn, while "Active", "Built" and
 * "Build failed" sit still. Motion is reserved for things that are moving, and
 * a settled failure is not one of them.
 */
export function ProjectStatusBadge() {
  const params = useParams({ strict: false }) as { projectName?: string };
  const projectName = params.projectName ?? "";
  const status = useProjectStatus(projectName);

  // Nothing outside a project, and nothing while the first read is in flight:
  // a placeholder here would be a word appearing next to the project name a
  // moment after the page settles, which is worse than arriving quietly.
  if (!projectName || !status.data) return null;

  const chip = projectChip(status.data);
  if (!chip) return null;

  return (
    <Chip
      role="status"
      size="small"
      sx={(theme) => ({
        ml: 0.5,
        bgcolor: alpha(toneColor(theme, chip.tone), 0.14),
        color: toneColor(theme, chip.tone),
        fontWeight: 500,
      })}
      label={
        <Box sx={{ display: "inline-flex", alignItems: "center", gap: 0.75 }}>
          {chip.busy ? (
            // aria-hidden: the chip's own text already says "Building". A
            // progressbar announced beside it would be a second, valueless
            // reading of the same fact.
            <CircularProgress size={10} thickness={6} color="inherit" aria-hidden />
          ) : (
            <Box
              aria-hidden
              sx={{ width: 6, height: 6, borderRadius: "50%", bgcolor: "currentColor" }}
            />
          )}
          <span>{chip.label}</span>
        </Box>
      }
    />
  );
}
