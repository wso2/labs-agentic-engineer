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
import type { PreToolUseHookInput } from "@anthropic-ai/claude-agent-sdk";
import {
  checkWebSearchQuery,
  createWebSearchDlpHook,
  webSearchDenial,
  stagedSecretValues,
  WEBSEARCH_DENIAL_MESSAGE,
} from "./websearch_dlp.js";

// ---- checkWebSearchQuery (pure predicate) ----

test("checkWebSearchQuery: denies a query containing a staged secret value", () => {
  const result = checkWebSearchQuery(
    "how does openweathermap-api-9f8a3b2c1d work",
    ["openweathermap-api-9f8a3b2c1d"],
  );
  assert.equal(result.denied, true);
  assert.equal(result.message, WEBSEARCH_DENIAL_MESSAGE);
});

test("checkWebSearchQuery: allows a clean query with no secret substrings", () => {
  const result = checkWebSearchQuery(
    "OpenWeatherMap current weather API docs",
    ["openweathermap-api-9f8a3b2c1d", "sk-ant-some-other-secret-value"],
  );
  assert.equal(result.denied, false);
  assert.equal(result.message, undefined);
});

test("checkWebSearchQuery: denies when ANY of multiple secrets matches", () => {
  const result = checkWebSearchQuery(
    "docs for sk-ant-some-other-secret-value integration",
    ["openweathermap-api-9f8a3b2c1d", "sk-ant-some-other-secret-value"],
  );
  assert.equal(result.denied, true);
});

test("checkWebSearchQuery: empty secrets list never denies", () => {
  const result = checkWebSearchQuery("anything at all", []);
  assert.equal(result.denied, false);
});

test("checkWebSearchQuery: ignores empty-string secret entries", () => {
  const result = checkWebSearchQuery("some query", ["", "short"]);
  assert.equal(result.denied, false);
});

// ---- stagedSecretValues (env -> candidate secrets) ----

test("stagedSecretValues: includes a dependency secret env value (arbitrary name via envFrom)", () => {
  const values = stagedSecretValues({ OPENWEATHER_API_KEY: "sk-live-abcdef123456" });
  assert.deepEqual(values, ["sk-live-abcdef123456"]);
});

test("stagedSecretValues: includes ANTHROPIC_API_KEY, AEP_BEARER, AEP_MCP_TOKEN, publisher secret", () => {
  const values = stagedSecretValues({
    ANTHROPIC_API_KEY: "sk-ant-abcdef123456",
    AEP_BEARER: "bearer-token-abcdef123456",
    AEP_MCP_TOKEN: "mcp-token-abcdef123456",
    PUBLISHER_CLIENT_SECRET: "publisher-secret-abcdef123456",
  });
  assert.equal(values.length, 4);
  assert.ok(values.includes("sk-ant-abcdef123456"));
  assert.ok(values.includes("bearer-token-abcdef123456"));
  assert.ok(values.includes("mcp-token-abcdef123456"));
  assert.ok(values.includes("publisher-secret-abcdef123456"));
});

test("stagedSecretValues: excludes allowlisted structural env vars (AEP_* plumbing, PATH, HOME, ...)", () => {
  const values = stagedSecretValues({
    AEP_TASK_ID: "11111111-1111-1111-1111-111111111111",
    AEP_REPO_URL: "https://github.com/example/example-repo",
    AEP_GIT_SERVICE_URL: "https://git-service.internal.example.com",
    AEP_PLATFORM_URL: "https://platform.internal.example.com",
    PATH: "/usr/local/bin:/usr/bin:/bin",
    HOME: "/home/aep",
  });
  assert.deepEqual(values, []);
});

test("stagedSecretValues: excludes short values below the minimum length", () => {
  const values = stagedSecretValues({ SOME_FLAG: "true", PORT: "3000" });
  assert.deepEqual(values, []);
});

test("stagedSecretValues: excludes empty and undefined values", () => {
  const values = stagedSecretValues({ EMPTY: "", MISSING: undefined });
  assert.deepEqual(values, []);
});

