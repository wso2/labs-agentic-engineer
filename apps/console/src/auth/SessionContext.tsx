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

import { createContext, useContext } from "react";

// What the app knows about the signed-in user, mode-agnostic: real claims
// in thunder mode, the fixed dev identity in mock mode. Everything under
// AuthGuard can rely on it existing.
export interface Session {
  /**
   * `id` is the token's `sub`: the id the design agent stamps a turn's and a
   * message's author with, so the chat can tell the user's own from a
   * teammate's. `""` when the token carries none.
   */
  user: { id: string; name: string; email: string; role?: string };
  /** Active org, resolved with the BFF's precedence (ouHandle > ouName > ouId). */
  orgHandle: string | null;
  signOut: () => void;
}

export const SessionContext = createContext<Session | null>(null);

export function useSession(): Session {
  const session = useContext(SessionContext);
  if (!session) {
    throw new Error("useSession must be used inside AuthGuard");
  }
  return session;
}
