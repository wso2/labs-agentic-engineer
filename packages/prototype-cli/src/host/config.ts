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

/** The host page's config, from the inert JSON the server or export wrote into the page. */

import { HOST_CONFIG_ID, type HostConfig } from "../host-config.js";

export function readHostConfig(): HostConfig {
  const text = document.getElementById(HOST_CONFIG_ID)?.textContent;
  if (!text) throw new Error(`the host page has no #${HOST_CONFIG_ID}`);
  return JSON.parse(text) as HostConfig;
}
