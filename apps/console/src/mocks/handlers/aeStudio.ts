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

import { http, HttpResponse } from "msw";
import {
  PROVISIONING_POLLS,
  aeStudioFor,
  type AeStudioScenario,
} from "../fixtures/aeStudio";

const SCENARIOS: readonly AeStudioScenario[] = ["ready", "provisioning", "failed", "absent"];

function scenario(): AeStudioScenario {
  const v = localStorage.getItem("aep:mock:aeStudio");
  return SCENARIOS.find((s) => s === v) ?? "ready";
}

// Answers served under `provisioning` since the scenario was last switched
// to it (or the page loaded); switching the knob starts the upgrade over, so
// a restart can be replayed on a page that is already in.
let lastScenario: AeStudioScenario | null = null;
let provisioningAnswers = 0;

// The org's AE Studio state and, once ready, its fake URLs.
export const aeStudioHandlers = [
  http.get("*/api/v1/ae-studio", () => {
    const s = scenario();
    if (s !== lastScenario) {
      lastScenario = s;
      provisioningAnswers = 0;
    }
    if (s !== "provisioning") return HttpResponse.json(aeStudioFor(s));
    provisioningAnswers += 1;
    return HttpResponse.json(
      aeStudioFor(provisioningAnswers > PROVISIONING_POLLS ? "ready" : "provisioning"),
    );
  }),
];
