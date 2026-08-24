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

// @vitest-environment jsdom

// What startCollabTurn actually puts ON THE WIRE (#428). The rest of this
// feature's tests assert either side of the request — the screening that decides
// what to send, the server that parses it — and none of them would notice the
// branch itself going wrong. Two defects on this feature were exactly that
// shape: a value carried correctly up to a boundary and never across it.

import { beforeEach, describe, expect, it, vi } from "vitest";

// The signature is explicit so the spread below has a rest parameter to land in
// (TS2556 otherwise) — and so `post.mock.calls` is typed rather than an empty
// tuple, which is what the assertions read.
const post = vi.fn<(path: string, init: Record<string, unknown>) => Promise<unknown>>(async () => ({
  data: { turnId: "t-1" },
  error: undefined,
  response: { status: 202 },
}));
vi.mock("../../../api/client", () => ({
  client: { POST: (path: string, init: Record<string, unknown>) => post(path, init) },
}));

const { startCollabTurn } = await import("./turns");

function fileOf(name: string, body = "x"): File {
  return new File([body], name);
}

/** The single POST recorded, as (path, init). */
function sent(): { path: string; init: Record<string, unknown> } {
  expect(post).toHaveBeenCalledOnce();
  const [path, init] = post.mock.calls[0]!;
  return { path, init };
}

describe("startCollabTurn transport", () => {
  beforeEach(() => post.mockClear());

  it("sends JSON when nothing is attached", async () => {
    await startCollabTurn("shop", "conv-1", "tidy the requirements");
    const { path, init } = sent();
    expect(path).toBe("/projects/{projectName}/agents/{conversationId}/messages");
    // The overwhelmingly common send must stay byte-identical to the
    // pre-attachment contract: a plain JSON object, not a FormData with one field.
    expect(init.body).toEqual({ instruction: "tidy the requirements", collab: true });
    expect(init.body).not.toBeInstanceOf(FormData);
  });

  it("sends JSON when the files array is explicitly empty", async () => {
    await startCollabTurn("shop", "conv-1", "hello", []);
    expect(sent().init.body).toEqual({ instruction: "hello", collab: true });
  });

  it("switches to multipart when files are attached", async () => {
    await startCollabTurn("shop", "conv-1", "what is wrong here?", [
      fileOf("error.png"),
      fileOf("rows.csv"),
    ]);
    const body = sent().init.body;
    expect(body).toBeInstanceOf(FormData);
    const form = body as FormData;
    expect(form.get("instruction")).toBe("what is wrong here?");
    // A form field has no boolean type; the server parses this string.
    expect(form.get("collab")).toBe("true");
    // EVERY file, under the repeated `files` name the server reads.
    expect(form.getAll("files").map((f) => (f as File).name)).toEqual([
      "error.png",
      "rows.csv",
    ]);
  });

  it("carries the caller's own File, not a re-wrapped copy", async () => {
    // Identity, not content: jsdom's File has no .text(), and identity proves
    // more anyway — the exact object the picker produced reaches the wire, so
    // nothing between here and the boundary can substitute a placeholder.
    const file = fileOf("brief.md", "# real content");
    await startCollabTurn("shop", "conv-1", "read this", [file]);
    const form = sent().init.body as FormData;
    expect(form.getAll("files")[0]).toBe(file);
    expect((form.getAll("files")[0] as File).size).toBe(file.size);
  });

  it("passes the path params through on both branches", async () => {
    await startCollabTurn("shop", "conv-9", "hi");
    expect(sent().init.params).toEqual({ path: { projectName: "shop", conversationId: "conv-9" } });
    post.mockClear();
    await startCollabTurn("shop", "conv-9", "hi", [fileOf("a.md")]);
    expect(sent().init.params).toEqual({ path: { projectName: "shop", conversationId: "conv-9" } });
  });

  it("returns the server's turn id", async () => {
    expect(await startCollabTurn("shop", "conv-1", "hi")).toBe("t-1");
  });
});
