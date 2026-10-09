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

import { describe, expect, it } from "vitest";
import * as Y from "yjs";
import { setDocFile } from "@aep/collab-doc";
import { checkPrototypeFiles, resolveTheme } from "@wso2/prototype-kit/check";
import { manifestPath, sourcePath } from "../../features/prototype/model/prototypes";
import { SAMPLE_COMPONENT, SAMPLE_MANIFEST, SAMPLE_SOURCE, scriptPrototypeTurn } from "./prototype";

const oxygen = resolveTheme("@wso2/prototype-theme-oxygen", [new URL(".", import.meta.url).pathname]);

/** The file writes a turn streams, in order: tool name and path. */
function writes(frames: { part: { type: string; toolName?: string; input?: unknown } }[]): string[] {
  return frames
    .filter((f) => f.part.type === "tool-result")
    .map((f) => `${f.part.toolName} ${(f.part.input as { path: string }).path}`);
}

function text(turn: { reply: unknown[] }): string {
  return JSON.stringify(turn.reply);
}

const request = (text: string) => ({ screenId: "screen.claim", roleId: "manager", stateId: "state.default", elementIds: ["btn.approve"], text });

function docWith(files: Record<string, string>): Y.Doc {
  const doc = new Y.Doc();
  for (const [path, content] of Object.entries(files)) setDocFile(doc, path, content, "test");
  return doc;
}

const made = () => docWith({ [manifestPath(SAMPLE_COMPONENT)]: SAMPLE_MANIFEST, [sourcePath(SAMPLE_COMPONENT)]: SAMPLE_SOURCE });

describe("the mock's sample prototype", () => {
  it("passes the kit's checks on the Oxygen theme, as written and as every request it knows revises it", async () => {
    expect(await checkPrototypeFiles({ manifest: SAMPLE_MANIFEST, source: SAMPLE_SOURCE }, { theme: oxygen })).toEqual([]);
    const feedback = { prototypeHash: "a".repeat(64), component: SAMPLE_COMPONENT, requests: [request("Approve on the right"), request("A bigger total"), request("Remove the Reject button")] };
    const revised = scriptPrototypeTurn({ instruction: "/prototype", feedback, webApps: [SAMPLE_COMPONENT], doc: made(), turnKey: "t" })!;
    expect(writes(revised.frames)).toHaveLength(3);
    expect(await checkPrototypeFiles({ manifest: SAMPLE_MANIFEST, source: revised.files![sourcePath(SAMPLE_COMPONENT)]! }, { theme: oxygen })).toEqual([]);
  });
});

describe("scriptPrototypeTurn", () => {
  const base = { feedback: undefined, webApps: [SAMPLE_COMPONENT], turnKey: "t1" };

  it("answers only /prototype", () => {
    expect(scriptPrototypeTurn({ ...base, instruction: "Design 2 features", doc: new Y.Doc() })).toBeNull();
  });

  it("makes the prototype: the manifest first, then the source", () => {
    const turn = scriptPrototypeTurn({ ...base, instruction: "/prototype expense-web", doc: new Y.Doc() })!;
    expect(writes(turn.frames)).toEqual([`addFile ${manifestPath(SAMPLE_COMPONENT)}`, `addFile ${sourcePath(SAMPLE_COMPONENT)}`]);
    expect(turn.files).toEqual({ [manifestPath(SAMPLE_COMPONENT)]: SAMPLE_MANIFEST, [sourcePath(SAMPLE_COMPONENT)]: SAMPLE_SOURCE });
  });

  it("writes nothing once the prototype is there, and says so", () => {
    const turn = scriptPrototypeTurn({ ...base, instruction: "/prototype", doc: made() })!;
    expect(writes(turn.frames)).toEqual([]);
    expect(turn.files).toBeUndefined();
    expect(text(turn)).toMatch(/up to date/);
  });

  it("says when the design has no web application", () => {
    const turn = scriptPrototypeTurn({ ...base, webApps: [], instruction: "/prototype", doc: new Y.Doc() })!;
    expect(writes(turn.frames)).toEqual([]);
    expect(text(turn)).toMatch(/no web application/);
  });

  it("revises from a review's requests, answering each by number as applied or declined", () => {
    const doc = made();
    const feedback = {
      prototypeHash: "a".repeat(64),
      component: SAMPLE_COMPONENT,
      requests: [request("Put Approve on the right"), request("Add a dark mode")],
    };
    const turn = scriptPrototypeTurn({ ...base, instruction: "/prototype expense-web", feedback, doc })!;
    expect(writes(turn.frames)).toEqual([`editFile ${sourcePath(SAMPLE_COMPONENT)}`]);
    expect(text(turn)).toMatch(/1\. Applied: Approve is now on the right/);
    expect(text(turn)).toMatch(/2\. Declined/);
    expect(turn.files?.[sourcePath(SAMPLE_COMPONENT)]).not.toBe(SAMPLE_SOURCE);
    expect(turn.files?.[sourcePath(SAMPLE_COMPONENT)]?.indexOf('id="btn.reject"')).toBeLessThan(turn.files![sourcePath(SAMPLE_COMPONENT)]!.indexOf('id="btn.approve"'));
  });

  it("removes an element a request asks to remove, so comments on it are left without it", () => {
    const feedback = { prototypeHash: "a".repeat(64), component: SAMPLE_COMPONENT, requests: [request("Remove the Reject button")] };
    const turn = scriptPrototypeTurn({ ...base, instruction: "/prototype expense-web", feedback, doc: made() })!;
    expect(text(turn)).toMatch(/1\. Applied: Reject is gone/);
    expect(turn.files?.[sourcePath(SAMPLE_COMPONENT)]).not.toContain('id="btn.reject"');
  });

  it("fails the turn a request asks to fail, writing nothing", () => {
    const feedback = { prototypeHash: "a".repeat(64), component: SAMPLE_COMPONENT, requests: [request("Put Approve on the right"), request("Fail this revision")] };
    const turn = scriptPrototypeTurn({ ...base, instruction: "/prototype expense-web", feedback, doc: made() })!;
    expect(writes(turn.frames)).toEqual([]);
    expect(turn.files).toBeUndefined();
    expect(turn.frames.at(-1)?.part).toMatchObject({ type: "turn-failed" });
    expect(turn.failure).toMatch(/failed/);
  });

  it("fails the turn a request asks to stop partway, keeping what the requests before it wrote", () => {
    const feedback = { prototypeHash: "a".repeat(64), component: SAMPLE_COMPONENT, requests: [request("Remove the Reject button"), request("Stop partway")] };
    const turn = scriptPrototypeTurn({ ...base, instruction: "/prototype expense-web", feedback, doc: made() })!;
    expect(writes(turn.frames)).toEqual([`editFile ${sourcePath(SAMPLE_COMPONENT)}`]);
    expect(turn.files?.[sourcePath(SAMPLE_COMPONENT)]).not.toContain('id="btn.reject"');
    expect(turn.frames.at(-1)?.part).toMatchObject({ type: "turn-failed" });
    expect(turn.failure).toMatch(/request 2/);
  });
});
