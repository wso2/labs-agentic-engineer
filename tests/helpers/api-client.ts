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
 * Test API client — direct HTTP to the Go backend.
 */

const API_BASE = process.env.API_BASE_URL || 'http://localhost:8080';

export async function apiGet<T>(path: string): Promise<{ status: number; data: T }> {
  const res = await fetch(`${API_BASE}${path}`);
  const data = res.status === 204 ? (undefined as T) : await res.json();
  return { status: res.status, data };
}

export async function apiPost<T>(path: string, body: unknown): Promise<{ status: number; data: T }> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  const data = res.status === 204 ? (undefined as T) : await res.json();
  return { status: res.status, data };
}

export async function apiDelete(path: string): Promise<{ status: number }> {
  const res = await fetch(`${API_BASE}${path}`, { method: 'DELETE' });
  return { status: res.status };
}
