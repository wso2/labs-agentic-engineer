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

import type { MockSpecModel } from "./fixtures/spec";
import { interviewEffects, prototypeFileWrites } from "./chatServer";
import { createdProjects } from "./createdProjects";
import { designFileWrites, designSummary } from "./designState";
import { projects } from "./fixtures/projects";
import { acmeExpensesSpec, freshSpec, triageAgentSpec } from "./fixtures/spec";

// PROVISIONAL — mock-only until S3 lands the spec model's contract; see
// features/spec/api/specModel.ts. Deleted when the workspace reads the contract.
//
// Each project's model is copied from its fixture on first read, and lives
// as long as the page: a reload starts it over, as the local spec doc does.
// What the agent's interviews did is laid over it on every read, from the mock agent server
// (chatServer.ts), which keeps them across a reload with the conversation;
// so is what the design review did (designState.ts): the designed features'
// stage, the design summary, and a spec line a design comment rewrote; and the
// prototype files a `/prototype` turn wrote.

const seeds: Record<string, MockSpecModel> = {
  "acme-expenses": acmeExpensesSpec,
  "triage-agent": triageAgentSpec,
};

const live = new Map<string, MockSpecModel>();

function displayName(projectName: string): string {
  const project =
    projects.find((p) => p.name === projectName) ??
    createdProjects().find((c) => c.project.name === projectName)?.project;
  return project?.displayName ?? projectName;
}

/** The model the user's verdicts change. */
export function liveSpec(projectName: string): MockSpecModel {
  let model = live.get(projectName);
  if (!model) {
    model = structuredClone(seeds[projectName] ?? freshSpec(displayName(projectName)));
    live.set(projectName, model);
  }
  return model;
}

/** The model as served: the live one, with each interview's stage and written file, and the design's. */
export function specView(projectName: string): MockSpecModel {
  const model = liveSpec(projectName);
  const effects = interviewEffects(projectName);
  const design = designSummary(projectName);
  const files = { ...model.files };
  const features = model.features.map((f) => {
    const effect = effects.get(f.id);
    if (effect?.file) files[effect.file.path] = effect.file.content;
    const stage = effect?.stage ?? f.stage;
    return { ...f, stage: stage === "Interviewed" && design.designedFrom[f.id] !== undefined ? "Designed" : stage };
  });
  for (const [path, content] of designFileWrites(projectName)) files[path] = content;
  for (const [path, content] of prototypeFileWrites(projectName)) files[path] = content;
  return { ...model, features, files, design };
}
