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

/**
 * Test/eval-only helper: the one place the ephemeral-port listen/address/close
 * dance lives (shared by `server.test.ts` and the eval harness).
 */

import type { Server } from "node:http";
import type { AddressInfo } from "node:net";

export interface Listening {
  baseUrl: string;
  close: () => Promise<void>;
}

/** Wait for `server` (from `app.listen(0)`) to bind, then return its URL + a close fn. */
export async function listen0(server: Server): Promise<Listening> {
  await new Promise<void>((resolve) => server.once("listening", () => resolve()));
  const { port } = server.address() as AddressInfo;
  return {
    baseUrl: `http://127.0.0.1:${port}`,
    close: () => new Promise<void>((resolve) => server.close(() => resolve())),
  };
}
