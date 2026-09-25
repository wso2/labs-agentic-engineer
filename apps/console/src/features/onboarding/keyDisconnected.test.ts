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
import type { components } from "../../generated/aep-api";
import { anthropicKeyWasDisconnected } from "./keyDisconnected";

type ConfigProjection = components["schemas"]["ConfigProjection"];
type LLMProjection = components["schemas"]["LLMProjection"];

const key: LLMProjection = {
  kind: "anthropic",
  credentialKind: "api_key",
  status: "connected",
  keyPrefix: "sk-ant-",
  keyLast4: "wxyz",
  connectedAt: "2026-06-01T12:05:00Z",
};

// Only the fields the check reads; the rest of the projection is irrelevant.
const config = (over: object) => ({ llm: null, ...over }) as unknown as ConfigProjection;

describe("anthropicKeyWasDisconnected", () => {
  it("is false for an org that never had a key", () => {
    expect(anthropicKeyWasDisconnected(config({}))).toBe(false);
  });

  it("is true when the key was disconnected and none is connected", () => {
    expect(
      anthropicKeyWasDisconnected(config({ llmDisconnectedAt: "2026-09-20T08:00:00Z" })),
    ).toBe(true);
  });

  it("is false once a key is connected again", () => {
    expect(
      anthropicKeyWasDisconnected(
        config({ llm: key, llmDisconnectedAt: "2026-09-20T08:00:00Z" }),
      ),
    ).toBe(false);
  });
});
