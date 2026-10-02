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
  useEffect,
  useRef,
  useState,
  type PropsWithChildren,
} from "react";
import { useRouterState } from "@tanstack/react-router";
import type { components } from "../../../generated/aep-api";
import { useAeStudio, useReReadAeStudioOnOutage } from "../api/queries";
import { AeStudioFailed, AeStudioHold } from "./AeStudioHold";

type AeStudioState = components["schemas"]["AeStudio"]["state"];

const AeStudioRestartingContext = createContext(false);

// How long the first-visit hold may cover the console before it gives way to
// the console with the restarting banner. Above the pod's startup budget
// (200 s for its slowest container), below the backend's own not-Ready bound (10 min), so a pod that
// is merely slow still lands inside the hold and one that is stuck never
// locks the console for longer than this.
export const AE_STUDIO_HOLD_CAP_MS = 5 * 60_000;

// True while AE Studio is provisioning and the console is shown anyway: after
// it was ready this session (the user's own settings change rolled it), after
// the first-visit hold ran out, or after a `failed` answer (Try again). The
// banner reads it, and so do pod-backed surfaces: the console stays usable,
// only they wait.
export function useAeStudioRestarting(): boolean {
  return useContext(AeStudioRestartingContext);
}

// Sits right inside OnboardingGate and decides, from GET /ae-studio, whether
// the console renders:
// - the session's first answer is `provisioning` (an upgrade on visit): hold
//   the whole console until it leaves `provisioning`, for at most
//   AE_STUDIO_HOLD_CAP_MS; past the cap, the console with the restarting
//   banner while it stays `provisioning`;
// - a later `provisioning` (after `ready`, after the cap, or after `failed`):
//   no hold, the restarting flag;
// - `failed`, and only `failed`: a full page with Try again;
// - /settings is never held or failed: a setting is the usual fix;
// - `absent`, `ready`, the first request in flight, or a failed read: the
//   console. The read is fast and the config gate already showed a loader, so
//   waiting on it would only flash; and a read error is not a state the gate
//   can act on (the query retries it every 5 s).
// It also re-reads AE Studio whenever a pod read finds the pod not serving.
export function AeStudioGate({ children }: PropsWithChildren) {
  const studio = useAeStudio();
  useReReadAeStudioOnOutage();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const state: AeStudioState | undefined = studio.data?.state;

  // Session memory, in refs like OnboardingGate's `wizardShown`: whether the
  // first-visit hold is still on, and whether this page load has seen `ready`
  // or `failed`. The hold is decided once, by the first answer, and ends for
  // good the first time the state is anything but `provisioning`.
  const holding = useRef<boolean | null>(null);
  const seenReady = useRef(false);
  const seenFailed = useRef(false);
  if (state !== undefined) {
    if (holding.current === null) holding.current = state === "provisioning";
    if (state !== "provisioning") holding.current = false;
    if (state === "ready") seenReady.current = true;
    if (state === "failed") seenFailed.current = true;
  }

  const isHolding = holding.current === true;
  const [holdCapped, setHoldCapped] = useState(false);
  useEffect(() => {
    if (!isHolding) return;
    const timer = setTimeout(() => setHoldCapped(true), AE_STUDIO_HOLD_CAP_MS);
    return () => clearTimeout(timer);
  }, [isHolding]);

  if (!pathname.startsWith("/settings")) {
    if (isHolding && !holdCapped) return <AeStudioHold />;
    if (state === "failed") {
      return (
        <AeStudioFailed
          onRetry={() => void studio.refetch()}
          retrying={studio.isFetching}
        />
      );
    }
  }

  // Provisioning behind a visible console reads as a restart, however the
  // console got in: after ready, past the hold's cap, or out of `failed`.
  const restarting =
    state === "provisioning" && (seenReady.current || holdCapped || seenFailed.current);
  return (
    <AeStudioRestartingContext value={restarting}>
      {children}
    </AeStudioRestartingContext>
  );
}
