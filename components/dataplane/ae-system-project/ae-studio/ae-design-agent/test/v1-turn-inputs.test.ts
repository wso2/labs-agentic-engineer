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
 * What a `/v1` turn hands the model, built in the pod from the lookup, the
 * snapshots and the org's connection (07 §1, §4): `/<flow>` commands and
 * their reference documents, documents fitted to the connection, the
 * console's narration policy, the skills snapshot, and the connection's
 * reasoning options. Ported from the legacy route's tests (server.test.ts),
 * whose body fields (`turn`, `workspace`, `connection`, `surface`) are now
 * the pod's to build.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { mockModel } from "../src/shared/mock-model.js";
import { config } from "../src/shared/config.js";
import { unreadableReferencesNote } from "../src/prompts/turn.js";
import type { ModelConnection } from "../src/shared/model.js";
import { minimalPdf } from "./pdf-fixture.js";
import { CONNECTION, PROJECT, call, startEdge, startTurn, streamOf, type Edge, type EdgeOptions } from "./helpers/edge.js";

const OLLAMA: ModelConnection = {
  format: "openai-compatible",
  baseURL: "https://ollama.com/v1",
  authScheme: "bearer",
  contextWindow: 131072,
  outputLimit: 32000,
  capabilities: { claudeCode: false, claudeSubscription: false, promptCache: false, generatedAgents: false, nativePdf: false, webSearch: "none", imageInput: "no" },
  apiKey: "ollama-test-key-0000000000",
  model: "gpt-oss:20b",
};

const CONSOLE_SKILL_MD = `---
name: console
description: How the agent speaks to someone working in the console.
metadata:
  aep:
    kind: platform
    audience: [design]
---

## Never quote a repo path

Name the artifact instead.
`;

/** Run one turn to its end; resolves to the model and the stored first user message. */
async function oneTurn(
  opts: EdgeOptions,
  body: unknown,
): Promise<{ model: ReturnType<typeof mockModel>; status: Record<string, unknown>; firstUser: { content: unknown } | undefined }> {
  const model = mockModel([{ kind: "text", text: "ok" }]);
  const edge = await startEdge({ models: [model], ...opts });
  try {
    const tok = await edge.token();
    const res = await startTurn(edge, tok, body);
    assert.equal(res.status, 202, await res.clone().text());
    const { turnId } = (await res.json()) as { turnId: string };
    await streamOf(edge, tok, turnId);
    const status = (await (await call(edge, `/v1/projects/${PROJECT}/turns/${turnId}`, tok)).json()) as Record<string, unknown>;
    const stored = await edge.store.get(edge.threads.current(PROJECT).conversationId);
    return { model, status, firstUser: stored?.messages.find((m) => m.role === "user") };
  } finally {
    await edge.close();
  }
}

function systemPrompt(model: ReturnType<typeof mockModel>): string {
  const prompt = model.doStreamCalls[0]!.prompt as unknown as { role: string; content: unknown }[];
  const system = prompt.find((m) => m.role === "system");
  return typeof system?.content === "string" ? system.content : JSON.stringify(system?.content ?? "");
}

const parts = (content: unknown) => content as Array<Record<string, unknown>>;

test("a /<flow> command is parsed in the pod: the flow is the turn's, its references ride as native PDF parts", async () => {
  const pdf = Buffer.from("%PDF-1.4 minimal pdf\n");
  const { status, firstUser } = await oneTurn({ references: { "brief.pdf": pdf } }, { instruction: "/design the checkout" });
  assert.equal(status.flow, "design");
  assert.equal(status.instruction, "/design the checkout");
  const file = parts(firstUser?.content).find((p) => p.type === "file");
  assert.equal(file?.mediaType, "application/pdf");
  assert.equal(file?.filename, "specs/requirements/references/brief.pdf");
  assert.equal(Buffer.from(file!.data as string, "base64").toString("hex"), pdf.toString("hex"));
});

test("a chat turn carries no reference documents", async () => {
  const { status, firstUser } = await oneTurn({ references: { "brief.pdf": Buffer.from("%PDF-1.4\n") } }, { instruction: "hello" });
  assert.equal(status.flow, "");
  assert.equal(typeof firstUser?.content, "string");
});

