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
 * PlatformResourcePanel — provision form, async status polling, name-only
 * outputs, and re-provision (R6 rotation) for `platform-resource` dependencies.
 *
 * Security: outputs are name-only (values masked at the BFF; secret values
 * are never surfaced by the status endpoint and are never rendered here).
 *
 * Polling: stops once the task reaches a terminal state (deployed / failed) or
 * the OC binding's Ready condition is True. Tab-visibility gating is global
 * (QueryClient refetchIntervalInBackground:false set in main.tsx).
 */

import type { JSX } from 'react';
import { useState } from 'react';
import { Alert, Box, Button, CircularProgress, Stack, TextField, Typography } from '@wso2/oxygen-ui';
import { useMutation, useQuery } from '@tanstack/react-query';
import type { Dependency } from '../../services/api/types';
import {
  provisionResource,
  getResourceStatus,
} from '../../services/api/provisioning';
import { ApiError } from '../../services/api/rest';

// ---------------------------------------------------------------------------
// Terminal states — polling stops when any of these is reached or ready===true
// ---------------------------------------------------------------------------
const TERMINAL_STATUSES = new Set(['deployed', 'resolved', 'failed']);

function isTerminal(status: string | undefined, ready: boolean | undefined): boolean {
  if (ready) return true;
  if (!status) return false;
  return TERMINAL_STATUSES.has(status);
}

const POLL_INTERVAL_MS = 5_000;

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------
interface PlatformResourcePanelProps {
  dep: Dependency;
  orgHandle: string;
  projectId: string;
  component: string;
  onChanged: () => void;
}

// ---------------------------------------------------------------------------
// Param form — one TextField per dep.parameters key
// ---------------------------------------------------------------------------
function ParamForm({
  dep,
  orgHandle,
  projectId,
  component,
  onChanged,
  submitLabel = 'Provision',
}: PlatformResourcePanelProps & { submitLabel?: string }): JSX.Element {
  const params = dep.parameters ?? {};
  const paramKeys = Object.keys(params);

  const [values, setValues] = useState<Record<string, string>>(
    // Pre-fill from dep.parameters (architect defaults)
    Object.fromEntries(paramKeys.map((k) => [k, params[k] ?? ''])),
  );

  const mutation = useMutation({
    mutationFn: () =>
      provisionResource(orgHandle, projectId, component, dep.name, {
        params: paramKeys.length > 0 ? values : undefined,
        environments: ['development'],
      }),
    onSuccess: () => {
      onChanged();
    },
  });

  const errorMsg = mutation.isError
    ? mutation.error instanceof ApiError
      ? `${mutation.error.message} (HTTP ${mutation.error.status})`
      : mutation.error instanceof Error
        ? mutation.error.message
        : 'Provision failed'
    : null;

  return (
    <Box>
      <Typography variant="subtitle2" sx={{ mb: 1.5 }}>
        Provisioning parameters
      </Typography>

      {paramKeys.length === 0 ? (
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          No parameters required — the resource type defaults will be used.
        </Typography>
      ) : (
        <Stack spacing={2} sx={{ mb: 2 }}>
          {paramKeys.map((k) => (
            <TextField
              key={k}
              label={k}
              value={values[k] ?? ''}
              onChange={(e) => setValues((prev) => ({ ...prev, [k]: e.target.value }))}
              fullWidth
              size="small"
            />
          ))}
        </Stack>
      )}

      {errorMsg && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {errorMsg}
        </Alert>
      )}

      <Button
        variant="contained"
        onClick={() => mutation.mutate()}
        disabled={mutation.isPending}
      >
        {mutation.isPending ? 'Requesting…' : submitLabel}
      </Button>
    </Box>
  );
}

