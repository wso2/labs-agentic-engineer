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

import { beforeEach, describe, expect, it } from "vitest";
import { endpointAllowed, loadLaunch, parseLaunch, saveLaunch } from "./launch";

const hash =
  "#/agent?project=p&component=c&issuer=http%3A%2F%2Fidp&client_id=k" +
  "&resource=https%3A%2F%2Faep.wso2.com%2Forgs%2Fo%2Fprojects%2Fp&scopes=openid+triage%3Ause" +
  "&endpoint=http%3A%2F%2Fgw%2Fc-http";

describe("parseLaunch", () => {
  it("reads every coordinate off the agent route", () => {
    expect(parseLaunch(hash)).toEqual({
      project: "p",
      component: "c",
      issuer: "http://idp",
      clientId: "k",
      resource: "https://aep.wso2.com/orgs/o/projects/p",
      scopes: ["openid", "triage:use"],
      endpoint: "http://gw/c-http",
    });
  });

  it("is null for another route or a missing required value", () => {
    expect(parseLaunch("")).toBeNull();
    expect(parseLaunch("#/api?project=p")).toBeNull();
    expect(parseLaunch("#/agent?project=p&component=c")).toBeNull();
  });

  it("is null when the issuer or the endpoint is not somewhere to fetch from", () => {
    expect(parseLaunch(hash.replace("issuer=http%3A%2F%2Fidp", "issuer=javascript%3Aalert(1)"))).toBeNull();
    expect(parseLaunch(hash.replace("endpoint=http%3A%2F%2Fgw%2Fc-http", "endpoint=data%3Atext%2Fhtml%2Cx"))).toBeNull();
    expect(parseLaunch(hash.replace("issuer=http%3A%2F%2Fidp", "issuer=idp"))).toBeNull();
  });
});

describe("the launch across the sign-in redirect", () => {
  beforeEach(() => sessionStorage.clear());

  it("round-trips through sessionStorage", () => {
    const launch = parseLaunch(hash);
    if (!launch) throw new Error("fixture did not parse");
    saveLaunch(launch);
    expect(loadLaunch()).toEqual(launch);
  });

  it("is null when nothing was saved", () => {
    expect(loadLaunch()).toBeNull();
  });
});

describe("endpointAllowed", () => {
  const hosts = ["openchoreoapis.localhost", "gw.example.com"];

  it("admits a gateway host and anything under it", () => {
    expect(endpointAllowed("http://default-default.openchoreoapis.localhost:19080/c-http", hosts)).toBe(true);
    expect(endpointAllowed("https://gw.example.com/c", hosts)).toBe(true);
  });

  it("refuses another host, a lookalike, a bad scheme, and a non-URL", () => {
    expect(endpointAllowed("https://attacker.example/c", hosts)).toBe(false);
    expect(endpointAllowed("https://gw.example.com.attacker.example/c", hosts)).toBe(false);
    expect(endpointAllowed("https://evilopenchoreoapis.localhost/c", hosts)).toBe(false);
    expect(endpointAllowed("javascript:alert(1)", hosts)).toBe(false);
    expect(endpointAllowed("not a url", hosts)).toBe(false);
  });
});
