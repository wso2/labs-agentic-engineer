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
import { gridView } from "./grid";

const p = (name: string) => ({ name });

describe("gridView", () => {
  it("says an org with no projects is empty, and a search that found none matched none", () => {
    expect(gridView([{ items: [] }], "", false)).toEqual({ kind: "empty" });
    expect(gridView([{ items: null }], "xero", false)).toEqual({ kind: "no-match", search: "xero" });
  });

  it("lists every page read so far, in order, and says whether there are more", () => {
    expect(gridView([{ items: [p("a"), p("b")] }, { items: [p("c")] }], "", true)).toEqual({
      kind: "list",
      projects: [p("a"), p("b"), p("c")],
      more: true,
      count: null,
    });
  });

  it("keeps asking while a search's pages so far matched nothing but more remain", () => {
    expect(gridView([{ items: [] }], "xero", true)).toEqual({ kind: "list", projects: [], more: true, count: null });
  });

  it("counts the org's projects only when all are read and no search narrows them", () => {
    expect(gridView([{ items: [p("a"), p("b")] }], "", false)).toMatchObject({ count: 2 });
    expect(gridView([{ items: [p("a")] }], "a", false)).toMatchObject({ count: null });
  });
});
