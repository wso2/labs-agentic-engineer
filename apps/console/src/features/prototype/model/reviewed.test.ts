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

import { afterEach, describe, expect, it, vi } from "vitest";
import { markReviewed, subscribeReviewed, unreviewed } from "./reviewed";

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("reviewed revisions, per browser", () => {
  it("remembers the revision a review showed, per project and component", () => {
    markReviewed("acme", "expense-web", "h1");
    expect(unreviewed("acme", { "expense-web": "h1", "other-web": "h1" })).toEqual(["other-web"]);
    expect(unreviewed("beta", { "expense-web": "h1" })).toEqual(["expense-web"]);
  });

  it("lists the components whose current revision was not reviewed", () => {
    markReviewed("acme", "a", "h1");
    markReviewed("acme", "b", "old");
    expect(unreviewed("acme", { a: "h1", b: "new", c: "h3" })).toEqual(["b", "c"]);
  });

  it("tells subscribers when one is recorded, and not when it is unchanged", () => {
    const fn = vi.fn();
    const off = subscribeReviewed(fn);
    markReviewed("acme", "a", "h1");
    markReviewed("acme", "a", "h1");
    expect(fn).toHaveBeenCalledTimes(1);
    off();
    markReviewed("acme", "a", "h2");
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it("survives storage that throws: nothing is remembered, nothing breaks", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    expect(() => markReviewed("acme", "a", "h1")).not.toThrow();
    expect(unreviewed("acme", { a: "h1" })).toEqual(["a"]);
  });

  it("ignores stored junk", () => {
    localStorage.setItem("aep:prototype-reviewed:acme", "[1,2]");
    expect(unreviewed("acme", { a: "h1" })).toEqual(["a"]);
    localStorage.setItem("aep:prototype-reviewed:acme", "{not json");
    expect(unreviewed("acme", { a: "h1" })).toEqual(["a"]);
  });
});
