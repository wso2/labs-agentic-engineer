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

import { Box, Button, Typography } from "@wso2/oxygen-ui";
import { settleAssumedLine } from "../../spec/collab/specEdits";
import { parseLine, type LineBlock } from "../../spec/model/ids";
import { useOpenSpecTarget, useSpecWorkspace } from "../../spec/useSpecWorkspace";
import { useStartInterview } from "../useStartInterview";

// What the chat offers once an interview has written its feature: first a
// walk through the lines it assumed, then the next feature to interview.
// Both are read from the live documents, as Next up is, so a line settled on
// the page is gone here too, and each action here is the page's own: Keep and
// Remove are the edits the line's buttons make in the editor, I'll edit opens
// the line there.

/** A line's words without its `*assumed*` tag. */
function withoutTag(line: LineBlock): string {
  const tag = parseLine(line.text, line.emphasis).assumed;
  if (!tag) return line.text;
  return `${line.text.slice(0, tag.start)}${line.text.slice(tag.end)}`.trim();
}

const kicker = {
  fontSize: "0.6875rem",
  letterSpacing: "0.08em",
  textTransform: "uppercase",
  fontWeight: 600,
  color: "text.secondary",
} as const;

export function InterviewFollowUp({ projectName, path }: { projectName: string; path: string }) {
  const { doc, lines, workspace } = useSpecWorkspace(projectName);
  const openTarget = useOpenSpecTarget(projectName);
  const interview = useStartInterview(projectName);
  if (!doc || !lines || !workspace) return null;
  const feature = workspace.features.find((f) => f.path === path);
  if (!feature) return null;

  const assumed = (lines.get(path) ?? []).filter((l) => parseLine(l.text, l.emphasis).assumed);
  // The next feature to interview, by Next up's own rule.
  const nextFeature = workspace.features.find(
    (f) =>
      f.id !== feature.id &&
      workspace.nextUp.some((item) => item.kind === "interview" && item.target.card === "spec" && item.target.file === f.id),
  );

  return (
    <Box
      component="section"
      aria-label={`After the ${feature.name} interview`}
      sx={{
        ml: 4,
        border: 1,
        borderColor: "divider",
        borderRadius: 2.5,
        bgcolor: "background.paper",
        display: "flex",
        flexDirection: "column",
        "& > * + *": { borderTop: 1, borderColor: "divider" },
      }}
    >
      <Box sx={{ px: 1.5, py: 1.25, display: "flex", flexDirection: "column", gap: 1 }}>
        <Typography component="h3" sx={kicker}>
          Assumed in {feature.name}
        </Typography>
        {assumed.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            Nothing left to confirm: every line is settled.
          </Typography>
        ) : (
          assumed.map((line) => (
            <Box key={line.text} sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
              <Typography variant="body2">{withoutTag(line)}</Typography>
              <Box sx={{ display: "flex", gap: 0.75, flexWrap: "wrap" }}>
                <Button size="small" variant="outlined" onClick={() => settleAssumedLine(doc, path, line.text, "keep")}>
                  Keep
                </Button>
                <Button size="small" variant="outlined" color="inherit" onClick={() => settleAssumedLine(doc, path, line.text, "remove")}>
                  Remove
                </Button>
                <Button size="small" variant="text" onClick={() => openTarget({ file: feature.id, at: "assumed" })}>
                  I'll edit
                </Button>
              </Box>
            </Box>
          ))
        )}
      </Box>
      {nextFeature && (
        <Box sx={{ px: 1.5, py: 1.25, display: "flex", flexDirection: "column", alignItems: "flex-start", gap: 0.75 }}>
          <Typography component="h3" sx={kicker}>
            Next up
          </Typography>
          <Typography variant="body2" color="text.secondary">
            {nextFeature.name} isn't interviewed yet.
          </Typography>
          <Button size="small" variant="contained" disabled={!interview.ready} onClick={() => interview.start(nextFeature)}>
            Interview {nextFeature.name}
          </Button>
        </Box>
      )}
    </Box>
  );
}
