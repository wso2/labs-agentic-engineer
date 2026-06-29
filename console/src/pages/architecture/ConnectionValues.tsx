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
 * ConnectionValues — per-environment value entry form for an external
 * dependency. Adapted from the retired ProjectDependenciesPage for
 * drawer-embedded use (no dialog wrapper). Supports:
 *   - An environment Tabs row (only `development` active today; higher envs
 *     are rendered as disabled tabs for forward-compatibility).
 *   - One TextField per config key; `type="password"` + `autoComplete="off"`
 *     for secret keys, plain text otherwise.
 *   - All-filled gate: Save is disabled until every key has a non-empty value.
 *   - On success: shows a "Saved — redeploy consumers to apply" inline hint
 *     and calls `onSaved()`. Rotation is implicit: the form starts blank on
 *     every open (secrets are write-only) and a second submit replaces the
 *     values.
 */

import type { JSX } from 'react';
import { useState } from 'react';
import Tab from '@mui/material/Tab';
import Tabs from '@mui/material/Tabs';
import { Alert, Box, Button, Stack, TextField, Typography } from '@wso2/oxygen-ui';
import { useMutation } from '@tanstack/react-query';
import type { Dependency } from '../../services/api/types';
import { saveConnectionValues } from '../../services/api/connections';
import { ApiError } from '../../services/api/rest';

// ---------------------------------------------------------------------------
// Environment list — `development` is the only active env today. Others are
// rendered as disabled placeholders so the multi-env affordance is visible
// from day 1.
// ---------------------------------------------------------------------------

type Env = 'development' | 'staging' | 'production';

const ENVS: Array<{ id: Env; label: string; disabled: boolean }> = [
  { id: 'development', label: 'Development', disabled: false },
  { id: 'staging', label: 'Staging', disabled: true },
  { id: 'production', label: 'Production', disabled: true },
];

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

interface ConnectionValuesProps {
  orgHandle: string;
  projectId: string;
  dep: Dependency;
  onSaved: () => void;
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function ConnectionValues({
  orgHandle,
  projectId,
  dep,
  onSaved,
}: ConnectionValuesProps): JSX.Element {
  const [selectedEnv, setSelectedEnv] = useState<Env>('development');
  const [values, setValues] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState(false);

  const config = dep.config ?? [];
  const allFilled = config.every((k) => (values[k.key] ?? '').trim() !== '');

  const mutation = useMutation({
    mutationFn: () =>
      saveConnectionValues(orgHandle, projectId, dep.name, {
        [selectedEnv]: values,
      }),
    onSuccess: () => {
      setSaved(true);
      onSaved();
    },
  });

  const errorMsg = mutation.isError
    ? mutation.error instanceof ApiError
      ? `${mutation.error.message} (HTTP ${mutation.error.status})`
      : mutation.error instanceof Error
        ? mutation.error.message
        : 'Save failed'
    : null;

  return (
    <Box>
      {/* Section heading */}
      <Typography variant="subtitle2" sx={{ mb: 1.5 }}>
        Connection values
      </Typography>

      {/* Environment tabs */}
      <Tabs
        value={selectedEnv}
        onChange={(_e, v: Env) => {
          setSelectedEnv(v);
          // Reset form state when the env changes.
          setValues({});
          setSaved(false);
          mutation.reset();
        }}
        variant="scrollable"
        scrollButtons="auto"
        sx={{ mb: 2, borderBottom: 1, borderColor: 'divider' }}
      >
        {ENVS.map(({ id, label, disabled }) => (
          <Tab key={id} value={id} label={label} disabled={disabled} />
        ))}
      </Tabs>

      {/* Intro text */}
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        Provide values for the <strong>{selectedEnv}</strong> environment. Secret values are stored
        encrypted and injected into the component at runtime.
      </Typography>

      <Stack spacing={2}>
        {config.map((k) => (
          <TextField
            key={k.key}
            label={k.key}
            type={k.secret ? 'password' : 'text'}
            value={values[k.key] ?? ''}
            onChange={(e) =>
              setValues((prev) => ({ ...prev, [k.key]: e.target.value }))
            }
            fullWidth
            size="small"
            helperText={k.secret ? 'Secret — stored in the secret manager' : 'Plain value'}
            autoComplete="off"
            inputProps={{
              'aria-label': k.key,
            }}
          />
        ))}
      </Stack>

      {/* Error */}
      {errorMsg && (
        <Alert severity="error" sx={{ mt: 2 }}>
          {errorMsg}
        </Alert>
      )}

      {/* Success / rotation hint */}
      {saved && (
        <Alert severity="success" sx={{ mt: 2 }}>
          Saved — redeploy consumers to apply
        </Alert>
      )}

      <Box sx={{ mt: 2 }}>
        <Button
          variant="contained"
          onClick={() => mutation.mutate()}
          disabled={!allFilled || mutation.isPending}
        >
          {mutation.isPending ? 'Saving…' : 'Save & provision'}
        </Button>
      </Box>
    </Box>
  );
}
