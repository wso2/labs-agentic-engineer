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
 * Composition root. One mode, chosen by env (`modes.ts`): the AE Studio pod's
 * Room (`AE_*`: the public and local room listeners and the health port) or
 * dev mode (the same Room, auth bypassed, fake Files socket). Boot fails with
 * neither. SIGTERM/SIGINT run the close path: the shutdown flush, then the
 * listeners.
 *
 *   pnpm --filter @aep/ae-collab dev     # watch + reload (dev mode: COLLAB_DEV=1)
 *   pnpm --filter @aep/ae-collab start   # run once
 */

import { selectModes } from "./modes.js";
import type { PodListeners } from "./pod/listeners.js";
import { startDev, startPod } from "./pod/start.js";

const selected = selectModes(process.env);

closeOnSignal(selected.mode === "pod" ? await startPod(selected.config) : await startDev(selected.config));

function closeOnSignal(listeners: PodListeners): void {
  let closing = false;
  for (const signal of ["SIGINT", "SIGTERM"] as const) {
    process.once(signal, () => {
      if (closing) return;
      closing = true;
      listeners.close().then(
        () => process.exit(0),
        () => process.exit(1),
      );
    });
  }
}
