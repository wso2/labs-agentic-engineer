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

import { test } from "node:test";
import assert from "node:assert/strict";
import { hotspotToViewport } from "../src/hotspotOverlay.js";

test("identity at zoom 1 with no scroll", () => {
  const r = hotspotToViewport({ x: 10, y: 20, width: 100, height: 40 }, { scrollX: 0, scrollY: 0, zoom: { value: 1 } });
  assert.deepEqual(r, { left: 10, top: 20, width: 100, height: 40 });
});

test("applies scroll then zoom (Excalidraw scene→viewport order)", () => {
  const r = hotspotToViewport({ x: 10, y: 20, width: 100, height: 40 }, { scrollX: 5, scrollY: -10, zoom: { value: 0.5 } });
  assert.deepEqual(r, { left: 7.5, top: 5, width: 50, height: 20 });
});
