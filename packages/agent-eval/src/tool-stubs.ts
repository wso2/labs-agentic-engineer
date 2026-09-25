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

import { readFileSync } from "node:fs";
import { parse } from "yaml";
import { startStubServer, type StubCall } from "./stub-server.js";
import type { ToolStub } from "./agent-doc.js";

/** What starts one stub. Injectable so a partial start can be tested. */
export type StartStubServer = (
  spec: unknown,
  allow?: readonly string[],
) => Promise<{ url: string; calls: StubCall[]; close: () => Promise<void> }>;

export interface RunningStubs {
  /** The agent's tool base-address variables, pointed at the stubs. */
  env: Record<string, string>;
  /** Every request the agent made, per env var, for a test to assert on. */
  calls: Record<string, StubCall[]>;
  /**
   * Operations the agent reached for that its allow-list withholds, as
   * `ENV_VAR: operationId`. Empty is the normal answer; anything else is the
   * agent trying to leave its security boundary, and it belongs in front of
   * a human rather than buried in a transcript.
   */
  overReach: () => string[];
  close: () => Promise<void>;
}

/**
 * Serves each declared provider contract from its own stub, and hands back
 * the environment that points the agent at them.
 *
 * One server per contract rather than one shared: two providers may define
 * the same path, and a single server would answer the wrong one — a silent
 * wrong fixture is worse than a 404.
 *
 * A partial start is rolled back. Leaving half the stubs listening would
 * leak a port per scenario across a three-round loop.
 */
export async function startToolStubs(
  stubs: ToolStub[],
  // @knipkeep a test seam: rollback-on-partial-start is only observable if a
  // test can make the SECOND start fail and watch the first one close.
  startServer: StartStubServer = startStubServer,
): Promise<RunningStubs> {
  const started: Array<{ envVar: string; url: string; calls: StubCall[]; close: () => Promise<void> }> =
    [];
  const closeAll = async (): Promise<void> => {
    await Promise.all(started.splice(0).map((s) => s.close()));
  };

  try {
    for (const stub of stubs) {
      // YAML is a superset of JSON, so one parser reads either form of a
      // committed contract.
      const spec: unknown = parse(readFileSync(stub.specPath, "utf8"));
      const server = await startServer(spec, stub.allow);
      started.push({ envVar: stub.envVar, ...server });
    }
  } catch (e) {
    await closeAll();
    throw e;
  }

  const env: Record<string, string> = {};
  const calls: Record<string, StubCall[]> = {};
  for (const s of started) {
    env[s.envVar] = s.url;
    calls[s.envVar] = s.calls;
  }
  // Read at the END of a scenario rather than counted as it goes: the stubs
  // record, they do not judge.
  const overReach = (): string[] =>
    started.flatMap((s) =>
      s.calls
        .filter((c) => c.denied === true)
        .map((c) => `${s.envVar}: ${c.operationId ?? `${c.method} ${c.path}`}`),
    );
  return { env, calls, overReach, close: closeAll };
}
