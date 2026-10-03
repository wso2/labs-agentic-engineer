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

import { useMemo } from "react";
import { useSession } from "../../auth/SessionContext";

export interface ChatAuthor {
  id: string;
  displayName: string;
}

/**
 * The signed-in user's identity for stamping outgoing chat messages (#130
 * multi-user threads), so the send path can attribute a turn and the panel
 * can later tell "You" apart from a teammate. The id is the session's `sub`,
 * the same claim the design agent stamps a turn's and a message's author
 * with, so the user's own turns read as "You".
 */
export function useCurrentAuthor(): ChatAuthor {
  const { user } = useSession();
  // Stable identity across re-renders (not a fresh object each render) so
  // callers can safely put it in a hook dependency array.
  return useMemo(() => ({ id: user.id, displayName: user.name }), [user.id, user.name]);
}
