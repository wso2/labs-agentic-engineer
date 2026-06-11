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

import { describe, it, expect } from 'vitest';
import { apiGet, apiPost, apiDelete } from '../helpers/api-client';

describe('Projects API', () => {
  it('should list projects', async () => {
    const { status, data } = await apiGet<{ items: any[] }>('/api/v1/projects');
    expect(status).toBe(200);
    expect(data).toHaveProperty('items');
    expect(Array.isArray(data.items)).toBe(true);
  });

  it('should create a project', async () => {
    const name = `test-proj-${Date.now()}`;
    const { status, data } = await apiPost<any>('/api/v1/projects', {
      name,
      displayName: 'Test Project',
      description: 'Created by integration test',
    });
    expect(status).toBe(201);
    expect(data.name).toBe(name);

    // Cleanup
    await apiDelete(`/api/v1/projects/${name}`);
  });

  it('should get a project by name', async () => {
    const name = `test-proj-get-${Date.now()}`;
    await apiPost('/api/v1/projects', { name, displayName: 'Get Test' });

    const { status, data } = await apiGet<any>(`/api/v1/projects/${name}`);
    expect(status).toBe(200);
    expect(data.name).toBe(name);

    await apiDelete(`/api/v1/projects/${name}`);
  });

  it('should delete a project', async () => {
    const name = `test-proj-del-${Date.now()}`;
    await apiPost('/api/v1/projects', { name, displayName: 'Delete Test' });

    const { status } = await apiDelete(`/api/v1/projects/${name}`);
    expect(status).toBe(204);
  });

  it('should reject create without name', async () => {
    const { status } = await apiPost<any>('/api/v1/projects', {
      displayName: 'No Name',
    });
    expect(status).toBe(400);
  });
});
