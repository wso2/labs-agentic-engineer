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

import { http, HttpResponse, type RequestHandler } from "msw";
import { setupWorker } from "msw/browser";
import { handlers } from "./handlers";

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

// Last, so every handler the app authored wins over it.
export const worker = setupWorker(...handlers, unhandledApi);

export async function startMockWorker(): Promise<void> {
  // Everything else the page asks for — modules, assets, HMR — is the dev
  // server's and passes through untouched.
  await worker.start({ onUnhandledRequest: "bypass" });
}
