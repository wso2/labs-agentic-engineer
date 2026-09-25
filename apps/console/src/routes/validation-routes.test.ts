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

// URL semantics only — the pages drag half the app behind them.
vi.mock("../features/validation/components/ValidationLedger", () => ({
  ValidationLedger: () => null,
}));
import { Route as validationIndexRoute } from "./projects.$projectName.validations.index";

/**
 * Validations is a ledger with a page per version, at `/validations` — plural,
 * like `/builds` and `/deployments` beside it. The singular `/validation` it
 * replaced is gone rather than redirected: nothing outside the console ever
 * linked to it, so the only links to break are a developer's own bookmarks.
 */
describe("/projects/$projectName/validations — the ledger", () => {
  // `?view=logs` toggled the old page between the report and the log. Both now
  // sit on the version page, so the param has nothing left to select and is
  // dropped rather than carried as dead state in every shared URL.
  it("drops the retired view param", () => {
    const parse = validationIndexRoute.options.validateSearch as
      | ((s: Record<string, unknown>) => unknown)
      | undefined;
    expect(parse?.({ view: "logs" })).toEqual({});
    expect(parse?.({ view: "report", other: 1 })).toEqual({});
  });
});
