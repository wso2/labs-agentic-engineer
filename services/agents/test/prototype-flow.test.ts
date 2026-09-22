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
 * The `/prototype` flow end to end through the real route, with a mock model
 * (no tokens): a designed project — a cell with a web-application, its roles,
 * the API it reads — receives `/prototype`, and the model's writes go through
 * the real `FileBundle` write gate at the component path. The skills are the
 * REAL library files, so the eager block proves both bodies reach the design
 * agent (an audience refusal would drop one silently).
 *
 * What the BFF does with the landed file — commit it, serve it through
 * read-file with its blob SHA — is the Go rig's (files_component_test.go).
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import type { StreamPart } from "@aep/agent-stream";
import { SignJWT } from "jose";
import { createApp } from "../src/server.js";
import { listen0 } from "../src/shared/listen.js";
import { InMemoryConversationStore } from "../src/store/memory-store.js";
import { sha256Hex } from "../src/shared/hash.js";
import { mockModel, type MockStep } from "../src/shared/mock-model.js";

const REPO = resolve(fileURLToPath(import.meta.url), "../../../..");
const FIXTURE = readFileSync(join(REPO, "packages/prototype-model/fixtures/expense-approval.json"), "utf8");
const COMPONENT = (JSON.parse(FIXTURE) as { component: string }).component;
const PROTOTYPE = `specs/design/components/${COMPONENT}/prototype.json`;

/** A project `/design` has finished: one web-application reading one service's API. */
const DESIGNED: Record<string, string> = {
  "specs/requirements/prd.md": "# Expense approvals\n\n## Actors\n\n- Approver\n- Employee\n- Finance\n",
  "specs/design/design.cell":
    `title Expenses\n\ncomponent ${COMPONENT} as "Approvals" web-application\n` +
    `component expenses-api as "Expenses API" service\n\n${COMPONENT} -> expenses-api\n`,
  "specs/design/security.json": JSON.stringify({
    version: 3,
    roles: [{ name: "approver" }, { name: "employee" }, { name: "finance" }],
  }),
  [`specs/design/components/${COMPONENT}/design.json`]: JSON.stringify({
    name: COMPONENT,
    type: "web-application",
    dependencies: [{ kind: "component", name: "expenses-api" }],
  }),
  "specs/design/components/expenses-api/openapi.yaml":
    "openapi: 3.0.3\ninfo:\n  title: Expenses\n  version: 1.0.0\npaths:\n  /expenses:\n    get:\n" +
    '      responses:\n        "200":\n          description: ok\n',
};

const AUD = "agents-service";
const SECRET = "test-secret";
const ORG = "org-a1";
const PROJ = "proj-b2";
const SLUG = "spec-repo";
const REF = "1".repeat(40);
const SKILLS_REF = "2".repeat(40);
const CONV = `org_${ORG}--proj_${PROJ}--prototype--conv1`;

/** A mount holding the repo snapshot plus the real prototype and design-system skills. */
function mount(files: Record<string, string>): string {
  const root = mkdtempSync(join(tmpdir(), "aep-prototype-flow-"));
  const put = (abs: string, content: string): void => {
    mkdirSync(dirname(abs), { recursive: true });
    writeFileSync(abs, content);
  };
  const snap = join(root, "repos", ORG, PROJ, SLUG, "snapshots", REF);
  for (const [rel, content] of Object.entries(files)) put(join(snap, rel), content);
  const skills = join(root, "repos", ORG, "_skills", "org-skills", "snapshots", SKILLS_REF, "skills");
  for (const name of ["prototype", "oxygen-ui-design-system"]) {
    put(join(skills, name, "SKILL.md"), readFileSync(join(REPO, "skills", name, "SKILL.md"), "utf8"));
  }
  return root;
}

interface Run {
  frames: StreamPart[];
  /** Everything the model was sent on its first step. */
  firstPrompt: string;
}

