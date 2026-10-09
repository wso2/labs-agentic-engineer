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

const READ_REFUSALS = {
  restarting: { status: 503, code: "ae_studio_unavailable", message: "AE Studio is not ready — try again in a few seconds" },
  github: { status: 409, code: "github_not_connected", message: "GitHub is not connected for this organization — connect GitHub to continue" },
  misconfigured: { status: 503, code: "ae_studio_misconfigured", message: "AE Studio is not configured on this platform — contact your platform admin" },
} as const;

/**
 * aep-api's refusal of a read it serves through AE Studio (the spec state,
 * the versions), when the scenario knob asks for one, else null:
 *   localStorage.setItem('aep:mock:aeStudio:reads', 'restarting' | 'github' | 'misconfigured')
 * A restart carries Retry-After, as aep-api's does.
 */
export function aeStudioReadRefusal(): Response | null {
  const v = localStorage.getItem("aep:mock:aeStudio:reads");
  const refusal = v && v in READ_REFUSALS ? READ_REFUSALS[v as keyof typeof READ_REFUSALS] : null;
  if (!refusal) return null;
  return HttpResponse.json(
    { code: refusal.code, message: refusal.message },
    { status: refusal.status, ...(v === "restarting" ? { headers: { "Retry-After": "5" } } : {}) },
  );
}

