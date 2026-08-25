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
import { startLineOf } from "./startLine";

describe("startLineOf — the transcript's opening line", () => {
  // The ordinary case since #562: the platform fires the kickoff and the
  // server attaches the idea it read from the project descriptor.
  it("splits the command from the user's own words", () => {
    expect(startLineOf("/start employees submit expense claims")).toEqual({
      idea: "employees submit expense claims",
    });
  });

  // A project created with no prompt at all — the command still renders as a
  // command, with nothing beside it.
  it("recognises a bare command", () => {
    expect(startLineOf("/start")).toEqual({ idea: "" });
  });

  it("survives the whitespace a multi-line idea brings", () => {
    expect(startLineOf("  /start\n  a rota planner\n  for nurses  ")).toEqual({
      idea: "a rota planner\n  for nurses",
    });
  });

  // Anything unrecognised renders as ordinary text, which is always safe —
  // this is not a second copy of the server's command grammar.
  it("leaves ordinary prose alone", () => {
    expect(startLineOf("what did we decide about /start?")).toBeNull();
    expect(startLineOf("/design")).toBeNull();
  });

  // The boundary is load-bearing: without it a longer token beginning with the
  // command would render as the command plus a nonsense idea.
  it("requires a boundary after the token", () => {
    expect(startLineOf("/started")).toBeNull();
  });
});
