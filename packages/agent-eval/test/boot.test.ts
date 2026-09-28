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

import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { bootAgent, type BootedAgent } from "../src/boot.js";
import { writeFakeAgent, type FakeAgentMode } from "./fake-agent.js";

let dir: string;
let booted: BootedAgent | undefined;

// The bound a "never becomes ready" test uses: short enough that these cost
// seconds rather than the production minute, long enough that a loaded
// machine still gets the child listening first. A bound below process start
// time would make them pass for the WRONG reason — "no answer yet", where
// the point is what /healthz actually said.
const NEVER_READY_BOUND_MS = 5_000;

function boot(mode: FakeAgentMode, extra: Record<string, unknown> = {}): Promise<BootedAgent> {
  return bootAgent({
    appDir: dir,
    env: { FAKE_AGENT_MODE: mode },
    // Generous for the ready path, where the wait ends as soon as /healthz
    // says so: a tight bound here would only measure the machine's load.
    readyTimeoutMs: 20_000,
    pollIntervalMs: 50,
    ...extra,
  });
}

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "agent-eval-boot-"));
  writeFakeAgent(dir);
});

afterEach(async () => {
  await booted?.close();
  booted = undefined;
  rmSync(dir, { recursive: true, force: true });
});

describe("bootAgent", () => {
  it("returns the agent's own base URL once /healthz reports ready", async () => {
    booted = await boot("ready");
    expect(booted.url).toMatch(/^http:\/\/127\.0\.0\.1:\d+$/);
    const res = await fetch(new URL("/healthz", booted.url));
    expect(res.status).toBe(200);
  });

  // An ephemeral port, not the contract's 9090: a fix loop runs the agent
  // repeatedly and in parallel with whatever else the build is doing, and a
  // fixed port makes the second boot fail on an address already in use.
  it("boots on an ephemeral port rather than the component contract's 9090", async () => {
    booted = await boot("ready");
    expect(booted.url.endsWith(":9090")).toBe(false);
  });

  // The failure mode this guards: an agent that never came up scored as an
  // agent that behaved badly. `missing` names what the harness failed to
  // inject, so the message points at the wiring, not at the prompt.
  it("fails with the /healthz missing list when the agent never becomes ready", async () => {
    await expect(boot("missing-env", { readyTimeoutMs: NEVER_READY_BOUND_MS })).rejects.toThrow(
      /LUNCH_API_URL/,
    );
  }, 15_000);

  // Bounded, not merely eventual: a boot that waits forever turns a
  // misconfigured agent into a stuck build. And the reason it gives is the
  // store, so nobody mistakes it for an agent that answered badly.
  it("gives up inside the bound, saying the store never came up", async () => {
    const started = Date.now();
    await expect(
      boot("store-initialising", { readyTimeoutMs: NEVER_READY_BOUND_MS }),
    ).rejects.toThrow(/initialising/);
    expect(Date.now() - started).toBeLessThan(NEVER_READY_BOUND_MS * 3);
  }, 20_000);

  it("fails fast, quoting the child's output, when the process exits before listening", async () => {
    const started = Date.now();
    // A long bound: the point is that a DEAD child is noticed at once rather
    // than waited out, so the test must not be able to pass by the bound
    // simply being short.
    await expect(boot("crash", { readyTimeoutMs: 30_000 })).rejects.toThrow(/Cannot find module/);
    expect(Date.now() - started).toBeLessThan(10_000);
  }, 40_000);

  it("leaves no child behind when the boot itself fails", async () => {
    // A leaked agent per scenario would pile up across a three-round loop,
    // each one holding a port and a model credential. The agent that fails
    // here is one that IS listening (its store never came up), so the port
    // still answering afterwards is exactly the leak, observable.
    const failure = await boot("store-initialising", {
      readyTimeoutMs: NEVER_READY_BOUND_MS,
    }).then(
      () => undefined,
      (e: unknown) => (e instanceof Error ? e.message : String(e)),
    );
    const url = /http:\/\/127\.0\.0\.1:\d+/.exec(failure ?? "")?.[0];
    expect(url).toBeDefined();
    await expect(fetch(new URL("/healthz", url!))).rejects.toThrow();
  }, 20_000);

  it("close() terminates the child, and the port stops answering", async () => {
    const agent = await boot("ready");
    await agent.close();
    await expect(fetch(new URL("/healthz", agent.url))).rejects.toThrow();
  });

  it("close() is safe to call twice", async () => {
    const agent = await boot("ready");
    await agent.close();
    await expect(agent.close()).resolves.toBeUndefined();
  });

  // The spec's requirement, restated as a property of the boot: memory is
  // exercised with no Postgres, so the harness must never invent
  // MEMORY_DB_* values to make an agent look healthy.
  it("passes only the caller's env, leaving MEMORY_DB_* unset", async () => {
    booted = await bootAgent({
      appDir: dir,
      env: { FAKE_AGENT_MODE: "ready", MODEL_API_KEY: "sk-ant-test" },
      readyTimeoutMs: 20_000,
      pollIntervalMs: 50,
    });
    const seen = (await (await fetch(new URL("/debug/env", booted.url))).json()) as Record<
      string,
      string | null
    >;
    expect(seen.MEMORY_DB_HOST).toBeNull();
    expect(seen.MODEL_API_KEY).toBe("sk-ant-test");
    expect(seen.PORT).toBeTruthy();
  });
});
