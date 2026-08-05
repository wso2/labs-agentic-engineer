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

import { useMemo, useRef } from "react";
import { Alert, Box, Chip, CircularProgress, Typography } from "@wso2/oxygen-ui";
import { ExcalidrawView } from "@aep/ui-excalidraw-view";
import { useDerivedWireframe } from "../api/useDerivedDesign";
import { deriveWireframeScene } from "../derive/deriveWireframe";
import type { SpecFileEntry } from "../api/mapping";
import type { CollabSpec } from "../collab/useCollabSpec";
import { useYTextString } from "../collab/useYTextString";

export function WireframePanel({
  projectName,
  dslPath,
  files,
  collab,
}: {
  projectName: string;
  dslPath: string;
  files: SpecFileEntry[];
  collab: CollabSpec;
}) {
  const sha = files.find((f) => f.path === dslPath)?.sha;

  // The collab doc is the SOURCE while collab is up — the design.md rule.
  // Rooms are seeded with every committed specs/ file (non-md as Y.Text), and
  // the agents service mirrors each applied write, so the doc is always the
  // freshest truth: the committed content between turns, the growing DSL
  // during a generation, the edited DSL during an edit turn. The committed
  // fetch below runs ONLY when the doc has nothing (collab offline / room not
  // yet synced). Both paths feed the SAME compiler, so the renders match.
  const liveSource = useYTextString(collab.getFileText(dslPath));
  // The writer flushes on line boundaries, so the live text is whole lines —
  // but a mid-stream compile can still fail (e.g. a screen header typed ahead
  // of its body). Hold the last GOOD scene so a bad intermediate never blanks
  // the already-drawn screens.
  const lastGoodLive = useRef<string | null>(null);
  const liveScene = useMemo(() => {
    if (typeof liveSource !== "string" || liveSource.trim().length === 0) return null;
    const compiled = deriveWireframeScene(dslPath, liveSource);
    if (compiled) lastGoodLive.current = compiled;
    return lastGoodLive.current;
  }, [dslPath, liveSource]);

  const agentBusy = collab.peers.some((p) => p.kind === "agent");
  // The doc is always the source while collab is up (rooms seeded with #86
  // phase 4). Use live content whenever it exists, regardless of agent presence.
  const hasLiveContent = liveScene != null;
  // Streaming means the agent is ACTIVELY writing — show the "Drawing…" chip
  // and update the scene in place. Seeded content alone doesn't count.
  const streaming = hasLiveContent && agentBusy;
  // Committed fetch: the collab-less base path only (mirrors `usesCollab`
  // disabling the content query for markdown) — passing "" disables it. An
  // agent in the room also suppresses it: the doc WILL deliver the file, and
  // probing git for a not-yet-committed path just sprays retrying 404s.
  const { scene, isPending, isError } = useDerivedWireframe(
    projectName,
    hasLiveContent || agentBusy ? "" : dslPath,
    sha,
  );

  if (!hasLiveContent && agentBusy) {
    // The committed fetch is suppressed while an agent is in the room (the
    // doc will deliver the file) — "drawing about to start", not a failure.
    return (
      <Box sx={{ flex: 1, minHeight: 0, display: "flex", alignItems: "center", justifyContent: "center" }}>
        <Typography variant="body2" color="text.secondary">
          Waiting for the agent to draw the wireframes…
        </Typography>
      </Box>
    );
  }
  if (!hasLiveContent && isPending) {
    return (
      <Box sx={{ flex: 1, minHeight: 0, display: "flex", alignItems: "center", justifyContent: "center" }}>
        <CircularProgress aria-label="Loading wireframe" />
      </Box>
    );
  }
  if (!hasLiveContent && isError) {
    return <Alert severity="error">Failed to load {dslPath}.</Alert>;
  }
  if (!hasLiveContent && !scene) {
    return (
      <Typography variant="body2" color="text.secondary">
        This wireframe source could not be rendered.
      </Typography>
    );
  }

  // While streaming, ONE mounted canvas takes successive scenes through
  // ExcalidrawView's updateScene path (no `key`, no remount per line). The
  // committed render keeps the remount-by-sha behavior. ExcalidrawView
  // (fillHeight) sets `flex: 1` on its own root inside the flex column.
  return (
    <Box sx={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}>
      {streaming && (
        // The chip means "actively being generated" — agent in room AND writing.
        <Box
          sx={{
            px: 1.5,
            py: 1,
            display: "flex",
            alignItems: "center",
            borderBottom: 1,
            borderColor: "divider",
          }}
        >
          <Chip size="small" color="primary" variant="outlined" label="Drawing…" />
        </Box>
      )}
      {hasLiveContent ? (
        <ExcalidrawView scene={liveScene!} fillHeight />
      ) : (
        <ExcalidrawView key={sha} scene={scene!} fillHeight />
      )}
    </Box>
  );
}
