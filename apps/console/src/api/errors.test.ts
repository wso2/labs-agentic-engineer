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
import { ApiRequestError, apiErrorCode, apiErrorMessage } from "./errors";

// Two servers answer the console: aep-api with its flat envelope
// {code, message}, and the org's AE Studio pods with problem+json
// {type, title, status, detail?, code}. One reader serves both.
describe("api errors", () => {
  it("reads problem+json from the pods and the flat envelope from aep-api", () => {
    expect(
      apiErrorMessage(
        { type: "about:blank", title: "Not Found", status: 404, detail: "no such file", code: "path_not_found" },
        "x",
      ),
    ).toBe("no such file");
    expect(apiErrorCode({ title: "Not Found", status: 404, code: "path_not_found" })).toBe("path_not_found");
    expect(apiErrorMessage({ code: "not_found", message: "nope" }, "x")).toBe("nope");
    expect(apiErrorMessage({ title: "Service Unavailable", status: 503, code: "disk_full" }, "x")).toBe(
      "Service Unavailable",
    );
  });

  it("falls back when the body carries neither shape", () => {
    expect(apiErrorMessage(undefined, "fallback")).toBe("fallback");
    expect(apiErrorMessage("<html>bad gateway</html>", "fallback")).toBe("fallback");
    expect(apiErrorCode({ message: "no code" })).toBeUndefined();
  });

  it("keeps a problem's code and detail on ApiRequestError", () => {
    const err = new ApiRequestError({ title: "Not Found", status: 404, detail: "gone", code: "ref_not_found" }, "x");
    expect(err.message).toBe("gone");
    expect(err.code).toBe("ref_not_found");
  });
});
