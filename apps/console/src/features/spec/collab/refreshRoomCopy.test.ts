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

import { describe, expect, it, vi } from "vitest";
import * as Y from "yjs";
import { refreshRoomCopy } from "./refreshRoomCopy";

const fetchSpecFileContent = vi.fn();
vi.mock("../api/queries", () => ({
  fetchSpecFileContent: (...args: unknown[]) => fetchSpecFileContent(...args),
}));

describe("refreshRoomCopy", () => {
  it("replaces the room's copy with HEAD's content", async () => {
    const doc = new Y.Doc();
    const ytext = doc.getText("specs/design/dependencies/dhl/dependency.json");
    ytext.insert(0, '{"name":"dhl"}');
    fetchSpecFileContent.mockResolvedValue({ content: '{"name":"dhl","contract":"openapi.yaml"}', sha: "new" });

    const refreshed = await refreshRoomCopy("proj1", () => ytext, "specs/design/dependencies/dhl/dependency.json");

    expect(refreshed).toBe(true);
    expect(fetchSpecFileContent).toHaveBeenCalledWith("proj1", { path: "specs/design/dependencies/dhl/dependency.json", sha: "" });
    expect(ytext.toString()).toBe('{"name":"dhl","contract":"openapi.yaml"}');
  });

  it("leaves a path the room does not carry alone — the pane reads git for it", async () => {
    fetchSpecFileContent.mockClear();
    const refreshed = await refreshRoomCopy("proj1", () => null, "specs/design/dependencies/dhl/openapi.yaml");
    expect(refreshed).toBe(false);
    expect(fetchSpecFileContent).not.toHaveBeenCalled();
  });
});
