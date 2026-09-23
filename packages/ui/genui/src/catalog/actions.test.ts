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
import { dispatchGenUiAction } from "./actions.js";

describe("dispatchGenUiAction", () => {
  it("calls the handler with schema-parsed params", async () => {
    const approveDependency = vi.fn();
    const outcome = await dispatchGenUiAction(
      { approveDependency },
      "approveDependency",
      { dependencyId: "payments-db" },
    );
    expect(outcome).toEqual({ status: "handled", action: "approveDependency" });
    expect(approveDependency).toHaveBeenCalledWith({ dependencyId: "payments-db" });
  });

  it("rejects an action the catalog does not declare", async () => {
    const outcome = await dispatchGenUiAction({}, "deleteProject", {});
    expect(outcome).toEqual({ status: "unknown-action", action: "deleteProject" });
  });

  it("rejects params that fail the action schema without calling the handler", async () => {
    const openTask = vi.fn();
    const outcome = await dispatchGenUiAction({ openTask }, "openTask", {
      taskNumber: "43",
    });
    expect(outcome.status).toBe("invalid-params");
    expect(outcome.status === "invalid-params" && outcome.issues[0]).toMatch(
      /^taskNumber: /,
    );
    expect(openTask).not.toHaveBeenCalled();
  });

  it("reports a declared action the host did not handle", async () => {
    const outcome = await dispatchGenUiAction({}, "openTask", { taskNumber: 1 });
    expect(outcome).toEqual({ status: "unhandled", action: "openTask" });
  });

  it("reports a handler that throws instead of rejecting", async () => {
    const error = new Error("boom");
    const outcome = await dispatchGenUiAction(
      {
        openTask: () => {
          throw error;
        },
      },
      "openTask",
      { taskNumber: 1 },
    );
    expect(outcome).toEqual({ status: "failed", action: "openTask", error });
  });
});
