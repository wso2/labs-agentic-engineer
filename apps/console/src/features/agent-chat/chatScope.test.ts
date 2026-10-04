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

import { afterEach, describe, expect, it } from "vitest";
import { chatKeyForScope, dropSharedMarketplaceLogs, MARKETPLACE_SCOPE, projectScope } from "./chatScope";

// Node test env: a Map-backed localStorage stub.
const backing = new Map<string, string>();
const stub = {
  getItem: (k: string) => backing.get(k) ?? null,
  setItem: (k: string, v: string) => void backing.set(k, v),
  removeItem: (k: string) => void backing.delete(k),
  clear: () => backing.clear(),
  key: (i: number) => [...backing.keys()][i] ?? null,
  get length() {
    return backing.size;
  },
} as Storage;
globalThis.localStorage = stub;

afterEach(() => {
  backing.clear();
  globalThis.localStorage = stub;
});

describe("dropSharedMarketplaceLogs", () => {
  it("removes the pre-per-user shared marketplace log of every org, and nothing else", () => {
    const perUser = chatKeyForScope("acme", MARKETPLACE_SCOPE, "user-1");
    const project = chatKeyForScope("acme", projectScope("greeter"), "user-1");
    backing.set("aep.chat.v1.acme.~marketplace", "[]");
    backing.set("aep.chat.v1.globex.~marketplace", "[]");
    backing.set(perUser, '[{"id":"m1"}]');
    backing.set(project, '[{"id":"m2"}]');
    backing.set("unrelated", "x");

    dropSharedMarketplaceLogs();

    expect([...backing.keys()].sort()).toEqual([perUser, project, "unrelated"].sort());
    expect(backing.get(perUser)).toBe('[{"id":"m1"}]');
  });

  it("never throws when storage does", () => {
    globalThis.localStorage = {
      ...stub,
      get length(): number {
        throw new Error("SecurityError");
      },
      key: () => {
        throw new Error("SecurityError");
      },
    } as Storage;

    expect(() => dropSharedMarketplaceLogs()).not.toThrow();
  });
});
