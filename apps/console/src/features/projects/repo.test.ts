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
import { repoLabel } from "./repo";

describe("repoLabel", () => {
  it("names owner/repo and links the repository's page, without .git", () => {
    expect(repoLabel("https://github.com/acme/acme-expenses.git")).toEqual({
      href: "https://github.com/acme/acme-expenses",
      short: "acme/acme-expenses",
    });
  });

  it("reads a URL without .git or with a trailing slash the same way", () => {
    expect(repoLabel("https://github.com/acme/triage-agent/")?.short).toBe("acme/triage-agent");
  });

  it("is null when there is no repository to name", () => {
    expect(repoLabel(undefined)).toBeNull();
    expect(repoLabel("")).toBeNull();
    expect(repoLabel("not a url")).toBeNull();
    expect(repoLabel("https://github.com/")).toBeNull();
  });
});
