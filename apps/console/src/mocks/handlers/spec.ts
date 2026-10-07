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

import { http, HttpResponse } from "msw";
import { MOCK_SPEC_CHANGES_SEEN_PATH, MOCK_SPEC_PATH, type MockSpecExtras } from "../../features/spec/api/specModel";
import type { components } from "../../generated/aep-api";
import type { MockSpecModel } from "../fixtures/spec";
import { liveDesign, saveDesign } from "../designState";
import { specView } from "../specState";

type SpecState = components["schemas"]["SpecState"];

// The spec state (get-spec-state) the way the platform serves it, off the
// mock's model; and, on a mock-only path, what the mock stands in for: the
// files the local doc is seeded from (the room's, on the platform) and the
// design comments' marks on the spec (E5, parked).

function specState(model: MockSpecModel): SpecState {
  return {
    designedFrom: model.design.designedFrom,
    documents: model.documents.map((d) => ({ id: d.id, title: d.title, pages: d.pages, rows: d.rows })),
  };
}

function extras(model: MockSpecModel): MockSpecExtras {
  return { files: model.files, openComments: model.design.openComments, specChanges: model.design.specChanges };
}

export const specHandlers = [
  http.get("*/api/v1/projects/:projectName/spec/state", ({ params }) =>
    HttpResponse.json<SpecState>(specState(specView(String(params.projectName)))),
  ),

  http.get(`*${MOCK_SPEC_PATH}`, ({ params }) => HttpResponse.json<MockSpecExtras>(extras(specView(String(params.projectName))))),

  // The user opened the Spec tab: the lines design comments changed are seen.
  http.post(`*${MOCK_SPEC_CHANGES_SEEN_PATH}`, ({ params }) => {
    const projectName = String(params.projectName);
    const design = liveDesign(projectName);
    design.comments = design.comments.map((c) => ({ ...c, specSeen: true }));
    saveDesign(projectName, design);
    return HttpResponse.json<MockSpecExtras>(extras(specView(projectName)));
  }),
];
