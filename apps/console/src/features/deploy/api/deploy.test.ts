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

import { beforeEach, describe, expect, it, vi } from "vitest";

// The design's components as the Deploy Page and the Issue card read them: a
// project with no design yet has none, which aep-api answers with a 404.

const get = vi.fn<(path: string, init: Record<string, unknown>) => Promise<unknown>>();
vi.mock("../../../api/client", () => ({
  client: { GET: (path: string, init: Record<string, unknown>) => get(path, init) },
}));

const { designDependencies } = await import("./deploy");

describe("designDependencies", () => {
  beforeEach(() => get.mockReset());

  it("lists every component of the design", async () => {
    const design = [{ componentName: "api", dependencies: [] }];
    get.mockResolvedValueOnce({ data: design, error: undefined, response: { status: 200 } });
    await expect(designDependencies("shop")).resolves.toEqual(design);
  });

  it("reads no design as no components", async () => {
    get.mockResolvedValueOnce({ data: undefined, error: { code: "not_found", message: "design not found" }, response: { status: 404 } });
    await expect(designDependencies("shop")).resolves.toEqual([]);
  });

  it("fails on a 404 that is not the design's absence", async () => {
    get.mockResolvedValueOnce({ data: undefined, error: { code: "route_not_found", message: "no such route" }, response: { status: 404 } });
    await expect(designDependencies("shop")).rejects.toThrow("no such route");
  });

  it("fails on any other refusal", async () => {
    get.mockResolvedValueOnce({ data: undefined, error: { code: "internal", message: "failed to read design dependencies" }, response: { status: 500 } });
    await expect(designDependencies("shop")).rejects.toThrow("failed to read design dependencies");
  });
});
