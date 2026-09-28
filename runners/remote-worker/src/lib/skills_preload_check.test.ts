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
 * These pin the detection of a silent preload miss. The bug they encode really
 * happened: with `settingSources: []` the SDK discovered no filesystem skills,
 * so every bare-name pin resolved to nothing, the resolved list held only the
 * plugin's skills, and the run reported success anyway.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { checkPreload, preloadWarning } from "./skills_preload_check.js";

test("a pinned skill absent from the SDK's resolved set is reported missing", () => {
  // Shape of the failing run: plugin skills resolved, repo ones gone.
  const resolvedSkills = ["aep:aep", "aep:validation-task", "aep:agent-browser", "deep-research"];
  const { missing, resolved } = checkPreload(["aep:aep", "go", "api-management"], resolvedSkills);

  assert.deepEqual(missing, ["go", "api-management"]);
  assert.deepEqual(resolved, ["aep:aep"]);
});

test("a fully resolved preload reports nothing — the warning must stay rare", () => {
  const { missing } = checkPreload(["aep:aep", "go"], ["aep:aep", "go", "react-webapp"]);
  assert.deepEqual(missing, []);
});

test("a plugin-qualified resolution satisfies a bare request, and the reverse", () => {
  // The SDK may report a name qualified differently than we spelled it. A false
  // alarm here would train the reader to ignore the line that matters.
  assert.deepEqual(checkPreload(["aep"], ["aep:aep"]).missing, []);
  assert.deepEqual(checkPreload(["aep:aep"], ["aep"]).missing, []);
});

test("an empty resolved set means everything is missing, not everything is fine", () => {
  // The degenerate case is the dangerous one: `skills: []` from the SDK is
  // indistinguishable from "no skills configured" unless we treat it as a miss.
  const { missing } = checkPreload(["aep:aep", "go"], []);
  assert.deepEqual(missing, ["aep:aep", "go"]);
});

test("the warning names the skills and the two things that actually cause this", () => {
  const w = preloadWarning(["go", "api-management"]);
  assert.match(w, /go, api-management/);
  assert.match(w, /\.claude\/skills\//);
  assert.match(w, /settingSources/);
});
