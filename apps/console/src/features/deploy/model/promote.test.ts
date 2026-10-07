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
import type { EnvironmentColumn } from "./pipeline";
import { promoteView } from "./promote";

const column = (over: Partial<EnvironmentColumn>): EnvironmentColumn => ({
  name: "development",
  label: "Development",
  entry: true,
  version: "v2",
  running: true,
  state: { label: "Running", tone: "success" },
  components: [],
  dependencies: [],
  next: { name: "staging", label: "Staging" },
  ...over,
});

const staging = (ready: boolean) =>
  column({
    name: "staging",
    label: "Staging",
    entry: false,
    version: null,
    running: false,
    next: { name: "production", label: "Production" },
    dependencies: [
      { name: "Xero", state: ready ? "configured" : "unset", ready, label: "" },
      { name: "Stripe", state: "configured", ready: true, label: "" },
    ],
  });

describe("promoteView", () => {
  it("names the version and where it would go, and says promoting isn't available yet", () => {
    expect(promoteView(column({}), staging(true))).toEqual({
      label: "Promote v2 to Staging",
      reason: "Promoting isn't available yet.",
      warning: null,
    });
  });

  it("warns, without blocking, when the next environment lacks dependency values", () => {
    expect(promoteView(column({}), staging(false))?.warning).toBe("Staging has no values yet for Xero.");
  });

  it("says there is nothing to promote where nothing runs", () => {
    const empty = column({ version: null, running: false });
    expect(promoteView(empty, staging(false))).toEqual({
      label: "Promote to Staging",
      reason: "Nothing runs in Development to promote.",
      warning: null,
    });
  });

  it("offers nothing on the last environment", () => {
    expect(promoteView(column({ next: null }), undefined)).toBeNull();
  });
});
