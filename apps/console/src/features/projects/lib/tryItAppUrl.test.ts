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
import { tryItAppUrl } from "./tryItAppUrl";

const launch = {
  project: "small-call-triage",
  component: "incident-triage",
  issuer: "http://default-idp.amp.localhost:8080",
  clientId: "aep-dp-x-r-y",
  resource: "https://aep.wso2.com/orgs/default/projects/small-call-triage",
  scopes: ["openid", "triage:use"],
  endpoint: "http://default-default.openchoreoapis.localhost:19080/small-call-triage-incident-triage-http",
};

describe("tryItAppUrl", () => {
  it("puts every public coordinate in the hash query, encoded", () => {
    const url = new URL(tryItAppUrl("http://tryit.aep.localhost:8095", launch));
    expect(url.origin).toBe("http://tryit.aep.localhost:8095");
    expect(url.hash.startsWith("#/agent?")).toBe(true);
    const query = new URLSearchParams(url.hash.slice("#/agent?".length));
    expect(query.get("project")).toBe("small-call-triage");
    expect(query.get("component")).toBe("incident-triage");
    expect(query.get("issuer")).toBe(launch.issuer);
    expect(query.get("client_id")).toBe("aep-dp-x-r-y");
    expect(query.get("resource")).toBe(launch.resource);
    expect(query.get("scopes")).toBe("openid triage:use");
    expect(query.get("endpoint")).toBe(launch.endpoint);
  });

  it("strips a trailing slash from the base so the hash route is exact", () => {
    expect(tryItAppUrl("http://tryit.aep.localhost:8095/", launch)).toMatch(
      /^http:\/\/tryit\.aep\.localhost:8095\/#\/agent\?/,
    );
  });
});