/** POST one `/prototype` turn over `files`, the model playing `steps`. */
async function prototypeTurn(files: Record<string, string>, steps: MockStep[]): Promise<Run> {
  const root = mount(files);
  const model = mockModel(steps);
  const app = createApp({
    store: new InMemoryConversationStore(),
    buildModel: () => model,
    auth: { audience: AUD, secret: SECRET },
    workspaceMountRoot: root,
  });
  const { baseUrl, close } = await listen0(app.listen(0));
  try {
    const token = await new SignJWT({})
      .setProtectedHeader({ alg: "HS256" })
      .setAudience(AUD)
      .setIssuedAt()
      .setExpirationTime("1h")
      .sign(new TextEncoder().encode(SECRET));
    const res = await fetch(`${baseUrl}/conversations/${CONV}/turns`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        Authorization: `Bearer ${token}`,
        "X-Anthropic-Key": "sk-ant-test",
        "X-Org-Id": ORG,
      },
      body: JSON.stringify({
        turn: { kind: "flow", skill: "prototype" },
        workspace: { conversationId: CONV, turnId: "t-1", repoSlug: SLUG, ref: REF, skillsRef: SKILLS_REF },
      }),
    });
    assert.equal(res.status, 200);
    const frames = (await res.text())
      .split("\n")
      .filter((l) => l.startsWith("data: ") && l !== "data: [DONE]")
      .map((l) => JSON.parse(l.slice("data: ".length)) as StreamPart);
    return { frames, firstPrompt: JSON.stringify(model.doStreamCalls[0]!.prompt) };
  } finally {
    await close();
    rmSync(root, { recursive: true, force: true });
  }
}

function resultOf(frames: StreamPart[], toolCallId: string): Record<string, unknown> {
  const frame = frames.find((f) => f.type === "tool-result" && f.toolCallId === toolCallId);
  assert.ok(frame, `no tool-result for ${toolCallId}`);
  return frame.output as Record<string, unknown>;
}

function manifest(frames: StreamPart[]): Record<string, string> {
  const frame = frames.find((f) => f.type === "manifest");
  assert.ok(frame, "the turn ends with a manifest");
  return frame.files ?? {};
}

test("a designed project's /prototype turn lands the prototype at the component path", async () => {
  const { frames, firstPrompt } = await prototypeTurn(DESIGNED, [
    { kind: "toolCall", toolCallId: "w1", toolName: "addFile", input: { path: PROTOTYPE, content: FIXTURE } },
    { kind: "text", text: "done" },
  ]);

  assert.equal(resultOf(frames, "w1").ok, true, JSON.stringify(resultOf(frames, "w1")));
  assert.deepEqual(manifest(frames), { [PROTOTYPE]: sha256Hex(FIXTURE) }, "only the prototype changed");

  // Both skills were inlined — the design-system one is readable by the design agent.
  assert.match(firstPrompt, /## Skill: prototype/);
  assert.match(firstPrompt, /## Skill: oxygen-ui-design-system/);
  // The design the prototype is derived from is in front of the model.
  for (const input of ["specs/design/security.json", "specs/design/components/expenses-api/openapi.yaml"]) {
    assert.ok(firstPrompt.includes(`### ${input}`), `${input} is in the turn's files`);
  }
});

test("a revision reads and edits the prototype already in the snapshot", async () => {
  const { frames, firstPrompt } = await prototypeTurn({ ...DESIGNED, [PROTOTYPE]: FIXTURE }, [
    {
      kind: "toolCall",
      toolCallId: "e1",
      toolName: "editFile",
      input: { path: PROTOTYPE, oldString: '"name": "Expense approvals"', newString: '"name": "Expense review"' },
    },
    { kind: "text", text: "done" },
  ]);

  assert.ok(firstPrompt.includes(`### ${PROTOTYPE}`), "the existing prototype is in the turn's files");
  assert.equal(resultOf(frames, "e1").ok, true, JSON.stringify(resultOf(frames, "e1")));
  const revised = FIXTURE.replace('"name": "Expense approvals"', '"name": "Expense review"');
  assert.deepEqual(manifest(frames), { [PROTOTYPE]: sha256Hex(revised) });
});

test("a prototype filed under another component is refused by the write gate", async () => {
  const misfiled = "specs/design/components/expenses-api/prototype.json";
  const { frames } = await prototypeTurn(DESIGNED, [
    { kind: "toolCall", toolCallId: "w1", toolName: "addFile", input: { path: misfiled, content: FIXTURE } },
    { kind: "text", text: "done" },
  ]);

  const out = resultOf(frames, "w1");
  assert.equal(out.ok, false);
  assert.equal(out.code, "PROTOTYPE_COMPONENT_MISMATCH");
  assert.deepEqual(manifest(frames), {}, "nothing landed");
});
