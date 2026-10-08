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
 * One Agent SDK session that ends in ONE structured answer — the shape the
 * planner, the walker and the judge all share, so the three differ only in
 * prompt, tools and schema.
 *
 * Two things every session here must get right, and gets here once:
 *
 *   1. The prompt is a HELD-OPEN stream, never a string. A string prompt makes
 *      the SDK close the CLI's stdin at the first `result`, and stdin is the
 *      channel every SDK-side hook answers on — from that moment each hooked
 *      tool call reports as cancelled, which the agent reads as a denial.
 *      The walker's guard is such a hook.
 *   2. Isolation: `settingSources: []` loads no user or project settings, so a
 *      developer's own `apiKeyHelper`, hooks or CLAUDE.md cannot reach a
 *      harness agent and change what it does or what it bills.
 */

import { createWriteStream, type WriteStream } from "node:fs";
import { query, type HookCallbackMatcher, type HookEvent, type SDKMessage, type SDKUserMessage } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
import { assertNotApiKey, CredentialError } from "./credentials.js";

export interface SessionRequest<T> {
  prompt: string;
  systemPrompt: string;
  cwd: string;
  model: string;
  /** The built-in tools that EXIST for this session; `[]` for none. */
  tools: readonly string[];
  hooks?: Partial<Record<HookEvent, HookCallbackMatcher[]>>;
  /** The answer's shape. Its JSON Schema is what the SDK enforces; zod re-checks it here. */
  schema: z.ZodType<T>;
  maxTurns: number;
  timeoutMs: number;
  env: NodeJS.ProcessEnv;
  /** Every SDK message, one JSON line each — the record a later root-cause stage reads. */
  transcriptFile?: string;
  /** The CLI's own debug log. */
  debugFile?: string;
  /** Aborted by the sweep's SIGINT: the session is closed at once. */
  signal?: AbortSignal;
  /**
   * Sees every message as it arrives; a returned reason stops the session the
   * way the deadline does, and lands in `halted`. The walker's browser
   * watchdog is one.
   */
  watch?: (message: SDKMessage) => string | undefined;
}

export interface SessionResult<T> {
  /** The validated answer; absent when the session produced none or it failed the schema. */
  output?: T;
  /** Why there is no `output`, in one line. */
  error?: string;
  /** True when the credential check refused the session — a harness error, never the app's. */
  credentialRefused: boolean;
  timedOut: boolean;
  /** Why `watch` stopped the session, when it did. */
  halted?: string;
  apiKeySource?: string;
  /** Claude Code's own estimate (`total_cost_usd` on the last `result`); null when none arrived. */
  costUsd: number | null;
  /** Summed over `modelUsage`, which unlike `usage` covers every call the session made. */
  tokens: { input: number; output: number; cacheRead: number; cacheCreation: number };
  numTurns: number;
  durationMs: number;
}

/** The prompt as a stream the SDK cannot close on its own; `release` ends it. */
export function openPromptStream(prompt: string): { stream: AsyncIterable<SDKUserMessage>; release: () => void } {
  let release!: () => void;
  const released = new Promise<void>((resolve) => {
    release = resolve;
  });
  async function* stream(): AsyncGenerator<SDKUserMessage, void, undefined> {
    yield { type: "user", message: { role: "user", content: prompt }, parent_tool_use_id: null, session_id: "" };
    await released;
  }
  return { stream: stream(), release };
}

/** How long an interrupted session gets to answer with its `result` before it is closed outright. */
const INTERRUPT_GRACE_MS = 15_000;

