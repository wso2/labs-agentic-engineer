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

// The org's model connection as this harness reads it, and as each of its two
// model consumers must be handed it: the agent under test (its `MODEL_*`
// variables) and the judge (a promptfoo provider plus the variables that
// provider reads its credential from). Pure, so every mapping is a table test.
//
// The key never travels in anything this module returns for a FILE — the
// judge's provider entry lands in the emitted promptfoo config, under the
// build's output directory. It travels in the environment maps only.

export type ModelFormat = "anthropic" | "openai-compatible";
export type AuthScheme = "x-api-key" | "bearer";

export interface ModelConnection {
  format: ModelFormat;
  /** The API root the AI SDKs take, ending in its version segment (`…/v1`). */
  baseURL: string;
  model: string;
  authScheme: AuthScheme;
  key: string;
}

/**
 * Where a developer's `ANTHROPIC_API_KEY` goes, and what an evaluation key
 * that came without its connection is taken to be: Anthropic's own API.
 */
const FIRST_PARTY: Omit<ModelConnection, "key"> = {
  format: "anthropic",
  baseURL: "https://api.anthropic.com/v1",
  model: "claude-sonnet-5-5",
  authScheme: "x-api-key",
};

const FORMATS: readonly ModelFormat[] = ["anthropic", "openai-compatible"];
const AUTH_SCHEMES: readonly AuthScheme[] = ["x-api-key", "bearer"];

function oneOf<T extends string>(name: string, value: string | undefined, allowed: readonly T[], fallback: T): T {
  if (value === undefined || value === "") return fallback;
  if ((allowed as readonly string[]).includes(value)) return value as T;
  throw new Error(`agent-eval: ${name}=${value} is not one of ${allowed.join(", ")}`);
}

/**
 * The connection this run evaluates on, or `undefined` when it has none.
 *
 * `AEP_EVAL_MODEL_*` first; outside a managed pod, `ANTHROPIC_API_KEY` on
 * Anthropic's API only; never the OAuth token (design/running-in-a-build-pod.md).
 *
 * `||`, not `??`: ESO can materialise an EMPTY secret, and an empty key is no
 * key rather than a key that fails to authenticate. The difference decides
 * whether the report says the agent never became ready or the judge is pointed
 * at an endpoint with a blank credential.
 *
 * Throws for a format or auth scheme it does not know: evaluating on a guess
 * would send the key somewhere in a shape nobody chose.
 */
export function resolveConnection(env: NodeJS.ProcessEnv): ModelConnection | undefined {
  const evaluationKey = env.AEP_EVAL_MODEL_API_KEY || undefined;
  if (evaluationKey !== undefined) {
    return {
      format: oneOf("AEP_EVAL_MODEL_FORMAT", env.AEP_EVAL_MODEL_FORMAT, FORMATS, FIRST_PARTY.format),
      baseURL: env.AEP_EVAL_MODEL_BASE_URL || FIRST_PARTY.baseURL,
      model: env.AEP_EVAL_MODEL_NAME || FIRST_PARTY.model,
      authScheme: oneOf("AEP_EVAL_MODEL_AUTH_SCHEME", env.AEP_EVAL_MODEL_AUTH_SCHEME, AUTH_SCHEMES, FIRST_PARTY.authScheme),
      key: evaluationKey,
    };
  }
  if (env.AEP_EVAL_KEY_MANAGED) return undefined;
  const developerKey = env.ANTHROPIC_API_KEY || undefined;
  return developerKey === undefined ? undefined : { ...FIRST_PARTY, key: developerKey };
}

/**
 * The agent under test's model variables — the same set the platform injects
 * into a deployed ai-agent, so the agent is evaluated on the code path it runs
 * in production. `MODEL_API_KEY_HEADER` is absent: it names the governed
 * proxy's header, and there is no proxy between this run and the connection.
 */
export function agentModelEnv(conn: ModelConnection): Record<string, string> {
  return {
    MODEL_API_KEY: conn.key,
    MODEL_ENDPOINT: conn.baseURL,
    MODEL_NAME: conn.model,
    MODEL_API_FORMAT: conn.format,
    MODEL_API_AUTH_SCHEME: conn.authScheme,
  };
}

/** A promptfoo provider entry: JSON, written to disk — never a credential. */
export interface JudgeProvider {
  id: string;
  config: Record<string, unknown>;
}

/**
 * The judge on the connection, as promptfoo's own provider for the format.
 *
 * - `anthropic` → `anthropic:messages:<model>`. That provider is built on
 *   `@anthropic-ai/sdk`, which posts to `<root>/v1/messages`, so its base is
 *   the connection's URL minus the trailing `/v1` the AI SDK takes (the same
 *   rule as the runner's `claudeCodeBaseURL`).
 * - `openai-compatible` → `openai:chat:<model>`, the Chat Completions
 *   provider, which takes the `/v1` base as it is.
 *
 * Bearer on the Anthropic format: promptfoo's provider presents a key only as
 * `x-api-key`, so the credential goes out as an `Authorization` header from
 * `judgeEnv` (the environment), and this entry removes `x-api-key` from each
 * request — the key is not sent twice, and not under a header this host did
 * not ask for.
 *
 * Throws for an Anthropic-format model whose name holds a `:`
 * (`gpt-oss:20b`): promptfoo's `anthropic:` factory takes the model as the
 * third `:`-separated field and drops the rest, and has no config field that
 * names the model instead. Grading on a truncated name would score against a
 * model nobody chose; `AGENT_EVAL_GRADER` can name another judge.
 */
export function judgeProvider(conn: Omit<ModelConnection, "key">): JudgeProvider {
  if (conn.format === "openai-compatible") {
    return { id: `openai:chat:${conn.model}`, config: { apiBaseUrl: conn.baseURL } };
  }
  if (conn.model.includes(":")) {
    throw new Error(
      `agent-eval: the judge cannot run on the Anthropic-format model "${conn.model}" — ` +
        "promptfoo's anthropic provider reads a model name up to its first ':'. " +
        "Set AGENT_EVAL_GRADER to name a judge.",
    );
  }
  return {
    id: `anthropic:messages:${conn.model}`,
    config: {
      apiBaseUrl: anthropicSDKRoot(conn.baseURL),
      ...(conn.authScheme === "bearer" ? { headers: { "x-api-key": null } } : {}),
    },
  };
}

/** The judge when the run has no connection: the first-party default. */
export function defaultJudgeProvider(): JudgeProvider {
  return judgeProvider(FIRST_PARTY);
}

/**
 * The variables the judge's provider reads its credential from. Only the
 * format's own name is set: a key is never handed to a provider family that
 * would present it somewhere else.
 */
export function judgeEnv(conn: ModelConnection): Record<string, string> {
  if (conn.format === "openai-compatible") return { OPENAI_API_KEY: conn.key };
  return {
    // Set on the bearer path too: promptfoo refuses to start an anthropic
    // provider without it (the header itself is removed per request).
    ANTHROPIC_API_KEY: conn.key,
    // `@anthropic-ai/sdk` reads this at client construction and adds each
    // line as a default header.
    ...(conn.authScheme === "bearer" ? { ANTHROPIC_CUSTOM_HEADERS: `Authorization: Bearer ${conn.key}` } : {}),
  };
}

function anthropicSDKRoot(baseURL: string): string {
  return baseURL.replace(/\/+$/, "").replace(/\/v1$/, "");
}
