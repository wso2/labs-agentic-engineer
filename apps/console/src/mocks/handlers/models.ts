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
import { modelPricing } from "../fixtures/usage";

// Model pricing catalog (#245) — near-static reference data; a scenario switch
// exercises the "pricing unavailable" state (tokens render without USD):
//   localStorage.setItem('aep:mock:pricing', 'error')
export const modelsHandlers = [
  http.get("*/api/v1/models/pricing", () => {
    if (localStorage.getItem("aep:mock:pricing") === "error") {
      return HttpResponse.json(
        {
          type: "about:blank",
          status: 500,
          title: "Internal Server Error",
          detail: "pricing catalog unavailable",
        },
        { status: 500, headers: { "Content-Type": "application/problem+json" } },
      );
    }
    return HttpResponse.json(modelPricing);
  }),
];
