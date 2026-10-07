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

import { AuthGuard } from "../../../auth/AuthGuard";
import { AeStudioGate } from "../../ae-studio/components/AeStudioGate";
import { OnboardingGate } from "../../onboarding/components/OnboardingGate";
import { Shell } from "./Shell";

/**
 * The shell behind its gates, outside in: the auth gate, then the onboarding
 * gate, so routes only ever see a signed-in session in an org with GitHub and
 * a model connected; inside them, the AE Studio gate, which holds every route
 * while the org's AE Studio upgrades on a first visit or has failed to start
 * (Settings excepted). It sits inside onboarding: an org that is not set up
 * gets the wizard, never an AE Studio hold.
 */
export function GatedShell() {
  return (
    <AuthGuard>
      <OnboardingGate>
        <AeStudioGate>
          <Shell />
        </AeStudioGate>
      </OnboardingGate>
    </AuthGuard>
  );
}
