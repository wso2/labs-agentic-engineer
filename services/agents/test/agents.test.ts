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
 * Each agent is a `ToolLoopAgent` built in its own module: the factory owns
 * the agent's tools, instructions and stop behaviour, and `runTurn` streams
 * whichever agent the caller's `agentFor` hands it, with the per-turn settings
 * (model, step cap, cache breakpoints) it decided.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { tool, type ModelMessage, type ToolSet } from "ai";
import { z } from "zod";
import { buildAnswerInstruction } from "@aep/agent-stream";
import { createIssuesAgent } from "../src/agents/issues/agent.js";
import { createMainAgent } from "../src/agents/main/agent.js";
import { runTurn } from "../src/agents/main/run-turn.js";
import type { AgentRunSettings } from "../src/agents/run-settings.js";
import { describeFiling, FILE_IT, FILE_QUESTION } from "../src/agents/issues/filing-gate.js";
import { mockModel } from "../src/shared/mock-model.js";

const JEV = { apiKey: undefined, url: "http://jev.invalid", fetch: globalThis.fetch };

function runSettings(model = mockModel([{ kind: "text", text: "ok" }])): AgentRunSettings {
  return { model, maxSteps: 5, instructionsWrap: (s) => s };
}

/** MCP tools as the BFF serves them: a search and a create. */
function mcpTools(created: string[]): ToolSet {
  return {
    search_issues: tool({
      description: "search",
      inputSchema: z.object({ q: z.string() }),
      execute: async () => ({ issues: [] }),
    }),
    create_issue: tool({
      description: "create",
      inputSchema: z.object({ title: z.string() }),
      execute: async ({ title }) => {
        created.push(title);
        return { number: 1 };
      },
    }),
    // An MCP tool named like a built-in never shadows it.
    ask_question: tool({
      description: "shadow",
      inputSchema: z.object({}),
      execute: async () => "shadowed",
    }),
  };
}

test("createIssuesAgent: its own tools plus the MCP ones, built-ins winning", () => {
  const agent = createIssuesAgent({ jev: JEV, mcpTools: mcpTools([]), instruction: "it crashes", asked: undefined }, runSettings());
  assert.deepEqual(
    Object.keys(agent.tools).sort(),
    ["ask_question", "ask_questions", "classify_report", "create_issue", "search_issues"],
  );
  assert.notEqual(agent.tools.ask_question!.description, "shadow");
});

test("createIssuesAgent: no MCP tools → only the classifier and the question tools", () => {
  const agent = createIssuesAgent({ jev: JEV, mcpTools: {}, instruction: "it crashes", asked: undefined }, runSettings());
  assert.deepEqual(Object.keys(agent.tools).sort(), ["ask_question", "ask_questions", "classify_report"]);
});

test("createIssuesAgent: create_issue refuses until the instruction is the File it answer to the card that showed it", async () => {
  const created: string[] = [];
  const unconfirmed = createIssuesAgent({ jev: JEV, mcpTools: mcpTools(created), instruction: "file it now", asked: undefined }, runSettings());
  await assert.rejects(
    () => unconfirmed.tools.create_issue!.execute!({ title: "x" }, {} as never) as Promise<unknown>,
    /Not filed/,
  );
  assert.deepEqual(created, []);

  const confirmed = createIssuesAgent(
    {
      jev: JEV,
      mcpTools: mcpTools(created),
      instruction: buildAnswerInstruction(FILE_QUESTION, [FILE_IT]),
      // The File it card showed exactly this issue.
      asked: { question: FILE_QUESTION, options: [{ label: FILE_IT, description: describeFiling({ title: "y" }) }] },
    },
    runSettings(),
  );
  await (confirmed.tools.create_issue!.execute!({ title: "y" }, {} as never) as Promise<unknown>);
  assert.deepEqual(created, ["y"]);
});

test("createIssuesAgent: the turn stops on an accepted question call", async () => {
  const model = mockModel([
    {
      kind: "toolCall",
      toolCallId: "q1",
      toolName: "ask_question",
      input: { question: "What is broken?", options: [{ label: "Login" }] },
    },
    { kind: "text", text: "should never run" },
  ]);
  const messages: ModelMessage[] = [];
  await runTurn({
    model,
    messages,
    prompt: "something is off",
    agentFor: (run) => createIssuesAgent({ jev: JEV, mcpTools: {}, instruction: "something is off", asked: undefined }, run),
  });
  assert.equal(model.doStreamCalls.length, 1, "the question call ends the turn");
});

test("createMainAgent passes its tools and instructions through", async () => {
  const tools: ToolSet = {
    ping: tool({ description: "ping", inputSchema: z.object({}), execute: async () => "pong" }),
  };
  const model = mockModel([{ kind: "text", text: "ok" }]);
  const agent = createMainAgent({ instructions: "You are the main agent.", tools }, runSettings(model));
  assert.deepEqual(Object.keys(agent.tools), ["ping"]);

  await agent.stream({ messages: [{ role: "user", content: "hi" }] }).then((r) => r.consumeStream());
  const prompt = model.doStreamCalls[0]!.prompt as unknown as ModelMessage[];
  assert.deepEqual(prompt[0], { role: "system", content: "You are the main agent." });
});

test("runTurn streams the agent agentFor returns, with the turn's settings", async () => {
  const model = mockModel([{ kind: "text", text: "Done." }]);
  const seen: AgentRunSettings[] = [];
  const events: string[] = [];
  const messages: ModelMessage[] = [];
  const breakpoint = { anthropic: { cacheControl: { type: "ephemeral" } } };

  const res = await runTurn({
    model,
    messages,
    prompt: "hello",
    maxSteps: 7,
    maxOutputTokens: 1234,
    cacheBreakpoint: breakpoint,
    agentFor: (run) => {
      seen.push(run);
      return createMainAgent({ instructions: "sys", tools: {} }, run);
    },
    onEvent: (p) => events.push(p.type),
  });

  assert.equal(seen.length, 1, "agentFor is called once per turn");
  const run = seen[0]!;
  assert.equal(run.model, model);
  assert.equal(run.maxSteps, 7);
  assert.equal(run.maxOutputTokens, 1234);
  assert.deepEqual(run.instructionsWrap("sys"), { role: "system", content: "sys", providerOptions: breakpoint });
  assert.equal(typeof run.prepareStep, "function", "a cache breakpoint rolls");

  assert.ok(events.includes("text-delta"));
  assert.equal(res.finishReason, "stop");
  assert.equal(messages[0]?.role, "user");
  assert.equal(messages[1]?.role, "assistant");
});

test("runTurn without a cache breakpoint: the plain instructions and no prepareStep", async () => {
  const seen: AgentRunSettings[] = [];
  await runTurn({
    model: mockModel([{ kind: "text", text: "Done." }]),
    messages: [],
    prompt: "hello",
    agentFor: (run) => {
      seen.push(run);
      return createMainAgent({ instructions: "sys", tools: {} }, run);
    },
  });
  assert.equal(seen[0]!.instructionsWrap("sys"), "sys");
  assert.equal(seen[0]!.prepareStep, undefined);
  assert.equal(seen[0]!.maxSteps, 20, "the default step cap");
});
