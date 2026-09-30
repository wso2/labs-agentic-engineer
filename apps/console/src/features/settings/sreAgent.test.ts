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

// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import type { components } from "../../generated/aep-api";
import { llmConnectedFixture, openaiOrgFixture, sreAgentProjectionFixture, sreLlmFixture } from "../../mocks/fixtures/settings";
import { sreAgentPollInterval, sreRowState, sreStatusLabel } from "./sreAgent";

type ConfigProjection = components["schemas"]["ConfigProjection"];

function config(over: Partial<ConfigProjection> = {}): ConfigProjection {
  return {
    llm: llmConnectedFixture,
    llmFormats: [],
    agents: {
      runtime: "claude-code",
      availableRuntimes: ["claude-code", "opencode"],
      subscription: null,
      updatedAt: null,
      updatedBy: null,
    },
    gitProvider: null,
    idp: {
      kind: "platform",
      issuer: "https://idp.aep.local",
      jwksUrl: "https://idp.aep.local/.well-known/jwks.json",
      hasClientSecret: false,
      publisherClientId: "aep-console",
    },
    sreLlm: null,
    sreAgent: sreAgentProjectionFixture(),
    ...over,
  };
}

describe("sreRowState", () => {
  it("is hidden when the server has no SRE agent", () => {
    expect(sreRowState(config({ sreAgent: null }))).toEqual({ kind: "hidden" });
  });

  it("inherits a bearer OpenAI-compatible org connection", () => {
    const cfg = config({
      llm: openaiOrgFixture,
      sreAgent: sreAgentProjectionFixture({
        source: "organization",
        model: openaiOrgFixture.model,
        host: "api.openai.com",
        status: "running",
      }),
    });
    expect(sreRowState(cfg)).toEqual({ kind: "inherited", model: openaiOrgFixture.model, host: "api.openai.com" });
  });

  it("shows the override when one is set", () => {
    const cfg = config({
      sreLlm: sreLlmFixture,
      sreAgent: sreAgentProjectionFixture({ source: "override" }),
    });
    expect(sreRowState(cfg).kind).toBe("override");
  });

  it("is unavailable on an Anthropic org connection and names the format", () => {
    // The default fixture is already an Anthropic connection with no SRE
    // capability, so the default config already exercises this case.
    const s = sreRowState(config());
    expect(s.kind).toBe("unavailable");
    expect((s as { reason: string }).reason).toMatch(/anthropic/i);
  });

  it("is unavailable with no org connection at all", () => {
    const cfg = config({ llm: null });
    const s = sreRowState(cfg);
    expect(s.kind).toBe("unavailable");
    expect((s as { reason: string }).reason).toBe("No model connection. Set an SRE model to enable RCA.");
  });
});

describe("sreStatusLabel", () => {
  it("shows the failure reason", () => {
    expect(sreStatusLabel("failed", "exited (code 3)")).toEqual({ text: "Failed: exited (code 3)", severity: "error" });
  });

  it("shows applying, running and not running", () => {
    expect(sreStatusLabel("applying")).toEqual({ text: "Applying…", severity: "info" });
    expect(sreStatusLabel("running")).toEqual({ text: "Running", severity: "success" });
    expect(sreStatusLabel("unconfigured")).toEqual({ text: "Not running", severity: "info" });
  });
});

// The PATCH /config response that saves an override lands right after
// aep-api pushes it, so `sreAgent.status` reads "applying" for a beat — the
// config query must keep polling GET /config until the rollout resolves,
// or the row's status chip is stuck on "Applying…" until a page reload.
describe("sreAgentPollInterval", () => {
  it("polls while the agent is applying", () => {
    expect(sreAgentPollInterval(config({ sreAgent: sreAgentProjectionFixture({ status: "applying" }) }))).toBe(5000);
  });

  it.each(["running", "failed", "unconfigured"] as const)("stops polling once status is %s", (status) => {
    expect(sreAgentPollInterval(config({ sreAgent: sreAgentProjectionFixture({ status }) }))).toBe(false);
  });

  it("stops polling when the server has no SRE agent", () => {
    expect(sreAgentPollInterval(config({ sreAgent: null }))).toBe(false);
  });

  it("stops polling when there is no data yet", () => {
    expect(sreAgentPollInterval(undefined)).toBe(false);
  });
});
