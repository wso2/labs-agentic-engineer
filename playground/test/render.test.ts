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
 * `renderPart` verdicts: which streamed tool results read as SUCCESS and which
 * as failure. A resolved tool painted red is a lie the user acts on — a `/start`
 * whose `declare_plan` resolved `{status:"ok"}` showed a red `✗ error` beside a
 * plan that was perfectly fine.
 *
 * These assert the verdict sigil, never the prose around it.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { stdout } from "node:process";
import type { StreamPart } from "@aep/agent-stream";
import { renderPart } from "../src/kit/render.js";

type StdoutWrite = typeof stdout.write;

/** Capture what one part writes to stdout (colour is off — tests are not a TTY). */
function render(part: StreamPart): string {
  const written: string[] = [];
  const original = stdout.write.bind(stdout) as StdoutWrite;
  stdout.write = ((chunk: string | Uint8Array): boolean => {
    written.push(typeof chunk === "string" ? chunk : Buffer.from(chunk).toString("utf8"));
    return true;
  }) as StdoutWrite;
  try {
    renderPart(part);
  } finally {
    stdout.write = original;
  }
  return written.join("");
}

function toolResult(toolName: string, output: unknown): StreamPart {
  return { type: "tool-result", toolCallId: "c1", toolName, output } as StreamPart;
}

test("declare_plan resolves ok — it renders as a success, not a red error", () => {
  const out = render(toolResult("declare_plan", { status: "ok", paths: ["specs/design/design.cell", "specs/design/security.json"] }));
  assert.ok(out.includes("✓"), `expected a success sigil, got ${JSON.stringify(out)}`);
  assert.ok(!out.includes("✗"), `a resolved declare_plan must not render as an error: ${JSON.stringify(out)}`);
});

test("a write op keeps its own success and failure verdicts", () => {
  const ok = render(toolResult("addFile", { ok: true, path: "specs/design/design.md", op: "add", status: "applied" }));
  assert.ok(ok.includes("✓") && ok.includes("add"));

  const err = render(
    toolResult("editFile", { ok: false, path: "specs/design/design.md", op: "edit", code: "NO_SUCH_FILE", message: "gone" }),
  );
  assert.ok(err.includes("✗") && err.includes("NO_SUCH_FILE"));
});

test("a question tool renders as awaiting, neither success nor error", () => {
  const out = render(toolResult("ask_question", { status: "awaiting_user_response", question: "which?" }));
  assert.ok(!out.includes("✗") && !out.includes("✓"), `expected an awaiting line, got ${JSON.stringify(out)}`);
  assert.match(out, /awaiting/);
});

test("the tool-call line names declare_plan's paths", () => {
  const out = render({
    type: "tool-call",
    toolCallId: "c1",
    toolName: "declare_plan",
    input: { paths: ["specs/design/design.cell"] },
  } as StreamPart);
  assert.match(out, /declare_plan/);
  assert.match(out, /specs\/design\/design\.cell/);
});
