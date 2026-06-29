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

// Mock the specs module before importing ProvideSpec
vi.mock('../../services/api/specs', () => ({
  collectSpec: vi.fn(),
}));

import * as specsModule from '../../services/api/specs';
import { ProvideSpec } from './ProvideSpec';

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

const externalDep: Dependency = {
  kind: 'external',
  name: 'payment-gateway',
  needsSpec: true,
};

describe('ProvideSpec', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('submit button disabled state', () => {
    it('is disabled when all inputs are empty', () => {
      render(
        <ProvideSpec
          orgHandle="my-org"
          projectId="my-project"
          component="api"
          dep={externalDep}
          onChanged={vi.fn()}
        />,
        { wrapper },
      );

      const submitBtn = screen.getByRole('button', { name: /attach spec/i });
      expect(submitBtn).toBeDisabled();
    });
  });

  describe('paste spec text + submit', () => {
    it('calls collectSpec with { rawSpec } when paste field is filled', async () => {
      const sampleSpec = 'openapi: 3.0.3\ninfo:\n  title: Test\n  version: 1.0.0\npaths: {}';
      vi.mocked(specsModule.collectSpec).mockResolvedValue({
        specPath: 'components/api/payment-gateway.yaml',
        operationCount: 7,
      });

      const onChanged = vi.fn();

      render(
        <ProvideSpec
          orgHandle="my-org"
          projectId="my-project"
          component="api"
          dep={externalDep}
          onChanged={onChanged}
        />,
        { wrapper },
      );

      // Type into the paste textarea
      const pasteField = screen.getByRole('textbox', { name: /paste spec/i });
      fireEvent.change(pasteField, { target: { value: sampleSpec } });

      // Submit button should be enabled now
      const submitBtn = screen.getByRole('button', { name: /attach spec/i });
      expect(submitBtn).not.toBeDisabled();

      fireEvent.click(submitBtn);

      await waitFor(() => {
        expect(specsModule.collectSpec).toHaveBeenCalledWith(
          'my-org',
          'my-project',
          'api',
          'payment-gateway',
          { rawSpec: sampleSpec },
        );
      });

      // Should show success message with operation count
      expect(await screen.findByText(/7 operations/i)).toBeInTheDocument();

      // Should call onChanged
      expect(onChanged).toHaveBeenCalled();
    });
  });
});
