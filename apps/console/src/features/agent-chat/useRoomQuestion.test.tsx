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

// The log→room mirror the spec body's question form hangs off. The #485
// live-testing round made SpecView seed the log itself (useChatLogBootstrap),
// so this pins the seam that fix relies on: a question landing in the chat
// store — by any writer, at any time — surfaces as the room's pending entry.
//
// Rendered through a probe COMPONENT (`render`), not `renderHook`: this repo's
// `renderHook` hangs on this particular hook (reproduced in isolation — a
// harness/React-19 interaction, not a hook defect; the plain Yjs mirror/observe
// path and a `render`-based probe both settle immediately). Tracked as a
// harness quirk, not a behavior this suite needs to characterize.

import { describe, expect, it } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach } from "vitest";
import * as Y from "yjs";
import { chatKeyFor, replaceMessages } from "./chatStore";
import { useRoomQuestion } from "./useRoomQuestion";

let n = 0;
function freshKey(): string {
  n += 1;
  return chatKeyFor("acme", `room-${n}`);
}

function Probe({ doc, chatKey }: { doc: Y.Doc; chatKey: string }) {
  const entry = useRoomQuestion(doc, chatKey);
  return <div data-testid="entry">{entry ? entry.toolCallId : ""}</div>;
}

const questionMessage = (toolCallId: string) => ({
  id: `q-${toolCallId}`,
  role: "question" as const,
  turnId: "history",
  toolCallId,
  questions: [{ question: "Who signs in?", options: [{ label: "Anyone" }] }],
});

afterEach(cleanup);

describe("useRoomQuestion", () => {
  it("mirrors a question already in the log when the room doc arrives", () => {
    const key = freshKey();
    replaceMessages(key, [questionMessage("tc-pre")]);

    render(<Probe doc={new Y.Doc()} chatKey={key} />);

    expect(screen.getByTestId("entry")).toHaveTextContent("tc-pre");
  });

  it("mirrors a question written into the log AFTER mount (the bootstrap path)", () => {
    const key = freshKey();
    render(<Probe doc={new Y.Doc()} chatKey={key} />);
    expect(screen.getByTestId("entry")).toHaveTextContent("");

    act(() => {
      replaceMessages(key, [questionMessage("tc-late")]);
    });

    expect(screen.getByTestId("entry")).toHaveTextContent("tc-late");
  });

  it("does not surface a question a later delivered user message superseded", () => {
    const key = freshKey();
    replaceMessages(key, [
      questionMessage("tc-old"),
      {
        id: "u1",
        role: "user" as const,
        content: "answered in the composer",
        status: "completed" as const,
      },
    ]);

    render(<Probe doc={new Y.Doc()} chatKey={key} />);

    expect(screen.getByTestId("entry")).toHaveTextContent("");
  });
});
