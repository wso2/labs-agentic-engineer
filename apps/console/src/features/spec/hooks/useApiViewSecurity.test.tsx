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

import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as Y from "yjs";

import { SECURITY_JSON_PATH } from "../api/designTree";
import type { SpecFileEntry } from "../api/mapping";
import type { ProjectRolesLiveState } from "../api/roles";
import type { CollabSpec } from "../collab/useCollabSpec";
import { EXPENSE_TRACKER_TEXT } from "../lib/securityTestFixtures";
import { useApiViewSecurity } from "./useApiViewSecurity";

const mockFiles = vi.fn();
const mockContent = vi.fn();
vi.mock("../api/queries", () => ({
  useSpecFiles: (...args: unknown[]) => mockFiles(...args),
  useSpecFileContent: (...args: unknown[]) => mockContent(...args),
}));

const mockRoles = vi.fn();
vi.mock("../api/roles", async () => {
  const actual = await vi.importActual<typeof import("../api/roles")>(
    "../api/roles",
  );
  return { ...actual, useProjectRoles: (...args: unknown[]) => mockRoles(...args) };
});

const FILE: SpecFileEntry = {
  path: SECURITY_JSON_PATH,
  sha: "abc",
  group: "designs",
} as SpecFileEntry;

const RESOURCE_SERVER = "https://aep.wso2.com/orgs/acme/projects/expense";

/** A room holding the given files, as a real `Y.Doc`. A read never creates. */
function room(files: Record<string, string>): CollabSpec {
  const doc = new Y.Doc();
  const map = doc.getMap<Y.Text>("files");
  for (const [path, content] of Object.entries(files)) {
    const ytext = new Y.Text();
    ytext.insert(0, content);
    map.set(path, ytext);
  }
  return {
    doc,
    getFileText: (path: string) => map.get(path) ?? null,
  } as unknown as CollabSpec;
}

function live(over: Partial<ProjectRolesLiveState> = {}): ProjectRolesLiveState {
  return {
    directoryAvailable: true,
    roles: [],
    projectRoles: [],
    testUsers: [],
    ...over,
  };
}

beforeEach(() => {
  mockFiles.mockReset();
  mockFiles.mockReturnValue({ data: [FILE] });
  mockContent.mockReset();
  mockContent.mockReturnValue({ data: undefined });
  mockRoles.mockReset();
  mockRoles.mockReturnValue({ data: undefined });
});

function run(over: { active?: boolean; collab?: CollabSpec } = {}) {
  return renderHook(() =>
    useApiViewSecurity({
      projectName: "p",
      active: over.active ?? true,
      ...(over.collab ? { collab: over.collab } : {}),
    }),
  );
}

describe("useApiViewSecurity — where the catalog is read from", () => {
  it("prefers the room's copy, and asks git for nothing", () => {
    const { result } = run({
      collab: room({ [SECURITY_JSON_PATH]: EXPENSE_TRACKER_TEXT }),
    });

    expect(result.current.roles?.["claims:read-all"]).toEqual(["Approver"]);
    expect(mockFiles).toHaveBeenLastCalledWith("p", false);
    expect(mockContent).toHaveBeenLastCalledWith("p", null);
  });

  it("falls back to the committed copy when there is no room", () => {
    mockContent.mockReturnValue({ data: { content: EXPENSE_TRACKER_TEXT } });
    const { result } = run();

    expect(mockFiles).toHaveBeenLastCalledWith("p", true);
    expect(mockContent).toHaveBeenLastCalledWith("p", FILE);
    expect(result.current.roles?.["claims:read-all"]).toEqual(["Approver"]);
  });

  it("asks for nothing at all while no contract is on screen", () => {
    run({
      active: false,
      collab: room({ [SECURITY_JSON_PATH]: EXPENSE_TRACKER_TEXT }),
    });

    expect(mockFiles).toHaveBeenLastCalledWith("p", false);
    expect(mockContent).toHaveBeenLastCalledWith("p", null);
    expect(mockRoles).toHaveBeenLastCalledWith("p", false);
  });

  // A project mid-design has no catalog, and the view must then render exactly
  // as it did before scopes existed rather than labelling every row.
  it("answers undefined when neither source has a readable document", () => {
    const { result } = run();
    expect(result.current.roles).toBeUndefined();
  });
});

describe("useApiViewSecurity — the audience", () => {
  it("names the project's resource server once the platform has one", () => {
    mockRoles.mockReturnValue({
      data: live({
        projectRoles: [
          { name: "Employee", resourceServer: "", assignedTo: [] },
          { name: "Approver", resourceServer: RESOURCE_SERVER, assignedTo: [] },
        ],
      }),
    });
    const { result } = run();

    expect(result.current.resourceServer).toBe(RESOURCE_SERVER);
  });

  it("says nothing before the first Build", () => {
    mockRoles.mockReturnValue({ data: live() });
    const { result } = run();

    expect(result.current.resourceServer).toBeUndefined();
  });
});
