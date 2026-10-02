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
 * Room (`AE_*`: the public and local room listeners and the health port), dev
 * mode (the same Room, auth bypassed, fake Files socket), or the legacy
 * collab server of the chart Deployment (BFF client, listen). Boot fails with
 * none.
 *
 *   pnpm --filter @aep/ae-collab dev     # watch + reload (dev mode: COLLAB_DEV=1)
 *   pnpm --filter @aep/ae-collab start   # run once
 */

import type { CollabConfig } from "./env.js";
import { selectModes } from "./modes.js";
import { createBffClient } from "./bff.js";
import { createCollabServer, registerGracefulShutdown } from "./server.js";
import { startMockBff } from "./mockbff.js";
import type { PodListeners } from "./pod/listeners.js";
import { startDev, startPod } from "./pod/start.js";

const selected = selectModes(process.env);

if (selected.mode === "pod") {
  closeOnSignal(await startPod(selected.config));
} else if (selected.mode === "dev") {
  closeOnSignal(await startDev(selected.config));
} else {
  await startLegacyServer(selected.config);
}

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

/** Today's collab server over the BFF (real or mock); deleted in Task 2.12. */
async function startLegacyServer(config: CollabConfig): Promise<void> {
  if (config.mockBff) {
    await startMockBff(config.mockBffPort);
    console.warn(
      `[collab] MOCK BFF on http://127.0.0.1:${config.mockBffPort} — real auth/seed paths, mocked backend (#81 stand-in)`,
    );
  }

  const bff = config.aepApiBase ? createBffClient(config.aepApiBase) : null;

  const log = (m: string) => console.log(`[collab] ${m}`);

  const server = createCollabServer(config, { bff, log });

  registerGracefulShutdown(server, config, { bff, log });

  await server.listen(config.port);
  console.log(`[collab] listening on ws://localhost:${config.port}`);
}
