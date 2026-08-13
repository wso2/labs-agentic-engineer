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

// D9 secure search (docs/design/... plan lines 169-229) — server-side
// WebSearch DLP gate for the coding-agent runner.
//
// SPIKE FINDING (Task 12 — see .superpowers/sdd/task-12-report.md for full
// evidence): the installed @anthropic-ai/claude-agent-sdk (0.2.141)
// NEVER routes the server-executed WebSearch tool through the `canUseTool`
// permission callback. WebSearch is fulfilled server-side by the model API
// itself (it shows up as `usage.server_tool_use.web_search_requests`) —
// there is no local dispatch step for canUseTool to intercept before the
// search runs. This was confirmed empirically with a live probe under BOTH
// `permissionMode: "default"` and this runner's actual
// `permissionMode: "bypassPermissions"` + `allowDangerouslySkipPermissions:
// true` — canUseTool was invoked zero times and the search executed for
// real in every case.
//
// The `PreToolUse` hook DOES intercept WebSearch before dispatch, with the
// query present at `tool_input.query`, and a `deny` permissionDecision
// genuinely prevents the search (verified: zero
// `server_tool_use.web_search_requests`, no search-result content in the
// transcript, an `is_error` tool_result returned to the model, and the
// denial recorded in the SDK's own `result.permission_denials`) — under
// bypassPermissions too. So this module wires a PreToolUse hook, not
// canUseTool — a genuine pre-execution control, not a post-hoc scan.

import type { HookCallback, PreToolUseHookInput } from "@anthropic-ai/claude-agent-sdk";

// MIN_SECRET_VALUE_LEN mirrors progress/scrubber.ts's MIN_LITERAL_LEN: long
// enough that everyday short env values (port numbers, "true", short slugs)
// don't cause noisy false-positive denials, short enough to still catch
// realistic API keys/tokens/secrets.
const MIN_SECRET_VALUE_LEN = 8;

// SAFE_ENV_KEYS is the explicit allowlist of env var NAMES the runner
// itself sets, or that the base container/K8s runtime always sets, which
// are known NOT to be secret values. Every OTHER env var is deny-by-default
// treated as a candidate secret — deliberately, because external-dependency
// secrets (Tasks 9-11; D9 threat model item 1) arrive via K8s `envFrom:
// secretRef` under names DERIVED FROM THE USER'S OWN dependency + output
// name (see services/aep-api/.../resources/naming.go: EnvVarName, e.g.
// "orders-db"+"host" -> "ORDERS_DB_HOST"). That naming is fully dynamic —
// there is no fixed prefix or pattern to allowlist FOR secrets — so the
// only safe posture is the reverse: allowlist the small, fixed set of
// names that are never secret, and treat everything else (including
// ANTHROPIC_API_KEY, CLAUDE_CODE_OAUTH_TOKEN, AEP_BEARER, AEP_MCP_TOKEN,
// PUBLISHER_CLIENT_SECRET, and any per-dependency key like
// OPENWEATHER_API_KEY) as a candidate.
const SAFE_ENV_KEYS: ReadonlySet<string> = new Set<string>([
  // Runner/K8s plumbing — see runner.ts childEnv and
  // oneshot.ts readDispatchFromEnv / coding_agent_component_type.go env stamping.
  "AEP_TASK_ID",
  "AEP_ORG_ID",
  "AEP_PROJECT_ID",
  "AEP_COMPONENT_NAME",
  "AEP_REPO_URL",
  "AEP_PROMPT",
  "AEP_GIT_SERVICE_URL",
  "AEP_PLATFORM_URL",
  "AEP_MCP_URL",
  "AEP_IDENTITY_NAME",
  "AEP_IDENTITY_EMAIL",
  "AEP_IDENTITY_LOGIN",
  "AEP_CORRELATION_ID",
  "AEP_TASK_KIND",
  "AEP_SKILLS_REPO_URL",
  "AEP_BEARER_FILE",
  "PUBLISHER_TOKEN_URL",
  "PUBLISHER_CLIENT_ID",
  "WORKSPACE_BASE_PATH",
  "GH_CONFIG_DIR",
  // Base OS / container / Node runtime vars.
  "PATH",
  "HOME",
  "PWD",
  "HOSTNAME",
  "LANG",
  "LC_ALL",
  "TERM",
  "SHLVL",
  "USER",
  "SHELL",
  "NODE_ENV",
  "NODE_VERSION",
  "YARN_VERSION",
  "TZ",
]);

/**
 * stagedSecretValues extracts candidate secret values from an env record.
 * Call it with the SAME env object runner.ts injects into the SDK-spawned
 * run (`childEnv`) — that is the "injection mechanism that provides
 * [secret values] to the run" the D9 design refers to, so this reuses it
 * rather than inventing a second channel. Every value not explicitly
 * allowlisted by name and at least MIN_SECRET_VALUE_LEN characters long is
 * treated as a candidate secret for the substring check in
 * checkWebSearchQuery. This deliberately over-includes — a benign
 * long-ish env var not on the allowlist just costs one denied search that
 * the agent can retry without the value; failing closed is the safe
 * direction for a DLP gate.
 */
export function stagedSecretValues(env: Readonly<Record<string, string | undefined>>): string[] {
  const values: string[] = [];
  for (const [key, value] of Object.entries(env)) {
    if (!value) continue;
    if (SAFE_ENV_KEYS.has(key)) continue;
    if (value.length < MIN_SECRET_VALUE_LEN) continue;
    values.push(value);
  }
  return values;
}

export interface WebSearchQueryCheck {
  denied: boolean;
  message?: string;
}

export const WEBSEARCH_DENIAL_MESSAGE =
  "WebSearch query blocked: it contains a staged secret value. Retry without values — " +
  "name the SDK, API, or technology only; never include the credential or config value itself.";

/**
 * checkWebSearchQuery is the pure DLP predicate: deny iff `query` contains
 * any of `secrets` as a substring. Exported standalone (no SDK types
 * involved) so it's unit-testable without constructing the hook plumbing
 * around it.
 */
export function checkWebSearchQuery(query: string, secrets: readonly string[]): WebSearchQueryCheck {
  for (const secret of secrets) {
    if (secret && query.includes(secret)) {
      return { denied: true, message: WEBSEARCH_DENIAL_MESSAGE };
    }
  }
  return { denied: false };
}

function isPreToolUseInput(input: unknown): input is PreToolUseHookInput {
  return (
    typeof input === "object" &&
    input !== null &&
    (input as { hook_event_name?: unknown }).hook_event_name === "PreToolUse"
  );
}

/**
 * createWebSearchDlpHook builds the PreToolUse HookCallback that gates
 * WebSearch — NOT a canUseTool callback (see the module doc comment for
 * why). Register it under `hooks.PreToolUse` with `matcher: "WebSearch"`
 * in the SDK query options. `secrets` is captured once per run; runner.ts
 * passes `stagedSecretValues(childEnv)` computed from the same env that's
 * injected into the run.
 */
export function createWebSearchDlpHook(secrets: readonly string[]): HookCallback {
  return async (input) => {
    if (!isPreToolUseInput(input) || input.tool_name !== "WebSearch") {
      return {};
    }
    const toolInput = input.tool_input as { query?: unknown } | undefined;
    const searchQuery = typeof toolInput?.query === "string" ? toolInput.query : "";
    const check = checkWebSearchQuery(searchQuery, secrets);
    if (!check.denied) {
      return {};
    }
    return {
      hookSpecificOutput: {
        hookEventName: "PreToolUse",
        permissionDecision: "deny",
        permissionDecisionReason: check.message,
      },
    };
  };
}
