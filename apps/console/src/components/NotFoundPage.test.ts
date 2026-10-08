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
import { projectNamed } from "./NotFoundPage";

describe("projectNamed", () => {
  it("names the project an old project address points into", () => {
    expect(projectNamed("/projects/acme-expenses/builds/14")).toBe("acme-expenses");
    expect(projectNamed("/projects/acme-expenses/tasks/")).toBe("acme-expenses");
  });

  it("names none for org addresses and New project", () => {
    expect(projectNamed("/marketplace")).toBeNull();
    expect(projectNamed("/settings/credentials")).toBeNull();
    expect(projectNamed("/projects/new/x")).toBeNull();
  });
});