test("on a connection that reads PDFs only as text, a PDF reference reaches the model as its text", async () => {
  const { firstUser } = await oneTurn(
    { connection: OLLAMA, references: { "brief.pdf": minimalPdf("Checkout brief for shoppers") } },
    { instruction: "/start an app" },
  );
  const part = parts(firstUser?.content).find((p) => p.type === "file")!;
  assert.equal(part.mediaType, "text/plain");
  assert.match(Buffer.from(part.data as string, "base64").toString("utf8"), /Checkout brief for shoppers/);
});

test("an image reference on a model that reads no images is left out and named in the prompt; the turn runs", async () => {
  const ref = "specs/requirements/references/flow.png";
  const { status, firstUser } = await oneTurn(
    { connection: OLLAMA, references: { "flow.png": Buffer.from("iVBORw0KGgo=", "base64") } },
    { instruction: "/start an app" },
  );
  assert.equal(status.status, "completed");
  assert.equal(typeof firstUser?.content, "string", "no image part reaches the model");
  assert.ok(
    (firstUser!.content as string).includes(unreadableReferencesNote([{ filename: ref, reason: "the model on this connection does not read images" }])),
  );
});

test("a chat attachment the model cannot read is 400 attachment_rejected naming the file; no turn starts", async () => {
  const edge: Edge = await startEdge({ connection: OLLAMA });
  try {
    const tok = await edge.token();
    const conv = edge.threads.current(PROJECT).conversationId;
    for (const [name, bytes, error] of [
      ["scan.pdf", minimalPdf(), /^scan\.pdf: the PDF has no extractable text/],
      ["mockup.png", Buffer.from("iVBORw0KGgo=", "base64"), /^mockup\.png: the model on this connection does not read images$/],
    ] as const) {
      const form = new FormData();
      form.set("instruction", "look");
      form.append("files", new Blob([new Uint8Array(bytes)]), name);
      const res = await call(edge, `/v1/projects/${PROJECT}/conversations/${conv}/turns`, tok, { body: form });
      assert.equal(res.status, 400, name);
      const body = (await res.json()) as { code: string; detail: string };
      assert.equal(body.code, "attachment_rejected");
      assert.match(body.detail, error);
    }
    assert.equal(edge.desk.active({ kind: "project", project: PROJECT }), null);
  } finally {
    await edge.close();
  }
});

test("a pod turn carries the console narration policy from the skills snapshot, and the skill catalog", async () => {
  const { model } = await oneTurn({ skillFiles: { "skills/console/SKILL.md": CONSOLE_SKILL_MD } }, { instruction: "hello" });
  const system = systemPrompt(model);
  assert.match(system, /# Narration policy/);
  assert.match(system, /Never quote a repo path/);
  assert.doesNotMatch(system, /- console:/, "standing policy, not a catalog entry");
});

test("reasoning effort rides a Sonnet 5 connection and is left off a Haiku 4.5 one", async () => {
  const effortOf = (m: ReturnType<typeof mockModel>) =>
    (m.doStreamCalls[0]!.providerOptions as { anthropic?: { effort?: string } } | undefined)?.anthropic?.effort;
  const sonnet = await oneTurn({}, { instruction: "hi" });
  const haiku = await oneTurn({ connection: { ...CONNECTION, model: "claude-haiku-4-5" } }, { instruction: "hi" });
  assert.equal(effortOf(sonnet.model), config.reasoningEffort);
  assert.equal(effortOf(haiku.model), undefined);
});

test("a JSON body that does not parse is 400 invalid_turn", async () => {
  const edge = await startEdge();
  try {
    const tok = await edge.token();
    const conv = edge.threads.current(PROJECT).conversationId;
    const res = await fetch(`${edge.base}/v1/projects/${PROJECT}/conversations/${conv}/turns`, {
      method: "POST",
      headers: { authorization: `Bearer ${tok}`, "content-type": "application/json" },
      body: "{not json",
    });
    assert.equal(res.status, 400);
    assert.equal(((await res.json()) as { code: string }).code, "invalid_turn");
  } finally {
    await edge.close();
  }
});

test("a run with no one to interview (the playground's phase verbs) tells the agent so; the pod's never does", async () => {
  const note = "No interview is possible in this run";
  const headless = await oneTurn({ headless: true }, { instruction: "/design" });
  assert.match(JSON.stringify(headless.firstUser?.content), new RegExp(note));
  const pod = await oneTurn({}, { instruction: "/design" });
  assert.doesNotMatch(JSON.stringify(pod.firstUser?.content), new RegExp(note));
});
