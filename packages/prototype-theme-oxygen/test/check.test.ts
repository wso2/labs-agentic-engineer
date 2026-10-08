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
 * `prototype check --theme @wso2/prototype-theme-oxygen` over the CLI's
 * fixture prototypes: every valid one passes, and every one that fails in the
 * isolated render reports what the default theme reports. (The manifest and
 * static-source fixtures stop before any theme runs.)
 */

import { describe, expect, it } from "vitest";
import { check, fixturePath, fixtures, THEME, type Finding } from "./harness.js";

const at = (f: Finding) => [f.code, f.file, f.location];

/** The fixtures whose findings come from rendering under a theme. */
const renderStage = fixtures("invalid").filter((name) => /^(render|form|escape)-/.test(name));

describe.concurrent(`prototype check --theme ${THEME}`, () => {
  it.each(fixtures("valid"))("passes the valid fixture %s", async (name) => {
    const { status, ok, findings } = await check(fixturePath(`valid/${name}`), THEME);
    expect(findings, JSON.stringify(findings, null, 2)).toEqual([]);
    expect(ok).toBe(true);
    expect(status).toBe(0);
  });

  it.each(renderStage)("reports %s as the default theme does", async (name) => {
    const [oxygen, standard] = await Promise.all([check(fixturePath(`invalid/${name}`), THEME), check(fixturePath(`invalid/${name}`))]);
    expect(standard.findings.length).toBeGreaterThan(0);
    expect(oxygen.findings.map(at)).toEqual(standard.findings.map(at));
    expect(oxygen.status).toBe(1);
  });
});
