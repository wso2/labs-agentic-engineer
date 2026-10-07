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
import { addressedReply } from "./designTurns";

// The Address comments turn's closing line reads naturally for every count:
// no "0 changed the design only", and one comment is not "each".

describe("addressedReply", () => {
  it.each<[number, number, string]>([
    [1, 0, "Done. It changed the design only. The comment now shows"],
    [0, 1, "Done. It changed the spec as well as the design (you'll see a dot on the Spec tab). The comment now shows"],
    [2, 0, "Done. Both changed the design only. Each comment now shows"],
    [0, 3, "Done. All 3 changed the spec as well as the design (you'll see a dot on the Spec tab). Each comment"],
    [2, 1, "Done. 2 changed the design only, and 1 also changed the spec (you'll see a dot on the Spec tab). Each"],
  ])("%i design only, %i spec too", (designOnly, specToo, start) => {
    const reply = addressedReply(designOnly, specToo);
    expect(reply.startsWith(start)).toBe(true);
    expect(reply).not.toMatch(/\b0 /);
  });
});
