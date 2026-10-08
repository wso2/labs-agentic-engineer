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

import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { skillEditorExtensions } from "./skillEditorExtensions";

// A skill's body goes through the editor and back on every edit, so what it
// numbers must come back numbered the same.

let editor: Editor | null = null;
afterEach(() => editor?.destroy());

function roundTrip(markdown: string): string {
  editor = new Editor({ extensions: skillEditorExtensions, content: markdown, contentType: "markdown" });
  return editor.getMarkdown();
}

describe("the skill editor's ordered lists", () => {
  it("keeps a list that counts from 0", () => {
    expect(roundTrip("0. Read the spec\n1. Write the tests\n")).toBe("0. Read the spec\n1. Write the tests");
  });

  it("keeps a list that starts further on, and an ordinary one", () => {
    expect(roundTrip("5. five\n6. six\n")).toBe("5. five\n6. six");
    expect(roundTrip("1. a\n2. b\n")).toBe("1. a\n2. b");
  });
});
