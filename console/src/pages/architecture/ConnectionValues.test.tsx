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

// Mock the connections module before importing ConnectionValues
vi.mock('../../services/api/connections', () => ({
  saveConnectionValues: vi.fn(),
}));

import * as connectionsModule from '../../services/api/connections';
import { ConnectionValues } from './ConnectionValues';

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

const externalDepWithConfig: Dependency = {
  kind: 'external',
  name: 'payment-gateway',
  config: [
    { key: 'API_URL', secret: false },
    { key: 'API_SECRET', secret: true },
  ],
};

const externalDepSingleKey: Dependency = {
  kind: 'external',
  name: 'email-service',
  config: [{ key: 'SMTP_PASSWORD', secret: true }],
};

describe('ConnectionValues', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('renders one input per dep.config key', () => {
    it('shows a field for each config key', () => {
      render(
        <ConnectionValues
          orgHandle="my-org"
          projectId="my-project"
          dep={externalDepWithConfig}
          onSaved={vi.fn()}
        />,
        { wrapper },
      );

      // Should have a field for each key
      expect(screen.getByLabelText(/API_URL/i)).toBeInTheDocument();
      expect(screen.getByLabelText(/API_SECRET/i)).toBeInTheDocument();
    });

    it('renders secret fields as type=password', () => {
      render(
        <ConnectionValues
          orgHandle="my-org"
          projectId="my-project"
          dep={externalDepWithConfig}
          onSaved={vi.fn()}
        />,
        { wrapper },
      );

      const secretInput = screen.getByLabelText(/API_SECRET/i);
      expect(secretInput).toHaveAttribute('type', 'password');

      const plainInput = screen.getByLabelText(/API_URL/i);
      expect(plainInput).toHaveAttribute('type', 'text');
    });
  });

  describe('Save button disabled state', () => {
    it('is disabled when all fields are empty', () => {
      render(
        <ConnectionValues
          orgHandle="my-org"
          projectId="my-project"
          dep={externalDepWithConfig}
          onSaved={vi.fn()}
        />,
        { wrapper },
      );

      const saveBtn = screen.getByRole('button', { name: /save/i });
      expect(saveBtn).toBeDisabled();
    });

    it('is disabled when only some fields are filled', () => {
      render(
        <ConnectionValues
          orgHandle="my-org"
          projectId="my-project"
          dep={externalDepWithConfig}
          onSaved={vi.fn()}
        />,
        { wrapper },
      );

      // Fill only one field
      fireEvent.change(screen.getByLabelText(/API_URL/i), {
        target: { value: 'https://api.example.com' },
      });

      const saveBtn = screen.getByRole('button', { name: /save/i });
      expect(saveBtn).toBeDisabled();
    });

    it('is enabled when all fields are filled', () => {
      render(
        <ConnectionValues
          orgHandle="my-org"
          projectId="my-project"
          dep={externalDepWithConfig}
          onSaved={vi.fn()}
        />,
        { wrapper },
      );

      fireEvent.change(screen.getByLabelText(/API_URL/i), {
        target: { value: 'https://api.example.com' },
      });
      fireEvent.change(screen.getByLabelText(/API_SECRET/i), {
        target: { value: 'super-secret-key' },
      });

      const saveBtn = screen.getByRole('button', { name: /save/i });
      expect(saveBtn).not.toBeDisabled();
    });
  });

  describe('submit calls saveConnectionValues with correct args', () => {
    it('calls saveConnectionValues(orgHandle, projectId, dep.name, { development: {...} }) and shows hint + calls onSaved', async () => {
      vi.mocked(connectionsModule.saveConnectionValues).mockResolvedValue(undefined);

      const onSaved = vi.fn();

      render(
        <ConnectionValues
          orgHandle="my-org"
          projectId="my-project"
          dep={externalDepWithConfig}
          onSaved={onSaved}
        />,
        { wrapper },
      );

      fireEvent.change(screen.getByLabelText(/API_URL/i), {
        target: { value: 'https://api.example.com' },
      });
      fireEvent.change(screen.getByLabelText(/API_SECRET/i), {
        target: { value: 'super-secret-key' },
      });

      fireEvent.click(screen.getByRole('button', { name: /save/i }));

      await waitFor(() => {
        expect(connectionsModule.saveConnectionValues).toHaveBeenCalledWith(
          'my-org',
          'my-project',
          'payment-gateway',
          {
            development: {
              API_URL: 'https://api.example.com',
              API_SECRET: 'super-secret-key',
            },
          },
        );
      });

      // Should show the rotation/propagation hint
      expect(await screen.findByText(/redeploy consumers to apply/i)).toBeInTheDocument();

      // Should call onSaved
      expect(onSaved).toHaveBeenCalled();
    });

    it('shows server error on failure', async () => {
      const { ApiError } = await import('../../services/api/rest');
      vi.mocked(connectionsModule.saveConnectionValues).mockRejectedValue(
        new ApiError(500, 'Internal Server Error'),
      );

      render(
        <ConnectionValues
          orgHandle="my-org"
          projectId="my-project"
          dep={externalDepSingleKey}
          onSaved={vi.fn()}
        />,
        { wrapper },
      );

      fireEvent.change(screen.getByLabelText(/SMTP_PASSWORD/i), {
        target: { value: 'password123' },
      });

      fireEvent.click(screen.getByRole('button', { name: /save/i }));

      expect(await screen.findByText(/Internal Server Error/i)).toBeInTheDocument();
    });
  });

  describe('env tabs', () => {
    it('renders a "development" tab that is enabled', () => {
      render(
        <ConnectionValues
          orgHandle="my-org"
          projectId="my-project"
          dep={externalDepWithConfig}
          onSaved={vi.fn()}
        />,
        { wrapper },
      );

      // development tab should be present and accessible
      expect(screen.getByRole('tab', { name: /development/i })).toBeInTheDocument();
      expect(screen.getByRole('tab', { name: /development/i })).not.toBeDisabled();
    });
  });
});
