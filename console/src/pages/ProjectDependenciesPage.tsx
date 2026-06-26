// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

/**
 * Project Dependencies tab — the user-facing surface for the marketplace
 * external-connection flow. It reads the published design, lists every `external`
 * connection a component depends on as a card, and lets the user provide that
 * connection's values (the config keys the architect declared). Saving posts to
 * the value-save endpoint, which provisions the OpenChoreo Resource model and
 * completes the gating config-collection task so the BFF cascade can dispatch the
 * dependent component build. Mirrors the API-driven flow but in the console.
 */

import { useCallback, useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  TextField,
  Typography,
} from '@wso2/oxygen-ui';
import { Plug, Lock, CheckCircle } from '@wso2/oxygen-ui-icons-react';

import { restApi } from '../services/api/rest';
import type { ConfigKey, Dependency } from '../services/api/types';
import { saveConnectionValues } from '../services/api/connections';
import { ApiError } from '../services/api/rest';

interface ExternalConnection {
  name: string;
  description?: string;
  config: ConfigKey[];
  consumers: string[]; // component names that depend on it
}

/** Collapse the design's components into the unique set of external connections. */
function collectExternalConnections(
  components: { name: string; dependencies?: Dependency[] }[],
): ExternalConnection[] {
  const byName = new Map<string, ExternalConnection>();
  for (const comp of components) {
    for (const dep of comp.dependencies ?? []) {
      if (dep.kind !== 'external' || !dep.name) continue;
      const existing = byName.get(dep.name);
      if (existing) {
        if (!existing.consumers.includes(comp.name)) existing.consumers.push(comp.name);
        if (existing.config.length === 0 && dep.config) existing.config = dep.config;
      } else {
        byName.set(dep.name, {
          name: dep.name,
          description: dep.description,
          config: dep.config ?? [],
          consumers: [comp.name],
        });
      }
    }
  }
  return Array.from(byName.values());
}

export default function ProjectDependenciesPage(): React.ReactElement {
  const { orgId, projectId } = useParams();
  const orgHandle = orgId ?? 'default';

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [connections, setConnections] = useState<ExternalConnection[]>([]);
  const [active, setActive] = useState<ExternalConnection | null>(null);
  const [saved, setSaved] = useState<Set<string>>(new Set());

  const load = useCallback(async () => {
    if (!projectId) return;
    setLoading(true);
    setError(null);
    try {
      const design = await restApi.getDesign(orgHandle, projectId);
      setConnections(collectExternalConnections(design?.components ?? []));
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load the design.');
    } finally {
      setLoading(false);
    }
  }, [orgHandle, projectId]);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', p: 4 }}>
        <Typography>Loading dependencies…</Typography>
      </Box>
    );
  }
  if (error) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error" action={<Button onClick={() => void load()}>Retry</Button>}>
          {error}
        </Alert>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3 }}>
      <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 1 }}>
        <Plug size={22} />
        <Typography variant="h5">Dependencies</Typography>
      </Stack>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
        External connections this project depends on. Provide each connection&apos;s values to
        provision it — the dependent components build once their connections are configured.
      </Typography>

      {connections.length === 0 ? (
        <Alert severity="info">
          No external connections in the published design. Publish a design that declares an
          <code> external </code> dependency to manage connections here.
        </Alert>
      ) : (
        <Stack spacing={2}>
          {connections.map((c) => (
            <Card key={c.name} variant="outlined">
              <CardContent>
                <Stack direction="row" justifyContent="space-between" alignItems="flex-start">
                  <Box sx={{ pr: 2 }}>
                    <Stack direction="row" spacing={1} alignItems="center">
                      <Typography variant="h6">{c.name}</Typography>
                      {saved.has(c.name) && (
                        <Chip
                          size="small"
                          color="success"
                          icon={<CheckCircle size={14} />}
                          label="Provisioned"
                        />
                      )}
                    </Stack>
                    {c.description && (
                      <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                        {c.description}
                      </Typography>
                    )}
                    <Stack direction="row" spacing={1} sx={{ mt: 1, flexWrap: 'wrap' }}>
                      {c.config.map((k) => (
                        <Chip
                          key={k.key}
                          size="small"
                          variant="outlined"
                          icon={k.secret ? <Lock size={12} /> : undefined}
                          label={k.key}
                        />
                      ))}
                    </Stack>
                    {c.consumers.length > 0 && (
                      <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: 'block' }}>
                        Used by: {c.consumers.join(', ')}
                      </Typography>
                    )}
                  </Box>
                  <Button variant="contained" onClick={() => setActive(c)} sx={{ flexShrink: 0 }}>
                    Provide configuration
                  </Button>
                </Stack>
              </CardContent>
            </Card>
          ))}
        </Stack>
      )}

      {active && (
        <ConnectionValuesDialog
          orgHandle={orgHandle}
          projectId={projectId ?? ''}
          connection={active}
          onClose={() => setActive(null)}
          onSaved={() => {
            setSaved((s) => new Set(s).add(active.name));
            setActive(null);
          }}
        />
      )}
    </Box>
  );
}

interface DialogProps {
  orgHandle: string;
  projectId: string;
  connection: ExternalConnection;
  onClose: () => void;
  onSaved: () => void;
}

function ConnectionValuesDialog({
  orgHandle,
  projectId,
  connection,
  onClose,
  onSaved,
}: DialogProps): React.ReactElement {
  const [values, setValues] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const allFilled = connection.config.every((k) => (values[k.key] ?? '').trim() !== '');

  const submit = async () => {
    setSaving(true);
    setErr(null);
    try {
      await saveConnectionValues(orgHandle, projectId, connection.name, {
        development: values,
      });
      onSaved();
    } catch (e) {
      const msg =
        e instanceof ApiError ? `${e.message} (HTTP ${e.status})` : e instanceof Error ? e.message : 'Save failed';
      setErr(msg);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open onClose={saving ? undefined : onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Configure “{connection.name}”</DialogTitle>
      <DialogContent>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Provide the values for the development environment. Secret values are stored encrypted
          and injected into the component at runtime.
        </Typography>
        <Stack spacing={2} sx={{ mt: 1 }}>
          {connection.config.map((k) => (
            <TextField
              key={k.key}
              label={k.key}
              type={k.secret ? 'password' : 'text'}
              value={values[k.key] ?? ''}
              onChange={(e) => setValues((v) => ({ ...v, [k.key]: e.target.value }))}
              fullWidth
              helperText={k.secret ? 'Secret — stored in the secret manager' : 'Plain value'}
              autoComplete="off"
            />
          ))}
        </Stack>
        {err && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {err}
          </Alert>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={saving}>
          Cancel
        </Button>
        <Button variant="contained" onClick={() => void submit()} disabled={!allFilled || saving}>
          {saving ? 'Provisioning…' : 'Save & provision'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
