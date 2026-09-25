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

import { runConversation, type AskFn } from "./conversation.js";
import { bootAgent } from "./boot.js";
import { askViaHttp } from "./ask.js";
import { startToolStubs } from "./tool-stubs.js";
import type { ToolStub } from "./agent-doc.js";
import type { Scenario } from "./scenario.js";

export interface ProviderConfig {
  maxTurns?: number;
  /** The component's App Path. Required unless the caller injects an `ask`. */
  appDir?: string;
  /** Provider contracts to stub, and the variables that address them. */
  toolStubs?: ToolStub[];
  /** The bound on a boot once this agent has proved it can start. */
  readyTimeoutMs?: number;
  /** The bound on the FIRST boot, which is a diagnosis, not a wait. */
  firstBootTimeoutMs?: number;
}

/**
 * The first boot is short on purpose. Until one has succeeded, a boot that
 * does not finish is far more likely to be a misconfigured agent than a busy
 * machine, and a long bound here is paid by every scenario of every round —
 * ten scenarios over three rounds at the full production bound is half an
 * hour spent learning one fact. Twenty seconds is many times what a node
 * process needs to bind a port and answer.
 */
const DEFAULT_FIRST_BOOT_TIMEOUT_MS = 20_000;

/**
 * promptfoo's unit is a prompt and its output; ours is a conversation. The
 * bridge is this provider: it runs the whole conversation and returns the
 * TRANSCRIPT as `output`, which promptfoo's rubrics then grade. promptfoo's
 * `prompts:` field is satisfied but unused — the scenario drives, not a prompt.
 *
 * It builds its own `ask` from CONFIG rather than receiving one: `vars` is
 * JSON in an emitted config file, and a function cannot survive that trip.
 * An injected `ask` still wins where one is present, which is the seam the
 * conversation-level tests use — they need no child process and no key.
 *
 * The stubs and the agent are started and torn down per SCENARIO. It costs a
 * process start against work that is dominated by model calls, and it buys
 * two things worth more: no conversation can leak into the next, and no
 * child can outlive the scenario that needed it.
 */
export default class AgentEvalProvider {
  private readonly config: ProviderConfig;
  /** Set once a boot succeeds: later boots are given the longer bound. */
  private everBooted = false;
  /**
   * Why the agent could not be started, when it never started at all. The
   * diagnosis does not change between scenarios, so it is made once and
   * repeated — never swallowed, which would turn a dead agent into a bad
   * score.
   */
  private bootFailure: string | undefined;

  constructor(options?: { config?: ProviderConfig }) {
    this.config = options?.config ?? {};
  }

  id(): string {
    return "aep:agent-eval";
  }

  async callApi(
    _prompt: string,
    context?: { vars?: { scenario?: Scenario; ask?: AskFn } },
  ): Promise<{ output: string; metadata: Record<string, unknown> }> {
    const scenario = context?.vars?.scenario;
    if (!scenario) throw new Error("agent-eval: a scenario var is required");

    const injected = context?.vars?.ask;
    if (injected !== undefined) return this.run(scenario, injected);

    const appDir = this.config.appDir;
    if (appDir === undefined) {
      throw new Error(
        "agent-eval: the provider needs an appDir in its config (or an injected ask var) " +
          "— there is nothing to evaluate without an agent to boot",
      );
    }

    if (this.bootFailure !== undefined) throw new Error(this.bootFailure);

    const stubs = await startToolStubs(this.config.toolStubs ?? []);
    try {
      const agent = await this.boot(appDir, stubs.env);
      try {
        const result = await this.run(scenario, askViaHttp(agent.url));
        // Carried out in the result rather than logged: an agent reaching
        // for an operation its allow-list withholds is a security finding,
        // and `out.json` is what a human and the fix loop actually read.
        const overReach = stubs.overReach();
        if (overReach.length > 0) result.metadata.toolOverReach = overReach;
        return result;
      } finally {
        await agent.close();
      }
    } finally {
      await stubs.close();
    }
  }

  /**
   * One boot, under the bound that fits what is already known about this
   * agent, remembering a failure that came before any success.
   */
  private async boot(appDir: string, stubEnv: Record<string, string>) {
    const bound = this.everBooted
      ? this.config.readyTimeoutMs
      : (this.config.firstBootTimeoutMs ?? DEFAULT_FIRST_BOOT_TIMEOUT_MS);
    try {
      const agent = await bootAgent({
        appDir,
        env: this.agentEnv(stubEnv),
        ...(bound === undefined ? {} : { readyTimeoutMs: bound }),
      });
      this.everBooted = true;
      return agent;
    } catch (e) {
      // Only a first failure is cached. Once an agent HAS booted, a later
      // failure is this machine, this moment — not a verdict on the agent.
      if (!this.everBooted) this.bootFailure = e instanceof Error ? e.message : String(e);
      throw e;
    }
  }

  /**
   * The child's whole environment. The model credential is read from THIS
   * process rather than carried in the provider's config, because the config
   * is written to disk under the build's output directory — a key belongs in
   * an environment variable, never in a file a PR might carry.
   *
   * `MEMORY_DB_*` is deliberately absent: the spec requires memory to be
   * exercised without Postgres, so the agent must serve this run from its own
   * in-memory store. If it cannot, `bootAgent` says so and the run fails
   * honestly instead of scoring an agent that cannot remember.
   */
  private agentEnv(stubEnv: Record<string, string>): Record<string, string> {
    const env: Record<string, string> = { ...stubEnv };
    for (const key of ["MODEL_API_KEY", "MODEL_NAME", "MODEL_ENDPOINT", "PATH"]) {
      const value = process.env[key];
      if (value !== undefined) env[key] = value;
    }
    return env;
  }

  private async run(
    scenario: Scenario,
    ask: AskFn,
  ): Promise<{ output: string; metadata: Record<string, unknown> }> {
    const t = await runConversation({
      scenario,
      ask,
      ...(this.config.maxTurns === undefined ? {} : { maxTurns: this.config.maxTurns }),
    });
    return { output: t.text, metadata: { turns: t.turns, scenarioId: scenario.id } };
  }
}
