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

import { describe, expect, it } from "vitest";
import type { components } from "../../generated/aep-api";
import {
  aiSettingsFrom,
  aiSettingsPatch,
  checkStatus,
  connectionView,
  draftFrom,
  draftProblem,
  infoLines,
  keyRequired,
  lastChange,
  modelReads,
  refusedField,
  runtimeMovedByFormat,
  subscriptionOffered,
  switchFormat,
  testBody,
  type AiDraft,
  type LLMCapabilities,
  type LLMCheck,
  type LLMFormatOption,
} from "./aiSettings";

type ConfigProjection = components["schemas"]["ConfigProjection"];
type LLMProjection = components["schemas"]["LLMProjection"];

const formats: LLMFormatOption[] = [
  {
    kind: "anthropic",
    defaultBaseURL: "https://api.anthropic.com/v1",
    defaultModel: "claude-sonnet-5",
    runtimes: ["claude-code", "opencode"],
  },
  { kind: "openai-compatible", defaultBaseURL: null, defaultModel: "glm-5.3", runtimes: ["opencode"] },
];

const anthropicCaps: LLMCapabilities = {
  claudeSubscription: true,
  webSearch: "anthropic-server-tool",
  imageInput: "yes",
  nativePdf: true,
  generatedAgents: true,
};

const ollamaCaps: LLMCapabilities = {
  claudeSubscription: false,
  webSearch: "ollama-api",
  imageInput: "no",
  nativePdf: false,
  generatedAgents: true,
};

const anthropic: LLMProjection = {
  kind: "anthropic",
  baseURL: "https://api.anthropic.com/v1",
  model: "claude-sonnet-5",
  keyPreview: "sk-a…wxyz",
  connectedAt: "2026-06-01T12:05:00Z",
  updatedAt: "2026-09-25T13:53:00Z",
  updatedBy: "dev@acme.example",
  priced: true,
  capabilities: anthropicCaps,
};

function config(over: Partial<ConfigProjection> = {}): ConfigProjection {
  return {
    llm: anthropic,
    llmFormats: formats,
    agents: {
      runtime: "claude-code",
      availableRuntimes: ["claude-code", "opencode"],
      subscription: null,
      updatedAt: null,
      updatedBy: null,
    },
    gitProvider: null,
    idp: { kind: "platform" },
    ...over,
  } as ConfigProjection;
}

const connected = aiSettingsFrom(config());
const unconnected = aiSettingsFrom(config({ llm: null }));
const draft = (over: Partial<AiDraft> = {}, saved = connected): AiDraft => ({ ...draftFrom(saved), ...over });

describe("draftFrom", () => {
  it("starts a first connect on the Anthropic format's defaults", () => {
    expect(draftFrom(unconnected)).toMatchObject({
      kind: "anthropic",
      baseURL: "https://api.anthropic.com/v1",
      model: "claude-sonnet-5",
      apiKey: "",
    });
  });

  it("starts from the saved connection when there is one", () => {
    expect(draftFrom(connected)).toMatchObject({ kind: "anthropic", model: "claude-sonnet-5" });
  });
});

describe("keyRequired", () => {
  it("asks for a key on first connect", () => {
    expect(keyRequired(unconnected, draft({}, unconnected))).toBe(true);
  });

  it("keeps the stored key on the same host, even across a format or model change", () => {
    expect(keyRequired(connected, draft({ model: "claude-haiku-4-5" }))).toBe(false);
    expect(keyRequired(connected, draft({ kind: "openai-compatible" }))).toBe(false);
  });

  it("asks for a new key when the host moves", () => {
    expect(keyRequired(connected, draft({ baseURL: "https://ollama.com/v1" }))).toBe(true);
  });
});

describe("switchFormat", () => {
  it("swaps the URL and model while they hold the old format's defaults, and moves off Claude Code", () => {
    const next = switchFormat(draft(), formats, "openai-compatible");
    expect(next).toMatchObject({ kind: "openai-compatible", baseURL: "", model: "glm-5.3", runtime: "opencode" });
  });

  it("keeps a URL and model the reader typed", () => {
    const typed = draft({ baseURL: "https://ollama.com", model: "kimi-k3" });
    expect(switchFormat(typed, formats, "openai-compatible")).toMatchObject({
      baseURL: "https://ollama.com",
      model: "kimi-k3",
    });
  });

  it("refills the Anthropic defaults on the way back from an empty URL", () => {
    const openai = draft({ kind: "openai-compatible", baseURL: "", model: "glm-5.3", runtime: "opencode" });
    expect(switchFormat(openai, formats, "anthropic")).toMatchObject({
      baseURL: "https://api.anthropic.com/v1",
      model: "claude-sonnet-5",
      // OpenCode runs both formats, so the runtime stays.
      runtime: "opencode",
    });
  });

  it("says the runtime was moved by the format, not chosen", () => {
    const next = switchFormat(draft(), formats, "openai-compatible");
    expect(runtimeMovedByFormat(connected, next)).toBe(true);
    expect(runtimeMovedByFormat(connected, draft({ runtime: "opencode" }))).toBe(false);
  });
});

