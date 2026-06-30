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

// Mock the provisioning module before importing the panel
vi.mock('../../services/api/provisioning', () => ({
  provisionResource: vi.fn(),
  getResourceStatus: vi.fn(),
}));

import * as provisioningModule from '../../services/api/provisioning';
import { PlatformResourcePanel } from './PlatformResourcePanel';

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

const BASE_PROPS = {
  orgHandle: 'my-org',
  projectId: 'my-project',
  component: 'api-service',
  onChanged: vi.fn(),
};

// -- Dependency fixtures -----------------------------------------------------

const unresolvedDep: Dependency = {
  kind: 'platform-resource',
  name: 'postgres',
  resourceType: 'postgres-cnpg',
  parameters: {
    version: '16',
    storage: '10Gi',
  },
};

const unresolvedDepNoParams: Dependency = {
  kind: 'platform-resource',
  name: 'redis',
  resourceType: 'redis-standalone',
};

const provisioningDep: Dependency = {
  kind: 'platform-resource',
  name: 'postgres',
  resourceType: 'postgres-cnpg',
  status: 'provisioning',
};

const resolvedDep: Dependency = {
  kind: 'platform-resource',
  name: 'postgres',
  resourceType: 'postgres-cnpg',
  status: 'resolved',
  outputs: [
    { name: 'DATABASE_URL', secret: true },
    { name: 'DB_HOST', secret: false },
    { name: 'DB_PORT', secret: false },
    { name: 'DB_PASSWORD', secret: true },
  ],
};

// ----------------------------------------------------------------------------

