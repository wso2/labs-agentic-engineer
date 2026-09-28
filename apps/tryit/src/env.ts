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

interface RuntimeEnv {
  /**
   * Space-separated gateway hosts the app may send a token to, each matching a
   * hostname exactly or as a parent domain (`openchoreoapis.localhost` admits
   * `default-default.openchoreoapis.localhost`). The operator's word, not the
   * launch URL's: it is what stops a crafted link from pairing the project's
   * real sign-in with an endpoint of its own choosing.
   */
  TRY_IT_GATEWAY_HOSTS?: string;
}

declare global {
  interface Window {
    _env_?: RuntimeEnv;
  }
}

// Runtime config (window._env_, served as /env-config.js by nginx) takes
// precedence over build-time env, then the local stack's default.
function getEnv(key: keyof RuntimeEnv): string | undefined {
  if (typeof window !== "undefined" && window._env_) {
    const runtimeValue = window._env_[key];
    if (runtimeValue !== undefined && runtimeValue !== "") return runtimeValue;
  }
  return import.meta.env[key];
}

export const env = {
  gatewayHosts: (getEnv("TRY_IT_GATEWAY_HOSTS") || "openchoreoapis.localhost").split(/\s+/).filter(Boolean),
} as const;
