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

import { useMemo, useRef, useState } from "react";
import {
  Alert,
  Box,
  Chip,
  CircularProgress,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from "@wso2/oxygen-ui";
import { ExcalidrawView, PrototypeView } from "@aep/ui-excalidraw-view";
import { useDerivedPrototype, useDerivedWireframe } from "../api/useDerivedDesign";
import { deriveWireframeScene } from "../derive/deriveWireframe";
import type { SpecFileEntry } from "../api/mapping";
import type { CollabSpec } from "../collab/useCollabSpec";
import { useYTextString } from "../collab/useYTextString";

type ViewMode = "canvas" | "prototype";

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
  const [mode, setMode] = useState<ViewMode>("canvas");

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
  // Streaming means the agent is actively writing. Seeded collab content alone
  // doesn't count — rooms are always seeded with committed files (#86 phase 4),
  // so streaming must require BOTH live content AND an agent in the room.
  const streaming = liveScene != null && agentBusy;
  // Committed fetch: the collab-less base path only (mirrors `usesCollab`
  // disabling the content query for markdown) — passing "" disables it. An
  // agent in the room also suppresses it: the doc WILL deliver the file, and
  // probing git for a not-yet-committed path just sprays retrying 404s.
  const { scene, isPending, isError } = useDerivedWireframe(
    projectName,
    streaming || agentBusy ? "" : dslPath,
    sha,
  );

  // The toggle (and prototype derivation) only makes sense on a settled,
  // committed render — the streaming/agent-busy paths above return early
  // before this ever renders. Called unconditionally to keep hooks order
  // stable; the "" path convention disables the query while unsettled.
  const settled = !streaming && !agentBusy;
  const {
    model: prototypeModel,
    isPending: prototypePending,
  } = useDerivedPrototype(projectName, settled ? dslPath : "", sha);

  if (!streaming && agentBusy) {
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
  if (!streaming && isPending) {
    return (
      <Box sx={{ flex: 1, minHeight: 0, display: "flex", alignItems: "center", justifyContent: "center" }}>
        <CircularProgress aria-label="Loading wireframe" />
      </Box>
    );
  }
  if (!streaming && isError) {
    return <Alert severity="error">Failed to load {dslPath}.</Alert>;
  }
  if (!streaming && !scene) {
    return (
      <Typography variant="body2" color="text.secondary">
        This wireframe source could not be rendered.
      </Typography>
    );
  }

  // The toggle only makes sense once we've settled on a committed scene — the
  // streaming/agent-busy/pending/error branches above all return before here.
  const showToggle = settled && scene != null;

  // A compact segmented control (Oxygen's ToggleButtonGroup, restyled as a
  // pill) rather than the default chunky outlined pair — reads as a native
  // small view switch instead of a stray widget. Colors come from theme
  // tokens so it follows the active Oxygen theme.
  const viewSwitch = showToggle && (
    <ToggleButtonGroup
      size="small"
      exclusive
      value={mode}
      onChange={(_, next: ViewMode | null) => {
        if (next) setMode(next);
      }}
      sx={{
        bgcolor: "action.hover",
        borderRadius: 999,
        p: 0.25,
        "& .MuiToggleButtonGroup-grouped": {
          border: 0,
          borderRadius: 999,
          px: 1.5,
          minHeight: 28,
          textTransform: "none",
          fontSize: "0.8125rem",
          color: "text.secondary",
          "&.Mui-selected": {
            bgcolor: "background.paper",
            color: "text.primary",
            boxShadow: 1,
            "&:hover": { bgcolor: "background.paper" },
          },
        },
      }}
    >
      <ToggleButton value="canvas">Canvas</ToggleButton>
      <ToggleButton value="prototype">Prototype</ToggleButton>
    </ToggleButtonGroup>
  );

  // Prototype mode with a rendered model hands the switch to PrototypeView's
  // `trailingSlot`, so the switch joins PrototypeView's own toolbar row (back ·
  // picker · description) instead of stacking a second gray bar above it.
  // Every other state (canvas, loading, error) keeps its own minimal header —
  // just the switch — since there's no PrototypeView toolbar to join.
  const showOwnHeader = showToggle && !(mode === "prototype" && prototypeModel && !prototypePending);

  // While streaming, ONE mounted canvas takes successive scenes through
  // ExcalidrawView's updateScene path (no `key`, no remount per line). The
  // committed render keeps the remount-by-sha behavior. ExcalidrawView
  // (fillHeight) sets `flex: 1` on its own root inside the flex column.
  return (
    <Box sx={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}>
      {streaming && agentBusy && (
        // The chip means "actively being generated", not "rendered from the
        // live doc" — the doc is ALWAYS the source while collab is up (rooms
        // are seeded), so gate the chip on the agent actually being in the room.
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
      {showOwnHeader && (
        <Box
          sx={{
            px: 1.5,
            py: 1,
            display: "flex",
            alignItems: "center",
            justifyContent: "flex-end",
            borderBottom: 1,
            borderColor: "divider",
          }}
        >
          {viewSwitch}
        </Box>
      )}
      {mode === "prototype" && showToggle ? (
        prototypePending ? (
          <Box sx={{ flex: 1, minHeight: 0, display: "flex", alignItems: "center", justifyContent: "center" }}>
            <CircularProgress aria-label="Loading prototype" />
          </Box>
        ) : prototypeModel ? (
          <PrototypeView key={sha} model={prototypeModel} fillHeight trailingSlot={viewSwitch} />
        ) : (
          <Typography variant="body2" color="text.secondary" sx={{ p: 2 }}>
            This wireframe could not be rendered as a prototype.
          </Typography>
        )
      ) : streaming ? (
        <ExcalidrawView scene={liveScene!} fillHeight />
      ) : (
        <ExcalidrawView key={sha} scene={scene!} fillHeight />
      )}
    </Box>
  );
}