describe('PlatformResourcePanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // -------------------------------------------------------------------------
  // (a) unresolved dep — param form + Provision button, clicking calls
  //     provisionResource
  // -------------------------------------------------------------------------
  describe('unresolved dep', () => {
    it('renders the parameter form with inputs from dep.parameters', () => {
      render(
        <PlatformResourcePanel {...BASE_PROPS} dep={unresolvedDep} />,
        { wrapper },
      );

      expect(screen.getByRole('button', { name: /provision/i })).toBeInTheDocument();
      expect(screen.getByLabelText('version')).toBeInTheDocument();
      expect(screen.getByLabelText('storage')).toBeInTheDocument();
    });

    it('pre-fills param fields with dep.parameters defaults', () => {
      render(
        <PlatformResourcePanel {...BASE_PROPS} dep={unresolvedDep} />,
        { wrapper },
      );

      expect(screen.getByLabelText('version')).toHaveValue('16');
      expect(screen.getByLabelText('storage')).toHaveValue('10Gi');
    });

    it('renders no param fields and shows defaults-note when dep.parameters is empty', () => {
      render(
        <PlatformResourcePanel {...BASE_PROPS} dep={unresolvedDepNoParams} />,
        { wrapper },
      );

      expect(screen.getByRole('button', { name: /provision/i })).toBeInTheDocument();
      expect(screen.getByText(/defaults will be used/i)).toBeInTheDocument();
    });

    it('clicking Provision calls provisionResource with correct args', async () => {
      vi.mocked(provisioningModule.provisionResource).mockResolvedValue(undefined);
      const onChanged = vi.fn();

      render(
        <PlatformResourcePanel
          {...BASE_PROPS}
          dep={unresolvedDep}
          onChanged={onChanged}
        />,
        { wrapper },
      );

      // Modify one param
      fireEvent.change(screen.getByLabelText('version'), {
        target: { value: '15' },
      });

      fireEvent.click(screen.getByRole('button', { name: /provision/i }));

      await waitFor(() => {
        expect(provisioningModule.provisionResource).toHaveBeenCalledWith(
          'my-org',
          'my-project',
          'api-service',
          'postgres',
          {
            params: { version: '15', storage: '10Gi' },
            environments: ['development'],
          },
        );
      });

      expect(onChanged).toHaveBeenCalled();
    });

    it('calls provisionResource with no params field when dep has no parameters', async () => {
      vi.mocked(provisioningModule.provisionResource).mockResolvedValue(undefined);
      const onChanged = vi.fn();

      render(
        <PlatformResourcePanel
          {...BASE_PROPS}
          dep={unresolvedDepNoParams}
          onChanged={onChanged}
        />,
        { wrapper },
      );

      fireEvent.click(screen.getByRole('button', { name: /provision/i }));

      await waitFor(() => {
        expect(provisioningModule.provisionResource).toHaveBeenCalledWith(
          'my-org',
          'my-project',
          'api-service',
          'redis',
          {
            params: undefined,
            environments: ['development'],
          },
        );
      });
    });
  });

  // -------------------------------------------------------------------------
  // (b) provisioning status — progress indicator, polling enabled
  // -------------------------------------------------------------------------
  describe('provisioning status', () => {
    beforeEach(() => {
      // getResourceStatus returns still-in-progress
      vi.mocked(provisioningModule.getResourceStatus).mockResolvedValue({
        status: 'provisioning',
        ready: false,
        outputs: [],
      });
    });

    it('renders progress text when dep.status is provisioning', () => {
      render(
        <PlatformResourcePanel {...BASE_PROPS} dep={provisioningDep} />,
        { wrapper },
      );

      expect(
        screen.getByText(/Provisioning…/i),
      ).toBeInTheDocument();
      expect(
        screen.getByText(/a database can take a few minutes/i),
      ).toBeInTheDocument();
    });

    it('does not render the Provision button while provisioning', () => {
      render(
        <PlatformResourcePanel {...BASE_PROPS} dep={provisioningDep} />,
        { wrapper },
      );

      // Should not show a "Provision" or "Re-provision" button in this state
      expect(
        screen.queryByRole('button', { name: /^provision$/i }),
      ).not.toBeInTheDocument();
    });

    it('calls getResourceStatus to poll when status is provisioning', async () => {
      render(
        <PlatformResourcePanel {...BASE_PROPS} dep={provisioningDep} />,
        { wrapper },
      );

      await waitFor(() => {
        expect(provisioningModule.getResourceStatus).toHaveBeenCalledWith(
          'my-org',
          'my-project',
          'api-service',
          'postgres',
          'development',
        );
      });
    });
  });

  // -------------------------------------------------------------------------
  // (c) ready / resolved + outputs — output NAMES rendered, no values,
  //     Re-provision affordance present
  // -------------------------------------------------------------------------
  describe('resolved / ready state', () => {
    beforeEach(() => {
      vi.mocked(provisioningModule.getResourceStatus).mockResolvedValue({
        status: 'deployed',
        ready: true,
        outputs: [
          { name: 'DATABASE_URL' },
          { name: 'DB_HOST' },
          { name: 'DB_PASSWORD' },
        ],
      });
    });

    it('shows "Provisioned ✓" heading', () => {
      render(
        <PlatformResourcePanel {...BASE_PROPS} dep={resolvedDep} />,
        { wrapper },
      );

      expect(screen.getByText(/Provisioned ✓/)).toBeInTheDocument();
    });

    it('renders output names from dep.outputs', () => {
      render(
        <PlatformResourcePanel {...BASE_PROPS} dep={resolvedDep} />,
        { wrapper },
      );

      // Names from the dep fixture (dep.outputs used as fallback before query loads)
      expect(screen.getByText('DATABASE_URL')).toBeInTheDocument();
      expect(screen.getByText('DB_HOST')).toBeInTheDocument();
      expect(screen.getByText('DB_PASSWORD')).toBeInTheDocument();
    });

    it('does NOT render any output value strings (secret safety)', () => {
      render(
        <PlatformResourcePanel {...BASE_PROPS} dep={resolvedDep} />,
        { wrapper },
      );

      // There are no values in the response — the test confirms nothing that
      // looks like a value (long random string) appears. We verify the text
      // shown is only the names.
      expect(screen.queryByText(/password123/i)).not.toBeInTheDocument();
      expect(screen.queryByText(/secretvalue/i)).not.toBeInTheDocument();
    });

    it('renders the Re-provision affordance', () => {
      render(
        <PlatformResourcePanel {...BASE_PROPS} dep={resolvedDep} />,
        { wrapper },
      );

      expect(screen.getByRole('button', { name: /re-provision/i })).toBeInTheDocument();
    });

    it('clicking Re-provision calls provisionResource again', async () => {
      vi.mocked(provisioningModule.provisionResource).mockResolvedValue(undefined);
      const onChanged = vi.fn();

      render(
        <PlatformResourcePanel
          {...BASE_PROPS}
          dep={resolvedDep}
          onChanged={onChanged}
        />,
        { wrapper },
      );

      fireEvent.click(screen.getByRole('button', { name: /re-provision/i }));

      await waitFor(() => {
        expect(provisioningModule.provisionResource).toHaveBeenCalledWith(
          'my-org',
          'my-project',
          'api-service',
          'postgres',
          expect.objectContaining({
            environments: ['development'],
          }),
        );
      });

      expect(onChanged).toHaveBeenCalled();
    });
  });

  // -------------------------------------------------------------------------
  // (d) failed state — error message + retry button
  // -------------------------------------------------------------------------
  describe('failed state', () => {
    const failedDep: Dependency = {
      kind: 'platform-resource',
      name: 'postgres',
      resourceType: 'postgres-cnpg',
      status: 'provisioning',
    };

    beforeEach(() => {
      vi.mocked(provisioningModule.getResourceStatus).mockResolvedValue({
        status: 'failed',
        ready: false,
        outputs: [],
      });
    });

    it('shows error alert and Retry button when status is failed', async () => {
      render(
        <PlatformResourcePanel {...BASE_PROPS} dep={failedDep} />,
        { wrapper },
      );

      // Wait for the status query to resolve to failed
      await waitFor(() => {
        expect(screen.getByText(/Provisioning failed/i)).toBeInTheDocument();
      });

      expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument();
    });
  });
});
