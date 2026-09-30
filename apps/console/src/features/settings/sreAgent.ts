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

/**
 * The SRE agent model row's pure logic: what `ConfigProjection` says about the
 * OpenChoreo SRE agent's model, in the terms the row draws.
 *
 * `sreRowState` picks one of four pictures: the row is absent (this server
 * does not push the SRE agent), the org's own model connection is doing the
 * job (`inherited`), an SRE-only connection overrides it (`override`), or
 * neither is usable (`unavailable`, with why). `sreStatusLabel` turns the
 * agent's rollout status into the chip beside the row. `sreAgentPollInterval`
 * is `useConfig`'s `refetchInterval`, so the chip does not get stuck on a
 * stale "applying" read.
 */

import type { components } from "../../generated/aep-api";

type ConfigProjection = components["schemas"]["ConfigProjection"];
type SreAgentProjection = components["schemas"]["SreAgentProjection"];
type LLMProjection = components["schemas"]["LLMProjection"];

export type SreRowState =
  | { kind: "hidden" }
  | { kind: "inherited"; model: string; host: string }
  | { kind: "override"; model: string; host: string; keyPreview: string }
  | { kind: "unavailable"; reason: string };

/**
 * The row's state, read from the projection alone. An override
 * (`config.sreLlm`) always wins over the org connection, even when a
 * misconfigured override leaves the agent worse off than inheriting would —
 * the reader chose it, and Remove is one click away. With no override, the
 * agent runs on the org connection when it can (`source === "organization"`)
 * or is unavailable, naming why.
 */
export function sreRowState(config: ConfigProjection): SreRowState {
  if (config.sreAgent === null) return { kind: "hidden" };
  if (config.sreLlm !== null) {
    return {
      kind: "override",
      model: config.sreLlm.model,
      host: config.sreLlm.host,
      keyPreview: config.sreLlm.keyPreview,
    };
  }
  if (config.sreAgent.source === "organization") {
    return { kind: "inherited", model: config.sreAgent.model, host: config.sreAgent.host };
  }
  return { kind: "unavailable", reason: unavailableReason(config.llm) };
}

/** Why the SRE agent has no usable model: the org connection's shape, or none at all. */
function unavailableReason(llm: LLMProjection | null): string {
  if (!llm) return "No model connection. Set an SRE model to enable RCA.";
  return `The organization's model connection is \`${llm.kind}\`; the SRE agent needs an OpenAI-compatible endpoint with a Bearer key. Set an SRE model to enable RCA.`;
}

/** The chip beside the row: what the agent's rollout is doing right now. */
export function sreStatusLabel(
  status: SreAgentProjection["status"],
  reason?: string,
): { text: string; severity: "info" | "success" | "error" } {
  switch (status) {
    case "applying":
      return { text: "Applying…", severity: "info" };
    case "running":
      return { text: "Running", severity: "success" };
    case "failed":
      return { text: `Failed: ${reason ?? ""}`, severity: "error" };
    case "unconfigured":
      return { text: "Not running", severity: "info" };
  }
}

// Short enough that the status chip leaves "Applying…" for "Running"/"Failed"
// soon after aep-api's push resolves, without hammering GET /config.
const SRE_AGENT_POLL_MS = 5_000;

/**
 * `useConfig`'s `refetchInterval`: a save's PATCH response is read right after
 * aep-api pushes, so `sreAgent.status` is still "applying" — poll GET /config
 * until the rollout lands on running/failed/unconfigured (or the agent goes
 * away), instead of leaving the chip stuck until a page reload.
 */
export function sreAgentPollInterval(config: ConfigProjection | undefined): number | false {
  return config?.sreAgent?.status === "applying" ? SRE_AGENT_POLL_MS : false;
}
