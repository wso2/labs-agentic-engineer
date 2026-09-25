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
import { tokenizeStep } from "./tokenize.js";

const joined = (text: string) => tokenizeStep(text).map((t) => t.text).join("");
const emphasised = (text: string) =>
  tokenizeStep(text).filter((t) => t.emphasis).map((t) => t.text);

describe("tokenizeStep", () => {
  // The whole point is emphasis, not rewriting: anything that did not survive
  // reassembly would be a step the reader is quietly shown wrong.
  it("is lossless — the pieces reassemble into the original", () => {
    for (const text of [
      'Priya adds "Milk" with quantity "2" to the list',
      "the list still has exactly 3 items",
      'the client posts grant type "<grant>" to the token endpoint',
      "no literals here at all",
      "",
      '"leading literal" then words',
      'trailing literal "here"',
    ]) {
      expect(joined(text)).toBe(text);
    }
  });

  it("emphasises the values that make a step falsifiable", () => {
    expect(emphasised('Priya adds "Milk" with quantity "2" to the list')).toEqual([
      '"Milk"',
      '"2"',
    ]);
    expect(emphasised("the list still has exactly 3 items")).toEqual(["3"]);
    expect(emphasised("the response status should be <status>")).toEqual(["<status>"]);
  });

  it("leaves a step with no literals as one piece", () => {
    const tokens = tokenizeStep("she views her todo list");
    expect(tokens).toHaveLength(1);
    expect(tokens[0]?.emphasis).toBe(false);
  });

  it("does not mistake a word for a number, or an apostrophe for a quote", () => {
    expect(emphasised("the round is closed after 3 hours, not 3rd")).toEqual(["3"]);
    expect(emphasised("Priya's item is unchanged")).toEqual([]);
  });

  it("has nothing to say about an empty step", () => {
    expect(tokenizeStep("")).toEqual([]);
  });
});