// ---- createWebSearchDlpHook (PreToolUse HookCallback) ----
//
// NOT canUseTool — see websearch_dlp.ts and task-12-report.md: the spike
// found canUseTool is never invoked for the server-executed WebSearch
// tool, but PreToolUse fires pre-dispatch with the query in
// tool_input.query, and a deny permissionDecision genuinely prevents
// execution. These tests exercise that exact SDK-facing shape.

// createWebSearchDlpHook returns HookJSONOutput, a union of Async/Sync
// shapes; only the sync shape carries hookSpecificOutput. Our hook never
// returns the async shape, so this narrows for test assertions only.
interface SyncOutput {
  hookSpecificOutput?: {
    hookEventName?: string;
    permissionDecision?: string;
    permissionDecisionReason?: string;
  };
}

function preToolUseInput(query: string): PreToolUseHookInput {
  return {
    session_id: "s1",
    transcript_path: "/tmp/transcript.jsonl",
    cwd: "/workspace",
    hook_event_name: "PreToolUse",
    tool_name: "WebSearch",
    tool_input: { query },
    tool_use_id: "tool-use-1",
  };
}

test("createWebSearchDlpHook: denies a WebSearch call whose query contains a staged secret value", async () => {
  const hook = createWebSearchDlpHook(webSearchDenial(["staged-secret-value-123456"]));
  const output = (await hook(
    preToolUseInput("how do I use staged-secret-value-123456 with this SDK"),
    "tool-use-1",
    { signal: new AbortController().signal },
  )) as SyncOutput;

  assert.equal(output.hookSpecificOutput?.hookEventName, "PreToolUse");
  assert.equal(output.hookSpecificOutput?.permissionDecision, "deny");
  assert.equal(output.hookSpecificOutput?.permissionDecisionReason, WEBSEARCH_DENIAL_MESSAGE);
});

test("createWebSearchDlpHook: allows a clean WebSearch query (no permissionDecision set)", async () => {
  const hook = createWebSearchDlpHook(webSearchDenial(["staged-secret-value-123456"]));
  const output = (await hook(
    preToolUseInput("Stripe API idempotency keys official docs"),
    "tool-use-1",
    { signal: new AbortController().signal },
  )) as SyncOutput;

  assert.equal(output.hookSpecificOutput, undefined);
});

test("createWebSearchDlpHook: denial message instructs retrying without values", async () => {
  const hook = createWebSearchDlpHook(webSearchDenial(["staged-secret-value-123456"]));
  const output = (await hook(preToolUseInput("staged-secret-value-123456"), "tool-use-1", {
    signal: new AbortController().signal,
  })) as SyncOutput;

  const reason = output.hookSpecificOutput?.permissionDecisionReason;
  assert.ok(reason);
  assert.match(reason ?? "", /retry without values/i);
});

test("createWebSearchDlpHook: ignores non-PreToolUse hook events", async () => {
  const hook = createWebSearchDlpHook(webSearchDenial(["staged-secret-value-123456"]));
  const output = await hook(
    { session_id: "s1", transcript_path: "/tmp/t.jsonl", cwd: "/workspace", hook_event_name: "PostToolUse" } as never,
    "tool-use-1",
    { signal: new AbortController().signal },
  );
  assert.deepEqual(output, {});
});

test("createWebSearchDlpHook: ignores PreToolUse calls for tools other than WebSearch", async () => {
  const hook = createWebSearchDlpHook(webSearchDenial(["staged-secret-value-123456"]));
  const input: PreToolUseHookInput = {
    session_id: "s1",
    transcript_path: "/tmp/transcript.jsonl",
    cwd: "/workspace",
    hook_event_name: "PreToolUse",
    tool_name: "Bash",
    tool_input: { command: "echo staged-secret-value-123456" },
    tool_use_id: "tool-use-2",
  };
  const output = await hook(input, "tool-use-2", { signal: new AbortController().signal });
  assert.deepEqual(output, {});
});
