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

// The two lines a run emits BEFORE there is a runtime to emit anything.
//
// A pod spends real time on the work that precedes its first model turn —
// cloning the repo, mirroring the org's skills, installing the git credential
// helper — and for that whole stretch the only thing on the feed is whatever
// says it is happening. Losing these lines would put the run's first minutes
// back in the dark zone they were measured in.
//
// **They are notices carrying a `code` and NO prose, and that is the point.**
// v1 had a `phase` kind and v2 does not: an agent's lifecycle is `run_started` /
// `agent_started` / `run_settled`, and the runner's own provisioning is not the
// agent's — so these stay notices. But they ARE closed conditions a consumer
// branches on, which is what `notice.code` is for, and the contract now names
// them: `workspace_provisioning` and `workspace_ready`, beside the six the
// platform stamps for the pod-truth half of the same dark zone.
//
// The prose that used to live here is gone with it. Wording belongs to
// `@aep/progress-view`, which both the console and the playground read; a
// producer that ships its own sentence is how one fact starts reading two ways.
// This file now says WHICH condition, and nothing about how it is worded.

import type { RunEventInput } from "./emitter.js";

export const PROVISIONING: RunEventInput = {
  kind: "notice",
  level: "info",
  code: "workspace_provisioning",
};

export const WORKSPACE_READY: RunEventInput = {
  kind: "notice",
  level: "info",
  code: "workspace_ready",
};
