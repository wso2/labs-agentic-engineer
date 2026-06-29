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

import type { JSX } from 'react';
import { Box, Chip, Stack, Typography } from '@wso2/oxygen-ui';
import type { DesignComponent, Dependency } from '../../services/api/types';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type DepRef = {
  component: string;
  dependency: Dependency;
};

// ---------------------------------------------------------------------------
// Row
// ---------------------------------------------------------------------------

function statusColor(
  status: Dependency['status'],
): 'success' | 'warning' | 'error' | 'default' {
  switch (status) {
    case 'resolved':
      return 'success';
    case 'ambiguous':
    case 'unresolved':
      return 'warning';
    case 'blocked':
      return 'error';
    default:
      return 'default';
  }
}

function statusLabel(status: Dependency['status']): string {
  return status ?? 'unknown';
}

interface DepRowProps {
  componentName: string;
  dependency: Dependency;
  onOpen: (ref: DepRef) => void;
}

function DepRow({ componentName, dependency, onOpen }: DepRowProps) {
  return (
    <Box
      role="button"
      tabIndex={0}
      onClick={() => onOpen({ component: componentName, dependency })}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onOpen({ component: componentName, dependency });
        }
      }}
      sx={{
        display: 'flex',
        alignItems: 'center',
        gap: 2,
        px: 2,
        py: 1.25,
        borderRadius: 1,
        cursor: 'pointer',
        border: 1,
        borderColor: 'divider',
        bgcolor: 'background.paper',
        '&:hover': {
          bgcolor: 'action.hover',
        },
      }}
    >
      <Stack direction="row" alignItems="center" gap={1.5} sx={{ flex: 1, minWidth: 0 }}>
        <Typography
          variant="body2"
          fontWeight={500}
          sx={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
        >
          {dependency.name}
        </Typography>
        <Chip
          label={statusLabel(dependency.status)}
          color={statusColor(dependency.status)}
          size="small"
          sx={{ flexShrink: 0 }}
        />
        <Typography variant="caption" color="text.secondary" sx={{ flexShrink: 0 }}>
          {dependency.kind}
        </Typography>
      </Stack>
      <Typography variant="caption" color="text.secondary" sx={{ flexShrink: 0 }}>
        used by {componentName}
      </Typography>
    </Box>
  );
}

// ---------------------------------------------------------------------------
// Section
// ---------------------------------------------------------------------------

interface DependenciesSectionProps {
  components: DesignComponent[];
  onOpen: (ref: DepRef) => void;
}

export function DependenciesSection({
  components,
  onOpen,
}: DependenciesSectionProps): JSX.Element {
  const rows: Array<{ componentName: string; dependency: Dependency }> = components.flatMap(
    (component) =>
      (component.dependencies ?? []).map((dep) => ({
        componentName: component.name,
        dependency: dep,
      })),
  );

  return (
    <Box sx={{ pt: 3, pb: 2, px: 3, maxWidth: 816, mx: 'auto' }}>
      <Typography
        variant="overline"
        component="h3"
        sx={{
          m: 0,
          mb: 1.5,
          color: 'text.secondary',
          letterSpacing: '0.08em',
          fontSize: 11,
          fontWeight: 600,
        }}
      >
        Dependencies
      </Typography>
      {rows.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          No dependencies.
        </Typography>
      ) : (
        <Stack gap={1}>
          {rows.map(({ componentName, dependency }) => (
            <DepRow
              key={`${componentName}:${dependency.name}`}
              componentName={componentName}
              dependency={dependency}
              onOpen={onOpen}
            />
          ))}
        </Stack>
      )}
    </Box>
  );
}
