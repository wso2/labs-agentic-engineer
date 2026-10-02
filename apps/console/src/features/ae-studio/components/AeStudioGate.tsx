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

import {
  createContext,
  useContext,
  useRef,
  type PropsWithChildren,
} from "react";
import { useRouterState } from "@tanstack/react-router";
import type { components } from "../../../generated/aep-api";
import { useAeStudio } from "../api/queries";
import { AeStudioFailed, AeStudioHold } from "./AeStudioHold";

type AeStudioState = components["schemas"]["AeStudio"]["state"];

const AeStudioRestartingContext = createContext(false);

// True while AE Studio is provisioning again after it was ready this session
// (the user's own settings change rolled it). The banner reads it, and so do
// pod-backed surfaces: the console stays usable, only they wait.
export function useAeStudioRestarting(): boolean {
  return useContext(AeStudioRestartingContext);
}

// Sits right inside OnboardingGate and decides, from GET /ae-studio, whether
// the console renders:
// - the session's first answer is `provisioning` (an upgrade on visit): hold
//   the whole console until it leaves `provisioning`;
// - a later `provisioning` after `ready`: no hold, the restarting flag;
// - `failed`: a full page with Try again, except under /settings, which stays
//   reachable because a setting is the usual fix;
// - `absent`, `ready`, the first request in flight, or a failed read: the
//   console. The read is fast and the config gate already showed a loader, so
//   waiting on it would only flash; and a read error is not a state the gate
//   can act on.
export function AeStudioGate({ children }: PropsWithChildren) {
  const studio = useAeStudio();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const state: AeStudioState | undefined = studio.data?.state;

  // Session memory, in refs like OnboardingGate's `wizardShown`: whether the
  // first-visit hold is still on, and whether this page load has seen `ready`.
  // The hold is decided once, by the first answer, and ends for good the
  // first time the state is anything but `provisioning`.
  const holding = useRef<boolean | null>(null);
  const seenReady = useRef(false);
  if (state !== undefined) {
    if (holding.current === null) holding.current = state === "provisioning";
    if (state !== "provisioning") holding.current = false;
    if (state === "ready") seenReady.current = true;
  }

  if (holding.current) return <AeStudioHold />;

  if (state === "failed" && !pathname.startsWith("/settings")) {
    return (
      <AeStudioFailed
        onRetry={() => void studio.refetch()}
        retrying={studio.isFetching}
      />
    );
  }

  return (
    <AeStudioRestartingContext
      value={seenReady.current && state === "provisioning"}
    >
      {children}
    </AeStudioRestartingContext>
  );
}
