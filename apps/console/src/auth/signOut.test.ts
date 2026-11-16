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

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const manager = {
  signoutRedirect: vi.fn<() => Promise<void>>(),
  revokeTokens: vi.fn<() => Promise<void>>(),
  removeUser: vi.fn<() => Promise<void>>(),
};

vi.mock("./userManager", () => ({ getUserManager: () => manager }));

// signOut.ts keeps "sign-out has started" for the life of the page, so each
// test loads its own copy of the module.
async function load() {
  vi.resetModules();
  return import("./signOut");
}

describe("signOut", () => {
  const assign = vi.fn();

  beforeEach(() => {
    manager.signoutRedirect.mockReset();
    manager.revokeTokens.mockReset().mockResolvedValue();
    manager.removeUser.mockReset().mockResolvedValue();
    assign.mockReset();
    // The suite runs without a DOM: signOut only needs window.location.assign.
    vi.stubGlobal("window", { location: { assign } });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("is not signing out until sign-out starts", async () => {
    const { isSigningOut } = await load();
    expect(isSigningOut()).toBe(false);
  });

  // The library drops the stored user before it navigates to the IdP's
  // logout. The guard reads isSigningOut() in that gap, so it has to be true
  // before the library is called, or the guard starts a sign-in that races
  // the logout navigation.
  it("says so before the library clears the session, and keeps saying so", async () => {
    const { isSigningOut, signOut } = await load();
    let duringRedirect: boolean | undefined;
    manager.signoutRedirect.mockImplementation(async () => {
      duringRedirect = isSigningOut();
    });
    await signOut();
    expect(duringRedirect).toBe(true);
    expect(isSigningOut()).toBe(true);
    expect(assign).not.toHaveBeenCalled();
  });

  it("falls back to dropping the local session and reloading when the IdP offers no logout", async () => {
    const { isSigningOut, signOut } = await load();
    manager.signoutRedirect.mockRejectedValue(new Error("no end session endpoint"));
    manager.revokeTokens.mockRejectedValue(new Error("revocation failed"));
    await signOut();
    expect(manager.removeUser).toHaveBeenCalledOnce();
    expect(assign).toHaveBeenCalledWith("/");
    expect(isSigningOut()).toBe(true);
  });

  it("still reloads when dropping the local session fails", async () => {
    const { signOut } = await load();
    manager.signoutRedirect.mockRejectedValue(new Error("no end session endpoint"));
    manager.removeUser.mockRejectedValue(new Error("storage unavailable"));
    await signOut();
    expect(assign).toHaveBeenCalledWith("/");
  });
});
