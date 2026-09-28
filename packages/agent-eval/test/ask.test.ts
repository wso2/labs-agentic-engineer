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
import { askViaHttp } from "../src/ask.js";
import { writeFakeAgent, type FakeAgentMode } from "./fake-agent.js";
import type { Message } from "../src/conversation.js";

let dir: string;
const running: BootedAgent[] = [];

async function agentIn(mode: FakeAgentMode): Promise<BootedAgent> {
  const agent = await bootAgent({
    appDir: dir,
    env: { FAKE_AGENT_MODE: mode },
    readyTimeoutMs: 20_000,
    pollIntervalMs: 50,
  });
  running.push(agent);
  return agent;
}

const user = (content: string): Message => ({ role: "user", content });

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "agent-eval-ask-"));
  writeFakeAgent(dir);
});

afterEach(async () => {
  await Promise.all(running.splice(0).map((a) => a.close()));
  rmSync(dir, { recursive: true, force: true });
});

describe("askViaHttp", () => {
  it("returns the agent's reply text", async () => {
    const agent = await agentIn("ready");
    const ask = askViaHttp(agent.url);
    expect(await ask([user("hello")])).toContain("first: hello");
  });

  // "A second turn must remember" is only observable if the id from the
  // first turn travels with the second — the caller's one piece of state.
  it("carries the first turn's conversationId into every later turn", async () => {
    const agent = await agentIn("ready");
    const ask = askViaHttp(agent.url);
    const first = await ask([user("book a hotel")]);
    const second = await ask([user("book a hotel"), { role: "assistant", content: first }, user("in Rome")]);
    expect(second).toMatch(/^turn 2 /);
    expect(second).toContain("first: book a hotel");
  });

  // Each scenario gets its OWN ask, so no scenario can read — or be blamed
  // for — another scenario's history.
  it("starts a fresh conversation per ask, so scenarios cannot leak into one another", async () => {
    const agent = await agentIn("ready");
    const first = await askViaHttp(agent.url)([user("scenario one")]);
    const second = await askViaHttp(agent.url)([user("scenario two")]);
    expect(second).toMatch(/^turn 1 /);
    expect(second).toContain("first: scenario two");
    expect(idOf(second)).not.toBe(idOf(first));
  });

  // A broken agent must read as broken. Swallowing this into "" would put an
  // empty answer in front of the grader and score an outage as bad behaviour.
  it("raises with the status and the body when /chat answers non-2xx", async () => {
    const agent = await agentIn("chat-error");
    await expect(askViaHttp(agent.url)([user("hello")])).rejects.toThrow(
      /500.*the model provider refused/s,
    );
  });

  it("raises when /chat answers 200 with a body that is not the contract", async () => {
    const agent = await agentIn("chat-malformed");
    await expect(askViaHttp(agent.url)([user("hello")])).rejects.toThrow(/malformed/i);
  });

  it("raises when nothing is listening at all", async () => {
    const agent = await agentIn("ready");
    const ask = askViaHttp(agent.url);
    await agent.close();
    await expect(ask([user("hello")])).rejects.toThrow();
  });

  it("refuses to send when the last message is not the user's", async () => {
    const agent = await agentIn("ready");
    await expect(
      askViaHttp(agent.url)([user("hi"), { role: "assistant", content: "hello" }]),
    ).rejects.toThrow(/user/i);
  });
});

function idOf(reply: string): string {
  return reply.split(" of ")[1]!.split(";")[0]!;
}
