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
 * The manifest hash (D14). Lives in the service (NOT `@aep/agent-stream`, which
 * stays fs/crypto-free and client-safe): the terminal manifest part maps each
 * mutated path to this digest of its FINAL content, and the aep-api fold
 * recomputes the same digest to gate its commit (fold-parity).
 */

import { createHash } from "node:crypto";

/** sha256 over the UTF-8 bytes of `text`, as lowercase hex. */
export function sha256Hex(text: string): string {
  return createHash("sha256").update(text, "utf8").digest("hex");
}
