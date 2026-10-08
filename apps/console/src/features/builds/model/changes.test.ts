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
import { seedSpecDoc } from "../../spec/collab/specDoc";
import { readSpecLines } from "../../spec/collab/useSpecLines";
import type { LineBlock } from "../../spec/model/ids";
import { builtLines, changeWords, featureChanges, lastBuildOf, lineChanges } from "./changes";

// The shared fixture the Go reader is held to as well
// (services/aep-api/internal/platform/reqspec FeatureLines): the lines a build
// keeps of each feature must read the same on both sides, or every feature
// would show as changed since it was built.
const FIXTURE = "../../../../../../packages/contracts/requirements/acme-expenses/";
const raw = import.meta.glob<string>("../../../../../../packages/contracts/requirements/acme-expenses/**/*.{md,json}", {
  query: "?raw",
  import: "default",
  eager: true,
});

describe("builtLines on the shared fixture", () => {
  it("keeps each feature's lines as feature-lines.json says", () => {
    const files = Object.fromEntries(
      Object.entries(raw)
        .filter(([path]) => path.endsWith(".md"))
        .map(([path, content]) => [`specs/requirements/${path.slice(FIXTURE.length)}`, content]),
    );
    const doc = new Y.Doc();
    seedSpecDoc(doc, { files });
    const lines = readSpecLines(doc);
    const expected = JSON.parse(raw[`${FIXTURE}feature-lines.json`]!) as Record<string, { id?: string; words: string }[]>;
    const got = Object.fromEntries(
      [...lines.keys()]
        .flatMap((path) => {
          const m = /^specs\/requirements\/features\/(F\d+)-/.exec(path);
          return m ? [[m[1]!, builtLines(lines.get(path) ?? [])] as const] : [];
        })
        .sort(([a], [b]) => a.localeCompare(b)),
    );
    const want = Object.fromEntries(
      Object.entries(expected).map(([id, ls]) => [id, ls.map((l) => ({ id: l.id ?? null, words: l.words }))]),
    );
    expect(got).toEqual(want);
  });
});

const li = (text: string, emphasis: LineBlock["emphasis"] = []): LineBlock => ({ kind: "listItem", text, emphasis });
const h2 = (text: string): LineBlock => ({ kind: "heading", level: 2, text, emphasis: [] });

const assumed = "F2.4 A deputy approves in my place. assumed";
const tag = { start: assumed.indexOf("assumed"), end: assumed.length };

const v1 = builtLines([
  h2("User Stories"),
  li("F2.1 I see pending claims."),
  li("F2.2 I approve with a reason."),
  li(assumed, [tag]),
  h2("Decisions"),
  li("A claim is approved by the line manager."),
]);

describe("builtLines", () => {
  it("keeps each line's own ID and words, without headings or the assumed tag", () => {
    const lines = builtLines([h2("User Stories"), li(assumed, [tag])]);
    expect(lines).toEqual([{ id: "F2.4", words: "F2.4 A deputy approves in my place." }]);
  });
});

describe("lineChanges since the last build", () => {
  it("is nothing when the spec reads the same, a confirmed assumption included", () => {
    const now = builtLines([
      li("F2.1 I see pending claims."),
      li("F2.2 I approve with a reason."),
      li("F2.4 A deputy approves in my place."),
      li("A claim is approved by the line manager."),
    ]);
    expect(lineChanges(v1, now)).toEqual({ added: 0, edited: 0, retired: 0 });
    expect(changeWords(lineChanges(v1, now))).toBe("");
  });

  it("follows a story by its ID: reworded is edited, gone is retired, new is added", () => {
    const now = builtLines([
      li("F2.1 I see pending claims, oldest first."),
      li("F2.2 I approve with a reason."),
      li("F2.5 Finance gives a second approval."),
      li("A claim is approved by the line manager."),
    ]);
    expect(lineChanges(v1, now)).toEqual({ added: 1, edited: 1, retired: 1 });
    expect(changeWords(lineChanges(v1, now))).toBe("1 added, 1 edited, 1 retired");
  });

  it("knows a line without an ID by its words: rewording it retires one and adds one", () => {
    const now = builtLines([
      li("F2.1 I see pending claims."),
      li("F2.2 I approve with a reason."),
      li(assumed, [tag]),
      li("A claim is approved by the employee's line manager."),
    ]);
    expect(changeWords(lineChanges(v1, now))).toBe("1 added, 1 retired");
  });
});

describe("featureChanges", () => {
  const line = (words: string, id: string | null = null) => ({ id, words });
  it("names each change: an edit with the words it was built with, an added and a retired line", () => {
    const built = [line("F2.1 I see pending claims.", "F2.1"), line("F2.2 I approve.", "F2.2"), line("Managers approve.")];
    const now = [line("F2.1 I see pending claims, oldest first.", "F2.1"), line("Finance approves too."), line("Managers approve.")];
    expect(featureChanges(built, now)).toEqual({
      added: [line("Finance approves too.")],
      edited: [{ id: "F2.1", was: "F2.1 I see pending claims.", now: "F2.1 I see pending claims, oldest first." }],
      retired: [line("F2.2 I approve.", "F2.2")],
    });
  });

  it("finds the build that last carried a feature", () => {
    const build = (version: string, ids: string[]) => ({
      version,
      status: "built" as const,
      features: ids.map((id) => ({ id, name: id, lines: [line(`${id} v${version}`)] })),
      productWide: [],
    });
    const builds = [build("v1", ["F1", "F2"]), build("v2", ["F2"]), build("v3", ["F3"])];
    expect(lastBuildOf(builds, "F2")?.version).toBe("v2");
    expect(lastBuildOf(builds, "F1")?.version).toBe("v1");
    expect(lastBuildOf(builds, "F4")).toBeNull();
  });
});
