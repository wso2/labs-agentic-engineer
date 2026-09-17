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
import { activeStep, wizardStep } from "./OnboardingWizard";

describe("activeStep", () => {
  it("resumes at GitHub when nothing is connected", () => {
    expect(activeStep({ gitProviderConnected: false, llmConnected: false })).toBe(0);
  });

  it("resumes at Anthropic once GitHub is connected", () => {
    expect(activeStep({ gitProviderConnected: true, llmConnected: false })).toBe(1);
  });

  it("resumes at repository setup once both are connected", () => {
    expect(activeStep({ gitProviderConnected: true, llmConnected: true })).toBe(2);
  });

  // GitHub is checked first regardless of which one is actually missing —
  // the steps run in a fixed order (issue #102), not the caller's choice.
  it("still resumes at GitHub when only Anthropic is connected", () => {
    expect(activeStep({ gitProviderConnected: false, llmConnected: true })).toBe(0);
  });
});

describe("wizardStep", () => {
  // Workspace authz is a hard gate ahead of everything else — GitHub/Anthropic
  // Connect mirror secrets into OpenChoreo, which 403s if the org's AuthzRole
  // doesn't exist yet. Regardless of server-reported config status, an
  // unconfirmed authz gate always wins and pins the wizard to step 0.
  it("stays on the workspace-authz step regardless of config status until it's ready", () => {
    expect(wizardStep({ gitProviderConnected: false, llmConnected: false }, false)).toBe(0);
    expect(wizardStep({ gitProviderConnected: true, llmConnected: true }, false)).toBe(0);
  });

  // Once authz is ready, the rest of the wizard resumes exactly where
  // activeStep says, shifted by one slot for the workspace-authz step ahead
  // of it.
  it("resumes at activeStep + 1 once authz is ready", () => {
    expect(wizardStep({ gitProviderConnected: false, llmConnected: false }, true)).toBe(1);
    expect(wizardStep({ gitProviderConnected: true, llmConnected: false }, true)).toBe(2);
    expect(wizardStep({ gitProviderConnected: true, llmConnected: true }, true)).toBe(3);
  });
});
