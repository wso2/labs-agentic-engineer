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

import { useCallback, useMemo } from "react";
import { useNavigate } from "@tanstack/react-router";
import { interviewingIn, useProjectChat } from "../agent-chat/useProjectChat";
import {
  sourceDocuments,
  useMockSpecExtras,
  useSpecState,
  type DesignSpecChange,
  type SpecFeature,
  type SpecModel,
} from "./api/specModel";
import { useSpecDoc } from "./collab/specDoc";
import { useSpecLines } from "./collab/useSpecLines";
import { withCoverage } from "./model/coverage";
import { deriveFeatures } from "./model/features";
import { deriveWorkspace } from "./model/workspace";

// One empty list, not a new one per document change: what is read from it
// (the editor's design marks) then stays the same while it is empty.
const NO_SPEC_CHANGES: DesignSpecChange[] = [];

/**
 * The spec model: the features worked out from the live documents, with the
 * platform's word on what a design has read, and the attached documents. The
 * design comments' marks on the spec are the mock's alone until commenting
 * ships (E5); on the platform there are none.
 */
export function useSpecModel(projectName: string) {
  const state = useSpecState(projectName);
  const extras = useMockSpecExtras(projectName);
  const lines = useSpecLines(useSpecDoc(projectName));
  const interviewing = interviewingIn(useProjectChat(projectName));
  const data = useMemo((): SpecModel | undefined => {
    if (!state.data || !lines) return undefined;
    const designedFrom = state.data.designedFrom;
    return {
      features: deriveFeatures(lines, designedFrom, interviewing),
      documents: withCoverage(sourceDocuments(state.data), lines),
      design: {
        designedFrom,
        openComments: extras.data?.openComments ?? 0,
        specChanges: extras.data?.specChanges ?? NO_SPEC_CHANGES,
      },
    };
  }, [state.data, lines, interviewing, extras.data]);
  return { data, isError: state.isError, error: state.error, refetch: state.refetch };
}

/**
 * The spec workspace for a project: the model's state, the live doc, and what
 * both work out to (feature chips, the ID index, Fog, Next up). The spec card
 * and the overview's feature list read the same one, so a line confirmed in
 * the card is one fewer "to confirm" on the overview.
 */
export function useSpecWorkspace(projectName: string) {
  const model = useSpecModel(projectName);
  const doc = useSpecDoc(projectName);
  const lines = useSpecLines(doc);
  const workspace = useMemo(
    () => (model.data && lines ? deriveWorkspace(model.data, lines) : null),
    [model.data, lines],
  );
  return { model, doc, lines, workspace };
}

/** Where opening something in the spec goes: a file, and a line or place in it. */
export interface SpecTarget {
  file: string;
  at?: string;
}

/** Open a file of the spec card (and a line in it), from anywhere in the project. */
export function useOpenSpecTarget(projectName: string) {
  const navigate = useNavigate();
  return useCallback(
    (target: SpecTarget) =>
      void navigate({
        to: "/projects/$projectName/spec",
        params: { projectName },
        search: { file: target.file, ...(target.at ? { at: target.at } : {}) },
      }),
    [navigate, projectName],
  );
}

/** The feature a spec file key opens, or null when it is not a feature. */
export function useSpecFeature(projectName: string, fileKey: string | null): SpecFeature | null {
  const features = useSpecModel(projectName).data?.features;
  return (fileKey && features?.find((f) => f.id === fileKey)) || null;
}
