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

// @vitest-environment jsdom

import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { SessionContext, type Session } from "../../auth/SessionContext";
import { useCurrentAuthor } from "./currentUser";

const session: Session = {
  user: { id: "sub-ada", name: "Ada Lovelace", email: "ada@example.com" },
  orgHandle: "acme",
  signOut: () => undefined,
};

function wrapper({ children }: { children: ReactNode }) {
  return <SessionContext.Provider value={session}>{children}</SessionContext.Provider>;
}

describe("useCurrentAuthor", () => {
  // The design agent stamps a turn's and a message's author with the verified
  // token's sub, so "me" has to be the same claim or the user's own turns read
  // as a teammate's.
  it("is the session's sub, named by the session's display name", () => {
    const { result } = renderHook(() => useCurrentAuthor(), { wrapper });
    expect(result.current).toEqual({ id: "sub-ada", displayName: "Ada Lovelace" });
  });
});
