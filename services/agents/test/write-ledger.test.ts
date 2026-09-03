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
 * The write ledger's own rules, away from the SDK: apply exactly once per
 * toolCallId, project one verdict per call onto the wire, and keep hands off a
 * call the SDK itself would refuse. The real-loop ordering contract these serve
 * is pinned in frame-order.test.ts.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { FileBundle, type OpResult, type StreamPart } from "@aep/agent-stream";
import { buildFileToolSet, ADD_FILE } from "../src/agents/main/tools/files.js";
import { WriteLedger, tapWrites } from "../src/agents/main/tools/write-ledger.js";

/** The frames one addFile call produces, up to and including its tool-call. */
function callFrames(id: string, args: unknown, toolName = ADD_FILE): StreamPart[] {
  const json = JSON.stringify(args);
  return [
    { type: "tool-input-start", id, toolName },
    { type: "tool-input-delta", id, delta: json.slice(0, 8) },
    { type: "tool-input-delta", id, delta: json.slice(8) },
    { type: "tool-input-end", id },
    { type: "tool-call", toolCallId: id, toolName, input: args },
  ];
}

/** The result frame the SDK flushes for that call at the end of the step. */
const sdkResult = (id: string, output: unknown, toolName = ADD_FILE): StreamPart => ({
  type: "tool-result",
  toolCallId: id,
  toolName,
  input: {},
  output,
});

function fixture(files: Record<string, string> = {}) {
  const bundle = new FileBundle(files);
  const { tools, writes } = buildFileToolSet(bundle);
  const wire: StreamPart[] = [];
  return { bundle, tools, writes, wire, feed: tapWrites(writes, (p) => wire.push(p)) };
}

const outputs = (wire: StreamPart[]): OpResult[] =>
  wire.filter((p) => p.type === "tool-result").map((p) => p.output as OpResult);

test("a write is applied at its own tool-input-end, and its verdict rides its tool-call", () => {
  const { bundle, wire, feed } = fixture();
  for (const part of callFrames("c1", { path: "specs/a.md", content: "# A\n" })) feed(part);

  assert.equal(bundle.read("specs/a.md"), "# A\n", "the op ran without waiting for the step to end");
  assert.deepEqual(
    wire.map((p) => p.type),
    ["tool-input-start", "tool-input-delta", "tool-input-delta", "tool-input-end", "tool-call", "tool-result"],
    "the verdict follows the call — never announced before the consumer has seen the call",
  );
  const verdict = outputs(wire)[0]!;
  assert.equal(verdict.ok, true);
  assert.equal(verdict.path, "specs/a.md");
});

test("execute() returns the SAME verdict — the op is never applied twice", async () => {
  const { tools, wire, feed } = fixture();
  for (const part of callFrames("c1", { path: "specs/a.md", content: "# A\n" })) feed(part);
  const early = outputs(wire)[0]!;

  // What the SDK does at model-call-end: run the queued call. A second apply
  // would answer ALREADY_EXISTS and the model would read a failure for a write
  // that succeeded.
  const execute = tools[ADD_FILE]!.execute as (
    input: unknown,
    opts: { toolCallId: string },
  ) => Promise<OpResult>;
  const late = await execute({ path: "specs/a.md", content: "# A\n" }, { toolCallId: "c1" });

  assert.equal(late.ok, true);
  assert.deepEqual(late, early, "the ledger hands back the recorded verdict, byte for byte");
});

test("exactly one tool-result reaches the wire per call", () => {
  const { wire, feed } = fixture();
  for (const part of callFrames("c1", { path: "specs/a.md", content: "# A\n" })) feed(part);
  feed(sdkResult("c1", { ok: true, op: "add", path: "specs/a.md", status: "applied" }));

  assert.equal(wire.filter((p) => p.type === "tool-result").length, 1, "the SDK's copy is suppressed");
});

test("a REJECTED write reports its own error at its own call, mid-batch", () => {
  const { wire, feed } = fixture({ "specs/a.md": "# A\n" });
  for (const part of callFrames("c1", { path: "specs/a.md", content: "# different\n" })) feed(part);

  const verdict = outputs(wire)[0]!;
  assert.equal(verdict.ok, false);
  assert.equal(verdict.ok === false && verdict.code, "ALREADY_EXISTS");
});

test("invalid args are left to the SDK — nothing applied, nothing claimed", () => {
  const { bundle, wire, feed } = fixture();
  // `content` is required by addFileInputSchema, so the SDK marks this call
  // invalid and never executes it. The ledger must reach the same verdict by
  // using that same schema, not a looser hand-rolled shape check.
  for (const part of callFrames("c1", { path: "specs/a.md" })) feed(part);

  assert.equal(bundle.has("specs/a.md"), false);
  assert.equal(wire.filter((p) => p.type === "tool-result").length, 0);
  // ...and the SDK's own frames for that call still pass through untouched.
  feed(sdkResult("c1", { ok: false }));
  assert.equal(wire.filter((p) => p.type === "tool-result").length, 1);
});

test("a call whose tool-call never arrives leaves the SDK's frames alone", () => {
  const { wire, feed } = fixture();
  const frames = callFrames("c1", { path: "specs/a.md", content: "# A\n" });
  for (const part of frames.slice(0, -1)) feed(part); // stream severed before the tool-call
  assert.equal(wire.filter((p) => p.type === "tool-result").length, 0);

  feed(sdkResult("c1", { ok: true, op: "add", path: "specs/a.md", status: "applied" }));
  assert.equal(
    wire.filter((p) => p.type === "tool-result").length,
    1,
    "never settled here ⇒ never suppressed",
  );
});

test("frames for tools the ledger does not own pass through verbatim", () => {
  const { wire, feed } = fixture();
  const parts: StreamPart[] = [
    { type: "tool-input-start", id: "q1", toolName: "ask_question" },
    { type: "tool-input-delta", id: "q1", delta: '{"question":"?"}' },
    { type: "tool-input-end", id: "q1" },
    { type: "tool-call", toolCallId: "q1", toolName: "ask_question", input: {} },
    { type: "tool-result", toolCallId: "q1", toolName: "ask_question", output: {} },
    { type: "text-delta", delta: "hello" },
  ];
  for (const part of parts) feed(part);
  assert.deepEqual(wire, parts);
});

test("an unregistered tool is a programming error, not a silent no-op", () => {
  const ledger = new WriteLedger({});
  assert.throws(() => ledger.apply("c1", ADD_FILE, {}), /no write op registered/);
});
