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
 * `--persist`: the host keeps the frame's mock-data snapshots in
 * localStorage, keyed by the revision's hash, so a reload resumes and a
 * changed source starts from the seed. (The sandboxed frame has an opaque
 * origin and no storage of its own.) Storage failures are never fatal.
 */

import { isDataSnapshot, type DataSnapshot } from "@wso2/prototype-kit/host";

const key = (hash: string) => `proto:data:${hash}`;

export function loadSnapshot(hash: string): DataSnapshot | undefined {
  try {
    const text = localStorage.getItem(key(hash));
    if (text === null) return undefined;
    const value: unknown = JSON.parse(text);
    return isDataSnapshot(value) ? value : undefined;
  } catch {
    return undefined;
  }
}

export function saveSnapshot(hash: string, data: DataSnapshot): void {
  try {
    localStorage.setItem(key(hash), JSON.stringify(data));
  } catch {
    // Storage full or disabled: the review goes on without persistence.
  }
}

export function clearSnapshot(hash: string): void {
  try {
    localStorage.removeItem(key(hash));
  } catch {
    // Nothing stored, or storage disabled.
  }
}
