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
import { parseLine } from "../model/ids";
import { seedSpecDoc } from "./specDoc";
import { readSpecLines } from "./useSpecLines";
import cases from "../../../../../../packages/contracts/requirements/inline-markup-cases.json";

// The shared inline-markup cases the Go reader is held to as well
// (services/aep-api/internal/platform/reqspec inline_test.go): a design's
// basis is compared with the console's by string equality, so a line's
// markup must read as the same words on both sides. Each case goes through a
// real room, as the console reads it.

const PATH = "specs/requirements/features/F1-greeting.md";

function roomLine(markdown: string) {
  const doc = new Y.Doc();
  seedSpecDoc(doc, { files: { [PATH]: `# Greeting\n\n## User Stories\n\n- ${markdown}\n` } });
  const items = (readSpecLines(doc).get(PATH) ?? []).filter((l) => l.kind === "listItem");
  expect(items).toHaveLength(1);
  return items[0]!;
}

describe("inline markup in the room, as inline-markup-cases.json says", () => {
  it.each(cases.cases.map((c) => [c.name, c] as const))("%s", (_name, c) => {
    const line = roomLine(c.markdown);
    expect(line.text).toBe(c.text);
    const parts = parseLine(line.text, line.emphasis);
    const tag = parts.assumed ? "assumed" : parts.blocking ? "blocking" : "";
    expect(tag).toBe("tag" in c ? c.tag : "");
  });
});
