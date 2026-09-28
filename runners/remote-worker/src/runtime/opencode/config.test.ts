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

import { test } from "node:test";
import assert from "node:assert/strict";
import { readModelConnection, type ModelConnection } from "../../lib/model_connection.js";
import { DENIED_CAPABILITIES } from "../port.js";
import { allowlist, buildOpencodeConfig, providerConfig, type OpencodeConfigInput } from "./config.js";
import { deniedTools, opencodeModel, platformModel, providerId } from "./tools.js";

/** Anthropic's own API on the org's model. */
function firstParty(model = "claude-sonnet-5"): ModelConnection {
  return readModelConnection(model, {});
}

/** Ollama's OpenAI-compatible endpoint, as a dispatch stamps it. */
function ollama(over: Record<string, string> = {}): ModelConnection {
  return readModelConnection("claude-sonnet-5", {
    AEP_AGENT_MODEL: "gpt-oss:20b",
    AEP_MODEL_FORMAT: "openai-compatible",
    AEP_MODEL_BASE_URL: "https://ollama.com/v1",
    AEP_MODEL_AUTH_SCHEME: "bearer",
    AEP_MODEL_CONTEXT_WINDOW: "131072",
    AEP_MODEL_OUTPUT_LIMIT: "32768",
    AEP_MODEL_WEB_SEARCH: "ollama-api",
    ...over,
  });
}

/** The config as the tests read it — the builder's return is deliberately loose. */
interface ConfigView {
  [key: string]: unknown;
  agent: Record<string, { mode?: string; model?: string; permission?: unknown; disable?: boolean }>;
  permission: Record<string, unknown>;
}

function config(over: Partial<OpencodeConfigInput> = {}): ConfigView {
  return buildOpencodeConfig(input(over)) as ConfigView;
}

function input(over: Partial<OpencodeConfigInput> = {}): OpencodeConfigInput {
  return {
    connection: firstParty(),
    instructionsPath: "/tmp/run/instructions.md",
    pluginDir: "/app/runtime/opencode/aep-guard",
    skillAllow: ["aep", "go"],
    deniedCapabilities: DENIED_CAPABILITIES,
    debug: false,
    ...over,
  };
}

// The silent failure spike S1 found: `{allowed: allow, "*": deny}` hides the
// WHOLE tool, because the last matching rule decides and a trailing `*` deny is
// "disabled". Key order is the policy, so it is asserted as order.
test("allowlist: `*: deny` comes FIRST, then each allowed name", () => {
  assert.deepEqual(Object.entries(allowlist(["a", "b"])), [
    ["*", "deny"],
    ["a", "allow"],
    ["b", "allow"],
  ]);
  assert.deepEqual(Object.entries(allowlist([])), [["*", "deny"]]);
});

