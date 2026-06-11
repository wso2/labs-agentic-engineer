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
  VITE_CORE_API_BASE_URL?: string;
  VITE_THUNDER_URL?: string;
  VITE_THUNDER_CLIENT_ID?: string;
  VITE_THUNDER_SCOPES?: string;
  VITE_SIGN_IN_REDIRECT_URL?: string;
  VITE_SIGN_OUT_REDIRECT_URL?: string;
  VITE_DEV_BYPASS_AUTH?: string;
  BILLING_API_BASE_URL?: string;
}

declare global {
  interface Window {
    _env_?: RuntimeEnv;
  }
}

function getEnv(key: keyof RuntimeEnv): string | undefined {
  if (typeof window !== 'undefined' && window._env_) {
    const runtimeValue = window._env_[key];
    if (runtimeValue !== undefined && runtimeValue !== '') {
      return runtimeValue;
    }
  }
  return import.meta.env[key];
}

export const env = {
  VITE_CORE_API_BASE_URL: getEnv('VITE_CORE_API_BASE_URL') || '/asdlc-api-service',
  VITE_THUNDER_URL: getEnv('VITE_THUNDER_URL') || '',
  VITE_THUNDER_CLIENT_ID: getEnv('VITE_THUNDER_CLIENT_ID') || '',
  VITE_THUNDER_SCOPES: getEnv('VITE_THUNDER_SCOPES') || 'openid profile email',
  VITE_SIGN_IN_REDIRECT_URL: getEnv('VITE_SIGN_IN_REDIRECT_URL') || undefined,
  VITE_SIGN_OUT_REDIRECT_URL: getEnv('VITE_SIGN_OUT_REDIRECT_URL') || undefined,
  VITE_DEV_BYPASS_AUTH: getEnv('VITE_DEV_BYPASS_AUTH') === 'true',
  BILLING_API_BASE_URL: getEnv('BILLING_API_BASE_URL') || '',
} as const;
