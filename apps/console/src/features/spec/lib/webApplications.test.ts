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
import { webApplicationsOf } from "./webApplications";

describe("webApplicationsOf", () => {
  it("names every component the cell declares as a web-application, in cell order", () => {
    const cell = [
      "title Shop",
      "component storefront as \"Storefront\" web-application",
      "component catalog-api service",
      "component admin-portal web-application",
    ].join("\n");
    expect(webApplicationsOf(cell)).toEqual(["storefront", "admin-portal"]);
  });

  it("is empty for a cell with no web-application", () => {
    expect(webApplicationsOf("component api service\ncomponent db database\n")).toEqual([]);
  });

  it("is empty with no cell at all", () => {
    expect(webApplicationsOf(null)).toEqual([]);
    expect(webApplicationsOf("")).toEqual([]);
  });

  // The canonical kind only: the design and prototype skills both key on
  // `web-application`, so an alias would show a stage the agent then refuses.
  it("does not count a non-canonical spelling", () => {
    expect(webApplicationsOf("component web webapp\ncomponent web2 web-app\n")).toEqual([]);
  });

  // A cell mid-stream carries a half-written last line; the lines above it
  // still say what they say.
  it("reads a partial cell", () => {
    expect(webApplicationsOf("component web web-application\ncomponent api ->")).toEqual(["web"]);
  });

  it("spans every cell of a multi-cell source", () => {
    const src = 'cell a {\n  component web web-application\n}\ncell b {\n  component portal web-application\n}\n';
    expect(webApplicationsOf(src)).toEqual(["web", "portal"]);
  });
});
