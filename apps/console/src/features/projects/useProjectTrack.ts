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

import { useMemo } from "react";
import { useBuilds } from "../builds/api/builds";
import { useBuildOffer } from "../builds/buildPicker";
import { useBuildOutcome } from "../builds/hooks/useBuildOutcome";
import { useProjectStatus } from "../deploy/api/deploy";
import { useDesignModel } from "../design/useDesignModel";
import { useSpecWorkspace } from "../spec/useSpecWorkspace";
import { projectTrack, type ProjectTrack } from "./model/track";

export interface ProjectTrackState {
  /** Null while any source is loading, or when one failed. */
  track: ProjectTrack | null;
  /** The newest version built, whose state the Build leg shows; null before the first build. */
  latestBuild: string | null;
  /** Why a source could not be read. */
  error: string | null;
  /** Read the failed sources again. */
  retry: () => void;
}

/**
 * The overview's track, from the queries the page already reads: the spec
 * workspace, the design review, the builds (with the build offer they
 * make), and the project's status for its deploy aggregate. A finished turn or a started build refreshes those, and so the
 * track, with nothing of its own to invalidate.
 */
export function useProjectTrack(projectName: string): ProjectTrackState {
  const { model, workspace } = useSpecWorkspace(projectName);
  const design = useDesignModel(projectName);
  const builds = useBuilds(projectName);
  const status = useProjectStatus(projectName);
  const deploy = status.data?.deploy;
  const offer = useBuildOffer(projectName);
  const latest = builds.data?.at(-1);
  const latestOutcome = useBuildOutcome(projectName, latest?.status === "built" ? latest.version : undefined).outcome;

  const track = useMemo(
    () =>
      model.data && workspace && design.data && builds.data && offer && deploy
        ? projectTrack({
            features: workspace.features,
            design: workspace.design,
            designedFrom: model.data.design.designedFrom,
            review: design.data,
            builds: builds.data,
            latestOutcome,
            offer,
            deploy,
          })
        : null,
    [model.data, workspace, design.data, builds.data, latestOutcome, offer, deploy],
  );

  const failed = [model, design, builds, status].filter((q) => q.isError);
  return {
    track: failed.length > 0 ? null : track,
    latestBuild: latest?.version ?? null,
    error: failed[0]?.error?.message ?? null,
    retry: () => {
      for (const q of failed) void q.refetch();
    },
  };
}
