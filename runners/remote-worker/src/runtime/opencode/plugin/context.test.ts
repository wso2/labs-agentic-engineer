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
import type { SessionContextRecord } from "../../../lib/run_context.js";
import { createSessionContext, withoutAppendix } from "./context.js";

const APPENDIX = "# Your workflow\n\nfan out\n\n## Tool glossary\nthe fan-out tool is `task`\n";

// The shape session/prompt.ts + llm/request.ts produce: the base prompt, the
// environment, then each instructions file as `Instructions from: <path>\n<content>`,
// all joined with "\n".
function system(...instructions: [string, string][]): string {
  return ["BASE PROMPT", "<env>\n</env>", ...instructions.map(([p, c]) => `Instructions from: ${p}\n${c}`), "SKILLS"].join("\n");
}

test("withoutAppendix: removes exactly the block carrying the appendix, whatever path OpenCode printed", () => {
  const agents: [string, string] = ["/work/AGENTS.md", "project rules"];
  assert.equal(
    withoutAppendix(system(agents, ["/private/var/t/instructions.md", APPENDIX]), APPENDIX),
    system(agents),
  );
});

test("withoutAppendix: a prompt without it, or an empty appendix, is left alone", () => {
  const text = system(["/work/AGENTS.md", "project rules"]);
  assert.equal(withoutAppendix(text, APPENDIX), text);
  assert.equal(withoutAppendix(system(["/t/i.md", APPENDIX]), ""), system(["/t/i.md", APPENDIX]));
});

function harness(): { ctx: ReturnType<typeof createSessionContext>; records: SessionContextRecord[] } {
  const records: SessionContextRecord[] = [];
  return { ctx: createSessionContext({ appendix: APPENDIX, lead: "aep", record: (r) => records.push(r) }), records };
}

test("shapeSystem: the lead keeps the appendix, a subagent and an unknown session do not", () => {
  const { ctx, records } = harness();
  ctx.noteAgent("ses_lead", "aep");
  ctx.noteAgent("ses_b", "general");
  const withIt = system(["/t/i.md", APPENDIX]);

  const lead = [withIt];
  ctx.shapeSystem("ses_lead", lead);
  const builder = [withIt];
  ctx.shapeSystem("ses_b", builder);
  const stranger = [withIt];
  ctx.shapeSystem("ses_x", stranger);

  assert.deepEqual(lead, [withIt]);
  assert.deepEqual(builder, [system()]);
  assert.deepEqual(stranger, [system()]);
  assert.deepEqual(records, [
    { session: "ses_lead", agent: "aep", appendix: true },
    { session: "ses_b", agent: "general", appendix: false },
    { session: "ses_x", agent: "unknown", appendix: false },
  ]);
});

test("shapeSystem: a session is recorded once — a later call (the compaction summariser) does not flip it", () => {
  const { ctx, records } = harness();
  ctx.noteAgent("ses_lead", "aep");
  ctx.shapeSystem("ses_lead", [system(["/t/i.md", APPENDIX])]);
  ctx.shapeSystem("ses_lead", ["COMPACTION PROMPT"]);
  assert.deepEqual(records, [{ session: "ses_lead", agent: "aep", appendix: true }]);
});

test("noteAgent: the first agent a session was prompted as is the one it keeps", () => {
  const { ctx, records } = harness();
  ctx.noteAgent("ses_lead", "aep");
  ctx.noteAgent("ses_lead", "general");
  ctx.shapeSystem("ses_lead", [system(["/t/i.md", APPENDIX])]);
  assert.deepEqual(records, [{ session: "ses_lead", agent: "aep", appendix: true }]);
});

test("noteSkill: a skill-tool call is recorded against its session; an unnamed one is not", () => {
  const { ctx, records } = harness();
  ctx.noteSkill("ses_b", "ballerina");
  ctx.noteSkill("ses_b", "");
  assert.deepEqual(records, [{ session: "ses_b", skill: "ballerina" }]);
});
