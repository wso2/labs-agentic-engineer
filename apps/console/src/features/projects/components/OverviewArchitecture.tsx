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

import { createLink } from "@tanstack/react-router";
import { Box, Button, Skeleton, Typography } from "@wso2/oxygen-ui";
import { ArrowUpRight } from "@wso2/oxygen-ui-icons-react";
import { CellDiagramView } from "@aep/ui-cell-diagram-view";
import { ErrorBoundary } from "../../../components/ErrorBoundary";
import { useSpecDoc } from "../../spec/collab/specDoc";
import { useRoomFiles } from "../../spec/collab/useRoomFiles";
import { PHONE } from "../../shell/layout";

// The project's shape on its overview: the design's architecture (the cell
// diagram), after the old console's OverviewArchitecture. A preview, drawn
// read-only and fitted to its box; the Design card is where it is read and
// discussed, so the heading links there. Read from the same room the Design
// card reads, so the two never disagree.

const ButtonLink = createLink(Button);

const CELL_PATH = "specs/design/design.cell";
const CELL_FILES = [CELL_PATH] as const;

/**
 * The renderer's pan and zoom controls belong to the Design card, not to a
 * preview: hidden here, and `readOnly` keeps the nodes out of reach and out of
 * the tab order.
 */
const PREVIEW_SX = {
  height: 360,
  minWidth: 0,
  display: "flex",
  flexDirection: "column",
  border: 1,
  borderColor: "divider",
  borderRadius: 2.5,
  overflow: "hidden",
  "& .zoom-controls, & .canvas-notification": { display: "none" },
  [PHONE]: { height: 300 },
} as const;

export function OverviewArchitecture({ projectName }: { projectName: string }) {
  const doc = useSpecDoc(projectName);
  const cell = useRoomFiles(doc, CELL_FILES)[CELL_PATH];

  return (
    <Box component="section" aria-labelledby="architecture-heading">
      <Box sx={{ display: "flex", alignItems: "center", mb: 1 }}>
        <Typography id="architecture-heading" component="h2" sx={{ fontSize: "0.8125rem", fontWeight: 600, flex: 1 }}>
          Architecture
        </Typography>
        {cell && (
          <ButtonLink
            to="/projects/$projectName/design"
            params={{ projectName }}
            search={{ art: "architecture" }}
            size="small"
            endIcon={<ArrowUpRight size={14} aria-hidden />}
            sx={{ minHeight: 0, py: 0 }}
          >
            Open in Design
          </ButtonLink>
        )}
      </Box>
      {!doc ? (
        <Skeleton variant="rounded" height={360} />
      ) : !cell ? (
        <Typography variant="body2" color="text.secondary">
          No architecture yet. Once the agent designs the product, this shows its components and how they connect.
        </Typography>
      ) : (
        <Box sx={PREVIEW_SX}>
          <ErrorBoundary label="The architecture diagram" resetKey={cell} fill>
            <CellDiagramView source={cell} layoutKey={`${projectName}:design`} compact readOnly />
          </ErrorBoundary>
        </Box>
      )}
    </Box>
  );
}
