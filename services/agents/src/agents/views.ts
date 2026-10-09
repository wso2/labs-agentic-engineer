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
 * What the platform says about each view's agent, wherever it names one to
 * another (a branch note in the main agent's prompt, for one). The agents
 * themselves live in `agents/<view>/`.
 */

import type { View } from "@aep/agent-stream";

export const VIEW_AGENTS: Record<View, { label: string }> = {
  issues: { label: "Issues" },
  issue: { label: "Issue" },
};