describe("infoLines", () => {
  it("draws Anthropic's API: priced, its search tool, images and PDFs", () => {
    const lines = infoLines(anthropicCaps, true, "claude-sonnet-5", "api.anthropic.com").map((l) => l.text);
    expect(lines).toEqual([
      "Prompts and code from every agent go to api.anthropic.com.",
      "Usage is priced in USD: claude-sonnet-5 has a rate.",
      "Agents can search the web through Anthropic's search tool.",
      "Chat reads attached images and PDFs.",
    ]);
  });

  it("draws a text-only open model on Ollama: tokens, Ollama search, no images", () => {
    const lines = infoLines(ollamaCaps, false, "glm-5.3", "ollama.com");
    expect(lines[0]).toEqual({ text: "Prompts and code from every agent go to ollama.com.", strong: "ollama.com" });
    expect(lines.map((l) => l.text).slice(1)).toEqual([
      "Usage shows tokens, not dollars: the platform has no rate for glm-5.3. Your provider bills you directly.",
      "Agents search the web through Ollama's search API, with this key.",
      "Chat does not accept images: glm-5.3 does not read them. PDFs are sent as extracted text.",
    ]);
  });

  it("says a host with no search adapter runs without search, and an image-reading model reads images", () => {
    const caps: LLMCapabilities = { ...ollamaCaps, webSearch: "none", imageInput: "yes" };
    const lines = infoLines(caps, false, "kimi-k3", "gateway.acme.example").map((l) => l.text);
    expect(lines).toContain("Agents work without web search on gateway.acme.example.");
    expect(lines).toContain("Chat reads attached images; PDFs are sent as extracted text.");
  });

  it("says unknown image support is left to the provider", () => {
    const lines = infoLines({ ...ollamaCaps, imageInput: "unknown" }, false, "m", "h").map((l) => l.text);
    expect(lines).toContain("Chat sends attached images; if m does not read them, the provider's error says so. PDFs are sent as extracted text.");
  });
});

