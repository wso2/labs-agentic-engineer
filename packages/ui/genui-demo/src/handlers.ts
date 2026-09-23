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

import type { GenUiActionHandlers } from "@aep/ui-genui";

// Every catalog action just logs here; each page shows the outcome of what
// reached it.
export const handlers: GenUiActionHandlers = {
  openTask: (params) => console.info("openTask", params),
  approveDependency: (params) => console.info("approveDependency", params),
  rejectDependency: (params) => console.info("rejectDependency", params),
  openDeployments: (params) => console.info("openDeployments", params),
  editExternalResource: (params) => console.info("editExternalResource", params),
};
