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
import { validateBuildsSearch } from "./projects.$projectName.builds.index";

describe("validateBuildsSearch", () => {
  it("keeps the validated tag and open configuration deep link", () => {
    expect(validateBuildsSearch({ tag: "v2", connections: "open" })).toEqual({
      tag: "v2",
      connections: "open",
    });
  });

  // Anything that is not `open` names a RESOURCE whose dialog should open, so
  // the value cannot be validated against a fixed set here — an unknown name
  // degrades to an expanded section inside the component, which beats erroring
  // the route on a link a colleague was sent.
  it("keeps a named resource as the configuration deep link", () => {
    expect(validateBuildsSearch({ connections: "email-provider" })).toEqual({
      connections: "email-provider",
    });
  });

  it("drops a configuration deep link that names nothing", () => {
    expect(validateBuildsSearch({ connections: "" })).toEqual({});
    expect(validateBuildsSearch({ connections: 7 })).toEqual({});
  });
});
