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
import { markdownToNode } from "@aep/collab-doc";
import { lineWords } from "../model/designWork";
import { docLines } from "./docLines";

// A hard break (`··` or `\` at a line's end) is a node of its own in the
// room, not text. A browser edit turns a soft-wrapped line's wrap into one, so
// it must read as the whitespace the markdown means: aep-api's reader joins
// the wrapped line with a space, and a design's basis is compared by string.

function item(markdown: string) {
  const doc = markdownToNode(`## User Stories\n\n- ${markdown}\n`);
  return { doc, line: docLines(doc).find((l) => l.kind === "listItem")! };
}

describe("docLines", () => {
  it.each([
    ["two trailing spaces", "F1.1 receive a  \ndefault greeting"],
    ["a trailing backslash", "F1.1 receive a\\\ndefault greeting"],
  ])("reads a hard break (%s) as a space", (_name, markdown) => {
    const { line } = item(markdown);
    expect(lineWords(line)).toBe("F1.1 receive a default greeting");
  });

  it("maps an offset after a hard break to its document position", () => {
    const { doc, line } = item("F1.1 receive a  \ndefault greeting");
    const offset = line.text.indexOf("default");
    const pos = line.posAt(offset);
    expect(doc.textBetween(pos, pos + "default greeting".length)).toBe("default greeting");
    expect(doc.textBetween(line.posAt(0), line.posAt("F1.1".length))).toBe("F1.1");
  });
});
