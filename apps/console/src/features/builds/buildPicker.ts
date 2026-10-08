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

import { createContext, useCallback, useContext, useMemo } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useDesignModel } from "../design/useDesignModel";
import { useSpecWorkspace } from "../spec/useSpecWorkspace";
import { useBuilds } from "./api/builds";
import { buildOffer, offeredRows, type BuildOffer } from "./model/picker";

// The build picker is one dialog per project, opened from wherever a build is
// offered: the overview's Build v1, the track's Build leg, the design card
// and Next up. The project route owns it (components/BuildPickerHost.tsx); a
// page only asks for it to open.

export interface BuildPickerControls {
  open: () => void;
}

export const BuildPickerContext = createContext<BuildPickerControls | null>(null);

/** The build offer for a project, from the live spec, the design review and the builds so far; null while any is loading. */
export function useBuildOffer(projectName: string): BuildOffer | null {
  const { model, workspace, lines } = useSpecWorkspace(projectName);
  const design = useDesignModel(projectName).data;
  const builds = useBuilds(projectName).data;
  const spec = model.data;
  return useMemo(
    () =>
      spec && workspace && lines && design && builds
        ? buildOffer({
            features: workspace.features,
            designedFrom: spec.design.designedFrom,
            productWide: workspace.productWide,
            lines,
            dependencies: design.dependencies,
            comments: design.comments,
            artifacts: design.artifacts,
            builds,
          })
        : null,
    [spec, workspace, lines, design, builds],
  );
}

/**
 * The build action as an entry point shows it: its words ("Build v1") while
 * something is designed and no build runs, else null; and what it does,
 * which opens the picker, or the running build's card while a build runs.
 */
export function useBuildAction(projectName: string): { offer: BuildOffer | null; label: string | null; open: () => void } {
  const controls = useContext(BuildPickerContext);
  if (!controls) throw new Error("useBuildAction outside the project's build picker");
  const offer = useBuildOffer(projectName);
  const navigate = useNavigate();
  const running = offer?.running ?? null;
  const open = useCallback(() => {
    if (running) void navigate({ to: "/projects/$projectName/builds/$version", params: { projectName, version: running.version } });
    else controls.open();
  }, [controls, navigate, projectName, running]);
  const label = offer && !running && offeredRows(offer).length > 0 ? `Build ${offer.version}` : null;
  return { offer, label, open };
}
