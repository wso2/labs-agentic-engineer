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
import { connectionTail, settledLabel } from "./feedTail";

describe("connectionTail", () => {
  it("speaks only while the stream is not carrying anything", () => {
    expect(connectionTail("connecting")).toBe("Attaching to the run feed…");
    expect(connectionTail("reconnecting")).toBe(
      "Connection lost — reconnecting…",
    );
  });

  // Silence is the healthy state. `idle` especially: a surface nobody opened
  // never attached to anything, and saying "attaching" there is the bug the
  // phase was introduced to kill.
  it("says nothing about a live, ended, or never-opened stream", () => {
    expect(connectionTail("live")).toBeUndefined();
    expect(connectionTail("ended")).toBeUndefined();
    expect(connectionTail("idle")).toBeUndefined();
  });
});

describe("settledLabel", () => {
  // The words are the run status chip's, not the wire's. `run settled —
  // succeeded` put the stream contract's own term in front of a reader and
  // left the state in its lower-case wire spelling.
  it("names the ending the way the rest of the page names it", () => {
    expect(settledLabel("succeeded")).toBe("Run finished successfully");
    expect(settledLabel("failed")).toBe("Run failed");
    expect(settledLabel("cancelled")).toBe("Run cancelled");
    expect(settledLabel("blocked")).toBe("Run blocked");
  });

  // A run row that carried no state still ended, so the header has to say
  // something — just nothing it cannot back up.
  it("still closes the section when the run named no state", () => {
    expect(settledLabel(undefined)).toBe("Run finished");
    expect(settledLabel("")).toBe("Run finished");
  });

  // Same rule as `runStatus`: a state nobody wrote a sentence for is rendered,
  // not swallowed. Hiding it would leave the section looking cleanly finished
  // when the platform said something the console does not understand.
  it("renders an unrecognised state rather than hiding it", () => {
    expect(settledLabel("timed-out")).toBe("Run ended — timed-out");
  });
});
