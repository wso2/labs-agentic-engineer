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
import { MIXED_CELL_MODE_DIAGNOSTIC_CODE, MIXED_CELL_MODE_MESSAGE, parseProject } from "./parseProject";

describe("parseProject", () => {
  it("parses a single implicit cell (backward compatible)", () => {
    const result = parseProject(`component API service\nnorth -> API`);
    expect(result.diagnostics).toEqual([]);
    expect(result.project.title).toBeUndefined();
    expect(result.project.cells).toHaveLength(1);
    expect(result.project.cells[0].id).toBe("main");
    expect(result.project.cells[0].document.components.map((c) => c.id)).toEqual(["API"]);
    expect(result.project.crossEdges).toEqual([]);
  });

  it("parses two cells, a project title, and an inline cross edge", () => {
    const source = [
      "title Commerce",
      "cell orders {",
      "  component api",
      "  api -> east products.api : get stock",
      "}",
      "cell products {",
      "  component api",
      "}"
    ].join("\n");
    const result = parseProject(source);
    expect(result.diagnostics).toEqual([]);
    expect(result.project.title).toBe("Commerce");
    expect(result.project.cells.map((c) => c.id)).toEqual(["orders", "products"]);
    expect(result.project.crossEdges).toEqual([
      {
        id: "cross-orders-api-products-api-4",
        sourceCell: "orders",
        sourceComp: "api",
        targetCell: "products",
        targetComp: "api",
        exit: "east",
        entry: "west",
        mode: "connected",
        label: "get stock",
        line: 4
      }
    ]);
  });

  it("stamps the owning cell on an inline cross edge with unqualified source", () => {
    const source = "cell a {\n  x -> b.y\n}\ncell b {\n  component y\n}";
    const result = parseProject(source);
    expect(result.project.crossEdges[0]).toMatchObject({ sourceCell: "a", sourceComp: "x", targetCell: "b", targetComp: "y", mode: "connected" });
  });

  it("parses a project-level cross edge outside blocks", () => {
    const source = "cell a {\n  component x\n}\ncell b {\n  component y\n}\na.x -> south-north b.y";
    const result = parseProject(source);
    expect(result.project.crossEdges[0]).toMatchObject({ sourceCell: "a", sourceComp: "x", targetCell: "b", targetComp: "y", exit: "south", entry: "north", mode: "decoupled" });
  });

  it("flags a bare-south cross edge", () => {
    const source = "cell a {\n  x -> south b.y\n}\ncell b {\n  component y\n}";
    const result = parseProject(source);
    expect(result.diagnostics.some((d) => /south/i.test(d.message))).toBe(true);
  });

  it("flags top-level component statements mixed with cell blocks", () => {
    const source = "component loose\ncell a {\n  component x\n}";
    const result = parseProject(source);
    expect(result.diagnostics).toContainEqual({
      severity: "error",
      code: MIXED_CELL_MODE_DIAGNOSTIC_CODE,
      message: MIXED_CELL_MODE_MESSAGE,
      line: 1,
      column: 1
    });
  });

  it("marks an unqualified source on a top-level cross edge as convertible mixed DSL", () => {
    const source = "cell a {\n  component x\n}\nx -> b.y";
    const result = parseProject(source);
    expect(result.diagnostics[0]).toMatchObject({ code: MIXED_CELL_MODE_DIAGNOSTIC_CODE, line: 4 });
    expect(result.project.crossEdges).toEqual([]);
  });

  it("keeps the generic outside-cell diagnostic for malformed loose syntax", () => {
    const result = parseProject("not valid DSL\ncell a {\n  component x\n}");
    expect(result.diagnostics[0]).toMatchObject({
      message: "Only `title`, comments, and cross-cell edges are allowed outside a cell block.",
      line: 1
    });
    expect(result.diagnostics[0].code).toBeUndefined();
  });

  it("flags a top-level bare-south cross edge", () => {
    const source = "cell a {\n  component x\n}\ncell b {\n  component y\n}\na.x -> south b.y";
    const result = parseProject(source);
    expect(result.diagnostics.some((d) => /south/i.test(d.message))).toBe(true);
    expect(result.project.crossEdges).toEqual([]);
  });

  it("flags a bad-token in-block cross edge", () => {
    const source = "cell a {\n  x -> west b.y\n}\ncell b {\n  component y\n}";
    const result = parseProject(source);
    expect(result.diagnostics.some((d) => /exit must be east or south/i.test(d.message))).toBe(true);
  });

  it("ignores a top-level comment line", () => {
    const source = "# a comment\ncell a {\n  component x\n}";
    const result = parseProject(source);
    expect(result.diagnostics).toEqual([]);
  });

  it("treats an empty title as undefined", () => {
    const source = "title \ncell a {\n  component x\n}";
    const result = parseProject(source);
    expect(result.project.title).toBeUndefined();
  });

  it("preserves original line numbers across blank lines for an implicit single cell", () => {
    const source = "component API service\n\nnorth -> API\n\ncomponent Extra\n\nAPI -> Extra";
    const result = parseProject(source);
    expect(result.diagnostics).toEqual([]);
    expect(result.project.cells[0].document.edges.map((e) => e.line)).toEqual([3, 7]);
  });

  it("preserves original line numbers across blank lines inside a cell block", () => {
    const source = [
      "title Commerce",       // line 1
      "",                     // line 2
      "cell orders {",        // line 3
      "  component api",      // line 4
      "",                     // line 5
      "  api -> odb",         // line 6
      "}"                     // line 7
    ].join("\n");
    const result = parseProject(source);
    expect(result.diagnostics).toEqual([]);
    expect(result.project.cells[0].document.edges.map((e) => e.line)).toEqual([6]);
  });

  it("skips a leading YAML frontmatter block in explicit cell mode, preserving line numbers", () => {
    const source = [
      "---",                  // line 1 — the platform's sourceSpec carrier
      "sourceSpec: v1",       // line 2
      "---",                  // line 3
      "title Commerce",       // line 4
      "cell orders {",        // line 5
      "  component api",      // line 6
      "  api -> odb",         // line 7
      "}"                     // line 8
    ].join("\n");
    const result = parseProject(source);
    expect(result.diagnostics).toEqual([]);
    expect(result.project.cells[0].document.edges.map((e) => e.line)).toEqual([7]);
  });
});
