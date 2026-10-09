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

import { setupWorker } from "msw/browser";
import { buildsHandlers } from "./handlers/builds";
import { conversationHandlers } from "./handlers/conversation";
import { deployHandlers } from "./handlers/deploy";
import { designHandlers } from "./handlers/design";
import { issuesHandlers } from "./handlers/issues";
import { projectsHandlers } from "./handlers/projects";
import { resourcesHandlers } from "./handlers/resources";
import { settingsHandlers } from "./handlers/settings";
import { skillsHandlers } from "./handlers/skills";
import { specHandlers } from "./handlers/spec";
import { usageHandlers } from "./handlers/usage";
import { aeStudioHandlers } from "./handlers/aeStudio";

// Mock mode's worker. Each screen adds its handlers here as it is built.
export const worker = setupWorker(
  ...projectsHandlers,
  ...conversationHandlers,
  ...specHandlers,
  ...designHandlers,
  ...buildsHandlers,
  ...deployHandlers,
  ...issuesHandlers,
  ...settingsHandlers,
  ...skillsHandlers,
  ...resourcesHandlers,
  ...usageHandlers,
  ...aeStudioHandlers,
);