export async function runSession<T>(req: SessionRequest<T>): Promise<SessionResult<T>> {
  const started = Date.now();
  const input = openPromptStream(req.prompt);
  const transcript: WriteStream | undefined = req.transcriptFile
    ? createWriteStream(req.transcriptFile, { flags: "a" })
    : undefined;
  const result: SessionResult<T> = {
    credentialRefused: false,
    timedOut: false,
    costUsd: null,
    tokens: { input: 0, output: 0, cacheRead: 0, cacheCreation: 0 },
    numTurns: 0,
    durationMs: 0,
  };

  const q = query({
    prompt: input.stream,
    options: {
      cwd: req.cwd,
      model: req.model,
      systemPrompt: req.systemPrompt,
      tools: [...req.tools],
      // The tools that exist are the tools allowed; a guard hook (the walker's)
      // narrows them further, and a hook's deny outranks this list.
      allowedTools: [...req.tools],
      permissionMode: "dontAsk",
      outputFormat: { type: "json_schema", schema: answerSchema(req.schema) },
      maxTurns: req.maxTurns,
      settingSources: [],
      strictMcpConfig: true,
      persistSession: false,
      env: req.env,
      ...(req.hooks ? { hooks: req.hooks } : {}),
      ...(req.debugFile ? { debugFile: req.debugFile } : {}),
    },
  });

  let closeTimer: NodeJS.Timeout | undefined;
  const stop = (): void => {
    void q.interrupt().catch(() => undefined);
    closeTimer ??= setTimeout(() => q.close(), INTERRUPT_GRACE_MS);
  };
  const deadline = setTimeout(() => {
    result.timedOut = true;
    stop();
  }, req.timeoutMs);
  const onAbort = (): void => {
    q.close();
  };
  req.signal?.addEventListener("abort", onAbort, { once: true });

  let structured: unknown;
  let structuredFailed: string | undefined;
  try {
    for await (const message of q) {
      transcript?.write(`${JSON.stringify(message)}\n`);
      if (result.halted === undefined) {
        const halt = req.watch?.(message);
        if (halt !== undefined) {
          result.halted = halt;
          stop();
        }
      }
      if (message.type === "system" && message.subtype === "init") {
        result.apiKeySource = message.apiKeySource;
        try {
          assertNotApiKey(message.apiKeySource);
        } catch (e) {
          result.credentialRefused = true;
          result.error = e instanceof Error ? e.message : String(e);
          q.close();
          break;
        }
      }
      if (message.type === "result") {
        // The LATEST result is the running total, per the SDK's own contract.
        result.costUsd = message.total_cost_usd;
        result.numTurns = message.num_turns;
        result.tokens = { input: 0, output: 0, cacheRead: 0, cacheCreation: 0 };
        for (const usage of Object.values(message.modelUsage)) {
          result.tokens.input += usage.inputTokens;
          result.tokens.output += usage.outputTokens;
          result.tokens.cacheRead += usage.cacheReadInputTokens;
          result.tokens.cacheCreation += usage.cacheCreationInputTokens;
        }
        if (message.subtype === "success") structured = message.structured_output;
        else structuredFailed = `${message.subtype}${message.errors.length ? `: ${message.errors.join("; ")}` : ""}`;
        // One answer is all a session here produces: let the stream end.
        input.release();
      }
    }
  } catch (e) {
    if (!req.signal?.aborted) result.error = e instanceof Error ? e.message : String(e);
  } finally {
    clearTimeout(deadline);
    if (closeTimer) clearTimeout(closeTimer);
    req.signal?.removeEventListener("abort", onAbort);
    input.release();
    await new Promise<void>((resolve) => {
      if (transcript) transcript.end(resolve);
      else resolve();
    });
  }
  result.durationMs = Date.now() - started;

  if (result.credentialRefused) return result;
  if (req.signal?.aborted) return { ...result, error: "interrupted" };
  if (structured === undefined) {
    result.error ??=
      result.halted ?? structuredFailed ?? (result.timedOut ? "timed out before answering" : "the session ended without an answer");
    return result;
  }
  const parsed = req.schema.safeParse(structured);
  if (!parsed.success) {
    result.error = `answer failed the schema: ${parsed.error.issues
      .slice(0, 5)
      .map((issue) => `${issue.path.join(".")}: ${issue.message}`)
      .join("; ")}`;
    return result;
  }
  result.output = parsed.data;
  return result;
}

/**
 * The JSON Schema the CLI enforces the answer against. Draft-07, because the
 * CLI's validator does not know zod's default 2020-12 meta-schema and refuses
 * to start (`--json-schema is not a valid JSON Schema`). The INPUT side of the
 * zod schema, so a field with a default is optional to the model too.
 */
export function answerSchema(schema: z.ZodType): Record<string, unknown> {
  return z.toJSONSchema(schema, { io: "input", target: "draft-7" }) as Record<string, unknown>;
}

/** A session that refused on its credential is a harness error everywhere it occurs. */
export function throwIfCredentialRefused(result: SessionResult<unknown>): void {
  if (result.credentialRefused) throw new CredentialError(result.error ?? "credential refused");
}
