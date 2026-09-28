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
import { WebStorageStateStore } from "oidc-client-ts";
import { displayName, managerSettings } from "./session";

const launch = {
  project: "p",
  component: "c",
  issuer: "http://idp",
  clientId: "k",
  resource: "https://aep.wso2.com/orgs/o/projects/p",
  scopes: ["openid", "triage:use"],
  endpoint: "http://gw",
};

describe("managerSettings", () => {
  const settings = managerSettings(launch, "http://tryit.aep.localhost:8095");

  it("signs in at the project's issuer as its public client, back to the registered callback", () => {
    expect(settings.authority).toBe("http://idp");
    expect(settings.client_id).toBe("k");
    expect(settings.redirect_uri).toBe("http://tryit.aep.localhost:8095/callback");
    expect(settings.response_type).toBe("code");
    expect(settings.scope).toBe("openid triage:use");
  });

  it("carries the resource indicator on both legs the library would otherwise drop", () => {
    // 1: authorize, from settings. 2: code→token, only via extraTokenParams.
    expect(settings.resource).toBe(launch.resource);
    expect(settings.extraTokenParams).toEqual({ resource: launch.resource });
  });

  it("keeps the session per tab and reads userinfo for the person's name", () => {
    expect(settings.userStore).toBeInstanceOf(WebStorageStateStore);
    expect(settings.loadUserInfo).toBe(true);
    expect(settings.automaticSilentRenew).toBe(false);
  });
});

describe("displayName", () => {
  it("prefers a username, then a name, then an email, and shows the id only as a last resort", () => {
    expect(displayName({ sub: "01a0-uuid", email: "t@x.io", preferred_username: "test-user" })).toBe("test-user");
    expect(displayName({ sub: "01a0-uuid", name: "Test User" })).toBe("Test User");
    expect(displayName({ sub: "01a0-uuid", email: "t@x.io" })).toBe("t@x.io");
    // A platform-minted test user: the username is the email's local part.
    expect(displayName({ sub: "01a0-uuid", email: "test-engineer@test-users.invalid" })).toBe("test-engineer");
    expect(displayName({ sub: "01a0-uuid" })).toBe("01a0-uuid");
    expect(displayName({})).toBeNull();
  });
});