test("buildOpencodeConfig: the config, policy clause by clause", () => {
  const c = config();
  assert.equal(c.model, "anthropic/claude-sonnet-5");
  // The helper calls (titles, summaries) bill to the org's one model — never
  // OpenCode's own pick, which the org's key may not reach.
  assert.equal(c.small_model, "anthropic/claude-sonnet-5");
  assert.equal(c.default_agent, "aep");
  assert.equal(c.subagent_depth, 3);
  assert.equal(c.autoupdate, false);
  assert.equal(c.share, "disabled");
  assert.equal(c.snapshot, false);
  assert.deepEqual(c.instructions, ["/tmp/run/instructions.md"]);
  // A DIRECTORY: a bare file spec is skipped without a word (S1, S1b).
  assert.deepEqual(c.plugin, ["/app/runtime/opencode/aep-guard"]);
  assert.deepEqual(c.tools, { lsp: false });
  // The key is a reference the server resolves from its own env — never a value.
  assert.deepEqual(c.provider, { anthropic: { options: { apiKey: "{env:AEP_MODEL_API_KEY}" } } });
  assert.equal(c.enabled_providers, undefined);

  assert.deepEqual(c.agent.aep, { mode: "primary", description: "AEP coding run lead" });
  assert.equal(c.agent.general.model, "anthropic/claude-sonnet-5");
  assert.equal(c.agent.general.mode, "subagent");
  // The lead owns the plan; a builder may fan out (the walk inside a build).
  assert.deepEqual(c.agent.general.permission, { todowrite: "deny", task: "allow" });
  const subagents = Object.keys(c.agent).filter((name) => c.agent[name].mode === "subagent");
  assert.deepEqual(subagents, ["general"], "one subagent, on the one model");
  for (const builtin of ["build", "plan", "explore", "scout"]) assert.deepEqual(c.agent[builtin], { disable: true });

  const p = c.permission as Record<string, Record<string, string> | string>;
  assert.deepEqual(Object.entries(p.task), [["*", "deny"], ["general", "allow"]]);
  assert.deepEqual(Object.entries(p.skill), [["*", "deny"], ["aep", "allow"], ["go", "allow"]]);
  assert.equal(p.question, "deny");
  // Reads (and bash paths) outside the project stay open, as on Claude Code: the
  // one permission gates every tool, so the guard plugin alone decides writes.
  assert.equal(p.external_directory, "allow");
  assert.equal(p.doom_loop, "allow");
  assert.equal(p.lsp, "deny");
  for (const tool of ["read", "glob", "grep", "list", "edit", "bash", "todowrite", "webfetch", "websearch"]) {
    assert.equal(p[tool], "allow", `${tool} is not allowed`);
  }
  // Nothing is ever `ask`: server mode has no one to ask.
  assert.ok(!JSON.stringify(p).includes('"ask"'));
  assert.equal(c.mcp, undefined, "no MCP server without the policy's clause");
  assert.equal(c.logLevel, undefined, "a normal run logs at the server's default");
});

test("buildOpencodeConfig: the org's model binds model, small_model and the subagent alike", () => {
  const c = config({ connection: firstParty("claude-haiku-4-5") });
  assert.equal(c.model, "anthropic/claude-haiku-4-5");
  assert.equal(c.small_model, "anthropic/claude-haiku-4-5");
  assert.equal(c.agent.general.model, "anthropic/claude-haiku-4-5");
});

test("buildOpencodeConfig: MCP rides the loopback proxy with a static header, and no OAuth discovery", () => {
  const c = config({ mcpUrl: "http://127.0.0.1:4321/" });
  assert.deepEqual(c.mcp, {
    aep: { type: "remote", url: "http://127.0.0.1:4321/", headers: { Authorization: "Bearer loopback" }, oauth: false },
  });
});

test("buildOpencodeConfig: debug raises the server's log level, and nothing else", () => {
  const plain = buildOpencodeConfig(input());
  const debug = buildOpencodeConfig(input({ debug: true }));
  assert.equal(debug.logLevel, "DEBUG");
  const rest = { ...debug };
  delete rest.logLevel;
  assert.deepEqual(rest, plain);
});

test("buildOpencodeConfig: no credential and no experimental flag anywhere in the object", () => {
  const text = JSON.stringify(buildOpencodeConfig(input({ mcpUrl: "http://127.0.0.1:1/" })));
  assert.doesNotMatch(text, /sk-ant/);
  assert.doesNotMatch(text, /EXPERIMENTAL|background/i);
});

test("deniedTools: the capability classes map to OpenCode's one tool in them", () => {
  assert.deepEqual(deniedTools(DENIED_CAPABILITIES), ["question"]);
  assert.deepEqual(deniedTools(["scheduling", "peer_messaging"]), []);
});

test("model spelling: platform id ↔ <provider>/ id, and anything else reported as is", () => {
  assert.equal(opencodeModel("anthropic", "claude-sonnet-5"), "anthropic/claude-sonnet-5");
  assert.equal(opencodeModel("aep", "gpt-oss:20b"), "aep/gpt-oss:20b");
  assert.equal(platformModel("anthropic/claude-haiku-4-5"), "claude-haiku-4-5");
  assert.equal(platformModel("aep/gpt-oss:20b"), "gpt-oss:20b");
  assert.equal(platformModel("claude-haiku-4-5"), "claude-haiku-4-5");
  // An unmapped id is left loud: it blanks the cycle's cost, which is the right failure.
  assert.equal(platformModel("openai/gpt-9"), "openai/gpt-9");
});

