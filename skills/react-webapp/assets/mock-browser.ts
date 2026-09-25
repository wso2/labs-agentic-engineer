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

// Mock mode — copied verbatim to <app-path>/mock/browser.ts, never edited.
// The handlers beside it are the app's; this file only starts them.
//
// THE ORDER IS THE DESIGN. MSW takes the first handler that matches AND
// answers; a resolver that returns nothing falls through to the next one. So
// the three layers below are the deployed request path, in the deployed order:
//
//   gatewayHandlers  the API gateway — may this caller call this at all? (401)
//   handlers         the service — the app's own data, ownership and 404s
//   unhandledApi     nothing answered: the mock's own gap, said out loud (501)

import { http, HttpResponse, type RequestHandler } from "msw";
import { setupWorker } from "msw/browser";
import { gatewayHandlers } from "./authz/gateway";
import { handlers } from "./handlers";
import { mountRoleBadge } from "./badge";

// Closes the API surface, and it has to be closed explicitly: MSW passes an
// unhandled request THROUGH to the network, so a call nobody wrote a handler
// for would otherwise reach the dev server and come back as index.html with
// status 200 — a successful response with an unparseable body, which reads to
// the caller as a working screen. 501 says unmistakably that it was the mock,
// not the app, that had no answer, and it cannot be confused with a 404 the
// contract genuinely declares.
const unhandledApi: RequestHandler = http.all("/api/*", ({ request }) => {
  const { pathname } = new URL(request.url);
  return HttpResponse.json(
    { error: `mock: no handler for ${request.method} ${pathname}` },
    { status: 501 },
  );
});

export const worker = setupWorker(...gatewayHandlers, ...handlers, unhandledApi);

export async function startMockWorker(): Promise<void> {
  // Who am I / switch. Mounted in both modes and before the early return below,
  // because wired mode is the one where a person walks every role in one
  // sitting and it is the only thing this module does there.
  mountRoleBadge();

  // WIRED MODE (the playground's `wire` verb): `/api` is proxied to the real
  // service and the dev server is the gateway in front of it — see
  // mock/wired.ts. Starting the worker here would intercept those calls in the
  // browser and answer them from seed data, which is precisely the thing wired
  // mode exists not to do.
  if ((globalThis as { __AEP_WIRED__?: boolean }).__AEP_WIRED__) return;

  // Everything else the page asks for — modules, assets, HMR — is the dev
  // server's and passes through untouched.
  await worker.start({ onUnhandledRequest: "bypass" });
}
