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
 * OrgServiceResolution — the drawer body for an org-service dependency.
 *
 * 4-state model (P4):
 *   blocked / access-required  — service exists but is project-only; show warning +
 *                                 "Request access" button. On success invalidates the
 *                                 design + access-requests query caches.
 *   blocked / access-pending   — an AccessRequest is already in flight; show a status
 *                                 chip (granted→success, rejected→error,
 *                                 requested|in_progress→warning) + a link to
 *                                 providerIssueUrl when present.
 *   unresolved / not-found     — no such component anywhere; plain warning, NO button.
 *
 * Ported from ProjectDependenciesPage.tsx (OrgServiceCard + AccessRequestAffordance,
 * lines 296–431) and re-keyed on the P4 4-state model. Does NOT modify the source
 * file (it is retired in B5).
 */

import type { JSX } from 'react';
import { Alert, Box, Button, Chip, Link, Stack, Typography } from '@wso2/oxygen-ui';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import type { Dependency } from '../../services/api/types';
import {
  listAccessRequests,
  requestAccess,
  type AccessRequest,
} from '../../services/api/accessRequests';
import { ApiError } from '../../services/api/rest';

// ---------------------------------------------------------------------------
// Stable query-key helpers — mirrored from ProjectDependenciesPage.tsx.
// These are intentionally co-located here so that OrgServiceResolution can
// invalidate the correct caches without importing from the (to-be-deleted)
// ProjectDependenciesPage. They must produce the same keys as the originals
// so that any existing cached queries are correctly invalidated.
// ---------------------------------------------------------------------------

export const designQueryKey = (orgHandle: string, projectId: string | undefined) => [
  'design',
  orgHandle,
  projectId,
];

export const accessRequestsQueryKey = (orgHandle: string, projectId: string | undefined) => [
  'accessRequests',
  orgHandle,
  projectId,
];

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

interface OrgServiceResolutionProps {
  orgHandle: string;
  projectId: string;
  /** The component that declares this dependency. */
  component: string;
  dep: Dependency;
  onChanged: () => void;
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function OrgServiceResolution({
  orgHandle,
  projectId,
  component,
  dep,
  onChanged,
}: OrgServiceResolutionProps): JSX.Element {
  const queryClient = useQueryClient();

  // Fetch all access requests for this project (consumer side).
  const accessRequestsQuery = useQuery<AccessRequest[]>({
    queryKey: accessRequestsQueryKey(orgHandle, projectId),
    queryFn: () => listAccessRequests(orgHandle, projectId),
  });

  // Match the dep to its AccessRequest by orgServiceName + consumerComponentName.
  const accessRequest = (accessRequestsQuery.data ?? []).find(
    (r) => r.orgServiceName === dep.name && r.consumerComponentName === component,
  );

  // Mutation to create an access request (blocked / access-required path).
  const mutation = useMutation({
    mutationFn: () => requestAccess(orgHandle, projectId, component, dep.name),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: accessRequestsQueryKey(orgHandle, projectId) });
      void queryClient.invalidateQueries({ queryKey: designQueryKey(orgHandle, projectId) });
      onChanged();
    },
  });

  const errorMsg = mutation.isError
    ? mutation.error instanceof ApiError
      ? `${mutation.error.message} (HTTP ${mutation.error.status})`
      : mutation.error instanceof Error
        ? mutation.error.message
        : 'Failed to request access.'
    : null;

  // ------ unresolved / not-found ------------------------------------------------
  if (dep.status === 'unresolved' && dep.reason === 'not-found') {
    return (
      <Box>
        <Alert severity="warning">
          Unknown org service &quot;{dep.name}&quot; — not found in this organization. Check
          the dependency name in the design.
        </Alert>
        {dep.description && (
          <Typography variant="body2" color="text.secondary" sx={{ mt: 1.5 }}>
            {dep.description}
          </Typography>
        )}
      </Box>
    );
  }

  // ------ blocked / access-pending (in-flight request) -------------------------
  if (dep.reason === 'access-pending' || (dep.status === 'blocked' && accessRequest)) {
    // Use the matched AccessRequest if available; fall through to access-required
    // affordance only when there is genuinely no request row.
    const req = accessRequest ?? null;

    let label: string;
    let color: 'default' | 'success' | 'error' | 'warning';

    if (req) {
      switch (req.status) {
        case 'granted':
          label = 'Access granted';
          color = 'success';
          break;
        case 'rejected':
          label = 'Access denied';
          color = 'error';
          break;
        default:
          // requested | in_progress
          label = 'Access requested · pending';
          color = 'warning';
          break;
      }
    } else {
      label = 'Access requested · pending';
      color = 'warning';
    }

    return (
      <Box>
        <Alert severity="info" sx={{ mb: 2 }}>
          An access request has been submitted. The provider project must publish this
          service org-wide for your components to connect.
        </Alert>
        {dep.description && (
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
            {dep.description}
          </Typography>
        )}
        <Stack spacing={0.5} alignItems="flex-start">
          <Chip size="small" color={color} label={label} />
          {req?.providerIssueUrl && (
            <Link
              href={req.providerIssueUrl}
              target="_blank"
              rel="noopener noreferrer"
              variant="caption"
            >
              View request
            </Link>
          )}
        </Stack>
      </Box>
    );
  }

  // ------ blocked / access-required (requestable) --------------------------------
  // Default: dep.status === 'blocked' && dep.reason === 'access-required' (no request yet).
  return (
    <Box>
      <Alert severity="warning" sx={{ mb: 2 }}>
        This service exists but is not published for cross-project use. Request the
        owning project to publish it org-wide.
      </Alert>
      {dep.description && (
        <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
          {dep.description}
        </Typography>
      )}
      {errorMsg && (
        <Alert severity="error" sx={{ mb: 1.5 }}>
          {errorMsg}
        </Alert>
      )}
      <Button
        variant="contained"
        onClick={() => mutation.mutate()}
        disabled={mutation.isPending}
        sx={{ flexShrink: 0 }}
      >
        {mutation.isPending ? 'Requesting…' : 'Request access'}
      </Button>
    </Box>
  );
}