describe("checkStatus", () => {
  const check = (over: Partial<LLMCheck>): LLMCheck => ({
    kind: "openai-compatible",
    baseURL: "https://ollama.com/v1",
    model: "glm-5.3",
    modelListed: "yes",
    priced: false,
    capabilities: ollamaCaps,
    ...over,
  });

  it("names the host and model when the model is listed", () => {
    expect(checkStatus(check({}))).toEqual({ severity: "success", text: "Connected to ollama.com · glm-5.3 is available" });
  });

  it("warns, without refusing, on an unlisted model or an endpoint with no listing", () => {
    expect(checkStatus(check({ modelListed: "no", model: "kimi-k3-preview" })).text).toBe(
      "Connected to ollama.com, but kimi-k3-preview is not in its model list. You can still save; runs fail if it is not served.",
    );
    expect(checkStatus(check({ modelListed: "unknown" })).severity).toBe("warning");
  });

  it("says a provider limit proves the key", () => {
    expect(checkStatus(check({ warning: "provider_limit" })).text).toMatch(/^The key works, but ollama.com's usage limit is reached/);
  });
});

describe("connectionView and subscriptionOffered", () => {
  it("reads the saved connection while the draft names it", () => {
    expect(connectionView(connected, draft(), null)?.priced).toBe(true);
  });

  it("knows nothing about an unprobed endpoint, and prefers a test result", () => {
    const moved = draft({ baseURL: "https://ollama.com/v1", kind: "openai-compatible" });
    expect(connectionView(connected, moved, null)).toBeNull();
    const check: LLMCheck = { kind: "openai-compatible", baseURL: "https://ollama.com/v1", model: "glm-5.3", modelListed: "yes", priced: false, capabilities: ollamaCaps };
    expect(connectionView(connected, moved, check)).toMatchObject({ host: "ollama.com", priced: false });
  });

  it("keeps the subscription on a model change at the same endpoint, not on a host change", () => {
    expect(subscriptionOffered(connected, draft({ model: "claude-haiku-4-5" }), null)).toBe(true);
    expect(subscriptionOffered(connected, draft({ baseURL: "https://proxy.acme.example/v1" }), null)).toBe(false);
  });
});

describe("aiSettingsPatch", () => {
  it("sends the whole connection on first connect", () => {
    expect(aiSettingsPatch(unconnected, draft({ apiKey: "sk-ant-api03-0123456789" }, unconnected), false)).toEqual({
      llm: {
        kind: "anthropic",
        baseURL: "https://api.anthropic.com/v1",
        model: "claude-sonnet-5",
        apiKey: "sk-ant-api03-0123456789",
      },
    });
  });

  it("sends nothing until something changes, and only what moved after", () => {
    expect(aiSettingsPatch(connected, draft(), true)).toBeNull();
    expect(aiSettingsPatch(connected, draft({ model: "claude-haiku-4-5" }), true)).toEqual({
      llm: { model: "claude-haiku-4-5" },
    });
  });

  it("switches format and runtime in one save, dropping a subscription the new connection cannot carry", () => {
    const withSub = aiSettingsFrom(
      config({
        agents: {
          ...config().agents,
          subscription: { kind: "claude", keyPrefix: "sk-ant-oat01-", keyLast4: "9f2c", status: "connected", connectedAt: "" },
        },
      }),
    );
    const next = switchFormat({ ...draftFrom(withSub), baseURL: "https://ollama.com/v1", apiKey: "ollama-key-0123456789" }, formats, "openai-compatible");
    expect(aiSettingsPatch(withSub, next, false)).toEqual({
      llm: { kind: "openai-compatible", baseURL: "https://ollama.com/v1", model: "glm-5.3", apiKey: "ollama-key-0123456789" },
      agents: { runtime: "opencode", subscription: null },
    });
  });

  it("sends a runtime change alone on an unconnected org", () => {
    expect(aiSettingsPatch(unconnected, draft({ runtime: "opencode" }, unconnected), false)).toEqual({
      agents: { runtime: "opencode" },
    });
  });
});

describe("draftProblem", () => {
  it("holds back a new host without a key, and names the host", () => {
    expect(draftProblem(connected, draft({ baseURL: "https://ollama.com/v1" }), false)).toEqual({
      field: "apiKey",
      message: "Paste the API key for ollama.com.",
    });
  });

  it("holds back a format no coding agent here runs", () => {
    const noOpenCode = aiSettingsFrom(config({ llmFormats: [formats[0]!, { ...formats[1]!, runtimes: [] }] }));
    const next = draft({ kind: "openai-compatible", baseURL: "https://ollama.com/v1", apiKey: "ollama-key-0123456789" }, noOpenCode);
    expect(draftProblem(noOpenCode, next, false)?.field).toBe("connection");
  });

  it("refuses an API key pasted as the subscription token", () => {
    expect(draftProblem(connected, draft({ token: "sk-ant-api03-x" }), true)?.field).toBe("subscription");
  });
});

describe("testBody", () => {
  it("omits the key unless one was typed", () => {
    expect(testBody(draft())).toEqual({ kind: "anthropic", baseURL: "https://api.anthropic.com/v1", model: "claude-sonnet-5" });
  });
});

describe("refusedField", () => {
  it.each([
    ["llm_key_rejected", "apiKey"],
    ["llm_key_too_short", "apiKey"],
    ["llm_key_required_for_new_host", "apiKey"],
    ["llm_host_refused", "baseURL"],
    ["llm_base_url_invalid", "baseURL"],
    ["llm_unreachable", "connection"],
    ["llm_upstream_error", "connection"],
    ["llm_test_rate_limited", "connection"],
  ])("puts %s on %s", (code, field) => {
    expect(refusedField(["body.llm"], code)).toBe(field);
  });

  it.each([
    ["agents_runtime_requires_anthropic_format", "runtime"],
    ["agents_runtime_unavailable", "runtime"],
    ["agents_subscription_requires_anthropic_host", "subscription"],
    ["agents_subscription_requires_connection", "subscription"],
  ])("puts %s on %s", (code, field) => {
    expect(refusedField(["body.agents"], code)).toBe(field);
  });
});

describe("lastChange", () => {
  it("takes the later of the connection's and the agents section's stamps", () => {
    const c = config({ agents: { ...config().agents, updatedAt: "2026-09-26T08:00:00Z", updatedBy: "alice@acme.test" } });
    expect(lastChange(c)).toEqual({ at: "2026-09-26T08:00:00Z", by: "alice@acme.test" });
    expect(lastChange(config())).toEqual({ at: "2026-09-25T13:53:00Z", by: "dev@acme.example" });
    expect(lastChange(config({ llm: null }))).toEqual({ at: null, by: null });
  });
});

describe("modelReads", () => {
  it("reads the attach fields off the connection, and nothing without one", () => {
    expect(modelReads(config())).toEqual({ model: "claude-sonnet-5", imageInput: "yes", nativePdf: true });
    expect(modelReads(config({ llm: null }))).toBeNull();
  });
});
