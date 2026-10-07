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

import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import * as Y from "yjs";
import { setDocFile } from "@aep/collab-doc";
import { useRoomFiles } from "./useRoomFiles";

const PREFIXES = ["specs/design/"];

function seeded(files: Record<string, string>): Y.Doc {
  const doc = new Y.Doc();
  for (const [path, text] of Object.entries(files)) setDocFile(doc, path, text, "seed");
  return doc;
}

describe("useRoomFiles", () => {
  it("reads a doc that arrives already seeded, after none", () => {
    const { result, rerender } = renderHook(({ doc }: { doc: Y.Doc | null }) => useRoomFiles(doc, PREFIXES), {
      initialProps: { doc: null as Y.Doc | null },
    });
    expect(result.current).toEqual({});
    rerender({ doc: seeded({ "specs/design/a.json": "{}", "specs/requirements/prd.md": "# PRD" }) });
    expect(result.current).toEqual({ "specs/design/a.json": "{}" });
  });

  it("follows the room as it changes", () => {
    const doc = seeded({});
    const { result } = renderHook(() => useRoomFiles(doc, PREFIXES));
    act(() => setDocFile(doc, "specs/design/b.json", "[]", "agent"));
    expect(result.current).toEqual({ "specs/design/b.json": "[]" });
  });
});