// ---------------------------------------------------------------------------
// Main panel
// ---------------------------------------------------------------------------
export function PlatformResourcePanel({
  dep,
  orgHandle,
  projectId,
  component,
  onChanged,
}: PlatformResourcePanelProps): JSX.Element {
  const statusQuery = useQuery({
    queryKey: ['resourceStatus', orgHandle, projectId, component, dep.name],
    queryFn: () => getResourceStatus(orgHandle, projectId, component, dep.name, 'development'),
    // Only fetch when provisioning is in-flight or resource was previously provisioned.
    // 'deployed' is a task-status, not a valid dep.status, but included defensively so
    // that if the BFF ever surfaces it on a dep, the query still fires and the ready/
    // provisioned branch renders correctly (driven by the query result, not dep.status).
    enabled:
      dep.status === 'provisioning' ||
      dep.status === 'resolved' ||
      dep.status === 'blocked' ||
      (dep.status as string) === 'deployed',
    refetchInterval: (q) => {
      const data = q.state.data;
      if (isTerminal(data?.status, data?.ready)) return false;
      return POLL_INTERVAL_MS;
    },
  });

  // Derive the effective status from the query response when available,
  // otherwise fall back to dep.status.
  const effectiveStatus = statusQuery.data?.status ?? dep.status;
  const effectiveReady = statusQuery.data?.ready ?? (dep.status === 'resolved');
  const outputs = statusQuery.data?.outputs ?? dep.outputs ?? [];

  // -------------------------------------------------------------------------
  // State: unresolved — show param form
  // -------------------------------------------------------------------------
  if (!dep.status || dep.status === 'unresolved' || dep.status === 'ambiguous') {
    return (
      <ParamForm
        dep={dep}
        orgHandle={orgHandle}
        projectId={projectId}
        component={component}
        onChanged={onChanged}
      />
    );
  }

  // -------------------------------------------------------------------------
  // State: provisioning / pending / building (in-flight)
  // -------------------------------------------------------------------------
  if (
    dep.status === 'provisioning' &&
    !isTerminal(effectiveStatus, effectiveReady)
  ) {
    return (
      <Box>
        <Stack direction="row" alignItems="center" spacing={1.5} sx={{ mb: 1.5 }}>
          <CircularProgress size={18} />
          <Typography variant="body2">
            Provisioning… (a database can take a few minutes)
          </Typography>
        </Stack>
        {statusQuery.data && (
          <Typography variant="caption" color="text.secondary">
            Status: {statusQuery.data.status}
          </Typography>
        )}
      </Box>
    );
  }

  // -------------------------------------------------------------------------
  // State: failed
  // -------------------------------------------------------------------------
  if (effectiveStatus === 'failed' || (dep.status === 'provisioning' && effectiveStatus === 'failed')) {
    return (
      <Box>
        <Alert severity="error" sx={{ mb: 2 }}>
          Provisioning failed. Review the task logs and retry below.
        </Alert>
        <ParamForm
          dep={dep}
          orgHandle={orgHandle}
          projectId={projectId}
          component={component}
          onChanged={onChanged}
          submitLabel="Retry"
        />
      </Box>
    );
  }

  // -------------------------------------------------------------------------
  // State: deployed / resolved / ready — show outputs + re-provision
  // -------------------------------------------------------------------------
  if (effectiveReady || effectiveStatus === 'deployed' || effectiveStatus === 'resolved' || dep.status === 'resolved') {
    return (
      <Box>
        <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 2 }}>
          <Typography variant="subtitle2">Provisioned ✓</Typography>
        </Stack>

        {outputs.length > 0 && (
          <Box sx={{ mb: 2 }}>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
              The following outputs are injected into your component at runtime.
              Values are stored in the OC-rendered Secret and are not displayed
              here.
            </Typography>
            <Stack spacing={0.5}>
              {outputs.map((o) => (
                <Typography key={o.name} variant="body2" fontFamily="monospace">
                  {o.name}
                </Typography>
              ))}
            </Stack>
          </Box>
        )}

        {/* Re-provision (R6 rotation) */}
        <Box sx={{ mt: 2, pt: 2, borderTop: 1, borderColor: 'divider' }}>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
            Re-provision to rotate credentials or apply updated parameters.
          </Typography>
          <ParamForm
            dep={dep}
            orgHandle={orgHandle}
            projectId={projectId}
            component={component}
            onChanged={onChanged}
            submitLabel="Re-provision"
          />
        </Box>
      </Box>
    );
  }

  // -------------------------------------------------------------------------
  // Fallback (blocked or unknown status) — provisioning in progress via
  // a status we don't explicitly enumerate
  // -------------------------------------------------------------------------
  return (
    <Box>
      <Stack direction="row" alignItems="center" spacing={1.5} sx={{ mb: 1.5 }}>
        <CircularProgress size={18} />
        <Typography variant="body2">
          Provisioning… (a database can take a few minutes)
        </Typography>
      </Stack>
    </Box>
  );
}
