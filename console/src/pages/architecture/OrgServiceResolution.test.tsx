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

import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { Dependency } from '../../services/api/types';
import type { AccessRequest } from '../../services/api/accessRequests';

// Mock the accessRequests module before importing OrgServiceResolution
vi.mock('../../services/api/accessRequests', () => ({
  listAccessRequests: vi.fn(),
  requestAccess: vi.fn(),
}));

import * as accessRequestsModule from '../../services/api/accessRequests';
import { OrgServiceResolution } from './OrgServiceResolution';

function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
}

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={createQueryClient()}>{children}</QueryClientProvider>
  );
}

const blockedDep: Dependency = {
  kind: 'org-service',
  name: 'payment-svc',
  status: 'blocked',
  reason: 'access-required',
};

const unresolvedDep: Dependency = {
  kind: 'org-service',
  name: 'missing-svc',
  status: 'unresolved',
  reason: 'not-found',
};

const accessPendingDep: Dependency = {
  kind: 'org-service',
  name: 'payment-svc',
  status: 'blocked',
  reason: 'access-pending',
};

const mockAccessRequest: AccessRequest = {
  id: 'ar-1',
  orgID: 'org-1',
  consumerProjectID: 'proj-1',
  consumerComponentName: 'api',
  orgServiceName: 'payment-svc',
  providerProjectID: 'proj-provider',
  providerComponentName: 'payment-svc',
  providerTaskID: 'task-1',
  providerIssueNumber: 42,
  providerIssueUrl: 'https://github.com/org/repo/issues/42',
  status: 'requested',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
};

describe('OrgServiceResolution', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('blocked / access-required — no prior request', () => {
    it('renders a warning and "Request access" button', async () => {
      vi.mocked(accessRequestsModule.listAccessRequests).mockResolvedValue([]);

      render(
        <OrgServiceResolution
          orgHandle="my-org"
          projectId="my-project"
          component="api"
          dep={blockedDep}
          onChanged={vi.fn()}
        />,
        { wrapper },
      );

      // Button should appear
      expect(await screen.findByRole('button', { name: /request access/i })).toBeInTheDocument();
    });

    it('calls requestAccess with (orgHandle, projectId, component, dep.name) when clicked', async () => {
      vi.mocked(accessRequestsModule.listAccessRequests).mockResolvedValue([]);
      vi.mocked(accessRequestsModule.requestAccess).mockResolvedValue(mockAccessRequest);

      const onChanged = vi.fn();

      render(
        <OrgServiceResolution
          orgHandle="my-org"
          projectId="my-project"
          component="api"
          dep={blockedDep}
          onChanged={onChanged}
        />,
        { wrapper },
      );

      const btn = await screen.findByRole('button', { name: /request access/i });
      fireEvent.click(btn);

      await waitFor(() => {
        expect(accessRequestsModule.requestAccess).toHaveBeenCalledWith(
          'my-org',
          'my-project',
          'api',
          'payment-svc',
        );
      });
    });
  });

  describe('unresolved / not-found', () => {
    it('renders a plain warning and NO "Request access" button', async () => {
      vi.mocked(accessRequestsModule.listAccessRequests).mockResolvedValue([]);

      render(
        <OrgServiceResolution
          orgHandle="my-org"
          projectId="my-project"
          component="api"
          dep={unresolvedDep}
          onChanged={vi.fn()}
        />,
        { wrapper },
      );

      // Wait for the query to resolve (loading → loaded)
      await waitFor(() => {
        expect(screen.queryByText(/loading/i)).not.toBeInTheDocument();
      });

      // No "Request access" button
      expect(screen.queryByRole('button', { name: /request access/i })).not.toBeInTheDocument();

      // Should have some warning text
      expect(screen.getByRole('alert')).toBeInTheDocument();
    });
  });

  describe('access-pending state with an in-flight request', () => {
    it('renders a status chip and a link to the provider issue', async () => {
      vi.mocked(accessRequestsModule.listAccessRequests).mockResolvedValue([mockAccessRequest]);

      render(
        <OrgServiceResolution
          orgHandle="my-org"
          projectId="my-project"
          component="api"
          dep={accessPendingDep}
          onChanged={vi.fn()}
        />,
        { wrapper },
      );

      // Should show a chip (not a button)
      expect(
        await screen.findByText(/access requested|pending/i),
      ).toBeInTheDocument();
      expect(screen.queryByRole('button', { name: /request access/i })).not.toBeInTheDocument();

      // Should have a link to the provider issue (wait for query to resolve)
      const link = await screen.findByRole('link', { name: /view request/i });
      expect(link).toHaveAttribute('href', 'https://github.com/org/repo/issues/42');
    });
  });
});
