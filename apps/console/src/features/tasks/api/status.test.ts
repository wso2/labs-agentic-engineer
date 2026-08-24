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
import { issueKindChip, issueStateChip } from "./status";

describe("issueStateChip", () => {
  it("renders the two-value derived status", () => {
    expect(issueStateChip("pending")).toEqual({ label: "Open", tone: "neutral" });
    expect(issueStateChip("merged")).toEqual({ label: "Done", tone: "success" });
  });

  // An unknown status is a contract the console has fallen behind, and hiding
  // it would make a row look finished when nobody knows what it is.
  it("shows an unknown status rather than hiding it", () => {
    expect(issueStateChip("on_hold")).toEqual({ label: "on_hold", tone: "error" });
  });
});

describe("issueKindChip", () => {
  // Each of these changes how the row should be read: unplanned work the
  // version picked up, a pull request waiting on a rebase, and a gate the
  // PLATFORM works rather than the agent.
  it("tags the kinds that change what a row means", () => {
    expect(issueKindChip("bug")?.label).toBe("Defect");
    expect(issueKindChip("conflict")?.label).toBe("Conflict");
    expect(issueKindChip("provision")?.label).toBe("Provisioning");
  });

  // Planned work is the majority of any version's list, so tagging it would be
  // noise. The untagged row IS planned work.
  it("leaves planned work untagged", () => {
    expect(issueKindChip("development")).toBeNull();
  });

  // The rule that lets the platform add a kind without shipping a console
  // change first, and that keeps a ledger issue (which carries no kind at all)
  // from being labelled something it is not.
  it("renders an unknown or absent kind untagged rather than guessing", () => {
    expect(issueKindChip("something-new")).toBeNull();
    expect(issueKindChip(undefined)).toBeNull();
    expect(issueKindChip("")).toBeNull();
  });
});
