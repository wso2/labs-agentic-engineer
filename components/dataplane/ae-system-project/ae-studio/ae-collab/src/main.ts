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
 * listeners (`AE_*`: the `/v1` user gate, the room upgrade path and the
 * health port), or the legacy collab server of the chart Deployment (load
 * config, wire the BFF client, listen). Boot fails with neither.
 *
 *   pnpm --filter @aep/ae-collab dev     # watch + reload (dev mode: COLLAB_DEV=1)
 *   pnpm --filter @aep/ae-collab start   # run once
 */

import type { CollabConfig } from "./env.js";
import { selectModes } from "./modes.js";
import { createBffClient } from "./bff.js";
import { createCollabServer, registerGracefulShutdown } from "./server.js";
import { startMockBff } from "./mockbff.js";
import { startPodListeners } from "./pod/listeners.js";

const modes = selectModes(process.env);

if (modes.pod) {
  const pod = await startPodListeners(modes.pod);
  let closing = false;
  for (const signal of ["SIGINT", "SIGTERM"] as const) {
    process.once(signal, () => {
      if (closing) return;
      closing = true;
      pod.close().then(
        () => process.exit(0),
        () => process.exit(1),
      );
    });
  }
} else {
  await startLegacyServer(modes.legacy);
}

/** Today's collab server, unchanged: oracle + seeds from the BFF, or fixtures in dev mode. */
async function startLegacyServer(config: CollabConfig): Promise<void> {
  if (config.mockBff) {
    await startMockBff(config.mockBffPort);
    console.warn(
      `[collab] MOCK BFF on http://127.0.0.1:${config.mockBffPort} — real auth/seed paths, mocked backend (#81 stand-in)`,
    );
  }

  const bff = config.aepApiBase ? createBffClient(config.aepApiBase) : null;

  if (config.devMode) {
    console.warn("[collab] DEV MODE (COLLAB_DEV): BFF oracle bypassed, rooms seed from fixtures.");
  }

  const log = (m: string) => console.log(`[collab] ${m}`);

  const server = createCollabServer(config, { bff, log });

  registerGracefulShutdown(server, config, { bff, log });

  await server.listen(config.port);
  console.log(`[collab] listening on ws://localhost:${config.port}`);
}
