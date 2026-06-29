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

import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import type { DesignComponent } from '../../services/api/types';
import { DependenciesSection, type DepRef } from './DependenciesSection';

const mockComponents: DesignComponent[] = [
  {
    name: 'web',
    componentType: 'web-app',
    language: 'typescript',
    entrypoint: 'deployment/service',
    buildpack: 'docker',
    appPath: './web',
    componentAgentInstructions: '',
    dependencies: [
      {
        kind: 'external',
        name: 'openweather',
        status: 'unresolved',
        reason: 'not-found',
        needsSpec: true,
      },
    ],
  },
  {
    name: 'api',
    componentType: 'service',
    language: 'go',
    entrypoint: 'deployment/service',
    buildpack: 'docker',
    appPath: './api',
    componentAgentInstructions: '',
    dependencies: [
      {
        kind: 'org-service',
        name: 'payment-service',
        status: 'ambiguous',
        reason: '',
      },
    ],
  },
];

describe('DependenciesSection', () => {
  it('renders a row per dependency with dep names and status chips', () => {
    const onOpen = vi.fn();
    render(<DependenciesSection components={mockComponents} onOpen={onOpen} />);

    expect(screen.getByText('openweather')).toBeInTheDocument();
    expect(screen.getByText('payment-service')).toBeInTheDocument();
  });

  it('calls onOpen with the correct DepRef when a row is clicked', () => {
    const onOpen = vi.fn();
    render(<DependenciesSection components={mockComponents} onOpen={onOpen} />);

    fireEvent.click(screen.getByText('openweather'));

    expect(onOpen).toHaveBeenCalledOnce();
    const callArg: DepRef = onOpen.mock.calls[0][0];
    expect(callArg.component).toBe('web');
    expect(callArg.dependency.name).toBe('openweather');
  });

  it('calls onOpen with the correct component when the second dep row is clicked', () => {
    const onOpen = vi.fn();
    render(<DependenciesSection components={mockComponents} onOpen={onOpen} />);

    fireEvent.click(screen.getByText('payment-service'));

    expect(onOpen).toHaveBeenCalledOnce();
    const callArg: DepRef = onOpen.mock.calls[0][0];
    expect(callArg.component).toBe('api');
    expect(callArg.dependency.name).toBe('payment-service');
  });

  it('shows empty state when no dependencies exist', () => {
    const onOpen = vi.fn();
    const emptyComponents: DesignComponent[] = [
      {
        name: 'web',
        componentType: 'web-app',
        language: 'typescript',
        entrypoint: 'deployment/service',
        buildpack: 'docker',
        appPath: './web',
        componentAgentInstructions: '',
        dependencies: [],
      },
    ];
    render(<DependenciesSection components={emptyComponents} onOpen={onOpen} />);

    expect(screen.getByText('No dependencies.')).toBeInTheDocument();
  });

  it('shows empty state when components array is empty', () => {
    const onOpen = vi.fn();
    render(<DependenciesSection components={[]} onOpen={onOpen} />);

    expect(screen.getByText('No dependencies.')).toBeInTheDocument();
  });
});