// SNAPSHOT — first party: the catalog provider, the key by reference, NO limits
// (models.dev supplies Claude's, from the image's pre-warmed cache).
test("providerConfig: on Anthropic's API, provider anthropic with the key and nothing else", () => {
  assert.equal(providerId(firstParty()), "anthropic");
  assert.deepEqual(providerConfig(firstParty()), { anthropic: { options: { apiKey: "{env:AEP_MODEL_API_KEY}" } } });
});

// SNAPSHOT — elsewhere: provider aep, which no catalog entry matches, so the
// connection's limits are the only ones; a model with none is never compacted.
test("providerConfig: on Ollama's OpenAI-compatible endpoint, provider aep with the connection's limits", () => {
  assert.equal(providerId(ollama()), "aep");
  assert.deepEqual(providerConfig(ollama()), {
    aep: {
      npm: "@ai-sdk/openai-compatible",
      name: "AEP model connection (ollama.com)",
      options: { baseURL: "https://ollama.com/v1", apiKey: "{env:AEP_MODEL_API_KEY}", includeUsage: true },
      models: { "gpt-oss:20b": { name: "gpt-oss:20b", limit: { context: 131072, output: 32768 } } },
    },
  });
});

// Run live once on Ollama's Anthropic endpoint.
test("providerConfig: an Anthropic-format host other than Anthropic's API takes a Bearer key as authToken", () => {
  const conn = ollama({ AEP_MODEL_FORMAT: "anthropic", AEP_MODEL_BASE_URL: "https://ollama.com/v1" });
  assert.deepEqual(providerConfig(conn), {
    aep: {
      npm: "@ai-sdk/anthropic",
      name: "AEP model connection (ollama.com)",
      // With its /v1: the SDK appends `/messages` (a root URL answers 405 on Ollama).
      options: { baseURL: "https://ollama.com/v1", authToken: "{env:AEP_MODEL_API_KEY}" },
      models: { "gpt-oss:20b": { name: "gpt-oss:20b", limit: { context: 131072, output: 32768 } } },
    },
  });
});

test("providerConfig: limits the connection does not state fall back, so compaction stays on", () => {
  const conn = readModelConnection("m", { AEP_MODEL_FORMAT: "openai-compatible", AEP_MODEL_BASE_URL: "https://llm.example.com/v1" });
  const models = (providerConfig(conn).aep as { models: Record<string, { limit: unknown }> }).models;
  assert.deepEqual(models.m?.limit, { context: 128000, output: 32000 });
});

test("buildOpencodeConfig: off Anthropic's API only the connection's provider loads, on its model", () => {
  const c = config({ connection: ollama() });
  assert.equal(c.model, "aep/gpt-oss:20b");
  assert.equal(c.small_model, "aep/gpt-oss:20b");
  assert.equal(c.agent.general.model, "aep/gpt-oss:20b");
  assert.deepEqual(c.enabled_providers, ["aep"]);
  // OpenCode's own search is off; the platform's aep-web is the search here.
  assert.equal(c.permission.websearch, "deny");
});

test("buildOpencodeConfig: the aep-web server is a local MCP server beside the platform's", () => {
  const c = config({
    connection: ollama(),
    mcpUrl: "http://127.0.0.1:4321/",
    webSearchServer: { name: "aep-web", command: "/usr/local/bin/node", args: ["/opt/aep/web-search/aep-web.mjs", "/tmp/aep-web-x/config.json"], tool: "web_search" },
  });
  assert.deepEqual(c.mcp, {
    aep: { type: "remote", url: "http://127.0.0.1:4321/", headers: { Authorization: "Bearer loopback" }, oauth: false },
    "aep-web": {
      type: "local",
      command: ["/usr/local/bin/node", "/opt/aep/web-search/aep-web.mjs", "/tmp/aep-web-x/config.json"],
      enabled: true,
    },
  });
});
