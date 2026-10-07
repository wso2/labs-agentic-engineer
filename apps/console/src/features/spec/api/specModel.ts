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

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { client } from "../../../api/client";
import { apiErrorMessage } from "../../../api/errors";
import { env } from "../../../config/env";
import type { components } from "../../../generated/aep-api";

// The spec workspace's model (N5). Almost all of it is the documents: the
// collab room holds every file, and the features, their stages, the ID index,
// the Fog, the lines to confirm and Next up are worked out from the live lines
// in the browser (useSpecWorkspace.ts, model/), so an edit shows at once and
// nothing says the same thing twice.
//
// What the documents cannot say comes from the platform: each designed
// feature's basis (what its last design read) and the attached documents —
// get-spec-state, here.
//
// MOCK MODE ONLY: the local doc stands in for the room, seeded from the mock's
// files, and the design comments' marks on the spec (E5, parked) come from the
// mock too, on a path that is not in the contract.

type SpecStateWire = components["schemas"]["SpecState"];

/** Where a feature is in its journey. */
export type FeatureStage = "Not interviewed" | "Interviewing" | "Interviewed" | "Designed";

export interface SpecFeature {
  /** "F2". */
  id: string;
  name: string;
  /** Its file in the spec room, e.g. "specs/requirements/features/F2-approvals.md". */
  path: string;
  purpose: string;
  stage: FeatureStage;
}

/** A product-wide item and the features it applies to. Its words are in product-wide.md. */
export interface ProductWideItem {
  /** "P4". */
  id: string;
  /** Feature IDs, or "all". */
  appliesTo: string[] | "all";
}

/** A document the user attached: what it says, page by page, and where each point landed. */
export interface SourceDocument {
  id: string;
  title: string;
  pages: number;
  rows: { page: string; says: string; landedIn: string | null }[];
}

/** Who wrote a pending change: the attributes of its agentInsertion marks in the doc. */
export interface AgentWriter {
  agent: string;
  at: string;
}

/**
 * A spec line a design comment changed: the comment was really a
 * requirement, so addressing it rewrote the line. It stays marked in the spec
 * until the comment is resolved; `seen` turns true once the user has opened
 * the Spec tab since (the tab's dot).
 */
export interface DesignSpecChange {
  /** The line's ID, "F2.2". */
  lineId: string;
  featureId: string;
  /** The comment's pin number. */
  comment: number;
  seen: boolean;
}

/**
 * What the spec workspace knows of the design review (the design card reads
 * the rest, features/design/api/designModel.ts). Which features wait for
 * design, and which designs are out of date, is worked out in the browser
 * from `designedFrom` and the live documents (model/designWork.ts).
 */
export interface DesignSummary {
  /** Each designed feature, and the spec it was designed from (its basis, model/designWork.ts). */
  designedFrom: Record<string, string>;
  /** Design comments not yet addressed. */
  openComments: number;
  specChanges: DesignSpecChange[];
}

export interface SpecModel {
  features: SpecFeature[];
  documents: SourceDocument[];
  design: DesignSummary;
}

/** The spec state's query key: what an agent turn invalidates. */
export function specKey(projectName: string) {
  return ["projects", projectName, "spec-state"] as const;
}

/** What the platform says beside the documents: each designed feature's basis, and the attached documents. */
export function useSpecState(projectName: string) {
  return useQuery({
    queryKey: specKey(projectName),
    queryFn: async (): Promise<SpecStateWire> => {
      const { data, error } = await client.GET("/projects/{projectName}/spec/state", {
        params: { path: { projectName } },
      });
      if (error || data === undefined) throw new Error(apiErrorMessage(error, "Couldn't load the spec"));
      return data;
    },
  });
}

/** The documents, as the workspace shows them. */
export function sourceDocuments(state: SpecStateWire): SourceDocument[] {
  return state.documents.map((d) => ({
    id: d.id,
    title: d.title,
    pages: d.pages,
    rows: d.rows.map((r) => ({ page: r.page, says: r.says, landedIn: r.landedIn ?? null })),
  }));
}

// ---- MOCK MODE ONLY ---------------------------------------------------------

/** What the mock stands in for: the room's files, and the design comments' marks on the spec (E5, parked). */
export interface MockSpecExtras {
  /** Markdown by room path ("specs/requirements/prd.md"): the local doc's seed. */
  files: Record<string, string>;
  openComments: number;
  specChanges: DesignSpecChange[];
}

/** The mock-only path MSW serves; `:projectName` is the project's slug. */
export const MOCK_SPEC_PATH = "/api/v1/projects/:projectName/mock/spec";

/** The user has seen the spec lines design comments changed: POST, answered with the extras. */
export const MOCK_SPEC_CHANGES_SEEN_PATH = `${MOCK_SPEC_PATH}/design-changes/seen`;

export function mockSpecKey(projectName: string) {
  return ["projects", projectName, "mock-spec"] as const;
}

function url(template: string, params: Record<string, string>): string {
  const path = template.replace(/:(\w+)/g, (_, name: string) => encodeURIComponent(params[name] ?? ""));
  return `${env.apiBaseUrl}${path}`;
}

async function readExtras(response: Response, failure: string): Promise<MockSpecExtras> {
  if (!response.ok) throw new Error(failure);
  return (await response.json()) as MockSpecExtras;
}

/** The mock's extras; never asked for on the platform. */
export function useMockSpecExtras(projectName: string) {
  return useQuery({
    queryKey: mockSpecKey(projectName),
    queryFn: async () => readExtras(await fetch(url(MOCK_SPEC_PATH, { projectName })), "Couldn't load the spec"),
    enabled: env.apiMode === "mock",
  });
}

/** Mark the spec lines design comments changed as seen: the Spec tab's dot goes. A no-op on the platform (E5). */
export function useSeeDesignChanges(projectName: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () =>
      env.apiMode === "mock"
        ? readExtras(await fetch(url(MOCK_SPEC_CHANGES_SEEN_PATH, { projectName }), { method: "POST" }), "Couldn't update the spec")
        : null,
    onSuccess: (extras) => {
      if (extras) queryClient.setQueryData(mockSpecKey(projectName), extras);
    },
  });
}
